package market

import (
	"fmt"
	"math"
	"sort"

	"loot-planner/internal/domain/bundle"
	"loot-planner/internal/domain/catalog"
)

// MinObs: items seen in fewer (weighted) trades get no fair price.
const MinObs = 5

// toBase rewrites crafting ingredients in units of what they craft into (12 ingots = 1 sphere, 12 spheres = 1 spark).
func toBase(b bundle.Bundle) bundle.Bundle {
	var out bundle.Bundle
	for _, p := range b {
		if c, ok := catalog.Craft[p.Code]; ok {
			out.Add(c.Base, p.Qty/c.Per)
		} else {
			out.Add(p.Code, p.Qty)
		}
	}
	return out
}

type sparse struct {
	idx []int
	val []float64
}

func (s sparse) dot(v []float64) float64 {
	t := 0.0
	for i, j := range s.idx {
		t += s.val[i] * v[j]
	}
	return t
}

type observation struct {
	g    sparse
	opts []sparse
	wts  bool // a WTS buyer pays the cheapest option, a WTB seller takes the richest
	w    float64
}

// FitValues is a robust log-least-squares fit of values so that value(goods) ~= value(chosen payment)
// across listings.
//
// A listing with several payment options is one observation: a WTS buyer pays with the cheapest option,
// a WTB seller takes the richest, so only that option is matched (re-chosen every iteration).
// Observations that still disagree with the fit more than 3x after a first pass are dropped as joke prices.
// Each listing also counts by its relevance (Trade.Weight), so early-season prices fade as the season goes on;
// the observation count does not, so a rarely traded item keeps its price, just set by its latest trades.
// Returns values in Ist and the weighted observation count per code.
func FitValues(rows []Trade) (map[string]float64, map[string]float64) {
	codeSet := map[string]bool{catalog.Unit: true}
	for _, r := range rows {
		for _, b := range []bundle.Bundle{toBase(r.Goods), toBase(r.Price)} {
			for _, p := range b {
				codeSet[p.Code] = true
			}
		}
	}
	codes := make([]string, 0, len(codeSet))
	for c := range codeSet {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	idx := map[string]int{}
	for i, c := range codes {
		idx[c] = i
	}
	vec := func(b bundle.Bundle) sparse {
		var s sparse
		for _, p := range toBase(b) {
			s.idx = append(s.idx, idx[p.Code])
			s.val = append(s.val, p.Qty)
		}
		return s
	}

	var order []string
	byAd := map[string][]*Trade{}
	for i := range rows {
		r := &rows[i]
		if _, seen := byAd[r.ID]; !seen {
			order = append(order, r.ID)
		}
		byAd[r.ID] = append(byAd[r.ID], r)
	}
	nObs := map[string]float64{}
	var obs []observation
	for _, id := range order {
		rs := byAd[id]
		r := rs[0]
		w := map[string]float64{"completed": 1.0, "active": 0.35, "unlisted": 0.15}[r.Status]
		if r.Status == "active" && r.Type == "wtb" {
			w = 0.5 // standing bids are informative too
		}
		if w == 0 {
			continue
		}
		touched := map[string]bool{}
		craft := false
		for _, o := range rs {
			for _, b := range []bundle.Bundle{o.Goods, o.Price} {
				for _, p := range b {
					if _, ok := catalog.Craft[p.Code]; ok {
						craft = true
					}
				}
			}
		}
		if craft {
			w *= 0.2 // ingots/spheres trade as small change; the chain is priced by its top item
		}
		// how much the listing counts for the price; nObs still counts it whole: an old price is still a price
		ob := observation{g: vec(r.Goods), wts: r.Type == "wts", w: w * r.Weight}
		for _, o := range rs {
			ob.opts = append(ob.opts, vec(o.Price))
		}
		obs = append(obs, ob)
		for _, p := range r.Goods {
			touched[p.Code] = true
		}
		for _, o := range rs {
			for _, p := range o.Price {
				touched[p.Code] = true
			}
		}
		for c := range touched {
			nObs[c] += w
		}
	}
	if len(obs) == 0 {
		return map[string]float64{}, nObs
	}

	x := make([]float64, len(codes))
	x, resid := fit(obs, x, codes, idx, false)
	var kept []observation
	for i, o := range obs {
		if math.Abs(resid[i]) < math.Log(3) {
			kept = append(kept, o)
		}
	}
	x, _ = fit(kept, x, codes, idx, true)

	values := map[string]float64{}
	for i, c := range codes {
		if nObs[c] >= MinObs {
			values[c] = math.Exp(x[i])
		}
	}
	for c, cr := range catalog.Craft {
		if v, ok := values[cr.Base]; ok && nObs[c]+nObs[cr.Base] >= MinObs {
			values[c] = v / cr.Per
		}
	}
	return values, nObs
}

func fit(obs []observation, x0 []float64, codes []string, idx map[string]int, sizeWeight bool) ([]float64, []float64) {
	const iters, lr = 3000, 0.05
	n := len(codes)
	x := append([]float64(nil), x0...)
	v := make([]float64, n)
	exp := func() {
		for i := range x {
			v[i] = math.Exp(x[i])
		}
	}
	exp()
	W := make([]float64, len(obs))
	for i, o := range obs {
		W[i] = o.w
		if sizeWeight {
			// Tiny trades are priced in whole coins (1 key for 1 sphere): they bound the value more than measure it.
			W[i] *= math.Min(1, math.Max(0.15, math.Sqrt(o.g.dot(v)/0.5)))
		}
	}
	load := make([]float64, n)
	for i, o := range obs {
		for k, j := range o.g.idx {
			if o.g.val[k] > 0 {
				load[j] += W[i]
			}
		}
		share := W[i] / float64(len(o.opts))
		for _, p := range o.opts {
			for k, j := range p.idx {
				if p.val[k] > 0 {
					load[j] += share
				}
			}
		}
	}
	for j := range load {
		load[j] = math.Max(1, load[j])
	}

	// rune ladder and cube priors, resolved once
	type pair struct{ a, b int }
	var ladder []pair
	for i := 1; i <= 32; i++ {
		a, okA := idx[fmt.Sprintf("r%02d", i)]
		b, okB := idx[fmt.Sprintf("r%02d", i+1)]
		if okA && okB {
			ladder = append(ladder, pair{a, b})
		}
	}
	type recipe struct {
		out   int
		parts []int
		qty   []float64
	}
	var cube []recipe
	for _, r := range catalog.Cube {
		out, ok := idx[r.Out]
		if !ok {
			continue
		}
		rc := recipe{out: out}
		for _, p := range r.Give {
			j, ok := idx[p.Code]
			if !ok {
				rc.parts = nil
				break
			}
			rc.parts = append(rc.parts, j)
			rc.qty = append(rc.qty, p.Qty)
		}
		if len(rc.parts) == len(r.Give) {
			cube = append(cube, rc)
		}
	}
	unit := idx[catalog.Unit]
	grad := make([]float64, n)
	pv := make([]float64, 0, 8)
	pick := make([]float64, 0, 8)
	for it := 0; it < iters; it++ {
		exp()
		for j := range grad {
			grad[j] = 0
		}
		for i, o := range obs {
			gv := o.g.dot(v)
			sign := 1.0
			if !o.wts {
				sign = -1
			}
			pv, pick = pv[:0], pick[:0]
			best := math.Inf(1)
			for _, p := range o.opts {
				x := p.dot(v)
				pv = append(pv, x)
				if sign*x < best {
					best = sign * x
				}
			}
			cnt := 0.0
			for _, x := range pv {
				if sign*x == best {
					pick = append(pick, 1)
					cnt++
				} else {
					pick = append(pick, 0)
				}
			}
			psel := 0.0
			for k := range pick {
				pick[k] /= cnt
				psel += pick[k] * pv[k]
			}
			e := math.Max(-1, math.Min(1, math.Log(gv)-math.Log(psel)))
			gw := W[i] * e / gv
			for k, j := range o.g.idx {
				grad[j] += gw * o.g.val[k]
			}
			pw := W[i] * e / psel
			for k, p := range o.opts {
				if pick[k] == 0 {
					continue
				}
				for m, j := range p.idx {
					grad[j] -= pick[k] * pw * p.val[m]
				}
			}
		}
		for j := range grad {
			grad[j] *= v[j]
		}
		// weak prior: a higher rune is never cheaper than the one below it
		for _, p := range ladder {
			if x[p.b] < x[p.a] {
				d := x[p.b] - x[p.a]
				grad[p.b] += 0.2 * d
				grad[p.a] -= 0.2 * d
			}
		}
		// weak prior: a cube product is never worth more than its ingredients (anyone can craft it)
		for _, r := range cube {
			sum := 0.0
			parts := make([]float64, len(r.parts))
			for k, j := range r.parts {
				parts[k] = r.qty[k] * math.Exp(x[j])
				sum += parts[k]
			}
			d := x[r.out] - math.Log(sum)
			if d > 0 {
				grad[r.out] += 0.2 * d
				for k, j := range r.parts {
					grad[j] -= 0.2 * d * parts[k] / sum
				}
			}
		}
		for j := range x {
			x[j] -= lr * grad[j] / load[j]
		}
		u := x[unit]
		for j := range x {
			x[j] -= u
		}
	}
	exp()
	resid := make([]float64, len(obs))
	for i, o := range obs {
		sign := 1.0
		if !o.wts {
			sign = -1
		}
		best := math.Inf(1)
		for _, p := range o.opts {
			if s := sign * p.dot(v); s < best {
				best = s
			}
		}
		resid[i] = math.Log(o.g.dot(v)) - math.Log(best*sign)
	}
	return x, resid
}

// Worth is the fair value of a bundle; false if any part has no price.
func Worth(b bundle.Bundle, values map[string]float64) (float64, bool) {
	t := 0.0
	for _, p := range b {
		v, ok := values[p.Code]
		if !ok {
			return 0, false
		}
		t += p.Qty * v
	}
	return t, true
}
