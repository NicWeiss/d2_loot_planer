package domain_test

import (
	"math"
	"testing"
	"time"

	"loot-planner/internal/domain/bundle"
	"loot-planner/internal/domain/catalog"
	"loot-planner/internal/domain/market"
	"loot-planner/internal/domain/routing"
	"loot-planner/internal/domain/stash"
	tk "loot-planner/internal/testkit"
)

func near(t *testing.T, name string, got, want, rel float64) {
	t.Helper()
	if math.Abs(got-want) > rel*math.Abs(want) {
		t.Errorf("%s = %v, want %v (±%v%%)", name, got, want, rel*100)
	}
}

func newMarket(t *testing.T, realm string) *market.RealmMarket {
	t.Helper()
	wts, wtb := tk.MarketAds()
	return market.NewRealmMarket(market.ParseRealm(realm), catalog.FromSite(tk.Currency), wts, wtb, market.Relevance{})
}

// ---- valuation

func TestFairValuesRecoverTradeRatios(t *testing.T) {
	v := newMarket(t, "softcore ladder").Values
	if v["r24"] != 1 {
		t.Fatalf("Ist is the unit, got %v", v["r24"])
	}
	near(t, "Gul", v["r25"], 1.5, 0.05)
	near(t, "Um", v["r22"], 0.4, 0.05)
	near(t, "key", v["pk2"], 0.15, 0.1)
}

// ---- relevance

var seasonStart = time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)

func onDay(d float64) string {
	return seasonStart.Add(time.Duration(d * 24 * float64(time.Hour))).Format(time.RFC3339)
}

func TestRelevanceFadesFastEarlyAndLevelsOffLate(t *testing.T) {
	rel := func(today float64) market.Relevance {
		return market.Relevance{Now: seasonStart.Add(time.Duration(today * 24 * float64(time.Hour))),
			SeasonStart: seasonStart, Pace: 7, Fade: 45}
	}
	near(t, "today", rel(28).Weight(onDay(28)), 1, 1e-9)
	if w := rel(28).Weight(onDay(-1)); w != 0 {
		t.Errorf("last season's listing weighs %v, want 0", w)
	}
	early, late := rel(5).Weight(onDay(2)), rel(120).Weight(onDay(90))
	if early > 0.4 || late < 0.85 {
		t.Errorf("3 days old on day 5 weighs %v, 30 days old on day 120 weighs %v: want fast fade early, plateau late", early, late)
	}
	if w := (market.Relevance{}).Weight(onDay(-100)); w != 1 {
		t.Errorf("zero Relevance weighs %v, want 1", w)
	}
}

func TestSeasonStartIsFirstDayAfterQuietStretch(t *testing.T) {
	var ads []market.Ad
	add := func(day float64, n int) {
		for i := 0; i < n; i++ {
			a := tk.Ad("wts", tk.B("r25", 1), tk.B("r24", 1.5), tk.AdOpt{})
			a.CreatedAt = onDay(day)
			ads = append(ads, a)
		}
	}
	for d := -120.0; d < -60; d++ {
		add(d, 30) // last season
	}
	add(-10, 1) // a stray listing in the quiet weeks
	for d := 0.0; d < 28; d++ {
		add(d, 40)
	}
	if got := market.DetectSeasonStart("ladder", ads); !got.Equal(seasonStart) {
		t.Errorf("season start = %v, want %v", got, seasonStart)
	}
	if got := market.DetectSeasonStart("ladder", ads[len(ads)-200:]); !got.IsZero() {
		t.Errorf("listings of one season: start = %v, want none", got)
	}
}

