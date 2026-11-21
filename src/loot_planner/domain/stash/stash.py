"""A player's stash: parsed from the site's stash export and priced against a realm market."""
from collections import Counter
from dataclasses import dataclass, field

from loot_planner.domain.catalog import CORE, Catalog, item_key
from loot_planner.domain.market.realm_market import RealmMarket, lot_url

PRICED = ("unique", "set", "runeword")  # qualities whose name alone says roughly what they are worth


class NotAStashExport(ValueError):
    pass


@dataclass
class RealmStash:
    """Stackable currency by code and items by (name, quality) -> count, for one realm."""
    realm: str
    currency: dict[str, int] = field(default_factory=dict)
    items: list[tuple[str, str, int]] = field(default_factory=list)

    @classmethod
    def from_export(cls, data: dict, catalog: Catalog) -> "RealmStash":
        """One realm's /me/stash/sources answer (or an export file of it)."""
        if not isinstance(data, dict) or "set" not in data or not (data.get("tabs") or data.get("materials")):
            raise NotAStashExport("это не выгрузка тайника")
        inv, items = Counter(), Counter()
        mats = data.get("materials") or {}
        for r in mats.get("records", []):
            if r.get("quantity") and r["baseCode"] in catalog:
                inv[r["baseCode"]] += r["quantity"]
        holders = [t for t in data.get("tabs", []) if t["index"] != mats.get("tabIndex")] + data.get("characters", [])
        for h in holders:
            for it in h.get("items", []):
                if it["baseCode"] in catalog:
                    qty = (it["item"].get("lot") or {}).get("quantity") or 0
                    if qty:
                        inv[it["baseCode"]] += qty
                else:
                    items[(it["item"]["displayName"], it["item"]["quality"])] += 1
        s = data["set"]
        realm = f"{'hardcore' if s.get('hardcore') else 'softcore'} {'ladder' if s.get('ladder') else 'non-ladder'}"
        return cls(realm, dict(inv), [(n, q, k) for (n, q), k in items.items()])

    def to_dict(self) -> dict:
        return {"realm": self.realm, "currency": self.currency, "items": [list(i) for i in self.items]}

    @classmethod
    def from_dict(cls, d: dict) -> "RealmStash":
        return cls(d["realm"], d["currency"], [tuple(i) for i in d["items"]])

    # ---- what the route solver may spend
    def pool(self, market: RealmMarket, mode: str) -> tuple[dict, dict]:
        """(pool, pool_cost) for the solver. mode: all | currency | gems | none."""
        pool, pool_cost = {}, {}
        if mode == "none":
            return pool, pool_cost
        for c, q in self.currency.items():
            gem = c in market.catalog and market.catalog[c].group == "gems"
            if mode in ("all", "currency") or (mode == "gems" and gem):
                pool[c] = q
        if mode == "all":
            for name, quality, have in self.items:
                if quality in PRICED:
                    k = item_key(name, quality)
                    it = market.items.get(k)
                    pool["own:" + k] = have
                    pool_cost["own:" + k] = it.median if it and it.median is not None else 0.05
        return pool, pool_cost

    def have(self, code: str) -> int:
        if code.startswith("i:"):
            return sum(n for name, q, n in self.items if item_key(name, q) == code)
        return self.currency.get(code, 0)

    # ---- valuation
    def appraise(self, market: RealmMarket) -> dict:
        """Currency at fair value with the best live bid, priced items by median sale, the rest listed."""
        v, quotes = market.values, market.quotes
        currency = []
        for c, q in self.currency.items():
            fair = v.get(c)
            bid = quotes.get(c, {}).get("bid")
            currency.append({"code": c, "qty": q, "fair": fair, "ist": fair * q if fair is not None else None,
                             "bid": bid.per if bid else None, "bidUrl": lot_url(bid.action.type, bid.action.id) if bid else None,
                             "bidVsFair": bid.per / fair - 1 if bid and fair else None})
        currency.sort(key=lambda r: -(r["ist"] or 0))
        total_cur = sum(r["ist"] or 0 for r in currency)
        for r in currency:
            r["share"] = r["ist"] / total_cur if r["ist"] and total_cur else None
        priced, other = [], []
        for name, quality, have in self.items:
            if quality in PRICED:
                k = item_key(name, quality)
                it = market.items.get(k)
                priced.append({"key": k, "name": name, "quality": quality, "have": have,
                               "median": it.median if it else None, "sold": it.sold if it else 0,
                               "live": it.live if it else 0, "cheapestAsk": it.ask if it else None,
                               "ist": it.median * have if it and it.median is not None else None})
            else:
                other.append({"name": name, "quality": quality, "have": have})
        priced.sort(key=lambda r: -(r["ist"] or 0))
        other.sort(key=lambda r: (r["quality"], -r["have"], r["name"]))
        items_ist = sum(r["ist"] or 0 for r in priced)
        return {
            "totals": {
                "spendable": sum(r["ist"] or 0 for r in currency if r["code"] in CORE),
                "currency": total_cur, "currencyPositions": len(currency),
                "items": items_ist, "itemsCount": sum(r["have"] for r in priced),
                "total": total_cur + items_ist, "unpriced": sum(r["have"] for r in other),
            },
            "currency": currency, "items": priced, "other": other,
        }


@dataclass
class Stash:
    """Everything one player holds, across realms."""
    owner: str
    source: str        # site | file
    fetched_at: float
    realms: dict[str, RealmStash]

    def to_dict(self) -> dict:
        return {"owner": self.owner, "source": self.source, "fetchedAt": self.fetched_at,
                "realms": {k: r.to_dict() for k, r in self.realms.items()}}

    @classmethod
    def from_dict(cls, d: dict) -> "Stash":
        return cls(d["owner"], d["source"], d["fetchedAt"], {k: RealmStash.from_dict(r) for k, r in d["realms"].items()})
