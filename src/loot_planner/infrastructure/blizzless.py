"""blizzless.trade HTTP API: public listings, and signed-in calls for a player's stash."""
import http.cookiejar
import json
import time
import urllib.error
import urllib.parse
import urllib.request
from typing import Callable

from loot_planner.application.ports import RawSnapshot, SiteError

HEADERS = {"User-Agent": "Mozilla/5.0 (loot-planner; personal use)", "Accept": "application/json"}


class _Client:
    def __init__(self, site: str, jar: http.cookiejar.CookieJar | None = None):
        self.api = site + "/api/be"
        self.headers = {**HEADERS, "Origin": site, "Referer": site + "/"}
        self.jar = jar or http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.jar))

    def call(self, method: str, path: str, body=None, params: dict | None = None):
        url = self.api + path
        if params:
            url += "?" + urllib.parse.urlencode({k: v for k, v in params.items() if v is not None})
        data = json.dumps(body).encode() if body is not None else None
        req = urllib.request.Request(url, data=data, method=method,
                                     headers={**self.headers, **({"Content-Type": "application/json"} if data else {})})
        status = None
        try:
            with self.opener.open(req, timeout=60) as r:
                res = json.load(r)
        except urllib.error.HTTPError as e:
            status = e.code
            try:
                res = json.load(e)
            except ValueError:
                raise SiteError(f"HTTP {e.code}", e.code) from None
        except (urllib.error.URLError, TimeoutError) as e:
            raise SiteError(f"сайт недоступен: {getattr(e, 'reason', e)}") from None
        if res.get("status") != "success":
            err = res.get("error") or {}
            raise SiteError(err.get("message") or err.get("code") or "ошибка сайта", status)
        return res["data"]


class BlizzlessAccount:
    """A signed-in session: its cookie jar, and calls that renew the access token when it runs out."""

    def __init__(self, client: _Client, user: dict):
        self.client, self.user = client, user

    def _authed(self, method: str, path: str, params: dict | None = None):
        try:
            return self.client.call(method, path, params=params)
        except SiteError as e:
            if not e.unauthorized:
                raise
        self.client.call("POST", "/auth/refresh")  # the site's own client renews tokens the same way
        return self.client.call(method, path, params=params)

    def stash(self, realm: str) -> dict:
        mode, season = realm.split(" ")
        return self._authed("GET", "/me/stash/sources", {"hardcore": str(mode == "hardcore").lower(),
                                                         "ladder": str(season == "ladder").lower()})

    def logout(self) -> None:
        try:
            self.client.call("POST", "/auth/logout")
        except SiteError:
            pass

    def cookies(self) -> list[dict]:
        return [{"name": c.name, "value": c.value, "domain": c.domain, "path": c.path, "secure": c.secure,
                 "expires": c.expires} for c in self.client.jar]


class BlizzlessSite:
    def __init__(self, site: str, page_delay: float = 0.3):
        self.site, self.page_delay = site, page_delay

    def fetch_listings(self, progress: Callable[[str], None] = lambda _: None) -> RawSnapshot:
        client = _Client(self.site)

        def fetch_all(ad_type):
            items, cursor = [], None
            while True:
                page = client.call("GET", "/advertisements", params={"type": ad_type, "limit": 100, "cursor": cursor})
                page = page["advertisements"]
                items += page["items"]
                cursor = page.get("nextCursor")
                progress(f"{ad_type}: {len(items)}")
                if not cursor or not page["items"]:
                    return items
                time.sleep(self.page_delay)

        wts, wtb = fetch_all("wts"), fetch_all("wtb")
        return RawSnapshot(wts, wtb, client.call("GET", "/items/currency"))

    def login(self, login: str, password: str) -> BlizzlessAccount:
        client = _Client(self.site)
        user = client.call("POST", "/auth/login", {"login": login, "password": password})["user"]
        return BlizzlessAccount(client, {"id": user["id"], "login": user["login"]})

    def resume(self, cookies: list[dict], user: dict) -> BlizzlessAccount:
        jar = http.cookiejar.CookieJar()
        for c in cookies:
            jar.set_cookie(http.cookiejar.Cookie(
                0, c["name"], c["value"], None, False, c["domain"], True, c["domain"].startswith("."), c["path"], True,
                c["secure"], c["expires"], False, None, None, {"HttpOnly": None}))
        return BlizzlessAccount(_Client(self.site, jar), user)
