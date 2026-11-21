// Command server runs the loot planner: the API plus the embedded front end.
//
// Run a single instance: the market lives in memory and is refreshed by a background goroutine.
package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"loot-planner/frontend"
	"loot-planner/internal/application"
	"loot-planner/internal/config"
	"loot-planner/internal/infrastructure/blizzless"
	"loot-planner/internal/infrastructure/storage"
	"loot-planner/internal/interfaces/httpapi"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false, "probe the running server's /api/health and exit (for Docker)")
	flag.Parse()
	cfg := config.FromEnv()
	if *healthcheck {
		c := http.Client{Timeout: 4 * time.Second}
		resp, err := c.Get("http://127.0.0.1" + cfg.Addr + "/api/health")
		if err != nil || resp.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
	if err := run(cfg); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

func run(cfg config.Settings) error {
	site := &blizzless.Site{URL: cfg.SiteURL, PageDelay: cfg.PageDelay}
	market := application.NewMarketService(site, storage.NewSnapshots(cfg.DataDir), cfg.Realms, cfg.RefreshCooldown)
	market.RefreshEvery = cfg.RefreshEvery
	market.SeasonStart, market.RelevancePace, market.RelevanceFade = cfg.SeasonStart, cfg.RelevancePace, cfg.RelevanceFade
	stash := application.NewStashService(site, storage.NewSessions(cfg.DataDir), market, cfg.SessionTTL)

	var front fs.FS = frontend.Files
	if cfg.FrontendDir != "" {
		front = os.DirFS(cfg.FrontendDir)
	}
	access := application.NewAccess(cfg.AdminLogins)
	srv := &http.Server{Addr: cfg.Addr, ReadHeaderTimeout: 10 * time.Second, Handler: (&httpapi.Server{
		Market: market, Stash: stash, Queries: &application.Queries{Market: market, Access: access}, Access: access,
		Frontend: front, CookieSecure: cfg.CookieSecure}).Handler()}

	// the market loads in the background: the page answers "not ready" until then
	go func() {
		start := time.Now()
		ok, err := market.Load()
		switch {
		case err != nil:
			slog.Error("market snapshot unreadable", "err", err)
		case ok:
			slog.Info("market loaded", "realms", len(cfg.Realms), "took", time.Since(start).Round(time.Millisecond))
		default:
			slog.Info("no market snapshot yet, fetching from the site")
		}
		market.Schedule(cfg.RefreshEvery)
		if n := stash.Purge(); n > 0 {
			slog.Info("expired sessions removed", "n", n)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("listening", "addr", cfg.Addr, "data", cfg.DataDir)
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shut, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shut); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}
