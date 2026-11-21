"""The current market: built from the stored snapshot, refreshed from the site in the background."""
import logging
import threading
import time
from dataclasses import dataclass

from loot_planner.application.ports import RawSnapshot, SnapshotRepository, TradeSite
from loot_planner.domain.catalog import Catalog
from loot_planner.domain.market.listing import Realm
from loot_planner.domain.market.realm_market import RealmMarket

log = logging.getLogger(__name__)


class MarketNotReady(Exception):
    pass


class UnknownRealm(KeyError):
    pass


class RefreshRefused(Exception):
    pass


@dataclass
class MarketState:
    catalog: Catalog
    fetched_at: str | None
    realms: dict[str, RealmMarket]


class MarketService:
    def __init__(self, site: TradeSite, repo: SnapshotRepository, realms: tuple[str, ...], cooldown_min: float):
        self.site, self.repo, self.realm_keys, self.cooldown = site, repo, realms, cooldown_min * 60
        self.state: MarketState | None = None
        self.job = {"running": False, "startedAt": None, "finishedAt": None, "error": None, "log": []}
        self._lock = threading.Lock()

    def build(self, snap: RawSnapshot) -> MarketState:
        catalog = Catalog.from_site(snap.currency)
        fetched = max((a.get("updatedAt") or "" for a in snap.wts + snap.wtb), default="") or None
        return MarketState(catalog, fetched, {k: RealmMarket(Realm.parse(k), catalog, snap.wts, snap.wtb)
                                              for k in self.realm_keys})

    def load(self) -> bool:
        snap = self.repo.load()
        if snap:
            self.state = self.build(snap)
        return snap is not None

    def realm(self, key: str) -> RealmMarket:
        if not self.state:
            raise MarketNotReady()
        if key not in self.state.realms:
            raise UnknownRealm(key)
        return self.state.realms[key]

    @property
    def catalog(self) -> Catalog:
        if not self.state:
            raise MarketNotReady()
        return self.state.catalog

    # ---- refresh
    def refresh(self, manual: bool = True) -> dict:
        with self._lock:
            if self.job["running"]:
                raise RefreshRefused("обновление уже идёт")
            done = self.job["finishedAt"]
            if manual and done and not self.job["error"] and time.time() - done < self.cooldown:
                wait = int((self.cooldown - (time.time() - done)) / 60) + 1
                raise RefreshRefused(f"маркет только что обновлён, повтори через {wait} мин.")
            self.job.update(running=True, startedAt=time.time(), finishedAt=None, error=None, log=[])
        threading.Thread(target=self._run, daemon=True, name="market-refresh").start()
        return self.job

    def _progress(self, line: str):
        self.job["log"] = (self.job["log"] + [line])[-20:]

    def _run(self):
        try:
            snap = self.site.fetch_listings(self._progress)
            self._progress("пересчёт цен…")
            state = self.build(snap)
            self.repo.save(snap)
            self.state = state  # swapped whole: readers see the old market or the new one, never a mix
            self._progress("готово")
        except Exception as e:  # reported to the page
            log.exception("market refresh failed")
            self.job["error"] = str(e)
        finally:
            self.job.update(running=False, finishedAt=time.time())

    def schedule(self, every_hours: float):
        """Refresh every `every_hours`, and right away when there is no snapshot yet."""
        def loop():
            if not self.state:
                self._try_refresh()
            while every_hours > 0:
                time.sleep(every_hours * 3600)
                self._try_refresh()

        threading.Thread(target=loop, daemon=True, name="market-schedule").start()

    def _try_refresh(self):
        try:
            self.refresh(manual=False)
        except RefreshRefused:
            pass
