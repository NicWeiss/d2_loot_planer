package market

import (
	"sort"
	"strings"

	"loot-planner/internal/domain/bundle"
	"loot-planner/internal/domain/catalog"
)

const (
	lotURL     = "https://blizzless.trade/"
	dealMinObs = 15   // only liquid currencies are trusted when flagging underpriced lots
	dealRatio  = 1.15 // gain/cost above this is a deal
	dealsKept  = 60
	itemLots   = 4 // cheapest live lots kept per item name
)

// LotURL links a listing on the site; cube crafts have none.
func LotURL(typ, id string) *string {
	if typ == "cube" {
		return nil
	}
	s := lotURL + typ + "/" + id
	return &s
}

type ItemInfo struct {
	Name    string   `json:"name"`
	Quality string   `json:"quality"`
	Ask     *float64 `json:"ask"`    // cheapest live lot, Ist
	Live    int      `json:"live"`   // live lots
	Sold    int      `json:"sold"`   // completed sales
	Median  *float64 `json:"median"` // median completed sale, Ist
}

type Recent struct {
	Date    string   `json:"date"`
	Code    string   `json:"code"`
	Qty     float64  `json:"qty"`
	PerUnit float64  `json:"perUnit"`
	Ist     *float64 `json:"ist"`
}

type Row struct {
	Code       string   `json:"code"`
	Name       string   `json:"name"`
	Group      string   `json:"group"`
	Ist        *float64 `json:"ist"`
	Obs        float64  `json:"obs"`
	Supply     float64  `json:"supply"`
	Demand     float64  `json:"demand"`
	AcceptedBy int      `json:"acceptedBy"`
	Recent     []Recent `json:"recent"`
}

type Edge struct {
	From     string   `json:"from"`
	To       string   `json:"to"`
	Rate     float64  `json:"rate"`
	FairRate *float64 `json:"fairRate"`
	Give     float64  `json:"give"`
	Get      float64  `json:"get"`
	N        int      `json:"n"`
	URL      *string  `json:"url"`
}

type Deal struct {
	Type    string        `json:"type"`
	Give    bundle.Bundle `json:"give"`
	Get     bundle.Bundle `json:"get"`
	Ratio   float64       `json:"ratio"`
	GiveIst float64       `json:"giveIst"`
	GetIst  float64       `json:"getIst"`
	Unit    bool          `json:"unit"`
	URL     *string       `json:"url"`
}

type Quote struct {
	Per    float64
	Action *Action
}

type Quotes struct{ Ask, Bid *Quote }

type Offer struct {
	Type     string        `json:"type"`
	Give     bundle.Bundle `json:"give"`
	Get      bundle.Bundle `json:"get"`
	Uses     float64       `json:"uses"`
	Check    bool          `json:"check"`
	Per      float64       `json:"per"`
	Discount *float64      `json:"discount"`
	Pure     bool          `json:"pure"`
	URL      *string       `json:"url"`
}

type CubeCost struct {
	Recipe   bundle.Bundle `json:"recipe"`
	Ist      float64       `json:"ist"`
	Complete bool          `json:"complete"`
	Discount *float64      `json:"discount"`
}

// RealmMarket is one realm's market, built from a snapshot of listings.
type RealmMarket struct {
	Realm      Realm
	Relevance  Relevance
	Catalog    *catalog.Catalog
	Values     map[string]float64
	NObs       map[string]float64
	Counts     map[string]int
	Table      []*Row // in catalog order
	TableBy    map[string]*Row
	Edges      []Edge
	Deals      []Deal
	Actions    []Action
	Items      map[string]*ItemInfo
	ItemsOrder []string
	Quotes     map[string]*Quotes
}

// NewRealmMarket builds the realm's market; rel weighs listings by date (the zero Relevance weighs all alike).
func NewRealmMarket(realm Realm, cat *catalog.Catalog, wts, wtb []Ad, rel Relevance) *RealmMarket {
	m := &RealmMarket{Realm: realm, Relevance: rel, Catalog: cat, Items: map[string]*ItemInfo{}}
	trades := CurrencyTrades(wts, cat, realm, rel)
	trades = append(trades, CurrencyTrades(wtb, cat, realm, rel)...)
	m.Values, m.NObs = FitValues(trades)
	m.Counts = map[string]int{"rows": len(trades)}
	for _, t := range trades {
		if t.Status == "active" {
			m.Counts["active"]++
		} else if t.Status == "completed" {
			m.Counts["completed"]++
		}
	}
	m.buildTable(trades, wts, wtb)
	m.buildEdges(trades)
	m.buildDeals(trades)
	for _, t := range trades {
		if t.Status == "active" {
			give, get := PlayerSide(&t)
			uses := 1.0
			if t.Unit {
				uses = t.Stock
			}
			m.Actions = append(m.Actions, Action{ID: t.ID, Type: t.Type, Give: give, Get: get, Uses: uses})
		}
	}
	m.Actions = append(m.Actions, m.itemActions(wts, wtb)...)
	m.Quotes = m.quotes()
	return m
}

