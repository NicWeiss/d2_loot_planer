"""Application assembly: wires infrastructure into services and serves the API plus the front end.

    uvicorn loot_planner.interfaces.http.app:create_app --factory

Run a single worker: the market lives in memory and the refresh job is a background thread of the process.
"""
import logging
from contextlib import asynccontextmanager

from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse
from fastapi.staticfiles import StaticFiles
from starlette.concurrency import run_in_threadpool

from loot_planner.application.market_service import MarketNotReady, MarketService, RefreshRefused, UnknownRealm
from loot_planner.application.ports import SiteError, TradeSite
from loot_planner.application.queries import MarketQueries
from loot_planner.application.stash_service import LoginExpired, NoStash, StashService
from loot_planner.config import Settings
from loot_planner.domain.stash.stash import NotAStashExport
from loot_planner.infrastructure.blizzless import BlizzlessSite
from loot_planner.infrastructure.storage import FileSessionRepository, FileSnapshotRepository
from loot_planner.interfaces.http.routes import router

MAX_BODY = 8 * 1024 * 1024  # a stash export is about 1 MB


def create_app(settings: Settings | None = None, site: TradeSite | None = None) -> FastAPI:
    settings = settings or Settings.from_env()
    site = site or BlizzlessSite(settings.site_url, settings.page_delay)
    market = MarketService(site, FileSnapshotRepository(settings.data_dir), settings.realms, settings.refresh_cooldown_min)
    stash = StashService(site, FileSessionRepository(settings.data_dir), market, settings.session_ttl_days)

    @asynccontextmanager
    async def lifespan(app: FastAPI):
        await run_in_threadpool(market.load)
        market.schedule(settings.refresh_hours)
        await run_in_threadpool(stash.purge)
        yield

    app = FastAPI(title="Loot planner", lifespan=lifespan, docs_url="/api/docs", redoc_url=None,
                  openapi_url="/api/openapi.json")
    app.state.settings, app.state.market, app.state.stash = settings, market, stash
    app.state.queries = MarketQueries(market)

    @app.middleware("http")
    async def guard(request: Request, call_next):
        if request.url.path.startswith("/api/") and request.method in ("POST", "PUT", "PATCH", "DELETE"):
            # JSON bodies force a CORS preflight that this API never grants, so other sites cannot post here
            if request.headers.get("content-type", "").split(";")[0].strip() != "application/json":
                return JSONResponse({"error": "нужен JSON"}, 415)
            if int(request.headers.get("content-length") or 0) > MAX_BODY:
                return JSONResponse({"error": "слишком большой запрос"}, 413)
        response = await call_next(request)
        response.headers.setdefault("Cache-Control", "no-cache")  # a new build or snapshot shows on the next load
        response.headers.setdefault("X-Content-Type-Options", "nosniff")
        return response

    errors = {
        MarketNotReady: (503, lambda e: "рынок ещё загружается, попробуй через пару минут"),
        UnknownRealm: (404, lambda e: f"нет такого режима: {e.args[0]}"),
        RefreshRefused: (409, str),
        LoginExpired: (401, str),
        NoStash: (404, str),
        NotAStashExport: (400, str),
        SiteError: (502, lambda e: f"сайт: {e}"),
    }
    for exc, (code, msg) in errors.items():
        def handler(request, e, code=code, msg=msg):
            status = 401 if isinstance(e, SiteError) and e.unauthorized else code
            return JSONResponse({"error": msg(e)}, status)
        app.add_exception_handler(exc, handler)

    app.include_router(router)
    if settings.frontend_dir.is_dir():
        app.mount("/", StaticFiles(directory=settings.frontend_dir, html=True), name="frontend")
    return app


logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s")
