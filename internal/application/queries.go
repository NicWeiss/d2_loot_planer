package application

import (
	"sort"
	"strings"

	"loot-planner/internal/domain/bundle"
	"loot-planner/internal/domain/catalog"
	"loot-planner/internal/domain/market"
	"loot-planner/internal/domain/routing"
	"loot-planner/internal/domain/stash"
)

// Read models for the screens: everything the page shows is computed here, the page only formats it.

var (
	QuickTargets = []string{"r32", "r25", "r26", "r27", "xa4", "pk2"}
	Bases        = []string{"r24", "pk2", "r20", "r22", "r26", "r29", "r30", "xa4"} // units the stash and rates can be shown in
	equivalents  = []string{"pk2", "r20", "r22", "r23", "r24", "r25", "r26", "r27", "r29", "xa4"}
	cross        = []string{"pk1", "pk2", "pk3", "xa4", "r20", "r22", "r23", "r24", "r25", "r26", "r27", "r28", "r29", "r30", "r31", "r32", "r33"}
	Keys         = []string{"pk1", "pk2", "pk3"}
)

const (
	mapMinObs    = 3
	offersShown  = 80
	wtbMaxPieces = 100 // a fair-price WTB paid in more pieces than this is impractical
)

type M = map[string]any

type RouteParams struct {
	Target    string
	Qty       int
	StashMode string   // all | currency | gems | none
	Keys      []string // keys that never run out
	UseCube   bool
	Effort    float64
}

type Queries struct {
	Market *MarketService
	Access *Access
}

func f64(x float64) *float64 { return &x }

func orNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// itemRefs names the items a response mentions (codes i:… and own:i:…), so the page can label them.
func itemRefs(m *market.RealmMarket, codes ...[]string) M {
	refs := M{}
	for _, cs := range codes {
		for _, c := range cs {
			k := strings.TrimPrefix(c, "own:")
			if it := m.Items[k]; strings.HasPrefix(k, "i:") && it != nil {
				refs[k] = M{"name": it.Name, "quality": it.Quality}
			}
		}
	}
	return refs
}

func codesOf(bs ...bundle.Bundle) []string {
	var out []string
	for _, b := range bs {
		out = append(out, b.Codes()...)
	}
	return out
}

func realmStash(sess *Session, realm string) *stash.RealmStash {
	if sess == nil || sess.Stash == nil {
		return nil
	}
	return sess.Stash.Realms[realm]
}

