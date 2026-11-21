// Package application holds the use cases: they combine the domain with storage and the trade site.
package application

import (
	"encoding/json"
	"fmt"

	"loot-planner/internal/domain/catalog"
	"loot-planner/internal/domain/market"
	"loot-planner/internal/domain/stash"
)

// RawSnapshot is the listings and the currency list as the site returns them (reduced to the fields used).
type RawSnapshot struct {
	WTS      []market.Ad
	WTB      []market.Ad
	Currency catalog.SiteCurrency
}

// SiteError is a failure on the trade site; Status 0 means it could not be reached.
type SiteError struct {
	Msg    string
	Status int
}

func (e *SiteError) Error() string      { return e.Msg }
func (e *SiteError) Unauthorized() bool { return e.Status == 401 }

func NewSiteError(status int, format string, a ...any) *SiteError {
	return &SiteError{fmt.Sprintf(format, a...), status}
}

type SiteUser struct {
	ID    string `json:"id"`
	Login string `json:"login"`
}

type Cookie struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain,omitempty"`
	Path   string `json:"path,omitempty"`
}

// SiteAccount is a signed-in session on the trade site.
type SiteAccount interface {
	User() SiteUser
	Stash(realm string) (json.RawMessage, error)
	Logout()
	Cookies() []Cookie
}

type TradeSite interface {
	FetchListings(progress func(string)) (*RawSnapshot, error)
	Login(login, password string) (SiteAccount, error)
	Resume(cookies []Cookie, user SiteUser) SiteAccount
}

type SnapshotRepository interface {
	Load() (*RawSnapshot, error) // nil, nil when there is none yet
	Save(*RawSnapshot) error
}

// Session is a browser's session with this app: its stash and, if the player asked to be remembered, site cookies.
type Session struct {
	ID          string       `json:"-"`
	Stash       *stash.Stash `json:"stash"`
	SiteUser    *SiteUser    `json:"siteUser"`
	SiteCookies []Cookie     `json:"siteCookies"`
	SiteLogin   string       `json:"siteLogin,omitempty"` // the login the site confirmed for this browser; empty if none
	Touched     float64      `json:"touched"`
}

func (s *Session) Remembered() bool { return len(s.SiteCookies) > 0 }

type SessionRepository interface {
	Get(id string) *Session
	Save(*Session) error
	Delete(id string)
	Purge(olderThan float64) int
}
