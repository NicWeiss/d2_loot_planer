package application

import (
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"loot-planner/internal/domain/catalog"
	"loot-planner/internal/domain/market"
)

var ErrMarketNotReady = errors.New("рынок ещё загружается, попробуй через пару минут")

type UnknownRealmError struct{ Realm string }

func (e *UnknownRealmError) Error() string { return "нет такого режима: " + e.Realm }

type RefreshRefusedError struct{ Msg string }

func (e *RefreshRefusedError) Error() string { return e.Msg }

type MarketState struct {
	Catalog     *catalog.Catalog
	FetchedAt   string
	SeasonStart time.Time // of the ladder realms; zero when unknown
	SeasonAuto  bool      // detected from the listings rather than set
	Realms      map[string]*market.RealmMarket
}

type Job struct {
	Running    bool     `json:"running"`
	StartedAt  *float64 `json:"startedAt"`
	FinishedAt *float64 `json:"finishedAt"`
	Error      *string  `json:"error"`
	Log        []string `json:"log"`
}

// MarketService keeps the current market: built from the stored snapshot, refreshed from the site in the background.
type MarketService struct {
	site         TradeSite
	repo         SnapshotRepository
	RealmKeys    []string
	RefreshEvery time.Duration // automatic refresh period, 0 = never
	cooldown     time.Duration
	// how listings are weighed by date: Pace and Fade of market.Relevance, and the season start (zero = detect)
	SeasonStart                  time.Time
	RelevancePace, RelevanceFade float64

	state atomic.Pointer[MarketState]
	mu    sync.Mutex
	job   Job
}

func NewMarketService(site TradeSite, repo SnapshotRepository, realms []string, cooldown time.Duration) *MarketService {
	return &MarketService{site: site, repo: repo, RealmKeys: realms, cooldown: cooldown, job: Job{Log: []string{}}}
}

func now() float64 { return float64(time.Now().UnixNano()) / 1e9 }

func (s *MarketService) build(snap *RawSnapshot) *MarketState {
	cat := catalog.FromSite(snap.Currency)
	fetched := ""
	for _, ads := range [][]market.Ad{snap.WTS, snap.WTB} {
		for i := range ads {
			if ads[i].UpdatedAt > fetched {
				fetched = ads[i].UpdatedAt
			}
		}
	}
	st := &MarketState{Catalog: cat, FetchedAt: fetched, SeasonStart: s.SeasonStart, Realms: map[string]*market.RealmMarket{}}
	if st.SeasonStart.IsZero() {
		st.SeasonStart, st.SeasonAuto = market.DetectSeasonStart("ladder", snap.WTS, snap.WTB), true
	}
	var rel market.Relevance
	if at, err := time.Parse(time.RFC3339, fetched); err == nil {
		rel = market.Relevance{Now: at, Pace: s.RelevancePace, Fade: s.RelevanceFade}
	}
	for _, k := range s.RealmKeys {
		realm, r := market.ParseRealm(k), rel
		if realm.Season == "ladder" {
			r.SeasonStart = st.SeasonStart
		}
		st.Realms[k] = market.NewRealmMarket(realm, cat, snap.WTS, snap.WTB, r)
	}
	return st
}

// Load builds the market from the stored snapshot; false when there is none yet.
func (s *MarketService) Load() (bool, error) {
	snap, err := s.repo.Load()
	if err != nil || snap == nil {
		return false, err
	}
	s.state.Store(s.build(snap))
	snap = nil
	debug.FreeOSMemory() // the raw listings are gone; give the memory back
	return true, nil
}

func (s *MarketService) State() *MarketState { return s.state.Load() }

func (s *MarketService) Realm(key string) (*market.RealmMarket, error) {
	st := s.state.Load()
	if st == nil {
		return nil, ErrMarketNotReady
	}
	m, ok := st.Realms[key]
	if !ok {
		return nil, &UnknownRealmError{key}
	}
	return m, nil
}

func (s *MarketService) Catalog() (*catalog.Catalog, error) {
	st := s.state.Load()
	if st == nil {
		return nil, ErrMarketNotReady
	}
	return st.Catalog, nil
}

// Job is a copy of the refresh job's state.
func (s *MarketService) Job() Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.job
	j.Log = append([]string{}, s.job.Log...)
	return j
}

// Refresh starts a background download of the market. Manual refreshes are refused within the cooldown.
func (s *MarketService) Refresh(manual bool) (Job, error) {
	s.mu.Lock()
	if s.job.Running {
		s.mu.Unlock()
		return Job{}, &RefreshRefusedError{"обновление уже идёт"}
	}
	if done := s.job.FinishedAt; manual && done != nil && s.job.Error == nil {
		if left := s.cooldown - time.Duration((now()-*done)*float64(time.Second)); left > 0 {
			s.mu.Unlock()
			return Job{}, &RefreshRefusedError{fmt.Sprintf("маркет только что обновлён, повтори через %d мин.", int(left.Minutes())+1)}
		}
	}
	t := now()
	s.job = Job{Running: true, StartedAt: &t, Log: []string{}}
	s.mu.Unlock()
	go s.run()
	return s.Job(), nil
}

func (s *MarketService) progress(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.job.Log = append(s.job.Log, line)
	if len(s.job.Log) > 20 {
		s.job.Log = s.job.Log[len(s.job.Log)-20:]
	}
}

func (s *MarketService) run() {
	var failure error
	defer func() {
		if r := recover(); r != nil {
			failure = fmt.Errorf("%v", r)
		}
		t := now()
		s.mu.Lock()
		s.job.Running, s.job.FinishedAt = false, &t
		if failure != nil {
			msg := failure.Error()
			s.job.Error = &msg
			slog.Error("market refresh failed", "err", failure)
		}
		s.mu.Unlock()
	}()
	snap, err := s.site.FetchListings(s.progress)
	if err != nil {
		failure = err
		return
	}
	s.progress("пересчёт цен…")
	st := s.build(snap)
	if err := s.repo.Save(snap); err != nil {
		failure = err
		return
	}
	s.state.Store(st) // swapped whole: readers see the old market or the new one, never a mix
	snap = nil
	debug.FreeOSMemory()
	s.progress("готово")
}

// Schedule refreshes every period, and right away when there is no snapshot yet.
func (s *MarketService) Schedule(period time.Duration) {
	go func() {
		if s.state.Load() == nil {
			_, _ = s.Refresh(false)
		}
		if period <= 0 {
			return
		}
		for range time.Tick(period) {
			_, _ = s.Refresh(false)
		}
	}()
}
