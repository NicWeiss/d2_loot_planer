// Package testkit builds synthetic market data shaped like the site's API answers, and a fake site, for tests.
package testkit

import (
	"encoding/json"
	"fmt"
	"sync"

	"loot-planner/internal/application"
	"loot-planner/internal/domain/bundle"
	"loot-planner/internal/domain/catalog"
	"loot-planner/internal/domain/market"
)

var Currency = catalog.SiteCurrency{CurrencyItems: []catalog.Currency{
	{Code: "r22", Name: "Um Rune", TypeCode: "rune", ItemClass: "misc"},
	{Code: "r23", Name: "Mal Rune", TypeCode: "rune", ItemClass: "misc"},
	{Code: "r24", Name: "Ist Rune", TypeCode: "rune", ItemClass: "misc"},
	{Code: "r25", Name: "Gul Rune", TypeCode: "rune", ItemClass: "misc"},
	{Code: "r26", Name: "Vex Rune", TypeCode: "rune", ItemClass: "misc"},
	{Code: "pk2", Name: "Key of Hate", TypeCode: "ques", ItemClass: "misc"},
	{Code: "xa4", Name: "Deep Worldstone Shard", TypeCode: "ques", ItemClass: "misc"},
	{Code: "gsr", Name: "Ruby", TypeCode: "gem", ItemClass: "gem"},
	{Code: "gsv", Name: "Amethyst", TypeCode: "gem", ItemClass: "gem"},
}}

var (
	idMu sync.Mutex
	ids  int
)

type AdOpt struct {
	Status    string
	Realm     string // default "softcore ladder"
	UnitStock float64
	Item      string // a non-currency item name (goods are then ignored)
	Quality   string
}

// Ad builds one listing. goods/price are ordered bundles.
func Ad(typ string, goods, price bundle.Bundle, o AdOpt) market.Ad {
	idMu.Lock()
	ids++
	id := fmt.Sprintf("ad%d", ids)
	idMu.Unlock()
	if o.Status == "" {
		o.Status = "completed"
	}
	r := market.ParseRealm("softcore ladder")
	if o.Realm != "" {
		r = market.ParseRealm(o.Realm)
	}
	a := market.Ad{ID: id, Type: typ, Status: o.Status, GameMode: r.Mode, Ladder: r.Season, AdType: "fixed",
		PayloadKind: "lot", UpdatedAt: "2026-11-20T10:00:00Z", ClosedAt: "2026-11-20T10:00:00Z", UnitSale: o.UnitStock > 0}
	for _, p := range price {
		a.Payments = append(a.Payments, market.Payment{Kind: "item", Code: p.Code, Amount: p.Qty})
	}
	switch {
	case o.Item != "":
		a.ItemName, a.ItemNameKey, a.Quality = o.Item, o.Item, o.Quality
		if a.Quality == "" {
			a.Quality = "unique"
		}
	case o.UnitStock > 0:
		a.EscrowMaterials = []market.Material{{BaseCode: goods[0].Code, Quantity: o.UnitStock}}
	default:
		for _, p := range goods {
			a.EscrowMaterials = append(a.EscrowMaterials, market.Material{BaseCode: p.Code, Quantity: p.Qty})
		}
	}
	return a
}

func B(kv ...any) bundle.Bundle {
	var b bundle.Bundle
	for i := 0; i < len(kv); i += 2 {
		b = append(b, bundle.Part{Code: kv[i].(string), Qty: toF(kv[i+1])})
	}
	return b
}

func toF(x any) float64 {
	switch v := x.(type) {
	case int:
		return float64(v)
	case float64:
		return v
	}
	panic("number")
}

