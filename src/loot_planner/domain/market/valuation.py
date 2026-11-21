"""Fair values: one price per currency that best explains every trade of the realm."""
import math
from collections import defaultdict

import numpy as np

from loot_planner.domain.catalog import CRAFT, CUBE, UNIT
from loot_planner.domain.market.listing import Trade

MIN_OBS = 5  # items seen in fewer (weighted) trades get no fair price


def to_base(d: dict) -> dict:
    """Rewrite crafting ingredients in units of what they craft into (12 ingots = 1 sphere, 12 spheres = 1 spark)."""
    out = defaultdict(float)
    for c, q in d.items():
        base, per = CRAFT.get(c, (c, 1))
        out[base] += q / per
    return dict(out)


def fit_values(rows: list[Trade]) -> tuple[dict[str, float], dict[str, float]]:
    """Robust log-least-squares fit of values so that value(goods) ~= value(chosen payment) across listings.

    A listing with several payment options is one observation: a WTS buyer pays with the cheapest option,
    a WTB seller takes the richest, so only that option is matched (re-chosen every iteration).
    Observations that still disagree with the fit more than 3x after a first pass are dropped as joke prices.
    Returns (values in Ist, weighted observation count per code).
    """
    codes = sorted({c for r in rows for d in (r.goods, r.price) for c in d} | {UNIT})
    codes = [c for c in codes if c not in CRAFT]
    idx = {c: i for i, c in enumerate(codes)}
    by_ad = defaultdict(list)
    for r in rows:
        by_ad[r.id].append(r)
    obs = []  # (goods vector, option vectors, is_wts, weight)
    n_obs = defaultdict(float)
    for rs in by_ad.values():
        r = rs[0]
        w = {"completed": 1.0, "active": 0.35, "unlisted": 0.15}.get(r.status, 0)
        if r.status == "active" and r.type == "wtb":
            w = 0.5  # standing bids are informative too
        if not w:
            continue
        if any(c in CRAFT for o in rs for c in list(o.goods) + list(o.price)):
            w *= 0.2  # ingots/spheres trade as small change; the chain is priced by its top item
        g = np.zeros(len(codes))
        for c, q in to_base(r.goods).items():
            g[idx[c]] += q
        opts = []
        for o in rs:
            p = np.zeros(len(codes))
            for c, q in to_base(o.price).items():
                p[idx[c]] += q
            opts.append(p)
        obs.append((g, np.array(opts), r.type == "wts", w))
        for c in set(r.goods) | {c for o in rs for c in o.price}:
            n_obs[c] += w

    def fit(obs, x, iters=3000, lr=0.05, size_weight=False):
        G = np.array([o[0] for o in obs])
        W = np.array([o[3] for o in obs])
        if size_weight:
            # Tiny trades are priced in whole coins (1 key for 1 sphere): they bound the value more than measure it.
            W = W * np.clip(np.sqrt(G @ np.exp(x) / 0.5), 0.15, 1.0)
        counts = np.array([len(o[1]) for o in obs])
        P = np.vstack([o[1] for o in obs])
        owner = np.repeat(np.arange(len(obs)), counts)
        starts = np.concatenate([[0], np.cumsum(counts)[:-1]])
        sign = np.repeat(np.array([1.0 if o[2] else -1.0 for o in obs]), counts)
        load = np.maximum(1.0, (G > 0).T @ W + (P > 0).T @ (W[owner] / counts[owner]))
        for _ in range(iters):
            v = np.exp(x)
            gv, pv = G @ v, P @ v
            best = np.minimum.reduceat(sign * pv, starts)
            pick = (sign * pv == best[owner]).astype(float)
            pick /= np.add.reduceat(pick, starts)[owner]
            psel = np.add.reduceat(pick * pv, starts)
            e = np.clip(np.log(gv) - np.log(psel), -1.0, 1.0)
            grad = v * (G.T @ (W * e / gv) - P.T @ (pick * (W * e / psel)[owner]))
            # weak prior: a higher rune is never cheaper than the one below it
            for i in range(1, 33):
                a, b = f"r{i:02d}", f"r{i + 1:02d}"
                if a in idx and b in idx and x[idx[b]] < x[idx[a]]:
                    d = x[idx[b]] - x[idx[a]]
                    grad[idx[b]] += 0.2 * d
                    grad[idx[a]] -= 0.2 * d
            # weak prior: a cube product is never worth more than its ingredients (anyone can craft it)
            for out, recipe in CUBE.items():
                if out not in idx or not all(c in idx for c in recipe):
                    continue
                parts = np.array([q * math.exp(x[idx[c]]) for c, q in recipe.items()])
                d = x[idx[out]] - math.log(parts.sum())
                if d > 0:
                    grad[idx[out]] += 0.2 * d
                    for (c, _), share in zip(recipe.items(), parts / parts.sum()):
                        grad[idx[c]] -= 0.2 * d * share
            x = x - lr * grad / load
            x -= x[idx[UNIT]]
        v = np.exp(x)
        resid = np.log(G @ v) - np.log(np.minimum.reduceat(sign * (P @ v), starts) * sign[starts])
        return x, resid

    if not obs:
        return {}, dict(n_obs)
    x, resid = fit(obs, np.zeros(len(codes)))
    kept = [o for o, e in zip(obs, resid) if abs(e) < math.log(3)]
    x, _ = fit(kept, x, size_weight=True)
    values = {c: float(math.exp(x[idx[c]])) for c in codes if n_obs[c] >= MIN_OBS}
    for c, (base, per) in CRAFT.items():
        if base in values and n_obs.get(c, 0) + n_obs.get(base, 0) >= MIN_OBS:
            values[c] = values[base] / per
    return values, dict(n_obs)


def worth(d: dict, values: dict) -> float | None:
    """Fair value of a bundle, or None if any part has no price."""
    if any(c not in values for c in d):
        return None
    return sum(q * values[c] for c, q in d.items())
