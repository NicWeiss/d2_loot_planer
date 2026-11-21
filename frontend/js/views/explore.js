// Tab "Карта обмена": target picker, target card, route from the stash, live offers, exchange map.
import { api } from "../api.js";
import { $, GROUP_NAMES, esc, fmtD, fmtN, fullName, itemLabel, label, link, nm, plural, pill, segButtons, short } from "../fmt.js";
import { cached, latest, state, targets } from "../state.js";

let ctx;  // { setTarget }
export const initExplore = c => {
  ctx = c;
  $("t-input").addEventListener("input", async e => { const c = await resolve(e.target.value); if (c && c !== state.target) ctx.setTarget(c, true); });
  $("t-input").addEventListener("change", async e => { const c = await resolve(e.target.value); if (c) ctx.setTarget(c); else e.target.value = await targetName(state.target); });
  $("t-qty").addEventListener("input", renderRoute);
  for (const id of ["q-stash", "q-keys", "q-pk1", "q-pk2", "q-pk3", "q-cube"]) $(id).addEventListener("change", renderRoute);
  $("q-effort").addEventListener("input", renderRoute);
  $("map-all").onchange = renderMap;
};

// ---- picker: names typed in the box resolve to codes through the realm's target list
async function targetIndex() {
  return cached("targetIndex", async () => {
    const t = await targets(), idx = new Map();
    for (const c of t.currencies) idx.set(c.display.replace(/ Rune$/, ""), c.code);
    for (const it of t.items) idx.set(itemLabel(it), it.code);
    $("t-list").innerHTML = [...idx.keys()].map(n => `<option value="${esc(n)}"></option>`).join("");
    return idx;
  });
}
async function targetName(code) {
  for (const [n, c] of await targetIndex()) if (c === code) return n;
  return fullName(code);
}
async function resolve(raw) {
  const idx = await targetIndex(), s = raw.trim().toLowerCase();
  if (idx.has(raw.trim())) return idx.get(raw.trim());
  for (const [n, c] of idx) if (n.toLowerCase() === s) return c;
  return null;
}
export async function renderPicker() {
  const inp = $("t-input");
  segButtons($("t-quick"), state.meta.quick.filter(c => state.meta.names[c]).map(c => [c, short(c)]), state.target, k => ctx.setTarget(k));
  if (document.activeElement !== inp) inp.value = await targetName(state.target);
}

// ---- target card
export async function renderTargetCard() {
  const ok = latest("card"), t = state.target;
  const d = await api.target(state.realm, t);
  if (!ok()) return;
  $("t-hint").textContent = d.known ? "" : "В этом режиме по цели нет данных.";
  const items = d.item ? { [t]: d.item } : {};
  const fair = d.fair;
  let sub, extra = "";
  if (d.isItem) {
    const it = d.item || {};
    sub = `${it.live ?? 0} в продаже · продано ${it.sold ?? 0}`;
    extra = `<span class="muted">У разных лотов «${esc(it.name || "")}» разные свойства. Медиана — по завершённым продажам вещей с этим названием; перед покупкой открой лот.</span>`;
  } else {
    sub = `${d.offers} живых предложений · ${d.acceptedBy} объявлений принимают в оплату`;
    const eq = d.equivalents.map(e => `<span class="chip">${fmtN(e.qty, e.qty < 10 ? 2 : 1)}× ${esc(short(e.code))}</span>`).join("");
    const recent = d.recent.map(r => `<tr><td class="num muted">${r.date ? new Date(r.date).toLocaleDateString("ru-RU", { day: "2-digit", month: "2-digit" }) : ""}</td>
      <td>${fmtN(r.perUnit, 3)}× ${esc(short(r.code))}</td><td class="r num">${fmtN(r.ist)}</td></tr>`).join("");
    extra = `${eq ? `<div><span class="label">В пересчёте</span><div class="equiv" style="margin-top:6px">${eq}</div></div>` : ""}
      ${recent ? `<div><span class="label">Последние сделки, за 1 шт.</span><div class="tbl-wrap" style="margin-top:6px"><table><tbody>${recent}</tbody></table></div></div>` : ""}`;
  }
  let cube = '<span class="muted">не крафтится</span>', cubeV = "—";
  if (d.cube) {
    cubeV = fmtN(d.cube.ist);
    const note = d.cube.complete ? "" : t.startsWith("r") ? " · камень не учтён" : " · не все ингредиенты оценены";
    cube = `<span class="muted">${esc(Object.entries(d.cube.recipe).map(([c, q]) => `${q} ${short(c)}`).join(" + "))}${note}</span> ${pill(d.cube.discount)}`;
  }
  const name = fullName(t, items);
  $("target-card").innerHTML = `
    <div class="target-top">
      <div class="glyph">${esc((d.item?.name || nm(t)).slice(0, 2))}</div>
      <div style="min-width:0"><h2>${esc(name)}</h2><span class="muted">${sub}${d.have ? ` · в тайнике ${d.have}` : ""}</span></div>
    </div>
    <div class="kpis">
      <div class="kpi"><span class="label">${d.isItem ? "Медиана продаж" : "Честная цена"}</span><span class="v">${fmtN(fair)}</span><span class="muted">${fair != null ? "Ist за 1 шт." : "мало сделок"}</span></div>
      <div class="kpi"><span class="label">Дешевле всех сейчас</span><span class="v">${d.best ? fmtN(d.best.per) : "—"}</span><span>${d.best ? `${pill(d.best.discount)} ${link(d.best.url, "лот")}` : '<span class="muted">нет лотов</span>'}</span></div>
      <div class="kpi"><span class="label">Через куб</span><span class="v">${cubeV}</span><span>${cube}</span></div>
    </div>
    ${extra}`;
}

