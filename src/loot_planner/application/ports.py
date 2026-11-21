"""What the application needs from the outside world; implemented in infrastructure."""
from dataclasses import dataclass
from typing import Callable, Protocol

from loot_planner.domain.stash.stash import Stash


@dataclass
class RawSnapshot:
    """Listings and the currency list exactly as the site returns them."""
    wts: list[dict]
    wtb: list[dict]
    currency: dict


class SiteError(Exception):
    def __init__(self, message: str, status: int | None = None):
        super().__init__(message)
        self.status = status  # HTTP status, None when the site could not be reached

    @property
    def unauthorized(self) -> bool:
        return self.status == 401


class SiteAccount(Protocol):
    """A signed-in session on the trade site."""
    user: dict

    def stash(self, realm: str) -> dict: ...
    def logout(self) -> None: ...
    def cookies(self) -> list[dict]: ...


class TradeSite(Protocol):
    def fetch_listings(self, progress: Callable[[str], None]) -> RawSnapshot: ...
    def login(self, login: str, password: str) -> SiteAccount: ...
    def resume(self, cookies: list[dict], user: dict) -> SiteAccount: ...


class SnapshotRepository(Protocol):
    def load(self) -> RawSnapshot | None: ...
    def save(self, snap: RawSnapshot) -> None: ...


@dataclass
class Session:
    """A browser's session with this app: its stash and, if the player asked to be remembered, site cookies."""
    id: str
    stash: Stash | None = None
    site_user: dict | None = None
    site_cookies: list[dict] | None = None
    touched: float = 0.0

    @property
    def remembered(self) -> bool:
        return bool(self.site_cookies)


class SessionRepository(Protocol):
    def get(self, session_id: str) -> Session | None: ...
    def save(self, session: Session) -> None: ...
    def delete(self, session_id: str) -> None: ...
    def purge(self, older_than: float) -> int: ...
