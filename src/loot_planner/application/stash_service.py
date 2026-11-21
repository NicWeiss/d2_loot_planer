"""Use cases around a browser session's stash: sign in and fetch, refresh a remembered login, upload, reset.

Without "remember" the site session is signed out right after the download and only the parsed stash is kept.
With "remember" the site cookies are kept too, so the stash can be refreshed without the password.
The password itself is never stored.
"""
import secrets
import time

from loot_planner.application.market_service import MarketService
from loot_planner.application.ports import Session, SessionRepository, SiteAccount, SiteError, TradeSite
from loot_planner.domain.stash.stash import RealmStash, Stash


class NoStash(Exception):
    pass


class LoginExpired(Exception):
    pass


class StashService:
    def __init__(self, site: TradeSite, sessions: SessionRepository, market: MarketService, ttl_days: float):
        self.site, self.sessions, self.market, self.ttl = site, sessions, market, ttl_days * 86400

    @staticmethod
    def new_id() -> str:
        return secrets.token_urlsafe(32)

    def get(self, session_id: str | None) -> Session | None:
        return self.sessions.get(session_id) if session_id else None

    def _fetch(self, account: SiteAccount) -> Stash:
        realms = {}
        for key in self.market.realm_keys:
            try:
                data = account.stash(key)
            except SiteError as e:
                if e.unauthorized:
                    raise
                continue  # a realm the site refuses is simply absent
            if data.get("linked") is False:
                continue
            rs = RealmStash.from_export(data, self.market.catalog)
            realms[rs.realm] = rs
        if not realms:
            raise NoStash("на аккаунте нет привязанного тайника")
        return Stash(account.user["login"], "site", time.time(), realms)

    def _forget_site(self, s: Session):
        if s.remembered:
            self.site.resume(s.site_cookies, s.site_user).logout()
        s.site_cookies = s.site_user = None

    def login(self, session_id: str | None, login: str, password: str, remember: bool) -> Session:
        self.market.catalog  # fail early when there is no market to parse against
        account = self.site.login(login, password)
        try:
            stash = self._fetch(account)
        except Exception:
            account.logout()
            raise
        if not remember:
            account.logout()
        s = self.get(session_id) or Session(session_id or self.new_id())
        self._forget_site(s)  # a previous remembered login of this browser is signed out
        s.stash = stash
        if remember:
            s.site_user, s.site_cookies = account.user, account.cookies()
        self.sessions.save(s)
        return s

    def refresh(self, session_id: str | None) -> tuple[Session, str | None]:
        """Re-download with the remembered login. Returns (session, warning when the old copy is shown)."""
        s = self.get(session_id)
        if not s or not s.remembered:
            raise LoginExpired("сохранённого входа нет, войди заново")
        account = self.site.resume(s.site_cookies, s.site_user)
        try:
            s.stash = self._fetch(account)
        except SiteError as e:
            if e.unauthorized:  # refresh token expired or revoked on the site
                s.site_cookies = s.site_user = None
                self.sessions.save(s)
                raise LoginExpired("сохранённый вход истёк, войди заново") from None
            if e.status is None and s.stash:
                return s, f"сайт недоступен ({e}), показан последний сохранённый тайник"
            raise
        s.site_cookies = account.cookies()  # the site may have renewed the tokens
        self.sessions.save(s)
        return s, None

    def upload(self, session_id: str | None, data: dict, name: str) -> Session:
        rs = RealmStash.from_export(data.get("data", data) if isinstance(data, dict) else data, self.market.catalog)
        s = self.get(session_id) or Session(session_id or self.new_id())
        self._forget_site(s)
        s.stash = Stash(name, "file", time.time(), {rs.realm: rs})
        self.sessions.save(s)
        return s

    def reset(self, session_id: str | None) -> None:
        s = self.get(session_id)
        if s:
            try:
                self._forget_site(s)
            finally:
                self.sessions.delete(s.id)

    def purge(self) -> int:
        return self.sessions.purge(time.time() - self.ttl)