// ---- offers
export async function renderOffers() {
  const ok = latest("offers"), t = state.target;
  const { offers, items } = await api.offers(state.realm, t);
  if (!ok()) return;
  $("offers-hint").textContent = t.startsWith("i:")
    ? "WTS — кто-то продаёт вещь с таким названием (взяты 4 самых дешёвых лота). Свойства у лотов разные."
    : "WTS — кто-то продаёт цель. WTB — кто-то покупает твой предмет и платит целью. Если в лоте есть ещё что-то, цена разделена по честной стоимости частей.";
  const el = $("offers");
  if (!offers.length) { el.innerHTML = `<tbody><tr><td class="empty">Живых предложений нет.</td></tr></tbody>`; return; }
  el.innerHTML = `<thead><tr><th>Тип</th><th>Отдаёшь</th><th>Получаешь</th><th class="r">Ist за 1 шт.</th><th class="r">Скидка</th><th></th></tr></thead><tbody>` +
    offers.map(o => `<tr>
      <td><span class="tag">${o.type.toUpperCase()}</span>${o.uses > 1 ? ` <span class="tag" title="Цена за штуку, столько в наличии">×${o.uses}</span>` : ""}${o.check ? ' <span class="tag">требования</span>' : ""}</td>
      <td class="give">${esc(fmtD(o.give, items))}</td><td class="get">${esc(fmtD(o.get, items))}</td>
      <td class="r num">${fmtN(o.per)}</td><td class="r">${pill(o.discount)}</td>
      <td>${link(o.url, "открыть")}</td></tr>`).join("") + "</tbody>";
}

