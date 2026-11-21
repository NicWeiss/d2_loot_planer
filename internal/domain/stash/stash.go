// Package stash is what a player holds, per realm, and what it is worth.
package stash

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"

	"loot-planner/internal/domain/bundle"
	"loot-planner/internal/domain/catalog"
	"loot-planner/internal/domain/market"
)

// priced: qualities whose name alone says roughly what they are worth.
var priced = map[string]bool{"unique": true, "set": true, "runeword": true}

var ErrNotAStashExport = errors.New("это не выгрузка тайника")

type ItemCount struct {
	Name    string
	Quality string
	N       int
}

// MarshalJSON stores an item count as [name, quality, n].
func (i ItemCount) MarshalJSON() ([]byte, error) { return json.Marshal([]any{i.Name, i.Quality, i.N}) }

func (i *ItemCount) UnmarshalJSON(b []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || len(raw) != 3 {
		return errors.New("item count: want [name, quality, n]")
	}
	_ = json.Unmarshal(raw[0], &i.Name)
	_ = json.Unmarshal(raw[1], &i.Quality)
	return json.Unmarshal(raw[2], &i.N)
}

// RealmStash is stackable currency by code and items by (name, quality) -> count, for one realm.
type RealmStash struct {
	Realm    string        `json:"realm"`
	Currency bundle.Bundle `json:"currency"`
	Items    []ItemCount   `json:"items"`
}

type export struct {
	Set *struct {
		Hardcore bool `json:"hardcore"`
		Ladder   bool `json:"ladder"`
	} `json:"set"`
	Materials *struct {
		TabIndex *int `json:"tabIndex"`
		Records  []struct {
			BaseCode string  `json:"baseCode"`
			Quantity float64 `json:"quantity"`
		} `json:"records"`
	} `json:"materials"`
	Tabs       []holder `json:"tabs"`
	Characters []holder `json:"characters"`
}

type holder struct {
	Index *int `json:"index"`
	Items []struct {
		BaseCode string `json:"baseCode"`
		Item     struct {
			DisplayName string `json:"displayName"`
			Quality     string `json:"quality"`
			Lot         *struct {
				Quantity float64 `json:"quantity"`
			} `json:"lot"`
		} `json:"item"`
	} `json:"items"`
}

// FromExport parses one realm's /me/stash/sources answer (or an export file of it).
func FromExport(data json.RawMessage, cat *catalog.Catalog) (*RealmStash, error) {
	var e export
	if err := json.Unmarshal(data, &e); err != nil || e.Set == nil || (len(e.Tabs) == 0 && e.Materials == nil) {
		return nil, ErrNotAStashExport
	}
	var inv bundle.Bundle
	items := map[[2]string]int{}
	var order [][2]string
	var matTab *int
	if e.Materials != nil {
		matTab = e.Materials.TabIndex
		for _, r := range e.Materials.Records {
			if r.Quantity != 0 && cat.Has(r.BaseCode) {
				inv.Add(r.BaseCode, r.Quantity)
			}
		}
	}
	var holders []holder
	for _, t := range e.Tabs {
		if matTab == nil || t.Index == nil || *t.Index != *matTab {
			holders = append(holders, t)
		}
	}
	holders = append(holders, e.Characters...)
	for _, h := range holders {
		for _, it := range h.Items {
			if cat.Has(it.BaseCode) {
				if it.Item.Lot != nil && it.Item.Lot.Quantity != 0 {
					inv.Add(it.BaseCode, it.Item.Lot.Quantity)
				}
				continue
			}
			k := [2]string{it.Item.DisplayName, it.Item.Quality}
			if _, ok := items[k]; !ok {
				order = append(order, k)
			}
			items[k]++
		}
	}
	rs := &RealmStash{Currency: inv, Items: []ItemCount{}}
	if rs.Currency == nil {
		rs.Currency = bundle.Bundle{}
	}
	mode, season := "softcore", "non-ladder"
	if e.Set.Hardcore {
		mode = "hardcore"
	}
	if e.Set.Ladder {
		season = "ladder"
	}
	rs.Realm = mode + " " + season
	for _, k := range order {
		rs.Items = append(rs.Items, ItemCount{k[0], k[1], items[k]})
	}
	return rs, nil
}

