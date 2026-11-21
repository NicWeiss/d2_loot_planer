"""Route solver: cheapest chain of live listings and cube crafts that turns your resources into `qty` of `target`.

Resources: `pool` (finite: what is in the stash) and `unlimited` (e.g. keys when you can always get more),
each with a unit cost in Ist. Every listing and craft is a recipe give -> get.

1. Unit cost of every item (Knuth's generalised Dijkstra over recipes: an item's cost is fixed once,
   cheapest first, so arbitrage loops cannot drive costs to zero). A listing that yields several items
   splits its price across them in proportion to fair value. Each trade adds a little effort cost.
2. Paper chain: that cheapest tree expanded for the wanted amount, ignoring how often a listing can be used,
   with the number of matching listings next to each step.
3. Real chain: buy the target from the cheapest recipe that still has uses left, recursively buying its
   inputs; whole lots only, leftovers go to a surplus pool that later steps draw from first.
"""
import heapq
import itertools
import json
import math
from dataclasses import dataclass, field

from loot_planner.domain.market.listing import Action

INF = math.inf
MAX_DEPTH = 14
MAX_AMOUNT = 500
BUDGET = 20000  # guards against pathological branching


@dataclass
class RouteRequest:
    target: str
    qty: int = 1
    pool: dict = field(default_factory=dict)       # finite resources (stash)
    pool_cost: dict = field(default_factory=dict)  # Ist per piece for pool items without a fair value
    unlimited: dict = field(default_factory=dict)  # resource -> Ist per piece, never runs out
    use_cube: bool = True
    effort: float = 0.03  # Ist charged per trade (a craft costs a fifth), so short chains beat marginally cheaper long ones


def _mul(d: dict, k: float) -> dict:
    return {c: q * k for c, q in d.items()}


def _add(bag: dict, c: str, q: float):
    bag[c] = bag.get(c, 0) + q


def _take(bag: dict, item: str, n: float) -> float:
    t = min(bag.get(item, 0), n)
    if t:
        bag[item] -= t
    return t


@dataclass
class _Recipe:
    id: str
    type: str
    give: dict
    get: dict
    uses: float
    check: bool
    cube: bool


class _State:
    __slots__ = ("surplus", "pool", "spent", "used", "cap", "steps")

    def __init__(self, surplus, pool, spent, used, cap, steps):
        self.surplus, self.pool, self.spent, self.used, self.cap, self.steps = surplus, pool, spent, used, cap, steps

    def snapshot(self):
        return _State(dict(self.surplus), dict(self.pool), dict(self.spent), dict(self.used), dict(self.cap),
                      len(self.steps))


