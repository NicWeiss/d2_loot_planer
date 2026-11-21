import dataclasses
import time

import pytest
from fastapi.testclient import TestClient

from conftest import stash_export
from loot_planner.config import Settings
from loot_planner.interfaces.http.app import create_app

R = "/api/realms/softcore%20ladder"


@pytest.fixture
def client(tmp_path, fake_site):
    settings = dataclasses.replace(Settings.from_env(), data_dir=tmp_path, refresh_hours=0, refresh_cooldown_min=0)
    app = create_app(settings, fake_site)
    with TestClient(app) as c:
        # an empty data dir makes the app fetch the market on start
        for _ in range(100):
            if c.get("/api/health").json()["ready"]:
                break
            time.sleep(0.05)
        c.site = fake_site
        yield c


def test_first_start_fetches_market(client):
    assert client.site.fetches == 1
    meta = client.get("/api/meta").json()
    assert meta["ready"] and meta["defaultRealm"] == "softcore ladder" and "r25" in meta["names"]


def test_market_queries(client):
    assert client.get(f"{R}/target", params={"code": "r25"}).json()["best"]["url"]
    assert client.get(f"{R}/offers", params={"code": "r25"}).json()["offers"]
    rates = client.get(f"{R}/rates").json()
    gul = next(r for r in rates["rows"] if r["code"] == "r25")
    assert gul["ask"] and gul["askVsFair"] < 0
    assert client.get(f"{R}/map").json()["nodes"]
    assert client.get("/api/realms/nowhere/rates").status_code == 404


def test_state_changes_need_json(client):
    assert client.post("/api/market/refresh", content="x", headers={"content-type": "text/plain"}).status_code == 415


def test_login_without_remember_signs_out(client):
    r = client.post("/api/stash/login", json={"login": "nic", "password": "secret"})
    assert r.status_code == 200 and r.json()["remembered"] is False
    assert client.site.logouts == 1
    assert "lp_session" in client.cookies
    st = client.get(f"{R}/stash").json()
    assert st["owner"] == "nic" and st["appraisal"]["totals"]["currencyPositions"] == 5
    # no remembered login: refresh asks to sign in again
    assert client.post("/api/stash/refresh", json={}).status_code == 401


def test_wrong_password(client):
    r = client.post("/api/stash/login", json={"login": "nic", "password": "nope"})
    assert r.status_code == 401 and "Invalid" in r.json()["error"]


def test_remembered_login_refreshes_until_expired(client):
    assert client.post("/api/stash/login", json={"login": "nic", "password": "secret", "remember": True}).json()["remembered"]
    assert client.site.logouts == 0
    assert client.post("/api/stash/refresh", json={}).status_code == 200
    client.site.expired = True
    r = client.post("/api/stash/refresh", json={})
    assert r.status_code == 401 and "войди заново" in r.json()["error"]
    assert client.get(f"{R}/stash").json()["loaded"]  # the last stash stays


def test_route_uses_the_sessions_stash(client):
    before = client.post(f"{R}/route", json={"target": "r25"}).json()
    assert not before["stash"]["loaded"]
    client.post("/api/stash/upload", json={"name": "export", "data": {"data": stash_export()}})
    after = client.post(f"{R}/route", json={"target": "r25"}).json()
    assert after["stash"] == {"loaded": True, "owner": "export", "realm": True}
    assert after["route"]["ok"]


def test_reset_forgets_everything(client):
    client.post("/api/stash/login", json={"login": "nic", "password": "secret", "remember": True})
    r = client.request("DELETE", "/api/stash", headers={"content-type": "application/json"})
    assert r.status_code == 200 and client.site.logouts == 1
    assert client.get(f"{R}/stash").json() == {"loaded": False}


def test_bad_upload(client):
    r = client.post("/api/stash/upload", json={"data": {"x": 1}})
    assert r.status_code == 400


def test_front_end_is_served(client):
    r = client.get("/")
    assert r.status_code == 200 and "Карта обмена" in r.text
