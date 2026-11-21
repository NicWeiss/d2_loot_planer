package routing

import (
	"sort"

	"loot-planner/internal/domain/bundle"
	"loot-planner/internal/domain/catalog"
	"loot-planner/internal/domain/market"
)

// A farm strategy answers "what should I gather by playing to reach the target": the route is solved with
// one farmable resource (or all of them, the mix) treated as unlimited at its fair value, on top of the stash
// and the live listings.

type Farm struct {
	Code string  `json:"code"`
	Need float64 `json:"need"` // to gather beyond what the stash already holds
	Have float64 `json:"have"`
}

type Strategy struct {
	Mix     bool    `json:"mix"`              // all farmables at once, the solver picks the mix
	Farm    []Farm  `json:"farm"`             // what to gather, most valuable first
	FarmIst float64 `json:"farmIst"`          // fair value of what is to be gathered
	OK      bool    `json:"ok"`               // a real chain exists, not only on paper
	Trades  int     `json:"trades"`           // listings to answer and crafts to make
	Route   *Result `json:"route"`            // the full route, for showing the chain
	Reason  *string `json:"reason,omitempty"` // why there is no real chain
}

// Strategies solves the route once per farmable resource and once with all of them, best first:
// routes that really work, then by the fair value of what has to be farmed.
func Strategies(actions []market.Action, cube []catalog.Recipe, values map[string]float64, base Request,
	farmables []string, have func(string) float64) []Strategy {
	run := func(codes []string, mix bool) *Strategy {
		req := base
		req.Unlimited = nil
		for _, c := range codes {
			if c != base.Target && values[c] > 0 {
				req.Unlimited = append(req.Unlimited, bundle.Part{Code: c, Qty: values[c]})
			}
		}
		if len(req.Unlimited) == 0 {
			return nil
		}
		res := Solve(actions, cube, values, req)
		if res.Reason != nil && *res.Reason == "unreachable" {
			return nil
		}
		spent := res.UnlimitedSpent
		if !res.OK && res.Paper != nil {
			spent = res.Paper.UnlimitedSpent // no real chain: show what the cheapest route would need
		}
		s := &Strategy{Mix: mix, OK: res.OK, Trades: len(res.Steps), Route: &res, Reason: res.Reason}
		if !res.OK && res.Paper != nil {
			s.Trades = len(res.Paper.Steps)
		}
		for _, p := range spent {
			h := have(p.Code)
			if need := p.Qty - h; need > 0 {
				s.Farm = append(s.Farm, Farm{p.Code, need, h})
				s.FarmIst += need * values[p.Code]
			}
		}
		if len(s.Farm) == 0 {
			return nil // the stash already covers it: that is the plain route, not a farming plan
		}
		sort.SliceStable(s.Farm, func(i, j int) bool {
			return s.Farm[i].Need*values[s.Farm[i].Code] > s.Farm[j].Need*values[s.Farm[j].Code]
		})
		return s
	}
	var out []Strategy
	for _, c := range farmables {
		if s := run([]string{c}, false); s != nil {
			out = append(out, *s)
		}
	}
	if s := run(farmables, true); s != nil && len(s.Farm) > 1 {
		out = append(out, *s) // a mix of one resource repeats a single-resource row
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].OK != out[j].OK {
			return out[i].OK
		}
		return out[i].FarmIst < out[j].FarmIst
	})
	return out
}
