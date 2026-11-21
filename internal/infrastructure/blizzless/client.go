// Package blizzless is the blizzless.trade HTTP API: public listings, and signed-in calls for a player's stash.
package blizzless

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"

	"loot-planner/internal/application"
	"loot-planner/internal/domain/market"
)

const userAgent = "Mozilla/5.0 (loot-planner; personal use)"

type client struct {
	site string
	api  string
	http *http.Client
}

func newClient(site string, jar http.CookieJar) *client {
	if jar == nil {
		jar, _ = cookiejar.New(nil)
	}
	return &client{site: site, api: site + "/api/be", http: &http.Client{Jar: jar, Timeout: 60 * time.Second}}
}

type envelope struct {
	Status string          `json:"status"`
	Data   json.RawMessage `json:"data"`
	Error  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// call performs one API request and decodes the envelope's data into out (when non-nil).
func (c *client) call(method, path string, params url.Values, body any, out any) error {
	u := c.api + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, u, rd)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", c.site)
	req.Header.Set("Referer", c.site+"/")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return application.NewSiteError(0, "сайт недоступен: %v", err)
	}
	defer resp.Body.Close()
	var env envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return application.NewSiteError(resp.StatusCode, "HTTP %d", resp.StatusCode)
	}
	if env.Status != "success" {
		msg := "ошибка сайта"
		if env.Error != nil && env.Error.Message != "" {
			msg = env.Error.Message
		} else if env.Error != nil && env.Error.Code != "" {
			msg = env.Error.Code
		}
		status := resp.StatusCode
		if status < 400 {
			status = 0
		}
		return &application.SiteError{Msg: msg, Status: status}
	}
	if out != nil {
		return json.Unmarshal(env.Data, out)
	}
	return nil
}

type Account struct {
	c    *client
	user application.SiteUser
}

func (a *Account) User() application.SiteUser { return a.user }

// authed is a call that needs the login; the site's own client renews an expired access token the same way.
func (a *Account) authed(method, path string, params url.Values, out any) error {
	err := a.c.call(method, path, params, nil, out)
	var se *application.SiteError
	if !errors.As(err, &se) || !se.Unauthorized() {
		return err
	}
	if err := a.c.call("POST", "/auth/refresh", nil, nil, nil); err != nil {
		return err
	}
	return a.c.call(method, path, params, nil, out)
}

func (a *Account) Stash(realm string) (json.RawMessage, error) {
	r := market.ParseRealm(realm)
	var data json.RawMessage
	err := a.authed("GET", "/me/stash/sources", url.Values{
		"hardcore": {fmt.Sprint(r.Mode == "hardcore")}, "ladder": {fmt.Sprint(r.Season == "ladder")}}, &data)
	return data, err
}

func (a *Account) Logout() { _ = a.c.call("POST", "/auth/logout", nil, nil, nil) }

func (a *Account) Cookies() []application.Cookie {
	u, _ := url.Parse(a.c.api + "/")
	var out []application.Cookie
	for _, ck := range a.c.http.Jar.Cookies(u) {
		out = append(out, application.Cookie{Name: ck.Name, Value: ck.Value, Path: "/"})
	}
	return out
}

type Site struct {
	URL       string
	PageDelay time.Duration
}

func (s *Site) FetchListings(progress func(string)) (*application.RawSnapshot, error) {
	c := newClient(s.URL, nil)
	fetchAll := func(adType string) ([]market.Ad, error) {
		var items []market.Ad
		cursor := ""
		for {
			params := url.Values{"type": {adType}, "limit": {"100"}}
			if cursor != "" {
				params.Set("cursor", cursor)
			}
			var page struct {
				Advertisements struct {
					Items      []market.Ad `json:"items"`
					NextCursor *string     `json:"nextCursor"`
				} `json:"advertisements"`
			}
			if err := c.call("GET", "/advertisements", params, nil, &page); err != nil {
				return nil, err
			}
			items = append(items, page.Advertisements.Items...)
			progress(fmt.Sprintf("%s: %d", adType, len(items)))
			if page.Advertisements.NextCursor == nil || *page.Advertisements.NextCursor == "" || len(page.Advertisements.Items) == 0 {
				return items, nil
			}
			cursor = *page.Advertisements.NextCursor
			time.Sleep(s.PageDelay)
		}
	}
	snap := &application.RawSnapshot{}
	var err error
	if snap.WTS, err = fetchAll("wts"); err != nil {
		return nil, err
	}
	if snap.WTB, err = fetchAll("wtb"); err != nil {
		return nil, err
	}
	if err := c.call("GET", "/items/currency", nil, nil, &snap.Currency); err != nil {
		return nil, err
	}
	return snap, nil
}

func (s *Site) Login(login, password string) (application.SiteAccount, error) {
	c := newClient(s.URL, nil)
	var data struct {
		User application.SiteUser `json:"user"`
	}
	if err := c.call("POST", "/auth/login", nil, map[string]string{"login": login, "password": password}, &data); err != nil {
		return nil, err
	}
	return &Account{c, data.User}, nil
}

func (s *Site) Resume(cookies []application.Cookie, user application.SiteUser) application.SiteAccount {
	jar, _ := cookiejar.New(nil)
	u, _ := url.Parse(s.URL + "/")
	var cs []*http.Cookie
	for _, ck := range cookies {
		path := ck.Path
		if path == "" {
			path = "/"
		}
		cs = append(cs, &http.Cookie{Name: ck.Name, Value: ck.Value, Path: path})
	}
	jar.SetCookies(u, cs)
	return &Account{newClient(s.URL, jar), user}
}