// Pool is what the route solver may spend. mode: all | currency | gems | none.
func (s *RealmStash) Pool(m *market.RealmMarket, mode string) (bundle.Bundle, map[string]float64) {
	pool, cost := bundle.Bundle{}, map[string]float64{}
	if mode == "none" {
		return pool, cost
	}
	for _, p := range s.Currency {
		cur, known := m.Catalog.ByCode[p.Code]
		gem := known && cur.Group() == "gems"
		if mode == "all" || mode == "currency" || (mode == "gems" && gem) {
			pool = append(pool, p)
		}
	}
	if mode == "all" {
		for _, it := range s.Items {
			if !priced[it.Quality] {
				continue
			}
			k := catalog.ItemKey(it.Name, it.Quality)
			pool = append(pool, bundle.Part{Code: catalog.Own(k), Qty: float64(it.N)})
			if info := m.Items[k]; info != nil && info.Median != nil {
				cost[catalog.Own(k)] = *info.Median
			} else {
				cost[catalog.Own(k)] = 0.05
			}
		}
	}
	return pool, cost
}

func (s *RealmStash) Have(code string) float64 {
	if len(code) > 2 && code[:2] == "i:" {
		n := 0
		for _, it := range s.Items {
			if catalog.ItemKey(it.Name, it.Quality) == code {
				n += it.N
			}
		}
		return float64(n)
	}
	return s.Currency.Get(code)
}

type CurrencyLine struct {
	Code      string   `json:"code"`
	Qty       float64  `json:"qty"`
	Fair      *float64 `json:"fair"`
	Ist       *float64 `json:"ist"`
	Bid       *float64 `json:"bid"`
	BidURL    *string  `json:"bidUrl"`
	BidVsFair *float64 `json:"bidVsFair"`
	Share     *float64 `json:"share"`
}

type ItemLine struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Quality     string   `json:"quality"`
	Have        int      `json:"have"`
	Median      *float64 `json:"median"`
	Sold        int      `json:"sold"`
	Live        int      `json:"live"`
	CheapestAsk *float64 `json:"cheapestAsk"`
	Ist         *float64 `json:"ist"`
}

type OtherLine struct {
	Name    string `json:"name"`
	Quality string `json:"quality"`
	Have    int    `json:"have"`
}

type Totals struct {
	Spendable         float64 `json:"spendable"`
	Currency          float64 `json:"currency"`
	CurrencyPositions int     `json:"currencyPositions"`
	Items             float64 `json:"items"`
	ItemsCount        int     `json:"itemsCount"`
	Total             float64 `json:"total"`
	Unpriced          int     `json:"unpriced"`
}

type Appraisal struct {
	Totals   Totals         `json:"totals"`
	Currency []CurrencyLine `json:"currency"`
	Items    []ItemLine     `json:"items"`
	Other    []OtherLine    `json:"other"`
}

func f(x float64) *float64 { return &x }

