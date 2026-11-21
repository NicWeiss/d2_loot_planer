package storage_test

import (
	"testing"

	"loot-planner/internal/application"
	"loot-planner/internal/domain/catalog"
	"loot-planner/internal/domain/market"
	"loot-planner/internal/infrastructure/storage"
	tk "loot-planner/internal/testkit"
)

func TestSnapshotRoundTripKeepsTheMarket(t *testing.T) {
	dir := t.TempDir()
	repo := storage.NewSnapshots(dir)
	if snap, err := repo.Load(); snap != nil || err != nil {
		t.Fatalf("empty dir: %v %v", snap, err)
	}
	wts, wtb := tk.MarketAds()
	if err := repo.Save(&application.RawSnapshot{WTS: wts, WTB: wtb, Currency: tk.Currency}); err != nil {
		t.Fatal(err)
	}
	back, err := repo.Load()
	if err != nil || back == nil {
		t.Fatal(err)
	}
	realm := market.ParseRealm("softcore ladder")
	a := market.NewRealmMarket(realm, catalog.FromSite(tk.Currency), wts, wtb, market.Relevance{})
	b := market.NewRealmMarket(realm, catalog.FromSite(back.Currency), back.WTS, back.WTB, market.Relevance{})
	if len(a.Values) != len(b.Values) || len(a.Actions) != len(b.Actions) || len(a.Items) != len(b.Items) {
		t.Fatalf("market changed after a round trip")
	}
	for c, v := range a.Values {
		if b.Values[c] != v {
			t.Errorf("%s: %v vs %v", c, v, b.Values[c])
		}
	}
}

func TestSessionsRoundTripAndPurge(t *testing.T) {
	repo := storage.NewSessions(t.TempDir())
	s := &application.Session{ID: "abc", SiteCookies: []application.Cookie{{Name: "rt", Value: "x"}}}
	if err := repo.Save(s); err != nil {
		t.Fatal(err)
	}
	got := repo.Get("abc")
	if got == nil || !got.Remembered() || repo.Get("other") != nil {
		t.Fatalf("get: %+v", got)
	}
	if n := repo.Purge(s.Touched + 10); n != 1 || repo.Get("abc") != nil {
		t.Errorf("purge removed %d", n)
	}
}