func (q *Queries) Meta(sess *Session) M {
	st := q.Market.State()
	var stashRealms []string
	if sess != nil && sess.Stash != nil {
		stashRealms = sess.Stash.RealmKeys()
	}
	has := func(k string) bool {
		for _, r := range stashRealms {
			if r == k {
				return true
			}
		}
		return false
	}
	realms := []M{}
	def := q.Market.RealmKeys[0]
	for _, r := range stashRealms {
		if contains(q.Market.RealmKeys, r) {
			def = r
			break
		}
	}
	names := M{}
	for _, k := range q.Market.RealmKeys {
		var counts any
		if st != nil {
			counts = st.Realms[k].Counts
		}
		realms = append(realms, M{"key": k, "counts": counts, "stash": has(k)})
	}
	var fetched, season any
	if st != nil {
		fetched = orNil(st.FetchedAt)
		if !st.SeasonStart.IsZero() {
			season = M{"start": st.SeasonStart.Format("2006-01-02"), "auto": st.SeasonAuto}
		}
		for _, c := range st.Catalog.Order {
			cur := st.Catalog.ByCode[c]
			names[c] = M{"name": cur.Name, "display": cur.Display(), "group": cur.Group()}
		}
	}
	return M{"ready": st != nil, "fetchedAt": fetched, "season": season, "realms": realms, "defaultRealm": def, "names": names,
		"groups": catalog.Groups, "quick": QuickTargets, "bases": Bases, "keys": Keys, "job": q.Market.Job(),
		"admin": q.Access.IsAdmin(sess), "refreshHours": q.Market.RefreshEvery.Hours()}
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func (q *Queries) Targets(realm string) (M, error) {
	m, err := q.Market.Realm(realm)
	if err != nil {
		return nil, err
	}
	cur := []M{}
	for _, row := range m.Table {
		if row.Ist != nil || row.Supply != 0 || row.Demand != 0 {
			cur = append(cur, M{"code": row.Code, "display": m.Catalog.ByCode[row.Code].Display()})
		}
	}
	type kv struct {
		k  string
		it *market.ItemInfo
	}
	var live []kv
	for _, k := range m.ItemsOrder {
		if it := m.Items[k]; it.Live > 0 {
			live = append(live, kv{k, it})
		}
	}
	sort.SliceStable(live, func(i, j int) bool { return live[i].it.Live > live[j].it.Live })
	items := []M{}
	for _, x := range live {
		items = append(items, M{"code": x.k, "name": x.it.Name, "quality": x.it.Quality})
	}
	return M{"currencies": cur, "items": items}, nil
}

func (q *Queries) Target(realm, code string, sess *Session) (M, error) {
	m, err := q.Market.Realm(realm)
	if err != nil {
		return nil, err
	}
	fair, offers := m.Fair(code), m.OffersFor(code)
	var best *market.Offer
	for i := range offers {
		if offers[i].Pure {
			best = &offers[i]
			break
		}
	}
	if best == nil && len(offers) > 0 {
		best = &offers[0]
	}
	have := 0.0
	if rs := realmStash(sess, realm); rs != nil {
		have = rs.Have(code)
	}
	out := M{"code": code, "known": m.Known(code), "isItem": strings.HasPrefix(code, "i:"), "fair": fair,
		"best": nil, "offers": len(offers), "have": have}
	if best != nil {
		out["best"] = M{"per": best.Per, "discount": best.Discount, "url": best.URL}
	}
	if strings.HasPrefix(code, "i:") {
		out["item"] = nil
		if it := m.Items[code]; it != nil {
			out["item"] = M{"name": it.Name, "quality": it.Quality, "live": it.Live, "sold": it.Sold}
		}
		return out, nil
	}
	row := m.TableBy[code]
	out["acceptedBy"], out["recent"] = 0, []market.Recent{}
	if row != nil {
		out["acceptedBy"] = row.AcceptedBy
		rec := row.Recent
		if len(rec) > 5 {
			rec = rec[:5]
		}
		out["recent"] = rec
	}
	eq := []M{}
	for _, c := range equivalents {
		if v, ok := m.Values[c]; fair != nil && *fair != 0 && c != code && ok && v != 0 {
			eq = append(eq, M{"code": c, "qty": *fair / v})
		}
	}
	out["equivalents"] = eq
	cube := m.CubeCost(code)
	if cube != nil && fair != nil && *fair != 0 {
		cube.Discount = f64(1 - cube.Ist / *fair)
	}
	out["cube"] = cube
	return out, nil
}

func (q *Queries) Offers(realm, code string) (M, error) {
	m, err := q.Market.Realm(realm)
	if err != nil {
		return nil, err
	}
	offers := m.OffersFor(code)
	if len(offers) > offersShown {
		offers = offers[:offersShown]
	}
	var codes []string
	for _, o := range offers {
		codes = append(codes, codesOf(o.Give, o.Get)...)
	}
	return M{"offers": offers, "items": itemRefs(m, codes, []string{code})}, nil
}

func (q *Queries) Route(realm string, p RouteParams, sess *Session) (M, error) {
	m, err := q.Market.Realm(realm)
	if err != nil {
		return nil, err
	}
	rs := realmStash(sess, realm)
	pool, poolCost := bundle.Bundle{}, map[string]float64{}
	if rs != nil {
		pool, poolCost = rs.Pool(m, p.StashMode)
	}
	var unlimited bundle.Bundle
	for _, c := range Keys { // in key order, whatever order the request listed them
		if contains(p.Keys, c) {
			v := m.Values[c]
			if v == 0 {
				v = 0.12
			}
			unlimited = append(unlimited, bundle.Part{Code: c, Qty: v})
		}
	}
	qty := max(1, min(20, p.Qty))
	res := routing.Solve(m.Actions, catalog.Cube, m.Values, routing.Request{Target: p.Target, Qty: qty, Pool: pool,
		PoolCost: poolCost, Unlimited: unlimited, UseCube: p.UseCube, Effort: max(0, p.Effort)})
	fair := m.Fair(p.Target)
	change := 0.0
	for _, x := range res.Leftover {
		change += x.Qty * m.Values[x.Code]
	}
	keysFair := []M{}
	if fair != nil {
		for _, u := range unlimited {
			keysFair = append(keysFair, M{"code": u.Code, "qty": float64(qty) * *fair / u.Qty})
		}
	}
	codes := []string{p.Target}
	steps := res.Steps
	if res.Paper != nil {
		steps = append(append([]*routing.Step{}, steps...), res.Paper.Steps...)
		codes = append(codes, codesOf(res.Paper.Missing)...)
	}
	for _, s := range steps {
		codes = append(codes, codesOf(s.Give, s.Get)...)
	}
	codes = append(codes, codesOf(res.PoolUsed, res.Leftover)...)
	for _, b := range res.Bottlenecks {
		if b.Kind == "stash" {
			codes = append(codes, b.Item)
		}
	}
	loaded := sess != nil && sess.Stash != nil
	var owner any
	if loaded {
		owner = sess.Stash.Owner
	}
	return M{"route": res, "fair": fair, "change": change, "keysFair": keysFair,
		"stash": M{"loaded": loaded, "owner": owner, "realm": rs != nil}, "items": itemRefs(m, codes)}, nil
}

// Strategy lists what to farm to reach the target, with the stash and live listings as they are.
func (q *Queries) Strategy(realm string, p RouteParams, sess *Session) (M, error) {
	m, err := q.Market.Realm(realm)
	if err != nil {
		return nil, err
	}
	rs := realmStash(sess, realm)
	pool, poolCost := bundle.Bundle{}, map[string]float64{}
	if rs != nil {
		pool, poolCost = rs.Pool(m, p.StashMode)
	}
	var farmables []string
	for _, c := range m.Catalog.Order {
		if m.Catalog.ByCode[c].Farmable() && m.Values[c] > 0 {
			farmables = append(farmables, c)
		}
	}
	qty := max(1, min(20, p.Qty))
	base := routing.Request{Target: p.Target, Qty: qty, Pool: pool, PoolCost: poolCost, UseCube: p.UseCube, Effort: max(0, p.Effort)}
	list := routing.Strategies(m.Actions, catalog.Cube, m.Values, base, farmables, pool.Get)
	if list == nil {
		list = []routing.Strategy{}
	}
	plain := routing.Solve(m.Actions, catalog.Cube, m.Values, base)
	// When no lots lead there, the player can still farm and post a WTB for the target at its fair price.
	// A payment the market takes readily (many listings accept it) makes such a WTB more likely to be filled.
	wtb := []M{}
	if fair := m.Fair(p.Target); fair != nil && *fair > 0 {
		for _, c := range farmables {
			if c == p.Target {
				continue
			}
			need := float64(qty) * *fair / m.Values[c]
			if need > wtbMaxPieces {
				continue // nobody fills a WTB paid in a thousand ingots
			}
			accepted := 0
			if row := m.TableBy[c]; row != nil {
				accepted = row.AcceptedBy
			}
			wtb = append(wtb, M{"code": c, "qty": need, "have": pool.Get(c), "need": max(0, need-pool.Get(c)), "acceptedBy": accepted})
		}
		sort.SliceStable(wtb, func(i, j int) bool { return wtb[i]["acceptedBy"].(int) > wtb[j]["acceptedBy"].(int) })
	}
	codes := []string{p.Target}
	for _, s := range list {
		r := s.Route
		for _, st := range r.Steps {
			codes = append(codes, codesOf(st.Give, st.Get)...)
		}
		if r.Paper != nil {
			for _, st := range r.Paper.Steps {
				codes = append(codes, codesOf(st.Give, st.Get)...)
			}
		}
		codes = append(codes, codesOf(r.PoolUsed, r.Leftover)...)
	}
	loaded := sess != nil && sess.Stash != nil
	return M{"strategies": list, "plainOK": plain.OK, "wtb": wtb, "fair": m.Fair(p.Target), "items": itemRefs(m, codes),
		"stash": M{"loaded": loaded, "realm": rs != nil}}, nil
}

func (q *Queries) Rates(realm string) (M, error) {
	m, err := q.Market.Realm(realm)
	if err != nil {
		return nil, err
	}
	rows := []M{}
	for _, row := range m.Table {
		var ask, bid *market.Quote
		if qq := m.Quotes[row.Code]; qq != nil {
			ask, bid = qq.Ask, qq.Bid
		}
		r := M{"code": row.Code, "name": row.Name, "group": row.Group, "ist": row.Ist,
			"ask": nil, "askUrl": nil, "askVsFair": nil, "bid": nil, "bidUrl": nil, "bidVsFair": nil, "spread": nil,
			"supply": row.Supply, "demand": row.Demand, "obs": row.Obs}
		rec := row.Recent
		if len(rec) > 3 {
			rec = rec[:3]
		}
		r["recent"] = rec
		if ask != nil {
			r["ask"], r["askUrl"] = ask.Per, market.LotURL(ask.Action.Type, ask.Action.ID)
			if row.Ist != nil && *row.Ist != 0 {
				r["askVsFair"] = ask.Per / *row.Ist - 1
			}
		}
		if bid != nil {
			r["bid"], r["bidUrl"] = bid.Per, market.LotURL(bid.Action.Type, bid.Action.ID)
			if row.Ist != nil && *row.Ist != 0 {
				r["bidVsFair"] = bid.Per / *row.Ist - 1
			}
		}
		if ask != nil && bid != nil {
			r["spread"] = ask.Per/bid.Per - 1
		}
		rows = append(rows, r)
	}
	codes := []string{}
	for _, c := range cross {
		if m.Values[c] != 0 {
			codes = append(codes, c)
		}
	}
	matrix := make([][]float64, len(codes))
	for i, r := range codes {
		matrix[i] = make([]float64, len(codes))
		for j, c := range codes {
			matrix[i][j] = m.Values[r] / m.Values[c]
		}
	}
	return M{"rows": rows, "cross": M{"codes": codes, "matrix": matrix}}, nil
}

func (q *Queries) Map(realm string) (M, error) {
	m, err := q.Market.Realm(realm)
	if err != nil {
		return nil, err
	}
	nodes, on := []M{}, map[string]bool{}
	for _, row := range m.Table {
		if row.Ist != nil && (row.Obs >= mapMinObs || row.Supply != 0 || row.Demand != 0) {
			nodes = append(nodes, M{"code": row.Code, "group": row.Group, "ist": row.Ist, "supply": row.Supply,
				"demand": row.Demand, "acceptedBy": row.AcceptedBy})
			on[row.Code] = true
		}
	}
	edges := []market.Edge{}
	for _, e := range m.Edges {
		if on[e.From] && on[e.To] {
			edges = append(edges, e)
		}
	}
	return M{"nodes": nodes, "edges": edges}, nil
}

func (q *Queries) Deals(realm string) (M, error) {
	m, err := q.Market.Realm(realm)
	if err != nil {
		return nil, err
	}
	return M{"deals": m.Deals}, nil
}

func (q *Queries) Stash(realm string, sess *Session) (M, error) {
	if sess == nil || sess.Stash == nil {
		return M{"loaded": false, "admin": q.Access.IsAdmin(sess)}, nil
	}
	s := sess.Stash
	out := M{"loaded": true, "owner": s.Owner, "source": s.Source, "fetchedAt": s.FetchedAt,
		"remembered": sess.Remembered(), "realms": s.RealmKeys(), "admin": q.Access.IsAdmin(sess)}
	if rs := s.Realms[realm]; rs != nil {
		m, err := q.Market.Realm(realm)
		if err != nil {
			return nil, err
		}
		out["appraisal"] = rs.Appraise(m)
	}
	return out, nil
}