// ---- route from the stash
export async function renderRoute() {
  const ok = latest("route"), t = state.target;
  const qty = Math.max(1, Math.min(20, +$("t-qty").value || 1));
  const keys = $("q-keys").checked ? ["pk1", "pk2", "pk3"].filter(c => $("q-" + c).checked) : [];
  const t0 = performance.now();
  let d;
  try {
    d = await api.route(state.realm, { target: t, qty, stashMode: $("q-stash").value, keys, useCube: $("q-cube").checked,
                                        effort: Math.max(0, Math.min(1, +$("q-effort").value || 0)) });
  } catch (e) {
    if (ok()) $("route-out").innerHTML = `<div class="target"><span class="err">${esc(e.message)}</span></div>`;
    return;
  }
  if (!ok()) return;
  const ms = Math.round(performance.now() - t0), res = d.route, items = d.items, out = $("route-out");
  const name = label(t, items), fair = d.fair;
  const fairKeys = d.keysFair.length ? `<div style="display:grid; gap:6px"><span class="label">Честная цена ${qty}× ${esc(name)} в ключах, для своего WTB</span><div class="equiv">${d.keysFair.map(k => `<span class="chip">${fmtN(k.qty, 1)}× ${esc(short(k.code))}</span>`).join("")}</div></div>` : "";
  const noStash = !d.stash.loaded ? `<span class="muted">Тайник не загружен, маршрут строится без него. Загрузи его через шестерёнку вверху справа.</span>`
    : !d.stash.realm ? `<span class="muted">В тайнике ${esc(d.stash.owner)} нет этого режима, маршрут строится без него.</span>` : "";

  if (res.reason === "unreachable") {
    out.innerHTML = `<div class="target"><h3>Из выбранных ресурсов не достать</h3>${noStash}
      <span class="muted">Ни одна цепочка живых лотов${$("q-cube").checked ? " и крафта" : ""} не ведёт к цели. Попробуй взять весь тайник или включить ключи без ограничений. Для высоких рун мостом служит куб (2 Vex + Emerald → Ohm и выше), а камни на рынке не продают.</span>${fairKeys}</div>`;
    return;
  }
  if (res.reason === "have") {
    out.innerHTML = `<div class="target"><span class="muted">Цель — это ресурс, который ты отметил как неограниченный.</span></div>`;
    return;
  }
  const p = res.paper, over = p.steps.filter(s => s.available != null && s.times > s.available);
  const row = (s, showAvail) => {
    const bad = showAvail && s.available != null && s.times > s.available;
    return `<tr${bad ? ' style="background: var(--bad-soft)"' : ""}>
      <td><span class="tag"${s.type === "cube" ? ' style="color: var(--rune); border-color: var(--rune)"' : ""}>${s.type === "cube" ? "КУБ" : s.type.toUpperCase()}</span>${s.check ? ' <span class="tag" title="У покупателя есть требования к свойствам">требования</span>' : ""}</td>
      <td class="give">${esc(fmtD(s.give, items))}</td><td class="get">${esc(fmtD(s.get, items))}</td>
      <td class="r num">${s.times}</td>${showAvail ? `<td class="r num">${s.available == null ? "∞" : s.available}</td>` : ""}
      <td>${s.url ? link(s.url, "лот") : ""}</td></tr>`;
  };
  const head = showAvail => `<thead><tr><th></th><th>Отдаёшь</th><th>Получаешь</th><th class="r">Раз</th>${showAvail ? '<th class="r">Лотов есть</th>' : ""}<th></th></tr></thead>`;
  const verdict = res.ok
    ? `<span class="pill good">собирается: ${res.steps.length} ${plural(res.steps.length, "сделка", "сделки", "сделок")}</span>`
    : `<span class="pill bad">реально не собрать</span>`;
  const count = dd => Object.values(dd).reduce((s, x) => s + x, 0);
  const keysTxt = dd => count(dd) ? `${count(dd)} ${plural(count(dd), "ключ", "ключа", "ключей")}` : "";
  const spent = [fmtD(res.poolUsed, items), count(res.unlimitedSpent) ? "ключи: " + fmtD(res.unlimitedSpent, items) : ""].filter(Boolean).join(" · ");
  const bott = res.bottlenecks.map(b => b.kind === "stash"
    ? `не хватает <b>${fmtN(b.missing)}× ${esc(label(b.item, items))}</b>`
    : `лот «${esc(fmtD(b.step.give, items))} → ${esc(fmtD(b.step.get, items))}» нужен ${b.step.times} раз, а есть ${b.step.available}`);
  out.innerHTML = `
    <div class="target">
      <div style="display:flex; flex-wrap:wrap; gap:8px 16px; align-items:baseline; justify-content:space-between">
        <h3>${qty}× ${esc(name)}</h3>${verdict}
      </div>
      ${noStash}
      <div class="kpis">
        <div class="kpi"><span class="label">Реально, чистыми</span><span class="v">${res.ok ? fmtN(res.costIst - d.change) : "—"}</span><span class="muted">${res.ok ? `Ist: отдашь ${fmtN(res.costIst)}${d.change > 0.005 ? `, сдача ${fmtN(d.change)}` : ""}${keysTxt(res.unlimitedSpent) ? `, ${keysTxt(res.unlimitedSpent)}` : ""}` : "лотов не хватает"}</span></div>
        <div class="kpi"><span class="label">На бумаге</span><span class="v">${fmtN(p.costIst)}</span><span class="muted">Ist${keysTxt(p.unlimitedSpent) ? `, ${keysTxt(p.unlimitedSpent)}` : ""}</span></div>
        <div class="kpi"><span class="label">${t.startsWith("i:") ? "Медиана продаж" : "Честная цена"}</span><span class="v">${fair != null ? fmtN(qty * fair) : "—"}</span><span class="muted">${fair != null ? "Ist" : "мало сделок"}</span></div>
      </div>
      ${res.ok ? `<span class="muted" style="font-size:13px">Отдаёшь: ${esc(spent) || "ничего"}${Object.keys(res.leftover).length ? `. Сдача: ${esc(fmtD(res.leftover, items))}` : ""}.</span>` : ""}
      ${!res.ok && bott.length ? `<span class="muted">Узкие места: ${bott.slice(0, 4).join("; ")}${bott.length > 4 ? ` и ещё ${bott.length - 4}` : ""}.</span>` : ""}
      ${fairKeys}
      <span class="muted" style="font-size:11px">Посчитано за ${ms} мс.</span>
    </div>
    ${res.ok ? `<div style="display:grid; gap:8px"><span class="label">Реальная цепочка, по порядку</span>
      <div class="tbl-wrap"><table>${head(false)}<tbody>${res.steps.map(s => row(s, false)).join("")}</tbody></table></div></div>` : ""}
    <details${res.ok ? "" : " open"}><summary>Цепочка на бумаге (${p.steps.length} ${plural(p.steps.length, "шаг", "шага", "шагов")}${over.length ? `, ${over.length} ${plural(over.length, "лот нужен", "лота нужны", "лотов нужны")} чаще, чем есть` : ""})</summary>
      <div class="tbl-wrap" style="margin-top:8px"><table>${head(true)}<tbody>${p.steps.map(s => row(s, true)).join("")}</tbody></table></div>
      ${Object.keys(p.missing).length ? `<span class="give" style="font-size:12px">Не из чего сделать: ${esc(fmtD(p.missing, items))}</span>` : ""}
    </details>`;
}

