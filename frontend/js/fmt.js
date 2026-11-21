// Formatting only: numbers, names, pills. No market logic lives here.
export const $ = id => document.getElementById(id);
export const esc = s => String(s ?? "").replace(/[&<>"]/g, ch => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[ch]));
export const fmtN = (x, d = 2) => x == null || !isFinite(x) ? "—" : (Math.abs(x) >= 100 ? x.toFixed(0) : +x.toFixed(d) + "");
export const when = t => new Date(typeof t === "number" ? t * 1000 : t).toLocaleString("ru-RU", { dateStyle: "short", timeStyle: "short" });
export const plural = (n, one, few, many) => {
  const m10 = n % 10, m100 = n % 100;
  return m10 === 1 && m100 !== 11 ? one : m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14) ? few : many;
};
export const store = {
  get(k, d) { try { return localStorage.getItem(k) ?? d; } catch { return d; } },
  set(k, v) { try { localStorage.setItem(k, v); } catch {} },
};

export const GROUP_NAMES = { runes: "Руны", keys: "Ключи", uber: "Убер / жетоны", essences: "Эссенции", shards: "Осколки", statues: "Статуэтки", craft: "Крафт", gems: "Камни", other: "Прочее" };
export const QUALITY = { unique: "уник", set: "сет", runeword: "рунворд", magic: "маг.", rare: "редк.", crafted: "крафт", normal: "обычн.", superior: "сверх.", consumables: "расх." };
export const rlName = r => r.replace("softcore", "Софткор").replace("hardcore", "Хардкор").replace("non-ladder", "нон-ладдер").replace("ladder", "ладдер");

// names: the catalog from /api/meta; items: {key: {name, quality}} that came with a response
let names = {};
export const setNames = n => { names = n; };
export const nm = c => names[c]?.name ?? c;
export const short = c => (names[c]?.display ?? c).replace(/ Worldstone Shard/, " shard").replace(/Essence of /, "Ess. ");
export const groupOf = c => names[c]?.group;
export const itemLabel = it => `${it.name} (${QUALITY[it.quality] || it.quality || "?"})`;
export function label(c, items = {}) {
  if (c.startsWith("own:")) return "твой " + label(c.slice(4), items);
  if (c.startsWith("i:")) return items[c] ? itemLabel(items[c]) : c.slice(2).replace("|", " · ");
  return short(c);
}
export const fullName = (c, items) => c.startsWith("i:") ? label(c, items) : nm(c);
export const fmtD = (d, items) => Object.entries(d || {}).map(([c, q]) => `${fmtN(q, 2)}× ${label(c, items)}`).join(" + ");

// discount: share below fair value (positive = cheaper than fair)
export const pill = d => {
  if (d == null || !isFinite(d)) return "";
  const p = Math.round(d * 100), cls = p >= 5 ? "good" : p <= -5 ? "bad" : "even";
  return `<span class="pill ${cls}">${p > 0 ? "−" + p : p < 0 ? "+" + -p : "±0"}%</span>`;
};
// rel: x / fair - 1; good when buying below fair (highIsGood=false) or selling above it (true)
export const vsFair = (rel, highIsGood) => {
  if (rel == null || !isFinite(rel)) return "";
  const p = Math.round(rel * 100), good = highIsGood ? p >= 5 : p <= -5, bad = highIsGood ? p <= -5 : p >= 5;
  return `<span class="pill ${good ? "good" : bad ? "bad" : "even"}">${p > 0 ? "+" : p < 0 ? "−" : "±"}${Math.abs(p)}%</span>`;
};
export const link = (url, text) => url ? `<a href="${esc(url)}" target="_blank" rel="noopener">${text}</a>` : text;

export function segButtons(el, items, current, onPick) {
  el.innerHTML = items.map(([k, l]) => `<button type="button" data-k="${esc(k)}" aria-pressed="${k === current}">${esc(l)}</button>`).join("");
  el.onclick = e => { const b = e.target.closest("button"); if (b) onPick(b.dataset.k); };
}
