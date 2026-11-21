import pytest

from conftest import CURRENCY, market_ads, stash_export
from loot_planner.domain.catalog import CUBE, Catalog
from loot_planner.domain.market.listing import Action, Realm
from loot_planner.domain.market.realm_market import RealmMarket
from loot_planner.domain.routing.solver import RouteRequest, solve_route
from loot_planner.domain.stash.stash import NotAStashExport, RealmStash


@pytest.fixture(scope="module")
def market():
    wts, wtb = market_ads()
    return RealmMarket(Realm.parse("softcore ladder"), Catalog.from_site(CURRENCY), wts, wtb)


# ---- valuation
def test_fair_values_recover_trade_ratios(market):
    v = market.values
    assert v["r24"] == 1
    assert v["r25"] == pytest.approx(1.5, rel=0.05)
    assert v["r22"] == pytest.approx(0.4, rel=0.05)
    assert v["pk2"] == pytest.approx(0.15, rel=0.1)


def test_other_realm_is_separate():
    wts, wtb = market_ads()
    hc = RealmMarket(Realm.parse("hardcore ladder"), Catalog.from_site(CURRENCY), wts, wtb)
    assert hc.values == {} and hc.counts["rows"] == 0


# ---- quotes and offers
def test_quotes_use_wts_for_asks_and_wtb_for_bids(market):
    q = market.quotes
    assert q["r25"]["ask"].per == pytest.approx(2 * market.values["r23"])  # the live Gul for 2 Mal
    assert q["xa4"]["bid"].per == pytest.approx(2 * market.values["r22"])  # the live bid of 2 Um
    assert "bid" not in q.get("r23", {})  # paying Mal for a Gul is not a bid for Mal


def test_offers_sorted_cheapest_first(market):
    offers = market.offers_for("r25")
    assert offers and offers[0]["per"] <= offers[-1]["per"]
    assert offers[0]["discount"] > 0  # cheaper than fair
    assert offers[0]["url"].startswith("https://blizzless.trade/wts/")


def test_items_get_median_and_live_lots(market):
    it = market.items["i:Harlequin Crest|unique"]
    assert it.live == 1 and it.sold == 1 and it.median == pytest.approx(3)
    assert market.fair("i:Harlequin Crest|unique") == pytest.approx(3)


# ---- solver
def _act(id_, give, get, uses=1, type_="wts"):
    return Action(id_, type_, give, get, uses)


def test_route_follows_cheapest_chain():
    values = {"a": 1, "b": 2, "t": 4}
    acts = [_act("1", {"a": 2}, {"b": 1}, 5), _act("2", {"b": 2}, {"t": 1}, 5), _act("3", {"a": 10}, {"t": 1}, 5)]
    r = solve_route(acts, {}, values, RouteRequest("t", pool={"a": 10}, use_cube=False, effort=0))
    assert r["ok"] and r["costIst"] == pytest.approx(4)
    assert [s["id"] for s in r["steps"]] == ["1", "2"]


def test_route_respects_listing_capacity():
    values = {"a": 1, "t": 2}
    acts = [_act("cheap", {"a": 1}, {"t": 1}, uses=1), _act("dear", {"a": 3}, {"t": 1}, uses=5)]
    one = solve_route(acts, {}, values, RouteRequest("t", qty=1, pool={"a": 10}, use_cube=False, effort=0))
    assert [s["id"] for s in one["steps"]] == ["cheap"]
    two = solve_route(acts, {}, values, RouteRequest("t", qty=2, pool={"a": 10}, use_cube=False, effort=0))
    assert two["ok"] and sum(s["times"] for s in two["steps"]) == 2
    assert all(s["times"] <= 1 for s in two["steps"] if s["id"] == "cheap")
    paper = two["paper"]["steps"][0]
    assert paper["id"] == "cheap" and paper["times"] == 2 and paper["available"] == 1  # on paper it is wanted twice


def test_route_unreachable_and_capacity():
    acts = [_act("1", {"a": 1}, {"t": 1}, uses=1)]
    assert solve_route(acts, {}, {}, RouteRequest("t", pool={"b": 5}))["reason"] == "unreachable"
    r = solve_route(acts, {}, {"a": 1, "t": 1}, RouteRequest("t", qty=3, pool={"a": 5}, use_cube=False))
    assert not r["ok"] and r["bottlenecks"][0]["kind"] == "lots"


def test_route_uses_cube_and_unlimited_keys():
    assert CUBE["r24"] == {"r23": 2, "gsv": 1}  # Ist = 2 Mal + Amethyst
    values = {"r23": 0.6, "r24": 1, "gsv": 0.05, "pk2": 0.15}
    acts = [_act("k", {"pk2": 4}, {"r23": 1}, uses=10)]
    r = solve_route(acts, CUBE, values, RouteRequest("r24", pool={"gsv": 1}, unlimited={"pk2": 0.15}, effort=0))
    assert r["ok"] and r["steps"][-1]["type"] == "cube"
    assert r["unlimitedSpent"]["pk2"] == 8  # 2 Mal from keys, then 2 Mal + Amethyst -> Ist


def test_arbitrage_loop_does_not_make_things_free():
    values = {"a": 1, "b": 1}
    acts = [_act("1", {"a": 1}, {"b": 2}, 100), _act("2", {"b": 1}, {"a": 2}, 100)]
    r = solve_route(acts, {}, values, RouteRequest("b", pool={"a": 1}, use_cube=False, effort=0))
    assert r["unitCost"] > 0


# ---- stash
def test_stash_export_is_parsed_once_per_item():
    rs = RealmStash.from_export(stash_export(), Catalog.from_site(CURRENCY))
    assert rs.realm == "softcore ladder"
    assert rs.currency == {"r23": 3, "pk2": 10, "gsr": 2, "xa4": 1, "r22": 2}
    assert ("Harlequin Crest", "unique", 1) in rs.items and ("Ring", "rare", 1) in rs.items


def test_stash_rejects_other_json():
    with pytest.raises(NotAStashExport):
        RealmStash.from_export({"hello": 1}, Catalog.from_site(CURRENCY))


def test_stash_pool_modes_and_appraisal(market):
    rs = RealmStash.from_export(stash_export(), market.catalog)
    pool, cost = rs.pool(market, "all")
    assert pool["own:i:Harlequin Crest|unique"] == 1 and cost["own:i:Harlequin Crest|unique"] == pytest.approx(3)
    assert set(rs.pool(market, "gems")[0]) == {"gsr"}
    assert rs.pool(market, "none") == ({}, {})
    a = rs.appraise(market)
    assert a["totals"]["unpriced"] == 1 and a["totals"]["items"] == pytest.approx(3)
    shard = next(r for r in a["currency"] if r["code"] == "xa4")
    assert shard["bid"] == pytest.approx(2 * market.values["r22"]) and shard["bidUrl"]