func ptr(f float64) *float64 { return &f }

func (m *RealmMarket) buildTable(trades []Trade, wts, wtb []Ad) {
	sup, dem, acc := map[string]float64{}, map[string]float64{}, map[string]int{}
	last := map[string]*Trade{}
	var ids []string
	for i := range trades {
		if _, ok := last[trades[i].ID]; !ok {
			ids = append(ids, trades[i].ID)
		}
		last[trades[i].ID] = &trades[i]
	}
	for _, id := range ids {
		t := last[id]
		if t.Status != "active" {
			continue
		}
		for _, p := range t.Goods {
			if t.Type == "wts" {
				sup[p.Code] += p.Qty
			} else {
				dem[p.Code] += p.Qty
			}
		}
	}
	// how many active listings (any item) accept each currency as payment = demand for it as money
	for _, ads := range [][]Ad{wts, wtb} {
		for i := range ads {
			a := &ads[i]
			if a.Status != "active" || a.Type != "wts" || a.GameMode != m.Realm.Mode || a.Ladder != m.Realm.Season {
				continue
			}
			seen := map[string]bool{}
			for _, p := range a.Payments {
				if p.Kind == "item" && !seen[p.code()] {
					seen[p.code()] = true
					acc[p.code()]++
				}
			}
		}
	}
	recent := map[string][]Recent{}
	for _, t := range trades {
		if t.Status != "completed" || len(t.Goods) != 1 || len(t.Price) != 1 {
			continue
		}
		g, p := t.Goods[0], t.Price[0]
		r := Recent{Date: t.Date, Code: p.Code, Qty: p.Qty, PerUnit: p.Qty / g.Qty}
		if v, ok := m.Values[p.Code]; ok {
			r.Ist = ptr(p.Qty * v / g.Qty)
		}
		recent[g.Code] = append(recent[g.Code], r)
	}
	m.TableBy = map[string]*Row{}
	for _, c := range m.Catalog.Order {
		v, valued := m.Values[c]
		if !valued && sup[c] == 0 && dem[c] == 0 {
			continue
		}
		cur := m.Catalog.ByCode[c]
		rs := recent[c]
		sort.SliceStable(rs, func(i, j int) bool { return rs[i].Date > rs[j].Date })
		if len(rs) > 8 {
			rs = rs[:8]
		}
		if rs == nil {
			rs = []Recent{}
		}
		row := &Row{Code: c, Name: cur.Name, Group: cur.Group(), Obs: m.NObs[c], Supply: sup[c], Demand: dem[c],
			AcceptedBy: acc[c], Recent: rs}
		if valued {
			row.Ist = ptr(v)
		}
		m.Table = append(m.Table, row)
		m.TableBy[c] = row
	}
}

// buildEdges keeps the best live single-item rate per (pay -> receive) pair, for the exchange map.
func (m *RealmMarket) buildEdges(trades []Trade) {
	type best struct {
		rate, qa, qb float64
		t            *Trade
	}
	type key struct{ a, b string }
	bests := map[key]*best{}
	var order []key
	pairs := map[key]int{}
	for i := range trades {
		t := &trades[i]
		if t.Status != "active" {
			continue
		}
		give, get := PlayerSide(t)
		for _, a := range give {
			for _, b := range get {
				if a.Code != b.Code {
					pairs[key{a.Code, b.Code}]++
				}
			}
		}
		if len(give) != 1 || len(get) != 1 || give[0].Code == get[0].Code {
			continue
		}
		k := key{give[0].Code, get[0].Code}
		rate := give[0].Qty / get[0].Qty
		if b, ok := bests[k]; !ok {
			order = append(order, k)
			bests[k] = &best{rate, give[0].Qty, get[0].Qty, t}
		} else if rate < b.rate {
			*b = best{rate, give[0].Qty, get[0].Qty, t}
		}
	}
	m.Edges = []Edge{}
	for _, k := range order {
		b := bests[k]
		e := Edge{From: k.a, To: k.b, Rate: b.rate, Give: b.qa, Get: b.qb, N: pairs[k], URL: LotURL(b.t.Type, b.t.ID)}
		va, okA := m.Values[k.a]
		vb, okB := m.Values[k.b]
		if okA && okB {
			fair := vb / va
			if q := fair / b.rate; !(q > 0.2 && q < 5) {
				continue // joke prices
			}
			e.FairRate = ptr(fair)
		}
		m.Edges = append(m.Edges, e)
	}
}

