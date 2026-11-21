// Tab "Недооценённые объявления".
import { api } from "../api.js";
import { $, esc, fmtD, fmtN, link } from "../fmt.js";
import { cached, latest, state } from "../state.js";

export const deals = () => cached("deals", () => api.deals(state.realm));

export async function renderDeals() {
  const ok = latest("deals");
  const ds = (await deals()).deals;
  if (!ok()) return;
  const el = $("deals");
  if (!ds.length) { el.innerHTML = `<tbody><tr><td class="empty">Ничего заметно недооценённого.</td></tr></tbody>`; return; }
  el.innerHTML = `<thead><tr><th>Тип</th><th>Отдаёшь</th><th>Получаешь</th><th class="r">Ist отдаёшь</th><th class="r">Ist получаешь</th><th class="r">Выгода</th><th></th></tr></thead><tbody>` +
    ds.map(d => `<tr><td><span class="tag">${d.type.toUpperCase()}</span>${d.unit ? ' <span class="tag">за шт.</span>' : ""}</td>
      <td class="give">${esc(fmtD(d.give))}</td><td class="get">${esc(fmtD(d.get))}</td>
      <td class="r num">${fmtN(d.giveIst, 3)}</td><td class="r num">${fmtN(d.getIst, 3)}</td>
      <td class="r"><span class="pill good">×${fmtN(d.ratio)}</span></td>
      <td>${link(d.url, "открыть")}</td></tr>`).join("") + "</tbody>";
}
