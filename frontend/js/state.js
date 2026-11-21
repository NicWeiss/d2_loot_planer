// Page state shared by the views, plus a small per-realm cache of API answers.
import { api } from "./api.js";
import { store } from "./fmt.js";

export const TABS = ["map", "stash", "rates", "deals"];

export const state = {
  meta: null,
  realm: null,
  target: store.get("target", "r32"),
  tab: TABS.includes(location.hash.slice(1)) ? location.hash.slice(1) : store.get("tab", "map"),
  base: store.get("base", "r24"),
  rGroup: store.get("ratesGroup", "all"),
  mSort: { key: "ist", dir: -1 },
  stash: null,  // the session's stash header: owner, source, fetchedAt, remembered, realms
};
if (!TABS.includes(state.tab)) state.tab = "map";

const cache = new Map();
// market answers only change with a new snapshot, so they are kept per realm until then
export function cached(kind, fn) {
  const key = state.realm + "|" + kind;
  if (!cache.has(key)) cache.set(key, fn().catch(e => { cache.delete(key); throw e; }));
  return cache.get(key);
}
export const clearCache = () => cache.clear();

export const rates = () => cached("rates", () => api.rates(state.realm));
export const targets = () => cached("targets", () => api.targets(state.realm));

// the unit the stash and rates are shown in: its fair value in Ist
export async function baseUnit() {
  const rows = (await rates()).rows, ist = c => rows.find(r => r.code === c)?.ist;
  const code = ist(state.base) ? state.base : "r24";
  return { code, ist: ist(code) || 1 };
}

// last-call-wins guard for async renders
const seqs = {};
export function latest(name) {
  const n = (seqs[name] = (seqs[name] || 0) + 1);
  return () => seqs[name] === n;
}
