// Package routing finds the cheapest chain of live listings and cube crafts that turns resources into a target.
//
// Resources: Pool (finite: what is in the stash) and Unlimited (e.g. keys when you can always get more),
// each with a unit cost in Ist. Every listing and craft is a recipe give -> get.
//
//  1. Unit cost of every item (Knuth's generalised Dijkstra over recipes: an item's cost is fixed once,
//     cheapest first, so arbitrage loops cannot drive costs to zero). A listing that yields several items
//     splits its price across them in proportion to fair value. Each trade adds a little effort cost.
//  2. Paper chain: that cheapest tree expanded for the wanted amount, ignoring how often a listing can be used,
//     with the number of matching listings next to each step.
//  3. Real chain: buy the target from the cheapest recipe that still has uses left, recursively buying its
//     inputs; whole lots only, leftovers go to a surplus pool that later steps draw from first.
package routing

import (
	"container/heap"
	"math"
	"sort"
	"strings"

	"loot-planner/internal/domain/bundle"
	"loot-planner/internal/domain/catalog"
	"loot-planner/internal/domain/market"
)

const (
	maxDepth  = 14
	maxAmount = 500
	budget0   = 20000 // guards against pathological branching
)

var inf = math.Inf(1)

type Request struct {
	Target    string
	Qty       int
	Pool      bundle.Bundle      // finite resources (stash)
	PoolCost  map[string]float64 // Ist per piece for pool items without a fair value
	Unlimited bundle.Bundle      // resource -> Ist per piece, never runs out
	UseCube   bool
	Effort    float64 // Ist per trade (a craft costs a fifth), so short chains beat marginally cheaper long ones
}

type Step struct {
	ID        string        `json:"id"`
	Type      string        `json:"type"`
	Check     bool          `json:"check"`
	Give      bundle.Bundle `json:"give"`
	Get       bundle.Bundle `json:"get"`
	Times     float64       `json:"times"`
	Available *float64      `json:"available,omitempty"` // paper chain only; nil = unlimited (cube)
	rank      int
	URL       *string `json:"url"`
}

type Bottleneck struct {
	Kind    string  `json:"kind"` // lots | stash
	Step    *Step   `json:"step,omitempty"`
	Item    string  `json:"item,omitempty"`
	Missing float64 `json:"missing,omitempty"`
}

type Paper struct {
	Steps          []*Step       `json:"steps"`
	UnlimitedSpent bundle.Bundle `json:"unlimitedSpent"`
	PoolUsed       bundle.Bundle `json:"poolUsed"`
	Missing        bundle.Bundle `json:"missing"`
	Leftover       bundle.Bundle `json:"leftover"`
	CostIst        float64       `json:"costIst"`
}

type Result struct {
	OK             bool          `json:"ok"`
	Reason         *string       `json:"reason"`
	Bottlenecks    []Bottleneck  `json:"bottlenecks"`
	UnitCost       float64       `json:"unitCost"`
	Paper          *Paper        `json:"paper"`
	Steps          []*Step       `json:"steps"`
	UnlimitedSpent bundle.Bundle `json:"unlimitedSpent"`
	PoolUsed       bundle.Bundle `json:"poolUsed"`
	CostIst        float64       `json:"costIst"`
	Leftover       bundle.Bundle `json:"leftover"`
}

type recipe struct {
	id, typ   string
	give, get bundle.Bundle
	uses      float64
	check     bool
	cube      bool
}

// bag is an amount per code that remembers insertion order (for stable output).
type bag struct {
	q     map[string]float64
	order []string
}

func newBag() *bag { return &bag{q: map[string]float64{}} }

func (b *bag) add(c string, x float64) {
	if _, ok := b.q[c]; !ok {
		b.order = append(b.order, c)
	}
	b.q[c] += x
}

func (b *bag) take(c string, n float64) float64 {
	t := math.Min(b.q[c], n)
	if t > 0 {
		b.q[c] -= t
		return t
	}
	return 0
}