// ---- exchange map: layout only; nodes and edges (with fair rates) come from the API
export async function renderMap() {
  const ok = latest("map");
  const data = await cached("map", () => api.map(state.realm));
  if (!ok()) return;
  const svg = $("map"), target = state.target;
  const mk = Object.fromEntries(data.nodes.map(n => [n.code, n]));
  const codes = Object.keys(mk), v = c => mk[c].ist;
  if (!codes.length) { svg.innerHTML = ""; return; }
  const W = 1000, padL = 118, padR = 40, laneH = 66, top = 52;
  const lo = Math.log10(Math.min(...codes.map(v))), hi = Math.log10(Math.max(...codes.map(v)));
  const x = c => padL + (Math.log10(v(c)) - lo) / (hi - lo || 1) * (W - padL - padR);
  const lanes = state.meta.groups.filter(g => codes.some(c => mk[c].group === g)).map(g => [g, GROUP_NAMES[g] || g]);
  const H = top + lanes.length * laneH + 30;
  svg.setAttribute("viewBox", `0 0 ${W} ${H}`);
  const pos = {};
  lanes.forEach(([g], i) => {
    let lastX = -1e9, flip = 0;
    for (const c of codes.filter(c => mk[c].group === g).sort((a, b) => v(a) - v(b))) {
      const px = x(c);
      flip = px - lastX < 46 ? (flip + 1) % 3 : 0;
      lastX = px;
      pos[c] = [px, top + i * laneH + laneH / 2 + (flip === 1 ? -14 : flip === 2 ? 14 : 0)];
    }
  });
  const maxAcc = Math.max(...codes.map(c => mk[c].acceptedBy + mk[c].supply));
  const rad = c => 5 + 13 * Math.sqrt((mk[c].acceptedBy + Math.min(mk[c].supply, 200)) / (maxAcc || 1));
  let s = "";
  for (let p = Math.ceil(lo * 2) / 2; p <= hi; p += 0.5) {
    const px = padL + (p - lo) / (hi - lo) * (W - padL - padR);
    s += `<line x1="${px}" x2="${px}" y1="${top - 8}" y2="${H - 26}" stroke="var(--line)" stroke-dasharray="2 4"/>`;
    s += `<text x="${px}" y="${top - 14}" text-anchor="middle" font-size="11" fill="var(--muted)">${fmtN(10 ** p, 2)} Ist</text>`;
  }
  lanes.forEach(([, l], i) => {
    const y = top + i * laneH;
    if (i) s += `<line x1="0" x2="${W}" y1="${y}" y2="${y}" stroke="var(--line)"/>`;
    s += `<text x="8" y="${y + laneH / 2 + 4}" font-size="12" fill="var(--muted)" font-weight="600">${l}</text>`;
  });
  const showAll = $("map-all").checked, f = pos[target] ? target : null;
  const edges = data.edges.filter(e => showAll || e.from === f || e.to === f);
  const linked = new Set(edges.flatMap(e => [e.from, e.to]));
  edges.forEach((e, i) => {
    const [x1, y1] = pos[e.from], [x2, y2] = pos[e.to];
    const qq = e.fairRate ? e.fairRate / e.rate : 1;
    const col = qq > 1.05 ? "var(--good)" : qq < 0.95 ? "var(--bad)" : "var(--edge)";
    const mx = (x1 + x2) / 2, my = (y1 + y2) / 2, dx = x2 - x1, dy = y2 - y1;
    const cx = mx - dy * 0.12, cy = Math.max(top - 4, Math.min(H - 30, my + dx * 0.12));
    const dd = `M${x1},${y1} Q${cx},${cy} ${x2},${y2}`;
    s += `<path class="edge" d="${dd}" stroke="${col}" stroke-width="${1.2 + Math.min(e.n, 6) * 0.4}" opacity="${showAll ? 0.35 : 0.85}" marker-end="url(#arr-${col.includes("good") ? "g" : col.includes("bad") ? "b" : "n"})"/>`;
    s += `<path class="edge-hit" d="${dd}" data-e="${i}"/>`;
  });
  const showLabel = (c, dim) => c === f || (showAll ? mk[c].acceptedBy >= 300 || /^r(2|3)/.test(c) : !dim);
  for (const c of codes) {
    const [px, py] = pos[c], isT = c === f, dim = !showAll && f && !linked.has(c) && c !== f;
    s += `<g class="node${isT ? " focus" : ""}${dim ? " dim" : ""}" data-c="${c}" tabindex="0" role="button" aria-label="${esc(nm(c))}">
      <circle cx="${px}" cy="${py}" r="${rad(c)}" fill="${isT ? "var(--rune)" : "var(--edge)"}"/>
      ${showLabel(c, dim) ? `<text x="${px}" y="${py - rad(c) - 4}" text-anchor="middle" font-size="${isT ? 13 : 11}" font-weight="${isT ? 700 : 500}" fill="${isT ? "var(--rune)" : "var(--fg)"}" paint-order="stroke" stroke="var(--panel)" stroke-width="3">${esc(short(c))}</text>` : ""}<title>${esc(nm(c))}</title></g>`;
  }
  const marker = (id, col) => `<marker id="arr-${id}" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse"><path d="M0,0 L10,5 L0,10 z" fill="${col}"/></marker>`;
  svg.innerHTML = `<defs>${marker("g", "var(--good)")}${marker("b", "var(--bad)")}${marker("n", "var(--edge)")}</defs>` + s;

  const tip = $("map-tip");
  const nodeTip = c => { const m = mk[c]; return `<b>${esc(nm(c))}</b> · ${fmtN(m.ist, 3)} Ist · в продаже ${m.supply} · ищут ${m.demand} · принимают в оплату ${m.acceptedBy} объявлений`; };
  tip.innerHTML = f ? nodeTip(f) + ` <span class="muted">— наведи на стрелку, чтобы увидеть курс</span>` : `<span class="muted">Цели нет на карте валют: это вещь. Включи «показать все связи» или выбери валюту.</span>`;
  svg.onclick = e => {
    const g = e.target.closest(".node");
    if (g) { ctx.setTarget(g.dataset.c); return; }
    const h = e.target.closest(".edge-hit");
    if (h) window.open(edges[+h.dataset.e].url, "_blank", "noopener");
  };
  svg.onkeydown = e => { const g = e.target.closest(".node"); if (g && (e.key === "Enter" || e.key === " ")) { e.preventDefault(); ctx.setTarget(g.dataset.c); } };
  svg.onmouseover = e => {
    const h = e.target.closest(".edge-hit"), g = e.target.closest(".node");
    if (h) {
      const ed = edges[+h.dataset.e], qq = ed.fairRate ? ed.fairRate / ed.rate : null;
      tip.innerHTML = `Отдаёшь <b>${esc(nm(ed.from))}</b>, получаешь <b>${esc(nm(ed.to))}</b>: лот ${fmtN(ed.give)}→${fmtN(ed.get)}, курс ${fmtN(ed.rate, 3)} за 1 · честный ${fmtN(ed.fairRate, 3)} ${qq ? pill(1 - 1 / qq) : ""} · таких объявлений ${ed.n} · клик открывает лот`;
    } else if (g) tip.innerHTML = nodeTip(g.dataset.c);
  };
}

export async function renderExplore() {
  await Promise.all([renderPicker(), renderTargetCard(), renderRoute(), renderOffers(), renderMap()]);
}
