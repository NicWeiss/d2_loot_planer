"""Site advertisements turned into trades: what changes hands, for what, how often."""
from collections import defaultdict
from dataclasses import dataclass, field

from loot_planner.domain.catalog import Catalog

EXCLUDE = {"gld"}  # gold quantities are too noisy to price against runes


@dataclass(frozen=True)
class Realm:
    mode: str    # softcore | hardcore
    season: str  # ladder | non-ladder

    @property
    def key(self) -> str:
        return f"{self.mode} {self.season}"

    @classmethod
    def parse(cls, key: str) -> "Realm":
        mode, season = key.replace(":", " ").split()
        return cls(mode, season)

    def holds(self, ad: dict) -> bool:
        return ad["gameMode"] == self.mode and ad["ladder"] == self.season and ad["adType"] != "auction"


@dataclass
class Trade:
    """A currency-for-currency listing with one payment option, from the poster's side."""
    id: str
    type: str      # wts | wtb
    status: str    # active | completed | unlisted | ...
    goods: dict    # what the listing is about (WTS: sold, WTB: wanted)
    price: dict    # one payment option
    unit: bool     # priced per piece, `stock` pieces left
    stock: int
    date: str | None


@dataclass
class Action:
    """A live listing seen from the player's side: give -> get, at most `uses` times."""
    id: str
    type: str  # wts | wtb | cube
    give: dict
    get: dict
    uses: float
    check: bool = False  # the buyer has stat requirements
    extra: dict = field(default_factory=dict)


def goods(ad: dict) -> dict:
    """Items the listing is about: what a WTS seller gives, or what a WTB buyer wants.

    Unit sales are priced per piece and their quantity is the remaining stock (0 once sold out),
    so they are normalised to one piece.
    """
    if ad.get("unitSale") and ad.get("escrowMaterials"):
        return {ad["escrowMaterials"][0]["baseCode"]: 1}
    if ad.get("escrowMaterials"):
        return {m["baseCode"]: m["quantity"] for m in ad["escrowMaterials"]}
    if ad["payloadKind"] in ("kit", "kit_wtb") and ad.get("kitItems"):
        out = defaultdict(int)
        for k in ad["kitItems"]:
            out[k["baseCode"]] += (k.get("params") or {}).get("quantity") or 1
        return dict(out)
    return {ad["itemNameKey"]: (ad.get("params") or {}).get("quantity") or 1}


def price_options(ad: dict) -> list[dict]:
    """Payment alternatives, one dict per option group. Groups with vague 'any runes' entries are dropped."""
    groups, vague = defaultdict(dict), set()
    for p in ad["payments"]:
        if p["kind"] != "item" or not p.get("amount"):
            vague.add(p["optionGroup"])
            continue
        code = p.get("currencyBaseCode") or p["code"]
        groups[p["optionGroup"]][code] = groups[p["optionGroup"]].get(code, 0) + p["amount"]
    return [g for k, g in sorted(groups.items()) if k not in vague]


def currency_trades(ads: list[dict], catalog: Catalog, realm: Realm) -> list[Trade]:
    """Currency-for-currency listings of the realm, one Trade per payment option."""
    def ok(d):
        return d and all(c in catalog and c not in EXCLUDE for c in d) and all(q > 0 for q in d.values())

    rows = []
    for ad in ads:
        if not realm.holds(ad):
            continue
        g = goods(ad)
        if not ok(g):
            continue
        unit = bool(ad.get("unitSale") and ad.get("escrowMaterials"))
        for opt in price_options(ad):
            if ok(opt):
                rows.append(Trade(ad["id"], ad["type"], ad["status"], g, opt, bool(ad.get("unitSale")),
                                  ad["escrowMaterials"][0]["quantity"] if unit else 1,
                                  ad.get("closedAt") or ad.get("bumpedAt") or ad.get("createdAt")))
    return rows


def player_side(t: Trade) -> tuple[dict, dict]:
    """(what the player gives, what the player gets) when answering this listing."""
    return (t.price, t.goods) if t.type == "wts" else (t.goods, t.price)
