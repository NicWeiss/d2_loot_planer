package application

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"loot-planner/internal/domain/stash"
)

// Use cases around a browser session's stash: sign in and fetch, refresh a remembered login, upload, reset.
//
// Without "remember" the site session is signed out right after the download and only the parsed stash is kept.
// With "remember" the site cookies are kept too, so the stash can be refreshed without the password.
// The password itself is never stored.

var ErrNoStash = errors.New("на аккаунте нет привязанного тайника")

type LoginExpiredError struct{ Msg string }

func (e *LoginExpiredError) Error() string { return e.Msg }

type StashService struct {
	site     TradeSite
	sessions SessionRepository
	market   *MarketService
	TTL      time.Duration
}

func NewStashService(site TradeSite, sessions SessionRepository, market *MarketService, ttl time.Duration) *StashService {
	return &StashService{site, sessions, market, ttl}
}

func NewSessionID() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (s *StashService) Get(id string) *Session {
	if id == "" {
		return nil
	}
	return s.sessions.Get(id)
}

func (s *StashService) fetch(account SiteAccount) (*stash.Stash, error) {
	cat, err := s.market.Catalog()
	if err != nil {
		return nil, err
	}
	st := &stash.Stash{Owner: account.User().Login, Source: "site", FetchedAt: now(), Realms: map[string]*stash.RealmStash{}}
	for _, key := range s.market.RealmKeys {
		data, err := account.Stash(key)
		if err != nil {
			var se *SiteError
			if errors.As(err, &se) && se.Unauthorized() {
				return nil, err
			}
			continue // a realm the site refuses is simply absent
		}
		var linked struct {
			Linked *bool `json:"linked"`
		}
		if json.Unmarshal(data, &linked) == nil && linked.Linked != nil && !*linked.Linked {
			continue
		}
		rs, err := stash.FromExport(data, cat)
		if err != nil {
			return nil, err
		}
		if _, dup := st.Realms[rs.Realm]; !dup {
			st.Order = append(st.Order, rs.Realm)
		}
		st.Realms[rs.Realm] = rs
	}
	if len(st.Realms) == 0 {
		return nil, ErrNoStash
	}
	return st, nil
}

func (s *StashService) forgetSite(sess *Session) {
	if sess.Remembered() && sess.SiteUser != nil {
		s.site.Resume(sess.SiteCookies, *sess.SiteUser).Logout()
	}
	sess.SiteCookies, sess.SiteUser = nil, nil
}

func (s *StashService) orNew(id string) *Session {
	if sess := s.Get(id); sess != nil {
		return sess
	}
	if id == "" {
		id = NewSessionID()
	}
	return &Session{ID: id}
}

func (s *StashService) Login(id, login, password string, remember bool) (*Session, error) {
	if _, err := s.market.Catalog(); err != nil {
		return nil, err // fail early when there is no market to parse against
	}
	account, err := s.site.Login(login, password)
	if err != nil {
		return nil, err
	}
	st, err := s.fetch(account)
	if err != nil {
		account.Logout()
		return nil, err
	}
	if !remember {
		account.Logout()
	}
	sess := s.orNew(id)
	s.forgetSite(sess) // a previous remembered login of this browser is signed out
	sess.Stash = st
	sess.SiteLogin = account.User().Login
	if remember {
		u := account.User()
		sess.SiteUser, sess.SiteCookies = &u, account.Cookies()
	}
	return sess, s.sessions.Save(sess)
}

// Refresh re-downloads with the remembered login. The warning is set when the old copy is shown instead.
func (s *StashService) Refresh(id string) (*Session, string, error) {
	sess := s.Get(id)
	if sess == nil || !sess.Remembered() || sess.SiteUser == nil {
		return nil, "", &LoginExpiredError{"сохранённого входа нет, войди заново"}
	}
	account := s.site.Resume(sess.SiteCookies, *sess.SiteUser)
	st, err := s.fetch(account)
	if err != nil {
		var se *SiteError
		if errors.As(err, &se) {
			if se.Unauthorized() { // refresh token expired or revoked on the site
				sess.SiteCookies, sess.SiteUser = nil, nil
				_ = s.sessions.Save(sess)
				return nil, "", &LoginExpiredError{"сохранённый вход истёк, войди заново"}
			}
			if se.Status == 0 && sess.Stash != nil {
				return sess, "сайт недоступен (" + se.Msg + "), показан последний сохранённый тайник", nil
			}
		}
		return nil, "", err
	}
	sess.Stash = st
	sess.SiteCookies = account.Cookies() // the site may have renewed the tokens
	return sess, "", s.sessions.Save(sess)
}

func (s *StashService) Upload(id string, data json.RawMessage, name string) (*Session, error) {
	cat, err := s.market.Catalog()
	if err != nil {
		return nil, err
	}
	var wrapped struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(data, &wrapped) == nil && len(wrapped.Data) > 0 && wrapped.Data[0] == '{' {
		data = wrapped.Data
	}
	rs, err := stash.FromExport(data, cat)
	if err != nil {
		return nil, err
	}
	sess := s.orNew(id)
	s.forgetSite(sess)
	sess.Stash = &stash.Stash{Owner: name, Source: "file", FetchedAt: now(),
		Realms: map[string]*stash.RealmStash{rs.Realm: rs}, Order: []string{rs.Realm}}
	return sess, s.sessions.Save(sess)
}

func (s *StashService) Reset(id string) {
	if sess := s.Get(id); sess != nil {
		s.forgetSite(sess)
		s.sessions.Delete(sess.ID)
	}
}

func (s *StashService) Purge() int {
	return s.sessions.Purge(now() - s.TTL.Seconds())
}
