// Tab "Курс": fair value, live ask/bid and spread per currency, plus cross rates. All numbers come from the API.
import { $, GROUP_NAMES, esc, fmtN, link, segButtons, short, store, vsFair } from "../fmt.js";
import { baseUnit, latest, rates, state } from "../state.js";

let onBaseChange = () => {};
export const initRates = cb => { onBaseChange = cb; };

export async function renderBaseSelects() {
  const rows = (await rates()).rows, unit = await baseUnit();
  for (const sel of document.querySelectorAll(".base-sel")) {
    sel.innerHTML = state.meta.bases.filter(c => rows.find(r => r.code === c)?.ist)
      .map(c => `<option value="${c}"${c === unit.code ? " selected" : ""}>${esc(short(c))}</option>`).join("");
    sel.onchange = e => { state.base = e.target.value; store.set("base", state.base); onBaseChange(); };
  }
}

export async function renderRates() {
  const ok = latest("rates");
  const [d, unit] = await Promise.all([rates(), baseUnit()]);
  if (!ok()) return;
  await renderBaseSelects();
  const fB = ist => { const x = ist == null ? null : ist / unit.ist; return fmtN(x, x != null && Math.abs(x) < 1 ? 3 : 2); };
  const groups = [["all", "Все"], ...state.meta.groups.filter(g => d.rows.some(r => r.group === g)).map(g => [g, GROUP_NAMES[g] || g])];
  if (!groups.some(([g]) => g === state.rGroup)) state.rGroup = "all";
  segButtons($("rates-group"), groups, state.rGroup, g => { state.rGroup = g; store.set("ratesGroup", g); renderRates(); });
  const rows = d.rows.filter(r => state.rGroup === "all" || r.group === state.rGroup);
  const cols = [
    ["name", "Валюта"], ["ist", `Честная цена, ${esc(short(unit.code))}`, 1], ["ask", "Купить от", 1], ["bid", "Продать до", 1],
    ["spread", "Спред", 1], ["supply", "Предложение", 1], ["demand", "Спрос", 1], ["obs", "Сделок", 1], [null, "Последние сделки, за 1 шт."],
  ];
  const { key, dir } = state.mSort, missing = dir > 0 ? Infinity : -Infinity;
  rows.sort((a, b) => {
    const A = a[key] ?? missing, B = b[key] ?? missing;
    return (typeof A === "string" ? A.localeCompare(B) : A - B) * dir;
  });
  const el = $("market");
  el.innerHTML = `<thead><tr>${cols.map(([k, l, r]) => `<th class="${k ? "sortable" : ""} ${r ? "r" : ""}" data-k="${k || ""}">${l}${key === k ? (dir > 0 ? " ▲" : " ▼") : ""}</th>`).join("")}</tr></thead><tbody>` +
    rows.map(m => `<tr class="pick" data-t="${m.code}"${m.code === state.target ? ' style="background: var(--rune-soft)"' : ""}>
      <td>${esc(m.name)}</td>
      <td class="r num">${fB(m.ist)}</td>
      <td class="r num"><span class="quote">${link(m.askUrl, fB(m.ask))}${vsFair(m.askVsFair, false)}</span></td>
      <td class="r num"><span class="quote">${link(m.bidUrl, fB(m.bid))}${vsFair(m.bidVsFair, true)}</span></td>
      <td class="r num">${m.spread == null ? "—" : m.spread < 0 ? '<span class="pill good" title="Продать можно дороже, чем купить">арбитраж</span>' : Math.round(m.spread * 100) + "%"}</td>
      <td class="r num">${m.supply > 99999 ? (m.supply / 1e6).toFixed(0) + "M" : m.supply}</td>
      <td class="r num">${m.demand}</td>
      <td class="r num">${fmtN(m.obs, 0)}</td>
      <td class="muted" style="font-size:12px">${m.recent.map(r => `${fmtN(r.perUnit, 3)}× ${esc(short(r.code))}`).join(" · ")}</td></tr>`).join("") + "</tbody>";
  el.querySelector("thead").onclick = e => {
    const th = e.target.closest("th.sortable"); if (!th) return;
    const k = th.dataset.k;
    state.mSort = { key: k, dir: state.mSort.key === k ? -state.mSort.dir : (k === "name" ? 1 : -1) };
    renderRates();
  };
  const { codes, matrix } = d.cross, f = x => fmtN(x, x < 0.1 ? 3 : x < 10 ? 2 : 1);
  $("cross").innerHTML = `<thead><tr><th class="row">1 шт. →</th>${codes.map(c => `<th class="r${c === unit.code ? " base" : ""}">${esc(short(c))}</th>`).join("")}</tr></thead><tbody>` +
    codes.map((r, i) => `<tr><th class="row">${esc(short(r))}</th>${codes.map((c, j) => i === j ? '<td class="r self">1</td>'
      : `<td class="r num${c === unit.code ? " base" : ""}">${f(matrix[i][j])}</td>`).join("")}</tr>`).join("") + "</tbody>";
}