func TestOldPricesFadeFromFairValue(t *testing.T) {
	var wts []market.Ad
	sell := func(day float64, ist float64) {
		for i := 0; i < 10; i++ {
			a := tk.Ad("wts", tk.B("r25", 1), tk.B("r24", ist), tk.AdOpt{})
			a.ClosedAt = onDay(day)
			wts = append(wts, a)
		}
	}
	sell(-100, 6) // last season
	sell(1, 3)    // the first days, everything is dear
	sell(28, 1.5) // today
	now := seasonStart.Add(28 * 24 * time.Hour)
	m := market.NewRealmMarket(market.ParseRealm("softcore ladder"), catalog.FromSite(tk.Currency), wts, nil,
		market.Relevance{Now: now, SeasonStart: seasonStart, Pace: 7, Fade: 45})
	near(t, "Gul now", m.Values["r25"], 1.5, 0.03)
	flat := market.NewRealmMarket(market.ParseRealm("softcore ladder"), catalog.FromSite(tk.Currency), wts, nil, market.Relevance{})
	if flat.Values["r25"] < 2.5 {
		t.Errorf("without relevance Gul = %v, the old prices should pull it up", flat.Values["r25"])
	}
}

func TestOtherRealmIsSeparate(t *testing.T) {
	hc := newMarket(t, "hardcore ladder")
	if len(hc.Values) != 0 || hc.Counts["rows"] != 0 {
		t.Fatalf("hardcore has no trades, got %v values", len(hc.Values))
	}
}

// ---- quotes, offers, items

func TestQuotesUseWTSForAsksAndWTBForBids(t *testing.T) {
	m := newMarket(t, "softcore ladder")
	near(t, "Gul ask", m.Quotes["r25"].Ask.Per, 2*m.Values["r23"], 1e-9)
	near(t, "shard bid", m.Quotes["xa4"].Bid.Per, 2*m.Values["r22"], 1e-9)
	if q := m.Quotes["r23"]; q != nil && q.Bid != nil {
		t.Error("paying Mal for a Gul is not a bid for Mal")
	}
}

func TestOffersSortedCheapestFirst(t *testing.T) {
	offers := newMarket(t, "softcore ladder").OffersFor("r25")
	if len(offers) == 0 || offers[0].Per > offers[len(offers)-1].Per {
		t.Fatal("offers must be sorted by price")
	}
	if offers[0].Discount == nil || *offers[0].Discount <= 0 {
		t.Error("the cheapest Gul is below fair value")
	}
	if *offers[0].URL != "https://blizzless.trade/wts/"+lotID(offers[0]) {
		t.Errorf("unexpected url %v", *offers[0].URL)
	}
}

func lotID(o market.Offer) string { u := *o.URL; return u[len("https://blizzless.trade/wts/"):] }

func TestItemsGetMedianAndLiveLots(t *testing.T) {
	m := newMarket(t, "softcore ladder")
	it := m.Items["i:Harlequin Crest|unique"]
	if it == nil || it.Live != 1 || it.Sold != 1 {
		t.Fatalf("item info %+v", it)
	}
	near(t, "median", *it.Median, 3, 1e-9)
	near(t, "fair", *m.Fair("i:Harlequin Crest|unique"), 3, 1e-9)
}

// ---- solver

func act(id string, give, get bundle.Bundle, uses float64) market.Action {
	return market.Action{ID: id, Type: "wts", Give: give, Get: get, Uses: uses}
}

func ids(steps []*routing.Step) []string {
	var out []string
	for _, s := range steps {
		out = append(out, s.ID)
	}
	return out
}

func TestRouteFollowsCheapestChain(t *testing.T) {
	values := map[string]float64{"a": 1, "b": 2, "t": 4}
	acts := []market.Action{act("1", tk.B("a", 2), tk.B("b", 1), 5), act("2", tk.B("b", 2), tk.B("t", 1), 5), act("3", tk.B("a", 10), tk.B("t", 1), 5)}
	r := routing.Solve(acts, nil, values, routing.Request{Target: "t", Qty: 1, Pool: tk.B("a", 10)})
	if !r.OK || r.CostIst != 4 {
		t.Fatalf("ok=%v cost=%v", r.OK, r.CostIst)
	}
	if got := ids(r.Steps); len(got) != 2 || got[0] != "1" || got[1] != "2" {
		t.Errorf("steps %v", got)
	}
}