func (b *bag) clone() *bag {
	c := &bag{q: make(map[string]float64, len(b.q)), order: append([]string(nil), b.order...)}
	for k, v := range b.q {
		c.q[k] = v
	}
	return c
}

func (b *bag) bundle(positive bool) bundle.Bundle {
	out := bundle.Bundle{}
	for _, c := range b.order {
		if q := b.q[c]; !positive || q > 0 {
			out = append(out, bundle.Part{Code: c, Qty: q})
		}
	}
	return out
}

type entry struct {
	cost float64
	seq  int
	item string
	r    int
}

type pq []entry

func (h pq) Len() int { return len(h) }
func (h pq) Less(i, j int) bool {
	if h[i].cost != h[j].cost {
		return h[i].cost < h[j].cost
	}
	return h[i].seq < h[j].seq
}
func (h pq) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *pq) Push(x any)   { *h = append(*h, x.(entry)) }
func (h *pq) Pop() any     { old := *h; e := old[len(old)-1]; *h = old[:len(old)-1]; return e }

type state struct {
	surplus, pool, spent, used *bag
	cap                        map[string]float64
	steps                      []*Step
}

func (s *state) snapshot() *state {
	cp := make(map[string]float64, len(s.cap))
	for k, v := range s.cap {
		cp[k] = v
	}
	return &state{s.surplus.clone(), s.pool.clone(), s.spent.clone(), s.used.clone(), cp, s.steps[:len(s.steps):len(s.steps)]}
}

func reason(s string) *string { return &s }

