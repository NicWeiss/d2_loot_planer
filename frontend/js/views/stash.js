// Tab "Мой тайник": the session's stash, appraised by the backend for the current realm.
import { api } from "../api.js";
import { $, QUALITY, esc, fmtN, link, nm, plural, rlName, short, groupOf, GROUP_NAMES, vsFair, when } from "../fmt.js";
import { baseUnit, latest, state } from "../state.js";
import { renderBaseSelects } from "./rates.js";

export async function renderStash() {
  const ok = latest("stash");
  const [d, unit] = await Promise.all([api.stash(state.realm), baseUnit()]);
  if (!ok()) return;
  await renderBaseSelects();
  const a = d.appraisal;
  $("mine-body").hidden = !a;
  if (!d.loaded) {
    $("mine-sum").textContent = "Тайник не загружен: открой шестерёнку вверху справа — войди через «Обновить тайник» или загрузи файл выгрузки. Маршрут и подсказки посчитаются по твоему тайнику.";
    return;
  }
  if (!a) {
    $("mine-sum").textContent = `В тайнике ${d.owner} нет режима «${rlName(state.realm)}».`;
    return;
  }
  const B = ist => ist == null ? null : ist / unit.ist;
  const fB = ist => { const x = B(ist); return fmtN(x, x != null && Math.abs(x) < 1 ? 3 : 2); };
  const t = a.totals;
  $("mine-sum").innerHTML = `<b>${esc(d.owner)}</b> · ${d.source === "file" ? "из файла" : "с сайта"}, ${when(d.fetchedAt)} · ${rlName(state.realm)}. Суммы в ${esc(short(unit.code))}.`;
  const kpi = (l, ist, sub) => `<div class="kpi"><span class="label">${l}</span><span class="v">${fB(ist)}</span><span class="sub">${sub}</span></div>`;
  $("mine-kpis").innerHTML =
    kpi("Ходовая валюта", t.spendable, "Lem и выше, Deep shard: ими платят за высокие руны") +
    kpi("Вся валюта", t.currency, `${t.currencyPositions} ${plural(t.currencyPositions, "позиция", "позиции", "позиций")} по честной цене`) +
    kpi("Уники, сеты, рунворды", t.items, `${t.itemsCount} шт. по медиане продаж`) +
    kpi("Итого", t.total, t.unpriced ? `ещё ${t.unpriced} ${plural(t.unpriced, "вещь", "вещи", "вещей")} без оценки` : "всё оценено");
  const hl = c => c === state.target ? ' style="background: var(--rune-soft)"' : "";
  $("mine-cur").innerHTML = `<thead><tr><th>Валюта</th><th class="r">Кол-во</th><th class="r">За шт.</th><th class="r">Сумма</th><th class="r">Доля</th><th class="r">Продать сейчас, за шт.</th></tr></thead><tbody>` +
    a.currency.map(i => `<tr class="pick" data-t="${esc(i.code)}"${hl(i.code)}><td>${esc(nm(i.code))} <span class="muted" style="font-size:11px">${esc(GROUP_NAMES[groupOf(i.code)] || "")}</span></td>
      <td class="r num">${i.qty}</td><td class="r num">${fB(i.fair)}</td><td class="r num">${fB(i.ist)}</td>
      <td class="r num muted">${i.share ? Math.round(100 * i.share) + "%" : ""}</td>
      <td class="r num">${i.bid != null ? `<span class="quote">${link(i.bidUrl, fB(i.bid))}${vsFair(i.bidVsFair, true)}</span>` : '<span class="muted">никто не берёт</span>'}</td></tr>`).join("") + "</tbody>";
  $("mine-items").innerHTML = a.items.length ? `<thead><tr><th>Вещь</th><th class="r">Есть</th><th class="r">Медиана</th><th class="r">Сумма</th><th class="r">Продано</th><th class="r">В продаже</th><th class="r">Дешевле всех</th></tr></thead><tbody>` +
    a.items.map(i => `<tr class="pick" data-t="${esc(i.key)}"${hl(i.key)}><td>${esc(i.name)} <span class="muted" style="font-size:11px">${esc(QUALITY[i.quality] || i.quality)}</span></td><td class="r num">${i.have}</td>
      <td class="r num">${fB(i.median)}</td><td class="r num">${fB(i.ist)}</td><td class="r num">${i.sold}</td><td class="r num">${i.live}</td><td class="r num">${fB(i.cheapestAsk)}</td></tr>`).join("") + "</tbody>"
    : `<tbody><tr><td class="empty">Нет уников, сетов и рунвордов.</td></tr></tbody>`;
  $("mine-other").hidden = !a.other.length;
  $("mine-other-sum").textContent = `Вещи без оценки: ${t.unpriced} — магические, редкие, обычные; цена у них зависит от свойств`;
  $("mine-other-list").innerHTML = a.other.map(i => `<span class="chip">${i.have > 1 ? i.have + "× " : ""}${esc(i.name)} · ${esc(QUALITY[i.quality] || i.quality)}</span>`).join("");
}