func (m *RealmMarket) buildDeals(trades []Trade) {
	m.Deals = []Deal{}
	for i := range trades {
		t := &trades[i]
		if t.Status != "active" {
			continue
		}
		give, get := PlayerSide(t)
		thin := false
		for _, b := range []bundle.Bundle{give, get} {
			for _, p := range b {
				if m.NObs[p.Code] < dealMinObs {
					thin = true // thinly traded items have unreliable fair values
				}
			}
		}
		if thin {
			continue
		}
		cv, ok1 := Worth(give, m.Values)
		gv, ok2 := Worth(get, m.Values)
		if ok1 && ok2 && cv != 0 && gv != 0 && gv/cv > dealRatio {
			m.Deals = append(m.Deals, Deal{t.Type, give, get, gv / cv, cv, gv, t.Unit, LotURL(t.Type, t.ID)})
		}
	}
	sort.SliceStable(m.Deals, func(i, j int) bool { return m.Deals[i].Ratio > m.Deals[j].Ratio })
	if len(m.Deals) > dealsKept {
		m.Deals = m.Deals[:dealsKept]
	}
}

// itemActions: non-currency goods; the cheapest live lots per item name and every WTB for an item.
func (m *RealmMarket) itemActions(wts, wtb []Ad) []Action {
	valued := func(a *Ad) []bundle.Bundle {
		var out []bundle.Bundle
		for _, o := range PriceOptions(a) {
			if _, ok := Worth(o, m.Values); ok {
				out = append(out, o)
			}
		}
		return out
	}
	info := func(a *Ad) string {
		k := catalog.ItemKey(a.ItemName, a.Quality)
		if _, ok := m.Items[k]; !ok {
			m.Items[k] = &ItemInfo{Name: a.ItemName, Quality: a.Quality}
			m.ItemsOrder = append(m.ItemsOrder, k)
		}
		return k
	}
	type ask struct {
		cheapest float64
		ad       *Ad
		opts     []bundle.Bundle
	}
	asks, sold := map[string][]ask{}, map[string][]float64{}
	var askOrder []string
	for i := range wts {
		a := &wts[i]
		if !m.Realm.Holds(a) || a.PayloadKind != "lot" {
			continue
		}
		allCur := true
		for _, p := range Goods(a) {
			if !m.Catalog.Has(p.Code) {
				allCur = false
			}
		}
		if allCur {
			continue
		}
		opts := valued(a)
		if len(opts) == 0 {
			continue
		}
		k := info(a)
		cheapest := 0.0
		for j, o := range opts {
			w, _ := Worth(o, m.Values)
			if j == 0 || w < cheapest {
				cheapest = w
			}
		}
		switch a.Status {
		case "active":
			if _, ok := asks[k]; !ok {
				askOrder = append(askOrder, k)
			}
			asks[k] = append(asks[k], ask{cheapest, a, opts})
		case "completed":
			if m.Relevance.Weight(a.Date()) > 0 { // a sale from the last season says nothing about this one
				sold[k] = append(sold[k], cheapest)
			}
		}
	}
	var actions []Action
	for _, k := range askOrder {
		lst := asks[k]
		sort.SliceStable(lst, func(i, j int) bool { return lst[i].cheapest < lst[j].cheapest })
		if len(lst) > itemLots {
			lst = lst[:itemLots]
		}
		for _, x := range lst {
			for _, o := range x.opts {
				actions = append(actions, Action{ID: x.ad.ID, Type: "wts", Give: o, Get: bundle.Bundle{{Code: k, Qty: 1}}, Uses: 1})
			}
		}
	}
	for i := range wtb {
		a := &wtb[i]
		if a.Status != "active" || !m.Realm.Holds(a) || m.Catalog.Has(a.ItemNameKey) {
			continue
		}
		k := info(a)
		// Only your own copy can fill an item WTB: a bought one has different rolls, so it is a separate code.
		for _, o := range valued(a) {
			actions = append(actions, Action{ID: a.ID, Type: "wtb", Give: bundle.Bundle{{Code: catalog.Own(k), Qty: a.quantity()}},
				Get: o, Uses: 1, Check: a.HasRequirements()})
		}
	}
	for _, k := range m.ItemsOrder {
		it := m.Items[k]
		if lst := asks[k]; len(lst) > 0 {
			it.Ask = ptr(lst[0].cheapest)
			it.Live = len(lst)
		}
		s := sold[k]
		sort.Float64s(s)
		it.Sold = len(s)
		if len(s) > 0 {
			it.Median = ptr(s[len(s)/2])
		}
	}
	return actions
}

