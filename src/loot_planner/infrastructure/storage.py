"""File storage under DATA_DIR: the raw market snapshot and browser sessions."""
import hashlib
import json
import os
import time
from pathlib import Path

from loot_planner.application.ports import RawSnapshot, Session
from loot_planner.domain.stash.stash import Stash


def _write_private(path: Path, text: str, mode: int = 0o600):
    """Atomic write: a crash mid-write leaves the previous file intact."""
    tmp = path.with_name(path.name + ".tmp")
    fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, mode)
    with os.fdopen(fd, "w") as f:
        f.write(text)
    tmp.replace(path)


class FileSnapshotRepository:
    """DATA_DIR/market/{wts,wtb,currency}.json"""

    def __init__(self, data_dir: Path):
        self.dir = data_dir / "market"

    def load(self) -> RawSnapshot | None:
        try:
            return RawSnapshot(*(json.loads((self.dir / f"{n}.json").read_text()) for n in ("wts", "wtb", "currency")))
        except FileNotFoundError:
            return None

    def save(self, snap: RawSnapshot) -> None:
        self.dir.mkdir(parents=True, exist_ok=True)
        for name, data in (("wts", snap.wts), ("wtb", snap.wtb), ("currency", snap.currency)):
            _write_private(self.dir / f"{name}.json", json.dumps(data, ensure_ascii=False), 0o644)


class FileSessionRepository:
    """DATA_DIR/sessions/<hash of session id>.json, readable by the service user only.

    Files are named by a hash so a directory listing does not reveal the session cookies.
    """

    def __init__(self, data_dir: Path):
        self.dir = data_dir / "sessions"

    def _path(self, session_id: str) -> Path:
        return self.dir / (hashlib.sha256(session_id.encode()).hexdigest()[:40] + ".json")

    def get(self, session_id: str) -> Session | None:
        try:
            d = json.loads(self._path(session_id).read_text())
        except (OSError, ValueError):
            return None
        return Session(session_id, Stash.from_dict(d["stash"]) if d.get("stash") else None,
                       d.get("siteUser"), d.get("siteCookies"), d.get("touched", 0.0))

    def save(self, s: Session) -> None:
        self.dir.mkdir(parents=True, exist_ok=True)
        os.chmod(self.dir, 0o700)
        s.touched = time.time()
        _write_private(self._path(s.id), json.dumps({
            "stash": s.stash.to_dict() if s.stash else None, "siteUser": s.site_user,
            "siteCookies": s.site_cookies, "touched": s.touched}, ensure_ascii=False))

    def delete(self, session_id: str) -> None:
        self._path(session_id).unlink(missing_ok=True)

    def purge(self, older_than: float) -> int:
        n = 0
        for p in self.dir.glob("*.json") if self.dir.exists() else ():
            if p.stat().st_mtime < older_than:
                p.unlink(missing_ok=True)
                n += 1
        return n