def solve_route(actions: list[Action], cube: dict, values: dict, req: RouteRequest) -> dict:
    target, qty, unlimited, effort_ist = req.target, req.qty, req.unlimited, req.effort
    pool_cost = req.pool_cost
    # the target itself is never spent: anything already in the stash is reported separately
    pool = {c: q for c, q in req.pool.items() if q > 0 and c not in unlimited and c != target}
    # never sell the target itself or buy more of an unlimited resource
    recipes = [_Recipe(a.id, a.type, a.give, a.get, a.uses, a.check, False) for a in actions
               if not any(c in unlimited for c in a.get) and target not in a.give]
    if req.use_cube:
        recipes += [_Recipe("cube:" + out, "cube", give, {out: 1}, INF, False, True) for out, give in cube.items()]

    def share(r: _Recipe, item: str) -> float:
        if len(r.get) == 1:
            return 1
        tot = sum(q * (values.get(c) or 0) for c, q in r.get.items())
        return r.get[item] * (values.get(item) or 0) / tot if tot > 0 else 1 / len(r.get)

    def effort(r: _Recipe) -> float:
        return effort_ist / 5 if r.cube else effort_ist

    # 1. unit costs
    cost, via, rank = {}, {}, {}
    order = itertools.count()

    def fix(c, v, r=None):
        cost[c] = v
        if r:
            via[c] = r
        rank[c] = next(order)

    seeds = list(unlimited.items()) + [(c, pool_cost.get(c, values.get(c, 0.001))) for c in pool]
    for c, v in sorted(seeds, key=lambda s: s[1]):
        if c not in cost:
            fix(c, v)

    def input_cost(r: _Recipe) -> float:
        return effort(r) + sum(q * cost.get(c, INF) for c, q in r.give.items())

    used_by: dict[str, list[int]] = {}
    missing = [sum(c not in cost for c in r.give) for r in recipes]
    for i, r in enumerate(recipes):
        for c in r.give:
            used_by.setdefault(c, []).append(i)
    heap, seq = [], itertools.count()

    def offer(i):
        r = recipes[i]
        total = input_cost(r)
        for x, q in r.get.items():
            if x not in cost:
                heapq.heappush(heap, (total * share(r, x) / q, next(seq), x, i))

    for i in range(len(recipes)):
        if not missing[i]:
            offer(i)
    while heap:
        v, _, x, i = heapq.heappop(heap)
        if x in cost:
            continue
        fix(x, v, recipes[i])
        for j in used_by.get(x, ()):
            missing[j] -= 1
            if missing[j] == 0:
                offer(j)
    if target not in cost:
        return {"ok": False, "reason": "unreachable"}
    if target not in via:
        return {"ok": True, "reason": "have", "steps": [], "paper": None}

    def sig(r):
        return json.dumps([r.type, r.give, r.get])

    avail: dict[str, float] = {}
    for r in recipes:
        if not r.cube:
            avail[sig(r)] = avail.get(sig(r), 0) + r.uses

    def step(r: _Recipe, times: int, **extra) -> dict:
        return {"id": r.id, "type": r.type, "check": r.check, "give": _mul(r.give, times),
                "get": _mul(r.get, times), "times": times, **extra}

    # 2. paper chain
    def paper() -> dict:
        demand, surplus, spent, used, lack, rows = {target: qty}, {}, {}, {}, {}, []
        left = dict(pool)
        while True:
            open_ = [c for c, q in demand.items() if q > 0]
            if not open_:
                break
            item = max(open_, key=lambda c: rank.get(c, -1))
            n = demand.pop(item)
            n -= _take(surplus, item, n)
            t2 = _take(left, item, n)
            if t2:
                _add(used, item, t2)
            n -= t2
            if n <= 0:
                continue
            if item in unlimited:
                _add(spent, item, n)
                continue
            r = via.get(item)
            if not r:
                _add(lack, item, n)  # stash ran out and nothing else makes it
                continue
            times = math.ceil(n / r.get[item])
            for c, q in r.give.items():
                _add(demand, c, q * times)
            for c, q in r.get.items():
                _add(surplus, c, q * times)
            surplus[item] -= n
            rows.append(step(r, times, available=None if r.cube else avail[sig(r)], rank=rank[item]))
        rows.sort(key=lambda s: s["rank"])
        return {"steps": rows, "unlimitedSpent": spent, "poolUsed": used, "missing": lack,
                "leftover": {c: q for c, q in surplus.items() if q > 0}}

    paper_chain = paper()

    # 3. real chain
    st = _State({}, dict(pool), {}, {}, {}, [])
    budget = [BUDGET]
    failed_at: dict[str, float] = {}  # item + route -> smallest amount that could not be bought (capacities only shrink)
    by_item: dict[str, list[_Recipe]] = {}
    for r in recipes:
        for x in r.get:
            by_item.setdefault(x, []).append(r)

    def cap_left(r: _Recipe) -> float:
        return INF if r.cube else st.cap.get(r.id, r.uses)

    def unit(c: str) -> float:
        return unlimited[c] if c in unlimited else pool_cost.get(c, values.get(c, 0))

    def state_cost() -> float:
        """What the plan so far really costs: resources spent, trades made, minus half the change left over."""
        spent_ist = sum(q * unit(c) for d in (st.spent, st.used) for c, q in d.items())
        change = sum(max(q, 0) * (values.get(c) or 0) for c, q in st.surplus.items())
        return spent_ist + sum(effort_ist / 5 if s["type"] == "cube" else effort_ist for s in st.steps) - change / 2

    def restore(s: _State):
        nonlocal st
        st = _State(s.surplus, s.pool, s.spent, s.used, s.cap, st.steps[:s.steps])

    def acquire(item: str, n: float, path: tuple) -> bool:
        nonlocal st
        budget[0] -= 1
        if budget[0] < 0:
            return False
        n -= _take(st.surplus, item, n)
        from_pool = _take(st.pool, item, n)
        if from_pool:
            _add(st.used, item, from_pool)
        n -= from_pool
        if n <= 0:
            return True
        if item in unlimited:
            _add(st.spent, item, n)
            return True
        fkey = item + "|" + ",".join(sorted(path))
        if len(path) > MAX_DEPTH or n > MAX_AMOUNT or n >= failed_at.get(fkey, INF):
            return False
        # the recipe that set the item's cost first, then recipes whose inputs were priced earlier (no hidden loops)
        own_rank = rank.get(item, INF)

        def tier(r):
            if r is via.get(item):
                return 0
            return 1 if all(rank.get(c, INF) < own_rank for c in r.give) else 2

        cands = sorted(({"r": r, "unit": input_cost(r) * share(r, item) / r.get[item], "tier": tier(r)}
                        for r in by_item.get(item, ())
                        if cap_left(r) > 0 and not any(c in path or c == item for c in r.give)
                        and math.isfinite(input_cost(r))),
                       key=lambda c: (c["tier"], c["unit"]))
        kept, cyclic = [], 0
        for c in cands:
            if c["tier"] < 2 or cyclic < 2:
                kept.append(c)
            cyclic += c["tier"] == 2
        cands = kept[:8]

        def try_recipe(r: _Recipe):
            times = min(math.ceil(n / r.get[item]), cap_left(r))
            nxt = path + (item,)
            if not all(acquire(c, q * times, nxt) for c, q in r.give.items()):
                return None
            if not r.cube:
                st.cap[r.id] = cap_left(r) - times
            st.steps.append(step(r, times))
            for c, q in r.get.items():
                _add(st.surplus, c, q * times)
            return n - _take(st.surplus, item, n)

        if len(path) <= 1:
            # near the top, try every candidate in full and keep the one that really costs least
            for _ in range(20):
                if n <= 0:
                    break
                best = None
                for c in cands:
                    r = c["r"]
                    if cap_left(r) <= 0:
                        continue
                    snap = st.snapshot()
                    left = try_recipe(r)
                    if left is not None and left < n:
                        score = (left, state_cost())
                        if best is None or score < best[0]:
                            s = st.snapshot()
                            best = (score, _State(s.surplus, s.pool, s.spent, s.used, s.cap, list(st.steps)))
                    restore(snap)
                if best is None:
                    break
                st = best[1]
                n = best[0][0]
        else:
            for c in cands:
                if n <= 0:
                    break
                snap = st.snapshot()
                left = try_recipe(c["r"])
                if left is None:
                    restore(snap)
                    continue
                n = left
        if n > 0:
            failed_at[fkey] = min(failed_at.get(fkey, INF), n)
        return n <= 0

    ok = acquire(target, qty, ())
    # bottlenecks, read off the cheapest (paper) route: listings needed more often than they exist, stash that runs out
    bottlenecks = [] if ok else (
        [{"kind": "lots", "step": s} for s in paper_chain["steps"]
         if s["available"] is not None and s["times"] > s["available"]]
        + [{"kind": "stash", "item": c, "missing": n} for c, n in paper_chain["missing"].items()])

    def ist(d):
        return sum(q * unit(c) for c, q in d.items())

    return {
        "ok": ok, "reason": None if ok else "budget" if budget[0] < 0 else "capacity", "bottlenecks": bottlenecks,
        "unitCost": cost[target],
        "paper": {**paper_chain, "costIst": ist(paper_chain["unlimitedSpent"]) + ist(paper_chain["poolUsed"])},
        "steps": st.steps, "unlimitedSpent": st.spent, "poolUsed": st.used, "costIst": ist(st.spent) + ist(st.used),
        "leftover": {c: q for c, q in st.surplus.items() if q > 0},
    }
