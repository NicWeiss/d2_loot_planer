"""Runtime settings, from environment variables."""
import os
from dataclasses import dataclass
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def _env(name: str, default: str) -> str:
    return os.environ.get(name, default)


@dataclass(frozen=True)
class Settings:
    data_dir: Path
    frontend_dir: Path
    site_url: str
    realms: tuple[str, ...]
    refresh_hours: float       # automatic market refresh period, 0 = never
    refresh_cooldown_min: float  # manual refresh is refused more often than this
    page_delay: float          # pause between listing pages, to be gentle with the site
    session_ttl_days: float
    cookie_secure: bool

    @classmethod
    def from_env(cls) -> "Settings":
        return cls(
            data_dir=Path(_env("DATA_DIR", str(ROOT / "data"))),
            frontend_dir=Path(_env("FRONTEND_DIR", str(ROOT / "frontend"))),
            site_url=_env("SITE_URL", "https://blizzless.trade").rstrip("/"),
            realms=tuple(r.strip().replace(":", " ") for r in _env("REALMS", "softcore:ladder,hardcore:ladder").split(",")),
            refresh_hours=float(_env("MARKET_REFRESH_HOURS", "6")),
            refresh_cooldown_min=float(_env("MARKET_REFRESH_COOLDOWN_MINUTES", "10")),
            page_delay=float(_env("FETCH_PAGE_DELAY", "0.3")),
            session_ttl_days=float(_env("SESSION_TTL_DAYS", "30")),
            cookie_secure=_env("COOKIE_SECURE", "false").lower() in ("1", "true", "yes"),
        )