func Solve(actions []market.Action, cube []catalog.Recipe, values map[string]float64, req Request) Result {
	target, qty, effortIst := req.Target, float64(req.Qty), req.Effort
	unlimited := map[string]float64{}
	for _, p := range req.Unlimited {
		unlimited[p.Code] = p.Qty
	}
	val := func(c string) float64 { return values[c] } // 0 when unknown
	// the target itself is never spent: anything already in the stash is reported separately
	var pool bundle.Bundle
	for _, p := range req.Pool {
		if _, unl := unlimited[p.Code]; p.Qty > 0 && !unl && p.Code != target {
			pool = append(pool, p)
		}
	}
	// never sell the target itself or buy more of an unlimited resource
	var recipes []*recipe
	for _, a := range actions {
		skip := a.Give.Has(target)
		for _, p := range a.Get {
			if _, ok := unlimited[p.Code]; ok {
				skip = true
			}
		}
		if !skip {
			recipes = append(recipes, &recipe{a.ID, a.Type, a.Give, a.Get, a.Uses, a.Check, false})
		}
	}
	if req.UseCube {
		for _, r := range cube {
			recipes = append(recipes, &recipe{"cube:" + r.Out, "cube", r.Give, bundle.Bundle{{Code: r.Out, Qty: 1}}, inf, false, true})
		}
	}
	share := func(r *recipe, item string) float64 {
		if len(r.get) == 1 {
			return 1
		}
		tot := 0.0
		for _, p := range r.get {
			tot += p.Qty * val(p.Code)
		}
		if tot > 0 {
			return r.get.Get(item) * val(item) / tot
		}
		return 1 / float64(len(r.get))
	}
	effort := func(r *recipe) float64 {
		if r.cube {
			return effortIst / 5
		}
		return effortIst
	}

	// 1. unit costs
	cost, rank, via := map[string]float64{}, map[string]int{}, map[string]*recipe{}
	order := 0
	fix := func(c string, v float64, r *recipe) {
		cost[c] = v
		if r != nil {
			via[c] = r
		}
		rank[c] = order
		order++
	}
	type seed struct {
		c string
		v float64
	}
	var seeds []seed
	for _, p := range req.Unlimited {
		seeds = append(seeds, seed{p.Code, p.Qty})
	}
	for _, p := range pool {
		v, ok := req.PoolCost[p.Code]
		if !ok {
			if v, ok = values[p.Code]; !ok {
				v = 0.001
			}
		}
		seeds = append(seeds, seed{p.Code, v})
	}
	sort.SliceStable(seeds, func(i, j int) bool { return seeds[i].v < seeds[j].v })
	for _, s := range seeds {
		if _, ok := cost[s.c]; !ok {
			fix(s.c, s.v, nil)
		}
	}
	costOf := func(c string) float64 {
		if v, ok := cost[c]; ok {
			return v
		}
		return inf
	}
	inputCost := func(r *recipe) float64 {
		t := effort(r)
		for _, p := range r.give {
			t += p.Qty * costOf(p.Code)
		}
		return t
	}
	usedBy := map[string][]int{}
	missing := make([]int, len(recipes))
	for i, r := range recipes {
		for _, p := range r.give {
			if _, ok := cost[p.Code]; !ok {
				missing[i]++
			}
			usedBy[p.Code] = append(usedBy[p.Code], i)
		}
	}
	h, seq := &pq{}, 0
	offer := func(i int) {
		r := recipes[i]
		total := inputCost(r)
		for _, p := range r.get {
			if _, ok := cost[p.Code]; !ok {
				heap.Push(h, entry{total * share(r, p.Code) / p.Qty, seq, p.Code, i})
				seq++
			}
		}
	}
	for i := range recipes {
		if missing[i] == 0 {
			offer(i)
		}
	}
	for h.Len() > 0 {
		e := heap.Pop(h).(entry)
		if _, ok := cost[e.item]; ok {
			continue
		}
		fix(e.item, e.cost, recipes[e.r])
		for _, j := range usedBy[e.item] {
			missing[j]--
			if missing[j] == 0 {
				offer(j)
			}
		}
	}
	if _, ok := cost[target]; !ok {
		return Result{OK: false, Reason: reason("unreachable"), Bottlenecks: []Bottleneck{}}
	}
	if via[target] == nil {
		return Result{OK: true, Reason: reason("have"), Steps: []*Step{}, Bottlenecks: []Bottleneck{}}
	}

	sig := func(r *recipe) string { return r.typ + "|" + r.give.Key() + "|" + r.get.Key() }
	avail := map[string]float64{}
	for _, r := range recipes {
		if !r.cube {
			avail[sig(r)] += r.uses
		}
	}
	mkStep := func(r *recipe, times float64) *Step {
		return &Step{ID: r.id, Type: r.typ, Check: r.check, Give: r.give.Mul(times), Get: r.get.Mul(times), Times: times,
			URL: market.LotURL(r.typ, r.id)}
	}
	rankOf := func(c string, dflt int) int {
		if v, ok := rank[c]; ok {
			return v
		}
		return dflt
	}

	// 2. paper chain
	paper := func() *Paper {
		demand, surplus, spent, used, lack := newBag(), newBag(), newBag(), newBag(), newBag()
		left := newBag()
		for _, p := range pool {
			left.add(p.Code, p.Qty)
		}
		demand.add(target, qty)
		rows := []*Step{}
		for {
			item, best := "", math.MinInt
			for _, c := range demand.order {
				if demand.q[c] > 0 && rankOf(c, -1) > best {
					item, best = c, rankOf(c, -1)
				}
			}
			if item == "" {
				break
			}
			n := demand.q[item]
			// pop: drop from demand keeping later re-adds at the end, as a dict delete would
			delete(demand.q, item)
			for i, c := range demand.order {
				if c == item {
					demand.order = append(demand.order[:i:i], demand.order[i+1:]...)
					break
				}
			}
			n -= surplus.take(item, n)
			t2 := left.take(item, n)
			if t2 > 0 {
				used.add(item, t2)
			}
			n -= t2
			if n <= 0 {
				continue
			}
			if _, ok := unlimited[item]; ok {
				spent.add(item, n)
				continue
			}
			r := via[item]
			if r == nil {
				lack.add(item, n) // stash ran out and nothing else makes it
				continue
			}
			times := math.Ceil(n / r.get.Get(item))
			for _, p := range r.give {
				demand.add(p.Code, p.Qty*times)
			}
			for _, p := range r.get {
				surplus.add(p.Code, p.Qty*times)
			}
			surplus.q[item] -= n
			s := mkStep(r, times)
			if !r.cube {
				a := avail[sig(r)]
				s.Available = &a
			}
			s.rank = rank[item]
			rows = append(rows, s)
		}
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].rank < rows[j].rank })
		return &Paper{Steps: rows, UnlimitedSpent: spent.bundle(false), PoolUsed: used.bundle(false),
			Missing: lack.bundle(false), Leftover: surplus.bundle(true)}
	}
	paperChain := paper()

	// 3. real chain
	poolBag := newBag()
	for _, p := range pool {
		poolBag.add(p.Code, p.Qty)
	}
	st := &state{newBag(), poolBag, newBag(), newBag(), map[string]float64{}, nil}
	budget := budget0
	failedAt := map[string]float64{} // item + route -> smallest amount that could not be bought (capacities only shrink)
	byItem := map[string][]*recipe{}
	for _, r := range recipes {
		for _, p := range r.get {
			byItem[p.Code] = append(byItem[p.Code], r)
		}
	}
	capLeft := func(r *recipe) float64 {
		if r.cube {
			return inf
		}
		if v, ok := st.cap[r.id]; ok {
			return v
		}
		return r.uses
	}
	unit := func(c string) float64 {
		if v, ok := unlimited[c]; ok {
			return v
		}
		if v, ok := req.PoolCost[c]; ok {
			return v
		}
		return val(c)
	}
	// what the plan so far really costs: resources spent, trades made, minus half the change left over
	stateCost := func() float64 {
		t := 0.0
		for _, b := range []*bag{st.spent, st.used} {
			for _, c := range b.order {
				t += b.q[c] * unit(c)
			}
		}
		for _, s := range st.steps {
			if s.Type == "cube" {
				t += effortIst / 5
			} else {
				t += effortIst
			}
		}
		change := 0.0
		for _, c := range st.surplus.order {
			change += math.Max(st.surplus.q[c], 0) * val(c)
		}
		return t - change/2
	}
	restore := func(s *state) {
		st = &state{s.surplus, s.pool, s.spent, s.used, s.cap, st.steps[:len(s.steps)]}
	}

	var acquire func(item string, n float64, path []string) bool
	acquire = func(item string, n float64, path []string) bool {
		budget--
		if budget < 0 {
			return false
		}
		n -= st.surplus.take(item, n)
		fromPool := st.pool.take(item, n)
		if fromPool > 0 {
			st.used.add(item, fromPool)
		}
		n -= fromPool
		if n <= 0 {
			return true
		}
		if _, ok := unlimited[item]; ok {
			st.spent.add(item, n)
			return true
		}
		sorted := append([]string(nil), path...)
		sort.Strings(sorted)
		fkey := item + "|" + strings.Join(sorted, ",")
		if f, ok := failedAt[fkey]; len(path) > maxDepth || n > maxAmount || (ok && n >= f) {
			return false
		}
		inPath := func(c string) bool {
			for _, x := range path {
				if x == c {
					return true
				}
			}
			return false
		}
		// the recipe that set the item's cost first, then recipes whose inputs were priced earlier (no hidden loops)
		ownRank := inf
		if v, ok := rank[item]; ok {
			ownRank = float64(v)
		}
		type cand struct {
			r    *recipe
			unit float64
			tier int
		}
		var cands []cand
		for _, r := range byItem[item] {
			if capLeft(r) <= 0 {
				continue
			}
			bad := false
			for _, p := range r.give {
				if inPath(p.Code) || p.Code == item {
					bad = true
				}
			}
			ic := inputCost(r)
			if bad || math.IsInf(ic, 0) || math.IsNaN(ic) {
				continue
			}
			tier := 2
			if r == via[item] {
				tier = 0
			} else {
				acyclic := true
				for _, p := range r.give {
					rk := inf
					if v, ok := rank[p.Code]; ok {
						rk = float64(v)
					}
					if !(rk < ownRank) {
						acyclic = false
					}
				}
				if acyclic {
					tier = 1
				}
			}
			cands = append(cands, cand{r, ic * share(r, item) / r.get.Get(item), tier})
		}
		sort.SliceStable(cands, func(i, j int) bool {
			if cands[i].tier != cands[j].tier {
				return cands[i].tier < cands[j].tier
			}
			return cands[i].unit < cands[j].unit
		})
		var kept []cand
		cyclic := 0
		for _, c := range cands {
			if c.tier < 2 || cyclic < 2 {
				kept = append(kept, c)
			}
			if c.tier == 2 {
				cyclic++
			}
		}
		if len(kept) > 8 {
			kept = kept[:8]
		}
		next := append(path[:len(path):len(path)], item)
		tryRecipe := func(r *recipe) (float64, bool) {
			times := math.Min(math.Ceil(n/r.get.Get(item)), capLeft(r))
			for _, p := range r.give {
				if !acquire(p.Code, p.Qty*times, next) {
					return 0, false
				}
			}
			if !r.cube {
				st.cap[r.id] = capLeft(r) - times
			}
			st.steps = append(st.steps, mkStep(r, times))
			for _, p := range r.get {
				st.surplus.add(p.Code, p.Qty*times)
			}
			return n - st.surplus.take(item, n), true
		}
		if len(path) <= 1 {
			// near the top, try every candidate in full and keep the one that really costs least
			for guard := 0; guard < 20 && n > 0; guard++ {
				var best *state
				var bestLeft, bestCost float64
				for _, c := range kept {
					if capLeft(c.r) <= 0 {
						continue
					}
					snap := st.snapshot()
					left, ok := tryRecipe(c.r)
					if ok && left < n {
						sc := stateCost()
						if best == nil || left < bestLeft || (left == bestLeft && sc < bestCost) {
							s := st.snapshot()
							s.steps = append([]*Step(nil), st.steps...)
							best, bestLeft, bestCost = s, left, sc
						}
					}
					restore(snap)
				}
				if best == nil {
					break
				}
				st = best
				n = bestLeft
			}
		} else {
			for _, c := range kept {
				if n <= 0 {
					break
				}
				snap := st.snapshot()
				left, ok := tryRecipe(c.r)
				if !ok {
					restore(snap)
					continue
				}
				n = left
			}
		}
		if n > 0 {
			if f, ok := failedAt[fkey]; !ok || n < f {
				failedAt[fkey] = n
			}
		}
		return n <= 0
	}

	ok := acquire(target, qty, nil)
	// bottlenecks, read off the cheapest (paper) route: listings needed more often than they exist, stash that runs out
	bott := []Bottleneck{}
	if !ok {
		for _, s := range paperChain.Steps {
			if s.Available != nil && s.Times > *s.Available {
				bott = append(bott, Bottleneck{Kind: "lots", Step: s})
			}
		}
		for _, p := range paperChain.Missing {
			bott = append(bott, Bottleneck{Kind: "stash", Item: p.Code, Missing: p.Qty})
		}
	}
	ist := func(b bundle.Bundle) float64 {
		t := 0.0
		for _, p := range b {
			t += p.Qty * unit(p.Code)
		}
		return t
	}
	paperChain.CostIst = ist(paperChain.UnlimitedSpent) + ist(paperChain.PoolUsed)
	res := Result{OK: ok, Bottlenecks: bott, UnitCost: cost[target], Paper: paperChain, Steps: st.steps,
		UnlimitedSpent: st.spent.bundle(false), PoolUsed: st.used.bundle(false), Leftover: st.surplus.bundle(true)}
	if res.Steps == nil {
		res.Steps = []*Step{}
	}
	res.CostIst = ist(res.UnlimitedSpent) + ist(res.PoolUsed)
	if !ok {
		if budget < 0 {
			res.Reason = reason("budget")
		} else {
			res.Reason = reason("capacity")
		}
	}
	return res
}