func TestRouteRespectsListingCapacity(t *testing.T) {
	values := map[string]float64{"a": 1, "t": 2}
	acts := []market.Action{act("cheap", tk.B("a", 1), tk.B("t", 1), 1), act("dear", tk.B("a", 3), tk.B("t", 1), 5)}
	one := routing.Solve(acts, nil, values, routing.Request{Target: "t", Qty: 1, Pool: tk.B("a", 10)})
	if got := ids(one.Steps); len(got) != 1 || got[0] != "cheap" {
		t.Errorf("one: %v", got)
	}
	two := routing.Solve(acts, nil, values, routing.Request{Target: "t", Qty: 2, Pool: tk.B("a", 10)})
	total := 0.0
	for _, s := range two.Steps {
		total += s.Times
		if s.ID == "cheap" && s.Times > 1 {
			t.Error("the cheap lot exists once")
		}
	}
	if !two.OK || total != 2 {
		t.Fatalf("two: ok=%v total=%v", two.OK, total)
	}
	p := two.Paper.Steps[0]
	if p.ID != "cheap" || p.Times != 2 || *p.Available != 1 {
		t.Errorf("on paper the cheap lot is wanted twice: %+v", p)
	}
}

func TestRouteUnreachableAndCapacity(t *testing.T) {
	acts := []market.Action{act("1", tk.B("a", 1), tk.B("t", 1), 1)}
	if r := routing.Solve(acts, nil, nil, routing.Request{Target: "t", Qty: 1, Pool: tk.B("b", 5)}); *r.Reason != "unreachable" {
		t.Errorf("reason %v", *r.Reason)
	}
	r := routing.Solve(acts, nil, map[string]float64{"a": 1, "t": 1}, routing.Request{Target: "t", Qty: 3, Pool: tk.B("a", 5), Effort: 0.03})
	if r.OK || len(r.Bottlenecks) == 0 || r.Bottlenecks[0].Kind != "lots" {
		t.Errorf("want a lots bottleneck, got ok=%v %+v", r.OK, r.Bottlenecks)
	}
}

func TestRouteUsesCubeAndUnlimitedKeys(t *testing.T) {
	if g := catalog.CubeByOut["r24"]; g.Get("r23") != 2 || g.Get("gsv") != 1 {
		t.Fatalf("Ist = 2 Mal + Amethyst, got %v", g)
	}
	values := map[string]float64{"r23": 0.6, "r24": 1, "gsv": 0.05, "pk2": 0.15}
	acts := []market.Action{act("k", tk.B("pk2", 4), tk.B("r23", 1), 10)}
	r := routing.Solve(acts, catalog.Cube, values, routing.Request{Target: "r24", Qty: 1, Pool: tk.B("gsv", 1),
		Unlimited: tk.B("pk2", 0.15), UseCube: true})
	if !r.OK || r.Steps[len(r.Steps)-1].Type != "cube" {
		t.Fatalf("ok=%v steps=%v", r.OK, ids(r.Steps))
	}
	if r.UnlimitedSpent.Get("pk2") != 8 {
		t.Errorf("2 Mal from keys = 8 keys, got %v", r.UnlimitedSpent)
	}
}

func TestArbitrageLoopDoesNotMakeThingsFree(t *testing.T) {
	values := map[string]float64{"a": 1, "b": 1}
	acts := []market.Action{act("1", tk.B("a", 1), tk.B("b", 2), 100), act("2", tk.B("b", 1), tk.B("a", 2), 100)}
	if r := routing.Solve(acts, nil, values, routing.Request{Target: "b", Qty: 1, Pool: tk.B("a", 1)}); r.UnitCost <= 0 {
		t.Errorf("unit cost %v", r.UnitCost)
	}
}

// ---- stash