// Appraise prices currency at fair value with the best live bid, items by median sale, and lists the rest.
func (s *RealmStash) Appraise(m *market.RealmMarket) Appraisal {
	a := Appraisal{Currency: []CurrencyLine{}, Items: []ItemLine{}, Other: []OtherLine{}}
	for _, p := range s.Currency {
		l := CurrencyLine{Code: p.Code, Qty: p.Qty}
		if v, ok := m.Values[p.Code]; ok {
			l.Fair, l.Ist = f(v), f(v*p.Qty)
		}
		if q := m.Quotes[p.Code]; q != nil && q.Bid != nil {
			l.Bid, l.BidURL = f(q.Bid.Per), market.LotURL(q.Bid.Action.Type, q.Bid.Action.ID)
			if l.Fair != nil && *l.Fair != 0 {
				l.BidVsFair = f(q.Bid.Per / *l.Fair - 1)
			}
		}
		a.Currency = append(a.Currency, l)
	}
	val := func(p *float64) float64 {
		if p == nil {
			return 0
		}
		return *p
	}
	sort.SliceStable(a.Currency, func(i, j int) bool { return val(a.Currency[i].Ist) > val(a.Currency[j].Ist) })
	for _, l := range a.Currency {
		a.Totals.Currency += val(l.Ist)
		if catalog.IsCore(l.Code) {
			a.Totals.Spendable += val(l.Ist)
		}
	}
	for i := range a.Currency {
		if l := &a.Currency[i]; l.Ist != nil && *l.Ist != 0 && a.Totals.Currency != 0 {
			l.Share = f(*l.Ist / a.Totals.Currency)
		}
	}
	for _, it := range s.Items {
		if !priced[it.Quality] {
			a.Other = append(a.Other, OtherLine{it.Name, it.Quality, it.N})
			continue
		}
		k := catalog.ItemKey(it.Name, it.Quality)
		l := ItemLine{Key: k, Name: it.Name, Quality: it.Quality, Have: it.N}
		if info := m.Items[k]; info != nil {
			l.Median, l.Sold, l.Live, l.CheapestAsk = info.Median, info.Sold, info.Live, info.Ask
			if info.Median != nil {
				l.Ist = f(*info.Median * float64(it.N))
			}
		}
		a.Items = append(a.Items, l)
	}
	sort.SliceStable(a.Items, func(i, j int) bool { return val(a.Items[i].Ist) > val(a.Items[j].Ist) })
	sort.SliceStable(a.Other, func(i, j int) bool {
		x, y := a.Other[i], a.Other[j]
		if x.Quality != y.Quality {
			return x.Quality < y.Quality
		}
		if x.Have != y.Have {
			return x.Have > y.Have
		}
		return x.Name < y.Name
	})
	for _, l := range a.Items {
		a.Totals.Items += val(l.Ist)
		a.Totals.ItemsCount += l.Have
	}
	for _, l := range a.Other {
		a.Totals.Unpriced += l.Have
	}
	a.Totals.CurrencyPositions = len(a.Currency)
	a.Totals.Total = a.Totals.Currency + a.Totals.Items
	return a
}

// Stash is everything one player holds, across realms.
type Stash struct {
	Owner     string                 `json:"owner"`
	Source    string                 `json:"source"` // site | file
	FetchedAt float64                `json:"fetchedAt"`
	Realms    map[string]*RealmStash `json:"realms"`
	Order     []string               `json:"-"`
}

// RealmKeys lists the realms in the order they were fetched.
func (s *Stash) RealmKeys() []string {
	if len(s.Order) == len(s.Realms) {
		return s.Order
	}
	keys := make([]string, 0, len(s.Realms))
	for k := range s.Realms {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// MarshalJSON writes realms in fetch order.
func (s *Stash) MarshalJSON() ([]byte, error) {
	var realms bytes.Buffer
	realms.WriteByte('{')
	for i, k := range s.RealmKeys() {
		if i > 0 {
			realms.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		vb, err := json.Marshal(s.Realms[k])
		if err != nil {
			return nil, err
		}
		realms.Write(kb)
		realms.WriteByte(':')
		realms.Write(vb)
	}
	realms.WriteByte('}')
	return json.Marshal(struct {
		Owner     string          `json:"owner"`
		Source    string          `json:"source"`
		FetchedAt float64         `json:"fetchedAt"`
		Realms    json.RawMessage `json:"realms"`
	}{s.Owner, s.Source, s.FetchedAt, realms.Bytes()})
}

// UnmarshalJSON reads realms keeping their order.
func (s *Stash) UnmarshalJSON(b []byte) error {
	var raw struct {
		Owner     string          `json:"owner"`
		Source    string          `json:"source"`
		FetchedAt float64         `json:"fetchedAt"`
		Realms    json.RawMessage `json:"realms"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	s.Owner, s.Source, s.FetchedAt, s.Realms, s.Order = raw.Owner, raw.Source, raw.FetchedAt, map[string]*RealmStash{}, nil
	dec := json.NewDecoder(bytes.NewReader(raw.Realms))
	if _, err := dec.Token(); err != nil {
		return err
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		var rs RealmStash
		if err := dec.Decode(&rs); err != nil {
			return err
		}
		s.Realms[kt.(string)] = &rs
		s.Order = append(s.Order, kt.(string))
	}
	return nil
}
