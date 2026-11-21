package application

import (
	"errors"
	"strings"
)

var ErrForbidden = errors.New("обновлять маркет может только администратор")

// Access decides who may run admin actions. An admin is a browser session that signed in to the trade site
// as one of the admin logins; the login comes from the site's answer, never from what was typed or uploaded.
type Access struct{ admins map[string]bool }

func NewAccess(logins []string) *Access {
	a := &Access{admins: map[string]bool{}}
	for _, l := range logins {
		if l = strings.ToLower(strings.TrimSpace(l)); l != "" {
			a.admins[l] = true
		}
	}
	return a
}

func (a *Access) IsAdmin(s *Session) bool {
	return s != nil && s.SiteLogin != "" && a.admins[strings.ToLower(s.SiteLogin)]
}