func TestStashExportIsParsedOncePerItem(t *testing.T) {
	rs, err := stash.FromExport(tk.StashExport(), catalog.FromSite(tk.Currency))
	if err != nil {
		t.Fatal(err)
	}
	if rs.Realm != "softcore ladder" {
		t.Errorf("realm %q", rs.Realm)
	}
	want := map[string]float64{"r23": 3, "pk2": 10, "gsr": 2, "xa4": 1, "r22": 2}
	if len(rs.Currency) != len(want) {
		t.Errorf("currency %v", rs.Currency)
	}
	for c, q := range want {
		if rs.Currency.Get(c) != q {
			t.Errorf("%s = %v, want %v", c, rs.Currency.Get(c), q)
		}
	}
	if len(rs.Items) != 2 {
		t.Errorf("items %v", rs.Items)
	}
}

func TestStashRejectsOtherJSON(t *testing.T) {
	if _, err := stash.FromExport([]byte(`{"hello": 1}`), catalog.FromSite(tk.Currency)); err != stash.ErrNotAStashExport {
		t.Errorf("err = %v", err)
	}
}

func TestStashPoolModesAndAppraisal(t *testing.T) {
	m := newMarket(t, "softcore ladder")
	rs, _ := stash.FromExport(tk.StashExport(), m.Catalog)
	pool, cost := rs.Pool(m, "all")
	if pool.Get("own:i:Harlequin Crest|unique") != 1 || cost["own:i:Harlequin Crest|unique"] != 3 {
		t.Errorf("own item in pool: %v %v", pool, cost)
	}
	if gems, _ := rs.Pool(m, "gems"); len(gems) != 1 || gems[0].Code != "gsr" {
		t.Errorf("gems %v", gems)
	}
	if none, _ := rs.Pool(m, "none"); len(none) != 0 {
		t.Errorf("none %v", none)
	}
	a := rs.Appraise(m)
	if a.Totals.Unpriced != 1 || a.Totals.Items != 3 {
		t.Errorf("totals %+v", a.Totals)
	}
	for _, l := range a.Currency {
		if l.Code == "xa4" {
			near(t, "shard bid", *l.Bid, 2*m.Values["r22"], 1e-9)
			if l.BidURL == nil {
				t.Error("bid has a link")
			}
		}
	}
}

// ---- farm strategies

func TestStrategyFarmsKeysIntoCubeRecipe(t *testing.T) {
	values := map[string]float64{"r23": 0.6, "r24": 1, "gsv": 0.05, "pk2": 0.15, "ua1": 0.2}
	acts := []market.Action{act("k", tk.B("pk2", 4), tk.B("r23", 1), 10)}
	base := routing.Request{Target: "r24", Qty: 1, Pool: tk.B("gsv", 1, "pk2", 3), UseCube: true}
	have := func(c string) float64 { return base.Pool.Get(c) }
	list := routing.Strategies(acts, catalog.Cube, values, base, []string{"pk2", "ua1"}, have)
	if len(list) != 1 {
		t.Fatalf("only keys lead to Mal here, got %d strategies", len(list))
	}
	s := list[0]
	if !s.OK || len(s.Farm) != 1 || s.Farm[0].Code != "pk2" || s.Farm[0].Need != 5 || s.Farm[0].Have != 3 {
		t.Errorf("want 8 keys, 3 in the stash, 5 to farm: %+v", s)
	}
}

func TestCraftChainMakesSparksFromIngots(t *testing.T) {
	values := map[string]float64{"cr1": 0.005, "cr2": 0.06, "cr3": 0.7}
	r := routing.Solve(nil, catalog.Cube, values, routing.Request{Target: "cr3", Qty: 1, Unlimited: tk.B("cr1", 0.005), UseCube: true})
	if !r.OK || r.UnlimitedSpent.Get("cr1") != 144 {
		t.Errorf("a spark is 144 ingots, got ok=%v %v", r.OK, r.UnlimitedSpent)
	}
}
