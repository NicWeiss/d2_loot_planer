// Entry point: header, tabs, the data menu (market refresh, stash login/upload/reset) and wiring of the views.
import { api } from "./js/api.js";
import { $, esc, rlName, segButtons, setNames, store, when } from "./js/fmt.js";
import { TABS, state, targets } from "./js/state.js";
import { deals, renderDeals } from "./js/views/deals.js";
import { initExplore, renderExplore, renderMap, renderOffers, renderPicker, renderRoute, renderStrategy, renderTargetCard } from "./js/views/explore.js";
import { initRates, renderRates } from "./js/views/rates.js";
import { renderStash } from "./js/views/stash.js";

let busy = false, sawRunning = false;

// ---- rendering
function renderHeader() {
  const realm = state.meta.realms.find(r => r.key === state.realm), c = realm?.counts, s = state.meta.season;
  const season = s && realm?.key.endsWith(" ladder")
    ? ` · сезон с ${new Date(s.start).toLocaleDateString("ru-RU", { day: "numeric", month: "short" })}, день ${Math.floor((Date.now() - Date.parse(s.start)) / 864e5) + 1}`
    : "";
  $("stamp").textContent = state.meta.fetchedAt
    ? `Снимок ${new Date(state.meta.fetchedAt).toLocaleString("ru-RU", { dateStyle: "medium", timeStyle: "short" })}${c ? ` · ${c.active} живых и ${c.completed} завершённых сделок валюта-на-валюту` : ""}${season}`
    : "Снимка рынка ещё нет";
  segButtons($("realm-seg"), state.meta.realms.map(r => [r.key, rlName(r.key) + (state.stash?.realms?.includes(r.key) ? " · мой тайник" : "")]),
    state.realm, k => { state.realm = k; render(); });
}

async function renderTabs() {
  for (const b of document.querySelectorAll("#tabs button")) {
    const on = b.dataset.tab === state.tab;
    b.setAttribute("aria-selected", on);
    b.tabIndex = on ? 0 : -1;
  }
  for (const p of document.querySelectorAll("[data-panel]")) p.hidden = p.dataset.panel !== state.tab;
  $("tab-stash").innerHTML = `Мой тайник${state.stash?.loaded ? "" : '<span class="count">не загружен</span>'}`;
  const n = (await deals()).deals.length;
  $("tab-deals").innerHTML = `Недооценённые объявления<span class="count">${n}</span>`;
}

// only the visible tab is drawn
function renderPanel() {
  const show = { map: renderExplore, stash: renderStash, rates: renderRates, deals: renderDeals, about: async () => {} }[state.tab];
  return show().catch(showError);
}

function render() {
  renderHeader();
  renderMenu();
  renderTabs().catch(showError);
  return renderPanel();
}

function showError(e) {
  console.error(e);
  const panel = document.querySelector(`[data-panel="${state.tab}"]`);
  let box = panel.querySelector(".load-error");
  if (!box) { box = document.createElement("p"); box.className = "err load-error"; panel.prepend(box); }
  box.textContent = "Не удалось загрузить: " + e.message;
}

function setTarget(code, fromInput = false) {
  state.target = code;
  store.set("target", code);
  if (state.tab !== "map") return showTab("map");
  if (!fromInput) renderPicker();
  Promise.all([renderTargetCard(), renderRoute(), renderStrategy(), renderOffers(), renderMap()]).catch(showError);
}

function showTab(t) {
  state.tab = t;
  store.set("tab", t);
  try { history.replaceState(null, "", "#" + t); } catch {}
  renderTabs().catch(showError);
  return renderPanel();
}

// ---- data menu
function renderMenu() {
  const job = state.meta.job;
  const auto = state.meta.refreshHours > 0 ? `Обновляется автоматически раз в ${+state.meta.refreshHours.toFixed(1)} ч.` : "";
  $("mk-stamp").textContent = (state.meta.fetchedAt ? `Снимок от ${when(state.meta.fetchedAt)}. ` : "Снимка ещё нет. ") + auto;
  $("mk-refresh").closest(".row").hidden = !state.meta.admin;  // refreshing by hand is for the admin only
  $("mk-refresh").disabled = !!job?.running;
  $("mk-status").innerHTML = job?.running ? `обновляю… <span class="num">${esc(job.log.at(-1) || "")}</span>`
    : job?.error ? `<span class="err">не вышло: ${esc(job.error)}</span>` : "";
  $("mk-status").hidden = !$("mk-status").innerHTML;
  const s = state.stash;
  $("st-who").innerHTML = s?.loaded
    ? `<b>${esc(s.owner)}</b> <span class="muted">· ${s.source === "file" ? "из файла" : "с сайта"}, ${when(s.fetchedAt)}${s.remembered ? " · вход запомнен" : ""}</span>`
    : '<span class="muted">не загружен</span>';
  $("st-refresh").disabled = busy;
  $("st-reset").disabled = !s?.loaded || busy;
  $("st-status").hidden = !$("st-status").innerHTML;
}
const stashMsg = html => { $("st-status").innerHTML = html; renderMenu(); };

async function loadStashHeader() {
  state.stash = await api.stash(state.realm);
  state.meta.admin = !!state.stash.admin;  // signing in or resetting may change it
  if (state.stash.loaded && !state.stash.realms.includes(state.realm)) {
    const r = state.stash.realms.find(k => state.meta.realms.some(x => x.key === k));
    if (r) state.realm = r;
  }
}
async function stashChanged(warning = "") {
  await loadStashHeader();
  stashMsg(warning ? `<span class="err">${esc(warning)}</span>` : "");
  return render();
}