// quotes: per currency, the cheapest WTS selling only it (ask) and the richest WTB asking only for it (bid).
// Paying with a currency for someone's goods is not a bid for it, so WTS never count as sells.
func (m *RealmMarket) quotes() map[string]*Quotes {
	q := map[string]*Quotes{}
	plain := func(b bundle.Bundle) bool {
		for _, p := range b {
			if strings.Contains(p.Code, ":") {
				return false
			}
		}
		return true
	}
	get := func(c string) *Quotes {
		if q[c] == nil {
			q[c] = &Quotes{}
		}
		return q[c]
	}
	for i := range m.Actions {
		a := &m.Actions[i]
		if !plain(a.Give) || !plain(a.Get) {
			continue
		}
		if a.Type == "wts" && len(a.Get) == 1 {
			c, n := a.Get[0].Code, a.Get[0].Qty
			if val, ok := Worth(a.Give, m.Values); ok && val != 0 {
				if cur := get(c); cur.Ask == nil || val/n < cur.Ask.Per {
					cur.Ask = &Quote{val / n, a}
				}
			}
		}
		if a.Type == "wtb" && len(a.Give) == 1 {
			c, n := a.Give[0].Code, a.Give[0].Qty
			if val, ok := Worth(a.Get, m.Values); ok && val != 0 {
				if cur := get(c); cur.Bid == nil || val/n > cur.Bid.Per {
					cur.Bid = &Quote{val / n, a}
				}
			}
		}
	}
	return q
}

// Fair is the fair value of a currency, or what an item of that name usually sells for.
func (m *RealmMarket) Fair(code string) *float64 {
	if strings.HasPrefix(code, "i:") {
		it := m.Items[code]
		if it == nil {
			return nil
		}
		if it.Median != nil {
			return it.Median
		}
		return it.Ask
	}
	if v, ok := m.Values[code]; ok {
		return &v
	}
	return nil
}

func (m *RealmMarket) Known(code string) bool {
	if strings.HasPrefix(code, "i:") {
		return m.Items[code] != nil
	}
	return m.TableBy[code] != nil
}

// OffersFor lists every live listing that yields the target, cheapest per piece first.
// A bundle's price is split across its parts in proportion to their fair values.
func (m *RealmMarket) OffersFor(target string) []Offer {
	fair := m.Fair(target)
	out := []Offer{}
	for i := range m.Actions {
		a := &m.Actions[i]
		if !a.Get.Has(target) {
			continue
		}
		cost, ok := Worth(a.Give, m.Values)
		if !ok {
			continue
		}
		gain := 0.0
		for _, p := range a.Get {
			if p.Code == target {
				if fair != nil {
					gain += p.Qty * *fair
				}
			} else {
				gain += p.Qty * m.Values[p.Code]
			}
		}
		per := cost / a.Get.Get(target)
		if fair != nil && *fair != 0 && gain != 0 {
			per = cost * *fair / gain
		}
		o := Offer{Type: a.Type, Give: a.Give, Get: a.Get, Uses: a.Uses, Check: a.Check, Per: per,
			Pure: len(a.Get) == 1, URL: LotURL(a.Type, a.ID)}
		if fair != nil && *fair != 0 {
			o.Discount = ptr(1 - per / *fair)
		}
		out = append(out, o)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Per < out[j].Per })
	return out
}

func (m *RealmMarket) CubeCost(target string) *CubeCost {
	recipe, ok := catalog.CubeByOut[target]
	if !ok {
		return nil
	}
	c := &CubeCost{Recipe: recipe, Complete: true}
	for _, p := range recipe {
		if v, ok := m.Values[p.Code]; ok {
			c.Ist += p.Qty * v
		} else {
			c.Complete = false
		}
	}
	return c
}
