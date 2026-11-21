"""API routes. Handlers are thin: parse the request, call a use case or a query, return its result."""
from fastapi import APIRouter, Request, Response

from loot_planner.application.queries import MarketQueries, RouteParams
from loot_planner.application.stash_service import StashService
from loot_planner.interfaces.http.schemas import LoginBody, RouteBody, UploadBody

SESSION_COOKIE = "lp_session"

router = APIRouter(prefix="/api")


def _queries(request: Request) -> MarketQueries:
    return request.app.state.queries


def _stash(request: Request) -> StashService:
    return request.app.state.stash


def _session(request: Request):
    return _stash(request).get(request.cookies.get(SESSION_COOKIE))


def _set_session_cookie(request: Request, response: Response, session_id: str):
    response.set_cookie(SESSION_COOKIE, session_id, max_age=int(_stash(request).ttl), httponly=True,
                        samesite="strict", secure=request.app.state.settings.cookie_secure, path="/")


# ---- service
@router.get("/health")
def health(request: Request):
    return {"ok": True, "ready": request.app.state.market.state is not None}


@router.get("/meta")
def meta(request: Request):
    return _queries(request).meta(_session(request))


@router.get("/market/status")
def market_status(request: Request):
    return {"job": request.app.state.market.job, "fetchedAt": getattr(request.app.state.market.state, "fetched_at", None)}


@router.post("/market/refresh", status_code=202)
def market_refresh(request: Request):
    return {"job": request.app.state.market.refresh()}


# ---- market of one realm
@router.get("/realms/{realm}/targets")
def targets(realm: str, request: Request):
    return _queries(request).targets(realm)


@router.get("/realms/{realm}/target")
def target(realm: str, code: str, request: Request):
    return _queries(request).target(realm, code, _session(request))


@router.get("/realms/{realm}/offers")
def offers(realm: str, code: str, request: Request):
    return _queries(request).offers(realm, code)


@router.post("/realms/{realm}/route")
def route(realm: str, body: RouteBody, request: Request):
    p = RouteParams(body.target, body.qty, body.stashMode, tuple(body.keys), body.useCube, body.effort)
    return _queries(request).route(realm, p, _session(request))


@router.get("/realms/{realm}/rates")
def rates(realm: str, request: Request):
    return _queries(request).rates(realm)


@router.get("/realms/{realm}/map")
def exchange_map(realm: str, request: Request):
    return _queries(request).map(realm)


@router.get("/realms/{realm}/deals")
def deals(realm: str, request: Request):
    return _queries(request).deals(realm)


@router.get("/realms/{realm}/stash")
def stash_view(realm: str, request: Request):
    return _queries(request).stash(realm, _session(request))


# ---- the browser session's stash
def _stash_reply(request: Request, response: Response, session, warning: str | None = None):
    _set_session_cookie(request, response, session.id)
    s = session.stash
    return {"owner": s.owner, "source": s.source, "fetchedAt": s.fetched_at, "remembered": session.remembered,
            "realms": list(s.realms), "warning": warning}


@router.post("/stash/login")
def stash_login(body: LoginBody, request: Request, response: Response):
    s = _stash(request).login(request.cookies.get(SESSION_COOKIE), body.login, body.password, body.remember)
    return _stash_reply(request, response, s)


@router.post("/stash/refresh")
def stash_refresh(request: Request, response: Response):
    s, warning = _stash(request).refresh(request.cookies.get(SESSION_COOKIE))
    return _stash_reply(request, response, s, warning)


@router.post("/stash/upload")
def stash_upload(body: UploadBody, request: Request, response: Response):
    s = _stash(request).upload(request.cookies.get(SESSION_COOKIE), body.data, body.name)
    return _stash_reply(request, response, s)


@router.delete("/stash")
def stash_reset(request: Request, response: Response):
    _stash(request).reset(request.cookies.get(SESSION_COOKIE))
    response.delete_cookie(SESSION_COOKIE, path="/")
    return {"ok": True}
