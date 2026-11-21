"""One realm's market, built from a snapshot of listings: fair values, live trades, quotes, offers."""
from collections import defaultdict
from dataclasses import dataclass
from functools import cached_property

from loot_planner.domain.catalog import CUBE, UNIT, Catalog, item_key, own
from loot_planner.domain.market.listing import (Action, Realm, Trade, currency_trades, goods, player_side,
                                                price_options)
from loot_planner.domain.market.valuation import fit_values, worth

LOT_URL = "https://blizzless.trade/{type}/{id}"
DEAL_MIN_OBS = 15   # only liquid currencies are trusted when flagging underpriced lots
DEAL_RATIO = 1.15
ITEM_LOTS = 4       # cheapest live lots kept per item name


def lot_url(type_: str, id_: str) -> str | None:
    return None if type_ == "cube" else LOT_URL.format(type=type_, id=id_)


@dataclass
class ItemInfo:
    name: str
    quality: str
    ask: float | None = None    # cheapest live lot, Ist
    live: int = 0               # live lots
    sold: int = 0               # completed sales
    median: float | None = None  # median completed sale, Ist


@dataclass
class Quote:
    per: float      # Ist per piece
    action: Action


class RealmMarket:
    def __init__(self, realm: Realm, catalog: Catalog, wts: list[dict], wtb: list[dict]):
        self.realm, self.catalog = realm, catalog
        ads = wts + wtb
        trades = currency_trades(ads, catalog, realm)
        self.values, self.n_obs = fit_values(trades)
        self.counts = {"rows": len(trades), "active": sum(t.status == "active" for t in trades),
                       "completed": sum(t.status == "completed" for t in trades)}
        self.table = self._table(trades, ads)
        self.edges = self._edges(trades)
        self.deals = self._deals(trades)
        self.actions = [Action(t.id, t.type, *player_side(t), t.stock if t.unit else 1)
                        for t in trades if t.status == "active"]
        self.items: dict[str, ItemInfo] = {}
        self.actions += self._item_actions(wts, wtb)

    # ---- building blocks
    def _table(self, trades: list[Trade], ads: list[dict]) -> dict[str, dict]:
        sup, dem, acc = defaultdict(int), defaultdict(int), defaultdict(int)
        for t in {t.id: t for t in trades}.values():
            if t.status == "active":
                for c, q in t.goods.items():
                    (sup if t.type == "wts" else dem)[c] += q
        # how many active listings (any item) accept each currency as payment = demand for it as money
        for ad in ads:
            if ad["status"] == "active" and ad["type"] == "wts" and ad["gameMode"] == self.realm.mode \
                    and ad["ladder"] == self.realm.season:
                for c in {p.get("currencyBaseCode") or p["code"] for p in ad["payments"] if p["kind"] == "item"}:
                    acc[c] += 1
        last = defaultdict(list)
        for t in trades:
            if t.status == "completed" and len(t.goods) == 1 and len(t.price) == 1:
                (g, qg), = t.goods.items()
                (p, qp), = t.price.items()
                last[g].append({"date": t.date, "code": p, "qty": qp, "perUnit": qp / qg,
                                "ist": qp * self.values[p] / qg if p in self.values else None})
        table = {}
        for c, cur in self.catalog.by_code.items():
            if c not in self.values and not sup[c] and not dem[c]:
                continue
            table[c] = {"code": c, "name": cur.name, "group": cur.group, "ist": self.values.get(c),
                        "obs": self.n_obs.get(c, 0), "supply": sup[c], "demand": dem[c], "acceptedBy": acc[c],
                        "recent": sorted(last[c], key=lambda x: x["date"] or "", reverse=True)[:8]}
        return table

    def _edges(self, trades: list[Trade]) -> list[dict]:
        """Best live single-item rate per (pay -> receive) pair, for the exchange map."""
        best, pair_count = {}, defaultdict(int)
        for t in trades:
            if t.status != "active":
                continue
            give, get = player_side(t)
            for a in give:
                for b in get:
                    if a != b:
                        pair_count[(a, b)] += 1
            if len(give) != 1 or len(get) != 1:
                continue
            (a, qa), = give.items()
            (b, qb), = get.items()
            if a != b and ((a, b) not in best or qa / qb < best[(a, b)][0]):
                best[(a, b)] = (qa / qb, t, qa, qb)
        v = self.values
        out = []
        for (a, b), (rate, t, qa, qb) in best.items():
            fair = v[b] / v[a] if a in v and b in v else None
            if fair and not 0.2 < fair / rate < 5:
                continue  # joke prices
            out.append({"from": a, "to": b, "rate": rate, "fairRate": fair, "give": qa, "get": qb,
                        "n": pair_count[(a, b)], "url": lot_url(t.type, t.id)})
        return out

    def _deals(self, trades: list[Trade]) -> list[dict]:
        deals = []
        for t in trades:
            if t.status != "active":
                continue
            give, get = player_side(t)
            if any(self.n_obs.get(c, 0) < DEAL_MIN_OBS for c in list(give) + list(get)):
                continue  # thinly traded items have unreliable fair values
            cv, gv = worth(give, self.values), worth(get, self.values)
            if cv and gv and gv / cv > DEAL_RATIO:
                deals.append({"type": t.type, "give": give, "get": get, "ratio": gv / cv, "giveIst": cv,
                              "getIst": gv, "unit": t.unit, "url": lot_url(t.type, t.id)})
        return sorted(deals, key=lambda d: -d["ratio"])[:60]

    def _item_actions(self, wts: list[dict], wtb: list[dict]) -> list[Action]:
        """Non-currency goods: the cheapest live lots per item name and every WTB for an item."""
        v = self.values

        def opts(a):
            return [o for o in price_options(a) if all(c in v for c in o)]

        asks, sold = defaultdict(list), defaultdict(list)
        for a in wts:
            if not self.realm.holds(a) or a["payloadKind"] != "lot" or all(c in self.catalog for c in goods(a)):
                continue
            os_ = opts(a)
            if not os_:
                continue
            k = item_key(a.get("itemName"), a.get("quality"))
            self.items.setdefault(k, ItemInfo(a.get("itemName"), a.get("quality")))
            cheapest = min(worth(o, v) for o in os_)
            if a["status"] == "active":
                asks[k].append((cheapest, a, os_))
            elif a["status"] == "completed":
                sold[k].append(cheapest)
        actions = []
        for k, lst in asks.items():
            lst.sort(key=lambda x: x[0])
            for _, a, os_ in lst[:ITEM_LOTS]:
                actions += [Action(a["id"], "wts", o, {k: 1}, 1) for o in os_]
        for a in wtb:
            if a["status"] != "active" or not self.realm.holds(a) or a.get("itemNameKey") in self.catalog:
                continue
            k = item_key(a.get("itemName"), a.get("quality"))
            self.items.setdefault(k, ItemInfo(a.get("itemName"), a.get("quality")))
            q = (a.get("params") or {}).get("quantity") or 1
            # Only your own copy can fill an item WTB: a bought one has different rolls, so it is a separate code.
            actions += [Action(a["id"], "wtb", {own(k): q}, o, 1, check=bool(a.get("wtbRequirements")))
                        for o in opts(a)]
        for k, it in self.items.items():
            s = sorted(sold.get(k, []))
            it.ask = asks[k][0][0] if asks.get(k) else None
            it.live, it.sold = len(asks.get(k, [])), len(s)
            it.median = s[len(s) // 2] if s else None
        return actions

    # ---- queries
    def fair(self, code: str) -> float | None:
        """Fair value of a currency, or what an item of that name usually sells for."""
        if code.startswith("i:"):
            it = self.items.get(code)
            return None if not it else it.median if it.median is not None else it.ask
        return self.values.get(code)

    def known(self, code: str) -> bool:
        return code in self.items if code.startswith("i:") else code in self.table

    @cached_property
    def quotes(self) -> dict[str, dict[str, Quote]]:
        """Per currency: the cheapest WTS selling only it (ask) and the richest WTB asking only for it (bid).

        Paying with a currency for someone's goods is not a bid for it, so WTS never count as sells.
        """
        q: dict[str, dict[str, Quote]] = {}
        plain = lambda d: all(":" not in c for c in d)  # noqa: E731
        for a in self.actions:
            if not plain(a.give) or not plain(a.get):
                continue
            if a.type == "wts" and len(a.get) == 1:
                (c, n), = a.get.items()
                val = worth(a.give, self.values)
                if val and (c not in q or "ask" not in q[c] or val / n < q[c]["ask"].per):
                    q.setdefault(c, {})["ask"] = Quote(val / n, a)
            if a.type == "wtb" and len(a.give) == 1:
                (c, n), = a.give.items()
                val = worth(a.get, self.values)
                if val and (c not in q or "bid" not in q[c] or val / n > q[c]["bid"].per):
                    q.setdefault(c, {})["bid"] = Quote(val / n, a)
        return q

    def offers_for(self, target: str) -> list[dict]:
        """Every live listing that yields the target, cheapest per piece first.

        A bundle's price is split across its parts in proportion to their fair values.
        """
        v, fair = self.values, self.fair(target)
        out = []
        for a in self.actions:
            if target not in a.get:
                continue
            cost = worth(a.give, v)
            if cost is None:
                continue
            gain = sum(q * ((fair or 0) if c == target else v.get(c, 0)) for c, q in a.get.items())
            per = cost * fair / gain if fair and gain else cost / a.get[target]
            out.append({"type": a.type, "give": a.give, "get": a.get, "uses": a.uses, "check": a.check,
                        "per": per, "discount": 1 - per / fair if fair else None, "pure": len(a.get) == 1,
                        "url": lot_url(a.type, a.id)})
        return sorted(out, key=lambda o: o["per"])

    def cube_cost(self, target: str) -> dict | None:
        recipe = CUBE.get(target)
        if not recipe:
            return None
        known = {c: q for c, q in recipe.items() if c in self.values}
        return {"recipe": recipe, "ist": sum(q * self.values[c] for c, q in known.items()),
                "complete": len(known) == len(recipe)}

    def unit_value(self) -> float:
        return self.values.get(UNIT, 1.0)
