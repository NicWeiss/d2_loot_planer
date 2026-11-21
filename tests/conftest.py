"""Synthetic market data shaped like the site's API answers."""
import itertools

import pytest

from loot_planner.application.ports import RawSnapshot, SiteError

_ids = itertools.count(1)

CURRENCY = {"currencyItems": [
    {"code": c, "name": n, "typeCode": t, "itemClass": k}
    for c, n, t, k in [
        ("r22", "Um Rune", "rune", "misc"), ("r23", "Mal Rune", "rune", "misc"), ("r24", "Ist Rune", "rune", "misc"),
        ("r25", "Gul Rune", "rune", "misc"), ("r26", "Vex Rune", "rune", "misc"), ("pk2", "Key of Hate", "ques", "misc"),
        ("xa4", "Deep Worldstone Shard", "ques", "misc"), ("gsr", "Ruby", "gem", "gem"), ("glr", "Flawless Ruby", "gem", "gem"),
    ]]}


def ad(type_, goods, price, status="completed", mode="softcore", ladder="ladder", unit_stock=None, item=None, quality=None):
    """One listing. goods/price: {code: qty}; item: a non-currency item name (then goods is ignored)."""
    a = {"id": f"ad{next(_ids)}", "type": type_, "status": status, "gameMode": mode, "ladder": ladder, "adType": "fixed",
         "pricingType": "fixed", "updatedAt": "2026-11-20T10:00:00Z", "closedAt": "2026-11-20T10:00:00Z",
         "payloadKind": "lot", "unitSale": unit_stock is not None, "params": {},
         "payments": [{"kind": "item", "optionGroup": 0, "code": c, "amount": q} for c, q in price.items()]}
    if item:
        a.update(itemName=item, itemNameKey=item.lower().replace(" ", "_"), quality=quality or "unique", escrowMaterials=None)
    elif unit_stock is not None:
        (c, _), = goods.items()
        a["escrowMaterials"] = [{"baseCode": c, "quantity": unit_stock}]
    else:
        a["escrowMaterials"] = [{"baseCode": c, "quantity": q} for c, q in goods.items()]
    return a


def market_ads():
    """Gul = 1.5 Ist, Vex = 2.4 Ist, Um = 0.4, Mal = 0.6, key = 0.15, Deep shard = 0.6, plus live lots."""
    wts, wtb = [], []
    for _ in range(8):
        wts += [ad("wts", {"r25": 2}, {"r24": 3}), ad("wts", {"r26": 1}, {"r25": 1.6}),
                ad("wts", {"r24": 2}, {"r22": 5}), ad("wts", {"r23": 2}, {"r22": 3}),
                ad("wts", {"r24": 3}, {"pk2": 20}), ad("wts", {"xa4": 1}, {"r23": 1})]
        wtb += [ad("wtb", {"r24": 1}, {"r23": 5 / 3}), ad("wtb", {"r25": 1}, {"pk2": 10})]
    wts += [ad("wts", {"r25": 1}, {"r23": 2}, status="active"),           # Gul for 2 Mal (1.2 Ist): cheap
            ad("wts", {"r23": 1}, {"pk2": 4}, status="active", unit_stock=3),
            ad("wts", {}, {"r24": 2}, status="active", item="Harlequin Crest"),
            ad("wts", {}, {"r24": 3}, status="completed", item="Harlequin Crest")]
    wtb += [ad("wtb", {"xa4": 1}, {"r22": 2}, status="active")]  # someone buys a Deep shard for 2 Um
    return wts, wtb


class FakeAccount:
    def __init__(self, site, user):
        self.site, self.user = site, user

    def stash(self, realm):
        if self.site.expired:
            raise SiteError("Unauthorized", 401)
        if realm != "softcore ladder":
            return {"linked": False}
        return stash_export()

    def logout(self):
        self.site.logouts += 1

    def cookies(self):
        return [{"name": "rt", "value": "x", "domain": "example.test", "path": "/", "secure": True, "expires": None}]


class FakeSite:
    def __init__(self):
        self.logouts, self.expired, self.fetches = 0, False, 0

    def fetch_listings(self, progress):
        self.fetches += 1
        wts, wtb = market_ads()
        return RawSnapshot(wts, wtb, CURRENCY)

    def login(self, login, password):
        if password != "secret":
            raise SiteError("Invalid credentials", 401)
        return FakeAccount(self, {"id": "u1", "login": login})

    def resume(self, cookies, user):
        return FakeAccount(self, user)


def stash_export():
    """A /me/stash/sources answer: materials tab, one normal tab, a character."""
    def it(code, name, quality, qty=None):
        return {"baseCode": code, "item": {"displayName": name, "quality": quality, "lot": {"quantity": qty} if qty else {}}}
    return {
        "set": {"hardcore": False, "ladder": True, "season": 4}, "linked": True,
        "materials": {"tabIndex": 9, "records": [{"baseCode": "r23", "quantity": 3}, {"baseCode": "pk2", "quantity": 10},
                                                 {"baseCode": "gsr", "quantity": 2}]},
        "tabs": [{"index": 0, "items": [it("xa4", "Deep Worldstone Shard", "normal", 1), it("cap", "Harlequin Crest", "unique"),
                                        it("rin", "Ring", "rare")]},
                 {"index": 9, "items": [it("r23", "Mal Rune", "normal", 99)]}],  # the materials tab itself is not counted twice
        "characters": [{"items": [it("r22", "Um Rune", "normal", 2)]}],
    }


@pytest.fixture
def fake_site():
    return FakeSite()
