"""Read models for the screens: everything the page shows is computed here, the page only formats it."""
from dataclasses import dataclass

from loot_planner.application.market_service import MarketService
from loot_planner.application.ports import Session
from loot_planner.domain.catalog import CUBE, GROUPS
from loot_planner.domain.market.realm_market import RealmMarket, lot_url
from loot_planner.domain.routing.solver import RouteRequest, solve_route

QUICK_TARGETS = ["r32", "r25", "r26", "r27", "xa4", "pk2"]
BASES = ["r24", "pk2", "r20", "r22", "r26", "r29", "r30", "xa4"]       # units the stash and rates can be shown in
EQUIVALENTS = ["pk2", "r20", "r22", "r23", "r24", "r25", "r26", "r27", "r29", "xa4"]
CROSS = ["pk1", "pk2", "pk3", "xa4", "r20", "r22", "r23", "r24", "r25", "r26", "r27", "r28", "r29", "r30", "r31", "r32", "r33"]
KEYS = ("pk1", "pk2", "pk3")
MAP_MIN_OBS = 3
OFFERS_SHOWN = 80


@dataclass
class RouteParams:
    target: str
    qty: int = 1
    stash_mode: str = "all"           # all | currency | gems | none
    unlimited_keys: tuple = ()        # keys that never run out
    use_cube: bool = True
    effort: float = 0.03


def _item_refs(m: RealmMarket, *bags) -> dict:
    """Names of the items a response mentions (codes i:… and own:i:…), so the page can label them."""
    refs = {}
    for bag in bags:
        for c in bag:
            k = c[4:] if c.startswith("own:") else c
            if k.startswith("i:") and k in m.items:
                refs[k] = {"name": m.items[k].name, "quality": m.items[k].quality}
    return refs


def _codes(*dicts_lists) -> list:
    return [c for dl in dicts_lists for d in dl for c in d]