async function pollMarket() {
  try { state.meta.job = (await api.marketStatus()).job; } catch (e) { state.meta.job = { running: false, error: e.message, log: [] }; }
  if (state.meta.job.running) { sawRunning = true; setTimeout(pollMarket, 2000); }
  else if (sawRunning && !state.meta.job.error) { $("mk-status").textContent = "готово, перезагружаю…"; location.reload(); return; }
  renderMenu();
}

function initMenu() {
  const menu = $("menu"), gear = $("gear"), dlg = $("login"), form = dlg.querySelector("form");
  const toggle = (open = menu.hidden) => { menu.hidden = !open; gear.setAttribute("aria-expanded", open); if (open) renderMenu(); };
  gear.onclick = e => { e.stopPropagation(); toggle(); };
  document.addEventListener("click", e => { if (!menu.hidden && !menu.contains(e.target) && !e.target.closest("dialog") && !dlg.open) toggle(false); });
  document.addEventListener("keydown", e => { if (e.key === "Escape" && !menu.hidden && !dlg.open) { toggle(false); gear.focus(); } });

  $("mk-refresh").onclick = async () => {
    try { state.meta.job = (await api.refreshMarket()).job; sawRunning = true; setTimeout(pollMarket, 2000); }
    catch (e) { state.meta.job = { ...state.meta.job, running: false, error: e.message }; }
    renderMenu();
  };

  const openLogin = (why = "") => {
    form.reset();
    $("login-msg").hidden = true;
    $("login-why").textContent = why || "Без «Запомнить» сервер выгружает тайник и сразу выходит из аккаунта на сайте. С «Запомнить» он хранит куки сайта, чтобы потом обновлять тайник без пароля. Пароль не сохраняется никогда.";
    dlg.showModal();
  };
  $("st-refresh").onclick = async () => {
    if (!state.stash?.remembered) return openLogin();
    busy = true; stashMsg("обновляю тайник…");
    try { await stashChanged((await api.refreshStash()).warning || ""); }
    catch (e) {
      stashMsg("");
      if (e.status === 401) { await loadStashHeader(); openLogin(e.message[0].toUpperCase() + e.message.slice(1) + "."); }
      else stashMsg(`<span class="err">${esc(e.message)}</span>`);
    } finally { busy = false; renderMenu(); }
  };
  $("login-cancel").onclick = () => dlg.close();
  dlg.addEventListener("close", () => form.reset());
  form.onsubmit = async e => {
    e.preventDefault();
    const f = new FormData(form), buttons = form.querySelectorAll("button");
    buttons.forEach(b => b.disabled = true);
    Object.assign($("login-msg"), { hidden: false, className: "muted", textContent: "вхожу и выгружаю тайник…" });
    try {
      await api.login(f.get("login"), f.get("password"), f.get("remember") === "on");
      dlg.close();
      await stashChanged();
    } catch (err) {
      Object.assign($("login-msg"), { className: "err", textContent: err.message });
    } finally { buttons.forEach(b => b.disabled = false); }
  };
  $("st-reset").onclick = async () => {
    try { await api.resetStash(); await stashChanged(); stashMsg("тайник сброшен"); }
    catch (e) { stashMsg(`<span class="err">${esc(e.message)}</span>`); }
  };
  $("st-file-btn").onclick = () => $("st-file").click();
  $("st-file").onchange = async e => {
    const file = e.target.files[0];
    e.target.value = "";
    if (!file) return;
    try { await api.uploadStash(file.name.replace(/\.json$/i, ""), JSON.parse(await file.text())); await stashChanged(); }
    catch (err) { stashMsg(`<span class="err">не удалось прочитать файл: ${esc(err.message)}</span>`); }
  };
}

// ---- start
async function main() {
  state.meta = await api.meta();
  if (!state.meta.ready) {
    // first start: the backend is fetching the market; wait for it
    document.querySelector(".wrap").insertAdjacentHTML("afterbegin", '<p class="muted" id="warming">Рынок загружается с сайта, это займёт несколько минут. Страница обновится сама.</p>');
    sawRunning = true;
    return pollMarket();
  }
  setNames(state.meta.names);
  state.realm = state.meta.defaultRealm;
  await loadStashHeader();
  initExplore({ setTarget });
  initRates(() => renderPanel());
  initMenu();
  $("tabs").addEventListener("click", e => { const b = e.target.closest("button[data-tab]"); if (b) showTab(b.dataset.tab); });
  $("tabs").addEventListener("keydown", e => {
    const i = TABS.indexOf(state.tab), d = { ArrowRight: 1, ArrowLeft: -1 }[e.key];
    if (d) { showTab(TABS[(i + d + TABS.length) % TABS.length]); $("tab-" + state.tab).focus(); }
  });
  addEventListener("hashchange", () => { const t = location.hash.slice(1); if (TABS.includes(t) && t !== state.tab) showTab(t); });
  for (const id of ["market", "mine-cur", "mine-items"]) {
    $(id).addEventListener("click", e => {
      if (e.target.closest("a")) return;
      const tr = e.target.closest("tr.pick");
      if (tr) { setTarget(tr.dataset.t); scrollTo({ top: 0, behavior: "smooth" }); }
    });
  }
  if (state.meta.job?.running) { sawRunning = true; setTimeout(pollMarket, 2000); }
  targets().catch(() => {});  // warm the picker list
  await render();
}

main().catch(e => {
  console.error(e);
  document.querySelector(".wrap").insertAdjacentHTML("afterbegin", `<p class="err">Сервер недоступен: ${esc(e.message)}</p>`);
});