// MarketAds: Gul = 1.5 Ist, Vex = 2.4 Ist, Um = 0.4, Mal = 0.6, key = 0.15, Deep shard = 0.6, plus live lots.
func MarketAds() (wts, wtb []market.Ad) {
	for i := 0; i < 8; i++ {
		wts = append(wts,
			Ad("wts", B("r25", 2), B("r24", 3), AdOpt{}), Ad("wts", B("r26", 1), B("r25", 1.6), AdOpt{}),
			Ad("wts", B("r24", 2), B("r22", 5), AdOpt{}), Ad("wts", B("r23", 2), B("r22", 3), AdOpt{}),
			Ad("wts", B("r24", 3), B("pk2", 20), AdOpt{}), Ad("wts", B("xa4", 1), B("r23", 1), AdOpt{}))
		wtb = append(wtb, Ad("wtb", B("r24", 1), B("r23", 5.0/3), AdOpt{}), Ad("wtb", B("r25", 1), B("pk2", 10), AdOpt{}))
	}
	wts = append(wts,
		Ad("wts", B("r25", 1), B("r23", 2), AdOpt{Status: "active"}), // Gul for 2 Mal (1.2 Ist): cheap
		Ad("wts", B("r23", 1), B("pk2", 4), AdOpt{Status: "active", UnitStock: 3}),
		Ad("wts", nil, B("r24", 2), AdOpt{Status: "active", Item: "Harlequin Crest"}),
		Ad("wts", nil, B("r24", 3), AdOpt{Status: "completed", Item: "Harlequin Crest"}))
	wtb = append(wtb, Ad("wtb", B("xa4", 1), B("r22", 2), AdOpt{Status: "active"})) // someone buys a Deep shard for 2 Um
	return
}

// StashExport is a /me/stash/sources answer: materials tab, a normal tab, a character.
func StashExport() json.RawMessage {
	it := func(code, name, quality string, qty float64) map[string]any {
		lot := map[string]any{}
		if qty > 0 {
			lot["quantity"] = qty
		}
		return map[string]any{"baseCode": code, "item": map[string]any{"displayName": name, "quality": quality, "lot": lot}}
	}
	b, _ := json.Marshal(map[string]any{
		"set": map[string]any{"hardcore": false, "ladder": true, "season": 4}, "linked": true,
		"materials": map[string]any{"tabIndex": 9, "records": []any{
			map[string]any{"baseCode": "r23", "quantity": 3}, map[string]any{"baseCode": "pk2", "quantity": 10},
			map[string]any{"baseCode": "gsr", "quantity": 2}}},
		"tabs": []any{
			map[string]any{"index": 0, "items": []any{it("xa4", "Deep Worldstone Shard", "normal", 1),
				it("cap", "Harlequin Crest", "unique", 0), it("rin", "Ring", "rare", 0)}},
			map[string]any{"index": 9, "items": []any{it("r23", "Mal Rune", "normal", 99)}}, // the materials tab is not counted twice
		},
		"characters": []any{map[string]any{"items": []any{it("r22", "Um Rune", "normal", 2)}}},
	})
	return b
}

// FakeSite stands in for blizzless.trade.
type FakeSite struct {
	mu               sync.Mutex
	Logouts, Fetches int
	Expired          bool
}

func (s *FakeSite) FetchListings(progress func(string)) (*application.RawSnapshot, error) {
	s.mu.Lock()
	s.Fetches++
	s.mu.Unlock()
	wts, wtb := MarketAds()
	return &application.RawSnapshot{WTS: wts, WTB: wtb, Currency: Currency}, nil
}

func (s *FakeSite) Login(login, password string) (application.SiteAccount, error) {
	if password != "secret" {
		return nil, &application.SiteError{Msg: "Invalid credentials", Status: 401}
	}
	return &fakeAccount{s, application.SiteUser{ID: "u1", Login: login}}, nil
}

func (s *FakeSite) Resume(cookies []application.Cookie, user application.SiteUser) application.SiteAccount {
	return &fakeAccount{s, user}
}

func (s *FakeSite) LogoutCount() int { s.mu.Lock(); defer s.mu.Unlock(); return s.Logouts }

type fakeAccount struct {
	site *FakeSite
	user application.SiteUser
}

func (a *fakeAccount) User() application.SiteUser { return a.user }

func (a *fakeAccount) Stash(realm string) (json.RawMessage, error) {
	if a.site.Expired {
		return nil, &application.SiteError{Msg: "Unauthorized", Status: 401}
	}
	if realm != "softcore ladder" {
		return json.RawMessage(`{"linked": false}`), nil
	}
	return StashExport(), nil
}

func (a *fakeAccount) Logout() { a.site.mu.Lock(); a.site.Logouts++; a.site.mu.Unlock() }

func (a *fakeAccount) Cookies() []application.Cookie {
	return []application.Cookie{{Name: "rt", Value: "x", Path: "/"}}
}
