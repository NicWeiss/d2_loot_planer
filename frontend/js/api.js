// Thin client for the backend API. Every number the page shows comes from here.
async function call(method, path, body) {
  const init = { method, cache: "no-store", headers: {} };
  if (method !== "GET") {
    init.headers["Content-Type"] = "application/json";  // the API refuses state changes without it
    init.body = JSON.stringify(body ?? {});
  }
  const r = await fetch(path, init);
  const j = await r.json().catch(() => ({}));
  if (!r.ok) {
    const detail = Array.isArray(j.detail) ? j.detail.map(d => d.msg).join("; ") : j.detail;
    const err = new Error(j.error || detail || `HTTP ${r.status}`);
    err.status = r.status;
    throw err;
  }
  return j;
}

const realmPath = (realm, rest) => `/api/realms/${encodeURIComponent(realm)}/${rest}`;
const q = code => `?code=${encodeURIComponent(code)}`;

export const api = {
  meta: () => call("GET", "/api/meta"),
  marketStatus: () => call("GET", "/api/market/status"),
  refreshMarket: () => call("POST", "/api/market/refresh"),

  targets: realm => call("GET", realmPath(realm, "targets")),
  target: (realm, code) => call("GET", realmPath(realm, "target" + q(code))),
  offers: (realm, code) => call("GET", realmPath(realm, "offers" + q(code))),
  route: (realm, params) => call("POST", realmPath(realm, "route"), params),
  strategy: (realm, params) => call("POST", realmPath(realm, "strategy"), params),
  rates: realm => call("GET", realmPath(realm, "rates")),
  map: realm => call("GET", realmPath(realm, "map")),
  deals: realm => call("GET", realmPath(realm, "deals")),
  stash: realm => call("GET", realmPath(realm, "stash")),

  login: (login, password, remember) => call("POST", "/api/stash/login", { login, password, remember }),
  refreshStash: () => call("POST", "/api/stash/refresh"),
  uploadStash: (name, data) => call("POST", "/api/stash/upload", { name, data }),
  resetStash: () => call("DELETE", "/api/stash"),
};
