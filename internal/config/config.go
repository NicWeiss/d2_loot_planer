// Package config reads runtime settings from environment variables.
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Settings struct {
	Addr            string        // listen address
	DataDir         string        // market snapshot and sessions
	FrontendDir     string        // serve the front end from disk instead of the embedded copy (development)
	SiteURL         string        // trade site
	Realms          []string      // realms to analyse, e.g. "softcore ladder"
	RefreshEvery    time.Duration // automatic market refresh period, 0 = never
	RefreshCooldown time.Duration // manual refresh is refused more often than this
	PageDelay       time.Duration // pause between listing pages, to be gentle with the site
	SessionTTL      time.Duration
	CookieSecure    bool
	AdminLogins     []string  // site logins allowed to run admin actions (market refresh)
	SeasonStart     time.Time // ladder season start; zero = detect from the listings
	RelevancePace   float64   // days: prices slide fast until about this season day, see market.Relevance
	RelevanceFade   float64   // days: how fast older listings fade, see market.Relevance; 0 = no fading
}

func env(name, def string) string {
	if v, ok := os.LookupEnv(name); ok {
		return v
	}
	return def
}

func num(name, def string) float64 {
	v, err := strconv.ParseFloat(env(name, def), 64)
	if err != nil {
		v, _ = strconv.ParseFloat(def, 64)
	}
	return v
}

func dur(x float64, unit time.Duration) time.Duration { return time.Duration(x * float64(unit)) }

func FromEnv() Settings {
	var realms []string
	for _, r := range strings.Split(env("REALMS", "softcore:ladder,hardcore:ladder"), ",") {
		if r = strings.TrimSpace(strings.ReplaceAll(r, ":", " ")); r != "" {
			realms = append(realms, r)
		}
	}
	secure := strings.ToLower(env("COOKIE_SECURE", "false"))
	season, _ := time.Parse("2006-01-02", strings.TrimSpace(env("SEASON_START", "")))
	return Settings{
		Addr:            ":" + env("PORT", "8000"),
		DataDir:         env("DATA_DIR", "data"),
		FrontendDir:     env("FRONTEND_DIR", ""),
		SiteURL:         strings.TrimRight(env("SITE_URL", "https://blizzless.trade"), "/"),
		Realms:          realms,
		RefreshEvery:    dur(num("MARKET_REFRESH_HOURS", "6"), time.Hour),
		RefreshCooldown: dur(num("MARKET_REFRESH_COOLDOWN_MINUTES", "10"), time.Minute),
		PageDelay:       dur(num("FETCH_PAGE_DELAY", "0.3"), time.Second),
		SessionTTL:      dur(num("SESSION_TTL_DAYS", "30"), 24*time.Hour),
		CookieSecure:    secure == "1" || secure == "true" || secure == "yes",
		AdminLogins:     strings.Split(env("ADMIN_LOGINS", "nicweiss"), ","),
		SeasonStart:     season,
		RelevancePace:   num("RELEVANCE_PACE_DAYS", "7"),
		RelevanceFade:   num("RELEVANCE_FADE_DAYS", "45"),
	}
}