class MarketQueries:
    def __init__(self, market: MarketService):
        self.market = market

    def meta(self, session: Session | None) -> dict:
        st = self.market.state
        stash_realms = list(session.stash.realms) if session and session.stash else []
        realms = list(self.market.realm_keys)
        return {
            "ready": st is not None,
            "fetchedAt": st.fetched_at if st else None,
            "realms": [{"key": k, "counts": st.realms[k].counts if st else None, "stash": k in stash_realms} for k in realms],
            "defaultRealm": next((r for r in stash_realms if r in realms), realms[0]),
            "names": {c: {"name": cur.name, "display": cur.display, "group": cur.group}
                      for c, cur in st.catalog.by_code.items()} if st else {},
            "groups": GROUPS, "quick": QUICK_TARGETS, "bases": BASES, "keys": list(KEYS),
            "job": self.market.job,
        }

    def targets(self, realm: str) -> dict:
        m = self.market.realm(realm)
        currencies = [{"code": c, "display": m.catalog[c].display} for c, row in m.table.items()
                      if row["ist"] is not None or row["supply"] or row["demand"]]
        items = sorted(((k, it) for k, it in m.items.items() if it.live), key=lambda kv: -kv[1].live)
        return {"currencies": currencies, "items": [{"code": k, "name": it.name, "quality": it.quality} for k, it in items]}

    def target(self, realm: str, code: str, session: Session | None) -> dict:
        m = self.market.realm(realm)
        fair, offers = m.fair(code), m.offers_for(code)
        best = next((o for o in offers if o["pure"]), offers[0] if offers else None)
        rs = session.stash.realms.get(realm) if session and session.stash else None
        out = {"code": code, "known": m.known(code), "isItem": code.startswith("i:"), "fair": fair,
               "best": {k: best[k] for k in ("per", "discount", "url")} if best else None,
               "offers": len(offers), "have": rs.have(code) if rs else 0}
        if out["isItem"]:
            it = m.items.get(code)
            out["item"] = {"name": it.name, "quality": it.quality, "live": it.live, "sold": it.sold} if it else None
        else:
            row = m.table.get(code) or {}
            out["acceptedBy"] = row.get("acceptedBy", 0)
            out["recent"] = row.get("recent", [])[:5]
            out["equivalents"] = [{"code": c, "qty": fair / m.values[c]} for c in EQUIVALENTS
                                  if fair and c != code and m.values.get(c)]
            cube = m.cube_cost(code)
            if cube:
                cube["discount"] = 1 - cube["ist"] / fair if fair else None
            out["cube"] = cube
        return out

    def offers(self, realm: str, code: str) -> dict:
        m = self.market.realm(realm)
        offers = m.offers_for(code)[:OFFERS_SHOWN]
        return {"offers": offers, "items": _item_refs(m, _codes([o["give"] for o in offers], [o["get"] for o in offers]), [code])}

    def route(self, realm: str, p: RouteParams, session: Session | None) -> dict:
        m = self.market.realm(realm)
        rs = session.stash.realms.get(realm) if session and session.stash else None
        pool, pool_cost = rs.pool(m, p.stash_mode) if rs else ({}, {})
        unlimited = {c: m.values.get(c) or 0.12 for c in p.unlimited_keys if c in KEYS}
        res = solve_route(m.actions, CUBE, m.values,
                          RouteRequest(p.target, max(1, min(20, p.qty)), pool, pool_cost, unlimited, p.use_cube,
                                       max(0.0, p.effort)))
        steps = list(res.get("steps") or []) + list((res.get("paper") or {}).get("steps") or [])
        for s in steps:
            s["url"] = lot_url(s["type"], s["id"])
        fair = m.fair(p.target)
        leftover = res.get("leftover") or {}
        return {
            "route": res,
            "fair": fair,
            "change": sum(q * m.values.get(c, 0) for c, q in leftover.items()),
            "keysFair": [{"code": c, "qty": p.qty * fair / v} for c, v in unlimited.items()] if fair else [],
            "stash": {"loaded": bool(session and session.stash), "owner": session.stash.owner if session and session.stash else None,
                      "realm": rs is not None},
            "items": _item_refs(m, [p.target], _codes([s["give"] for s in steps], [s["get"] for s in steps]),
                                *(res.get(k) or {} for k in ("poolUsed", "leftover")),
                                (res.get("paper") or {}).get("missing") or {},
                                [b["item"] for b in res.get("bottlenecks", []) if b["kind"] == "stash"]),
        }

    def rates(self, realm: str) -> dict:
        m = self.market.realm(realm)
        rows = []
        for c, row in m.table.items():
            q = m.quotes.get(c, {})
            ask, bid, fair = q.get("ask"), q.get("bid"), row["ist"]
            rows.append({
                "code": c, "name": row["name"], "group": row["group"], "ist": fair,
                "ask": ask.per if ask else None, "askUrl": lot_url(ask.action.type, ask.action.id) if ask else None,
                "askVsFair": ask.per / fair - 1 if ask and fair else None,
                "bid": bid.per if bid else None, "bidUrl": lot_url(bid.action.type, bid.action.id) if bid else None,
                "bidVsFair": bid.per / fair - 1 if bid and fair else None,
                "spread": ask.per / bid.per - 1 if ask and bid else None,
                "supply": row["supply"], "demand": row["demand"], "obs": row["obs"], "recent": row["recent"][:3],
            })
        codes = [c for c in CROSS if m.values.get(c)]
        return {"rows": rows, "cross": {"codes": codes, "matrix": [[m.values[r] / m.values[c] for c in codes] for r in codes]}}

    def map(self, realm: str) -> dict:
        m = self.market.realm(realm)
        nodes = [{k: row[k] for k in ("code", "group", "ist", "supply", "demand", "acceptedBy")}
                 for row in m.table.values()
                 if row["ist"] is not None and (row["obs"] >= MAP_MIN_OBS or row["supply"] or row["demand"])]
        on_map = {n["code"] for n in nodes}
        return {"nodes": nodes, "edges": [e for e in m.edges if e["from"] in on_map and e["to"] in on_map]}

    def deals(self, realm: str) -> dict:
        return {"deals": self.market.realm(realm).deals}

    def stash(self, realm: str, session: Session | None) -> dict:
        if not session or not session.stash:
            return {"loaded": False}
        s = session.stash
        out = {"loaded": True, "owner": s.owner, "source": s.source, "fetchedAt": s.fetched_at,
               "remembered": session.remembered, "realms": list(s.realms)}
        rs = s.realms.get(realm)
        if rs:
            out["appraisal"] = rs.appraise(self.market.realm(realm))
        return out
