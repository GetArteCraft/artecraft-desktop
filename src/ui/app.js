"use strict";
/* ============================================================================
   ArteCraft Launcher · UI logic
   Ни одной строки интерфейса здесь нет: всё через t('ключ') из i18n.js.
   ========================================================================== */

var S = {
  status: null,
  view: "discover",
  query: "",
  locale: "en",
  theme: "dark",
  market: null,
  marketLoading: false,
  marketFilters: { q: "", sort: "popular", platform: "all", verifiedOnly: false },
  account: { signedIn: false },
  myOs: "linux"
};

/* ---------- i18n ---------- */
function t(key, vars) { return I18N ? tf(S.locale, key, vars) : key; }
function localeInfo(code) { return I18N.meta(code); }

function applyLocale(code, persist) {
  S.locale = I18N.normalize(code) || "en";
  var meta = localeInfo(S.locale);
  document.documentElement.lang = S.locale;
  document.documentElement.dir = meta.dir;
  try { localStorage.setItem("artecraft.locale", S.locale); } catch (e) {}
  document.querySelectorAll("[data-i18n]").forEach(function (n) {
    var key = n.getAttribute("data-i18n");
    var attr = n.getAttribute("data-i18n-attr");
    if (attr) n.setAttribute(attr, t(key));
    else n.textContent = t(key);
  });
  var flagEl = document.querySelector("#langFlag");
  var nameEl = document.querySelector("#langName");
  if (flagEl) flagEl.textContent = S.locale.toUpperCase();
  if (nameEl) nameEl.textContent = meta.native;
  var bs = document.querySelector("#brandSub");
  if (bs) bs.setAttribute("data-brand", "1");
  if (persist) {
    API.post("/api/settings", { locale: S.locale }).catch(function () {});
  }
}

/* ---------- api ---------- */
var API = {
  get: async function (url) {
    var r = await fetch(url, { headers: { "Cache-Control": "no-store" } });
    if (!r.ok) throw new Error(await errText(r));
    return r.json();
  },
  post: async function (url, body) {
    var r = await fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body || {})
    });
    if (!r.ok) throw new Error(await errText(r));
    return r.json();
  }
};
/* Серверные ошибки приходят ключом i18n (errorKey) плюс техническая
   подробность (errorDetail). Так при немецком интерфейсе не всплывает
   русская строка из Go-кода. */
function srvErrorText(j, status) {
  if (j && j.errorKey) {
    var base = t(j.errorKey);
    return j.errorDetail ? base + " · " + j.errorDetail : base;
  }
  if (j && j.error) return j.error;
  return "HTTP " + status;
}
function srvText(j) {
  if (!j) return "";
  if (j.messageKey) return t(j.messageKey);
  if (j.message) return j.message;
  return "";
}
async function errText(r) {
  try { var j = await r.json(); return srvErrorText(j, r.status); }
  catch (e) { return "HTTP " + r.status; }
}

/* ---------- утилиты ---------- */
function $(sel, root) { return (root || document).querySelector(sel); }
function $$(sel, root) { return Array.prototype.slice.call((root || document).querySelectorAll(sel)); }
function el(tag, cls, html) {
  var n = document.createElement(tag);
  if (cls) n.className = cls;
  if (html != null) n.innerHTML = html;
  return n;
}
function esc(s) {
  return String(s == null ? "" : s).replace(/[&<>"']/g, function (c) {
    return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
  });
}
function num(n) { try { return new Intl.NumberFormat(S.locale).format(n || 0); } catch (e) { return String(n || 0); } }
function mb(n) {
  if (!n) return "—";
  return n >= 1024 ? (n / 1024).toFixed(2) + " GB" : Math.round(n) + " MB";
}
function initials(s) {
  return (String(s || "?").replace(/[^\p{L}\p{N}]+/gu, "").slice(0, 2) || "?").toUpperCase();
}
function toast(msg, kind) {
  var tst = el("div", "toast " + (kind || "ok"), esc(msg));
  $("#toasts").appendChild(tst);
  setTimeout(function () {
    tst.style.opacity = "0"; tst.style.transform = "translateY(6px)";
    setTimeout(function () { tst.remove(); }, 200);
  }, 4200);
}
var WASM = function (slug) { return "https://getartecraft.com/en/online/" + slug; };

/* ---------- загрузка статуса ---------- */
async function refresh() {
  try {
    var st = await API.get("/api/status");
    S.status = st;
    S.account = st.siteAccount || { signedIn: false };
    S.myOs = String(st.os || "linux").toLowerCase();
    document.body.classList.toggle("low-spec", !!st.lowSpecMode);
    if (st.locale && I18N.normalize(st.locale) && st.locale !== S.locale && !S.localeTouched) {
      applyLocale(st.locale, false);
    }
    $("#cntApps").textContent = (st.catalog || []).length;
    $("#cntLib").textContent = Object.keys(st.installed || {}).length + Object.keys(st.custom || {}).length;
    $("#cntDl").textContent = Object.keys(st.tasks || {}).length;
    $("#footStatus").textContent = st.osLabel + " · " + st.arch + " · v" + st.launcherVersion;
    $("#brandSub").textContent = t("brand.tagline") + " · " + (st.osLabel || "");
    renderAccount();
    updateDownloadBadge();
    renderLauncherUpdate(st.selfUpdate || {});
  } catch (e) {
    $("#footStatus").textContent = "offline";
  }
}
function updateDownloadBadge() {
  var tasks = (S.status && S.status.tasks) || {};
  var active = Object.keys(tasks).filter(function (k) {
    return ["download", "extracting", "queued"].indexOf(tasks[k].stage) >= 0;
  }).length;
  $("#cntDl").textContent = active || Object.keys(tasks).length;
}
function renderAccount() {
  var a = S.account || {};
  $("#accAvatar").textContent = a.signedIn ? initials(a.name || a.email) : "?";
  $("#accName").textContent = a.signedIn ? (a.name || String(a.email || "").split("@")[0]) : t("acc.guest");
  $("#accMail").textContent = a.signedIn ? (a.handle ? "@" + a.handle : a.email) : t("acc.signIn");
}

/* ---------- роутер ---------- */
var VIEWS = {};
function go(view) {
  S.view = view;
  $$(".nav-item").forEach(function (b) { b.classList.toggle("active", b.dataset.nav === view); });
  render();
  var v = $("#view"); if (v) v.scrollTop = 0;
}
function render() {
  var map = {
    discover: ["title.discover", "sub.discover"],
    apps: ["title.apps", "sub.apps"],
    library: ["title.library", "sub.library"],
    market: ["title.market", "sub.market"],
    downloads: ["title.downloads", "sub.downloads"],
    settings: ["title.settings", "sub.settings"]
  };
  var pair = map[S.view] || map.discover;
  $("#pageTitle").textContent = t(pair[0]);
  $("#pageSub").textContent = t(pair[1]);
  var v = $("#view");
  // Позиция прокрутки переживает перерисовку: раньше любой refresh (раз в 1.5 с)
  // сбрасывал список в начало, и казалось, что прокрутка «не работает».
  var keepTop = v.scrollTop;
  var sameView = v.getAttribute("data-view") === S.view;
  v.innerHTML = "";
  v.setAttribute("dir", localeInfo(S.locale).dir);
  v.setAttribute("data-view", S.view);
  (VIEWS[S.view] || VIEWS.discover)(v);
  if (sameView && keepTop > 0) {
    v.scrollTop = keepTop;
    requestAnimationFrame(function () { v.scrollTop = keepTop; });
  }
}
function sectionTitle(title, sub) {
  var d = el("div", "section");
  d.style.marginTop = "18px";
  d.innerHTML = '<div class="section-title">' + esc(title) + "</div>" +
    (sub ? '<div class="section-sub">' + esc(sub) + "</div>" : "");
  return d;
}

/* ---------- модальное окно (шапка/подвал закреплены, скроллится тело) ---------- */
function openModal(opts) {
  closeModal();
  var back = el("div", "modal-back");
  var box = el("div", "modal");
  var head = el("div", "modal-head");
  head.innerHTML = '<div><div style="font-weight:650;font-size:15px">' + esc(opts.title || "") + "</div>" +
    (opts.sub ? '<div style="font-size:11.5px;color:var(--t3);margin-top:3px">' + esc(opts.sub) + "</div>" : "") + "</div>";
  var x = el("button", "modal-x", "&times;");
  x.setAttribute("aria-label", t("common.close"));
  x.addEventListener("click", closeModal);
  head.appendChild(x);
  var body = el("div", "modal-body");
  if (typeof opts.body === "string") body.innerHTML = opts.body; else if (opts.body) body.appendChild(opts.body);
  var foot = el("div", "modal-foot");
  (opts.actions || []).forEach(function (a) {
    var b = el("button", "btn " + (a.kind || ""), esc(a.label));
    b.addEventListener("click", a.onClick);
    foot.appendChild(b);
  });
  box.appendChild(head); box.appendChild(body);
  if ((opts.actions || []).length) box.appendChild(foot);
  back.appendChild(box);
  back.addEventListener("click", function (e) { if (e.target === back) closeModal(); });
  $("#modalRoot").appendChild(back);
  document.addEventListener("keydown", escClose);
  return { back: back, box: box, body: body, foot: foot };
}
function closeModal() {
  $("#modalRoot").innerHTML = "";
  document.removeEventListener("keydown", escClose);
}
function escClose(e) { if (e.key === "Escape") closeModal(); }

/* ---------- поле с валидацией ---------- */
function field(opts) {
  var wrap = el("label", "field");
  wrap.innerHTML = '<span>' + esc(opts.label) + "</span>";
  var input;
  if (opts.textarea) { input = el("textarea", "input"); input.rows = opts.rows || 3; }
  else { input = el("input", "input"); input.type = opts.type || "text"; }
  input.value = opts.value || "";
  if (opts.placeholder) input.placeholder = opts.placeholder;
  if (opts.id) input.id = opts.id;
  wrap.appendChild(input);
  if (opts.hint) wrap.appendChild(el("div", "hint", esc(opts.hint)));
  var msg = el("div", "msg");
  wrap.appendChild(msg);
  function setState(state, text) {
    wrap.classList.remove("invalid", "valid");
    if (state) wrap.classList.add(state);
    msg.textContent = text || "";
  }
  if (opts.validate) {
    var run = function () {
      var res = opts.validate(input.value);
      if (!res) { setState("", ""); return null; }
      setState(res.ok ? "valid" : "invalid", res.message || "");
      return res.ok;
    };
    input.addEventListener("input", function () { clearTimeout(input._deb); input._deb = setTimeout(run, 260); });
    input.addEventListener("blur", run);
  }
  return { wrap: wrap, input: input, setState: setState, validate: opts.validate };
}

/* ============================ Discover ============================ */
VIEWS.discover = function (root) {
  var st = S.status || { catalog: [], installed: {}, custom: [] };
  var featured = (st.catalog || [])[0];
  var installedCount = Object.keys(st.installed || {}).length + Object.keys(st.custom || {}).length;

  var hero = el("div", "card hero");
  hero.innerHTML =
    '<div><span class="chip accent">' + esc(t("discover.badge")) + "</span>" +
    "<h1>" + esc(installedCount ? t("discover.h1.have") : t("discover.h1.new")) + "</h1>" +
    "<p>" + esc(installedCount ? t("discover.p.have", { n: installedCount }) : t("discover.p.new")) + "</p>" +
    '<div class="hero-actions">' +
    '<button class="btn primary" id="heroInstall">' +
    esc(featured ? t("discover.install", { name: featured.name }) : t("discover.catalog")) + "</button>" +
    '<button class="btn" id="heroImport">' + esc(t("discover.import")) + "</button>" +
    '<button class="btn ghost" data-nav-go="market">' + esc(t("nav.market")) + " →</button>" +
    "</div></div>" +
    '<div class="hero-shot">' + (featured && featured.slug
      ? '<img src="shots/' + esc(featured.slug) + '.jpg" alt="' + esc(featured.name) + '" onerror="this.style.display=\'none\'">'
      : "") + "</div>";
  root.appendChild(hero);
  var hi = $("#heroInstall", hero);
  if (hi) hi.addEventListener("click", function () { if (featured) openInstallDialog([featured.slug]); });
  $("#heroImport", hero).addEventListener("click", function () { openImport(); });
  $('[data-nav-go="market"]', hero).addEventListener("click", function () { go("market"); });

  var stats = el("div", "grid");
  stats.style.gridTemplateColumns = "repeat(auto-fit,minmax(190px,1fr))";
  stats.style.marginTop = "14px";
  var econ = (st.catalog || []).reduce(function (a, c) { return a + (c.savesUsd || 0); }, 0);
  var metrics = [
    ["m.savings", "$" + num(econ), "m.savingsSub"],
    ["m.installed", String(installedCount), "m.installedSub"],
    ["m.custom", String(Object.keys(st.custom || {}).length), "m.customSub"],
    ["m.active", String(Object.values(st.tasks || {}).filter(function (x) { return x.stage === "download"; }).length), "m.activeSub"]
  ];
  metrics.forEach(function (m) {
    var c = el("div", "card");
    c.style.padding = "15px";
    c.innerHTML = '<div style="font-size:11px;letter-spacing:.1em;text-transform:uppercase;color:var(--t3)">' +
      esc(t(m[0])) + "</div>" +
      '<div style="font-size:24px;font-weight:680;letter-spacing:-.03em;margin-top:6px">' + esc(m[1]) + "</div>" +
      '<div style="font-size:11.5px;color:var(--t3);margin-top:3px">' + esc(t(m[2])) + "</div>";
    stats.appendChild(c);
  });
  root.appendChild(stats);

  root.appendChild(sectionTitle(t("title.apps"), t("sub.apps")));
  root.appendChild(appGrid((st.catalog || []).slice(0, 6)));
};

function appGrid(apps) {
  var grid = el("div", "grid cards");
  apps.forEach(function (app) {
    var c = el("div", "app-card");
    var rel = (S.status && S.status.releases && S.status.releases[app.slug]) || {};
    var inst = (S.status && S.status.installed && S.status.installed[app.slug]) || null;
    c.innerHTML =
      '<div class="shot"><img loading="lazy" src="shots/' + esc(app.slug) + '.jpg" alt="' + esc(app.name) + '" ' +
      'onerror="this.parentNode.style.background=\'linear-gradient(135deg,rgba(78,123,251,.35),rgba(139,92,246,.25))\';this.remove()"></div>' +
      '<div style="padding:12px 13px 13px;display:flex;flex-direction:column;gap:8px;flex:1">' +
      '<div style="display:flex;align-items:center;gap:8px"><b style="font-size:13.5px">' + esc(app.name) + "</b>" +
      (inst ? '<span class="chip ok" style="margin-left:auto">' + esc(t("dl.done")) + "</span>" : "") + "</div>" +
      '<div class="repo-desc">' + esc(app.replaces ? "→ " + app.replaces : "") + "</div>" +
      '<div style="display:flex;gap:6px;flex-wrap:wrap;margin-top:auto">' +
      '<button class="btn primary sm" data-install="' + esc(app.slug) + '">' +
      esc(inst ? t("common.options") : t("common.install")) + "</button>" +
      (inst ? '<button class="btn sm" data-run="' + esc(app.slug) + '">' + esc(t("common.launch")) + "</button>" : "") +
      '<button class="btn ghost sm" data-detail="' + esc(app.slug) + '">' + esc(t("common.details")) + "</button>" +
      "</div></div>";
    grid.appendChild(c);
  });
  return grid;
}

/* ============================ All apps ============================ */
VIEWS.apps = function (root) {
  var apps = (S.status && S.status.catalog) || [];
  var q = S.query.toLowerCase().trim();
  if (q) {
    apps = apps.filter(function (a) {
      return [a.name, a.replaces, (a.formats || []).join(" "), a.category].join(" ").toLowerCase().indexOf(q) >= 0;
    });
  }
  if (!apps.length) { root.appendChild(el("div", "empty", esc(t("market.nothing")))); return; }
  root.appendChild(appGrid(apps));
};

/* ============================ Library ============================ */
VIEWS.library = function (root) {
  var st = S.status || {};
  var rows = [];
  Object.keys(st.installed || {}).forEach(function (slug) {
    var meta = (st.catalog || []).filter(function (c) { return c.slug === slug; })[0] || { name: slug };
    rows.push({ slug: slug, kind: "catalog", meta: meta, inst: st.installed[slug] });
  });
  Object.keys(st.custom || {}).forEach(function (slug) {
    rows.push({ slug: slug, kind: "custom", meta: st.custom[slug], inst: st.custom[slug] });
  });
  if (!rows.length) { root.appendChild(el("div", "empty", esc(t("common.empty")))); return; }
  var grid = el("div", "grid cards");
  rows.forEach(function (r) {
    var name = r.meta.name || r.slug;
    var opts = (st.launchOpts || {})[r.slug];
    var c = el("div", "card");
    c.style.padding = "14px"; c.style.display = "flex"; c.style.flexDirection = "column"; c.style.gap = "10px";
    c.innerHTML =
      '<div style="display:flex;align-items:center;gap:10px">' +
      '<div class="repo-avatar" style="background:linear-gradient(135deg,' +
      (r.kind === "custom" ? "#8b5cf6,#ec4899" : "#4e7bfb,#22d3ee") + ')">' + esc(initials(name)) + "</div>" +
      '<div style="min-width:0"><div style="font-weight:600;font-size:14px">' + esc(name) + "</div>" +
      '<div style="font-size:11px;color:var(--t3);font-family:var(--mono)">' +
      esc(r.kind === "custom" ? (r.meta.repo || "github") : t("nav.apps")) + " · v" + esc(r.inst.version || "") + "</div></div>" +
      (r.kind === "custom"
        ? '<span class="chip warn" style="margin-left:auto">' + esc(t("m.custom")) + "</span>"
        : '<span class="chip ok" style="margin-left:auto">' + esc(t("nav.apps")) + "</span>") + "</div>" +
      '<div style="font-size:11.5px;color:var(--t3);word-break:break-all">' + esc(r.inst.execPath || r.inst.installFolder || "") + "</div>" +
      '<div style="display:flex;gap:7px;flex-wrap:wrap;margin-top:auto">' +
      '<button class="btn primary sm" data-run="' + esc(r.slug) + '" data-kind="' + r.kind + '">' + esc(t("common.launch")) + "</button>" +
      '<button class="btn sm" data-launchopts="' + esc(r.slug) + '">' + esc(t("common.options")) +
      (opts && (opts.args || opts.postInstall) ? " •" : "") + "</button>" +
      (r.kind === "catalog" ? '<button class="btn ghost sm" data-wasm="' + esc(r.slug) + '">' + esc(t("common.inBrowser")) + "</button>" : "") +
      '<button class="btn ghost sm" data-uninstall="' + esc(r.slug) + '" data-kind="' + r.kind + '">' + esc(t("common.remove")) + "</button>" +
      "</div>";
    grid.appendChild(c);
  });
  root.appendChild(grid);
};

/* ============================ Marketplace ============================ */
var OS_KEYS = ["windows", "macos", "linux"];
var OS_ICON = { windows: "⊞", macos: "⌘", linux: "◆" };

VIEWS.market = function (root) {
  var f = S.marketFilters;
  var bar = el("div", "filters");
  function pill(key, label, attr, active) {
    return '<button class="pill ' + (active ? "active" : "") + '" ' + attr + ">" + esc(label) + "</button>";
  }
  bar.innerHTML =
    '<input class="input" id="mkSearch" placeholder="' + esc(t("market.search")) + '" value="' + esc(f.q) + '" style="max-width:240px">' +
    '<span style="width:1px;height:22px;background:var(--line);margin:0 2px"></span>' +
    pill("all", t("market.allOs"), 'data-platform="all"', f.platform === "all") +
    pill("mine", t("market.mine"), 'data-platform="mine"', f.platform === "mine") +
    pill("windows", "Windows", 'data-platform="windows"', f.platform === "windows") +
    pill("macos", "macOS", 'data-platform="macos"', f.platform === "macos") +
    pill("linux", "Linux", 'data-platform="linux"', f.platform === "linux") +
    '<span style="width:1px;height:22px;background:var(--line);margin:0 2px"></span>' +
    pill("popular", t("market.popular"), 'data-sort="popular"', f.sort === "popular") +
    pill("fresh", t("market.fresh"), 'data-sort="fresh"', f.sort === "fresh") +
    '<button class="pill ' + (f.verifiedOnly ? "active" : "") + '" data-verified="1">' + esc(t("market.verified")) + "</button>" +
    '<button class="btn sm" id="mkAdd" style="margin-left:auto">' + esc(t("market.add")) + "</button>";
  root.appendChild(bar);
  $("#mkSearch", bar).addEventListener("input", function (e) { f.q = e.target.value; renderMarketList(); });
  $$("[data-sort]", bar).forEach(function (b) { b.addEventListener("click", function () { f.sort = b.dataset.sort; render(); }); });
  $("[data-verified]", bar).addEventListener("click", function () { f.verifiedOnly = !f.verifiedOnly; render(); });
  $$("[data-platform]", bar).forEach(function (b) { b.addEventListener("click", function () { f.platform = b.dataset.platform; render(); }); });
  $("#mkAdd", bar).addEventListener("click", function () { openImport(); });

  var holder = el("div"); holder.id = "marketList"; root.appendChild(holder);
  renderMarketList();
  if (!S.market && !S.marketLoading) loadMarket();
};

async function loadMarket() {
  S.marketLoading = true;
  var holder = $("#marketList");
  if (holder) holder.innerHTML = '<div class="empty">' + esc(t("market.loading")) + "</div>";
  try {
    var data = await API.get("/api/market/catalog");
    S.market = data.items || data || [];
  } catch (e) {
    S.market = [];
    toast(t("market.offline"), "bad");
  }
  S.marketLoading = false;
  renderMarketList();
}


/* Описание карточки: для официальных записей — перевод из seeddesc.js,
   для остальных — оригинал + пометка языка автора. */
function localizedDesc(item) {
  var repo = item.repo || item.id || "";
  var table = null;
  if (typeof SEED_DESC !== "undefined") {
    if (SEED_DESC[repo]) table = SEED_DESC[repo];
    else {
      var lower = String(repo).toLowerCase();
      Object.keys(SEED_DESC).forEach(function (k) {
        if (!table && k.toLowerCase() === lower) table = SEED_DESC[k];
      });
    }
  }
  if (table) {
    var idx = I18N.LOCALES.map(function (l) { return l.code; }).indexOf(S.locale);
    return table[idx >= 0 ? idx : 0] || table[0];
  }
  var text = item.description || "";
  if (!text) return t("market.noDesc");
  var lang = detectTextLang(text);
  if (lang === S.locale || lang === "en") return text;
  var meta = localeInfo(lang);
  return text + "  ·  " + t("market.originalIn", { lang: (meta ? meta.native : lang.toUpperCase()) });
}

function osChips(platforms) {
  var list = Array.isArray(platforms) ? platforms : [];
  if (!list.length) return '<span class="chip">' + esc(t("market.noOs")) + "</span>";
  return OS_KEYS.map(function (k) {
    var on = list.indexOf(k) >= 0;
    var mine = S.myOs === k;
    var label = k === "windows" ? "Windows" : (k === "macos" ? "macOS" : "Linux");
    return '<span class="chip ' + (on ? (mine ? "ok" : "accent") : "") + '" style="' + (on ? "" : "opacity:.38") + '">' +
      OS_ICON[k] + " " + label + (mine && on ? " " + esc(t("market.chipMine")) : "") + "</span>";
  }).join("");
}

function renderMarketList() {
  var holder = $("#marketList"); if (!holder) return;
  var f = S.marketFilters;
  var items = (S.market && S.market.length ? S.market : []).slice();
  var q = f.q.toLowerCase().trim();
  if (q) items = items.filter(function (i) {
    return [i.name, i.repo, i.description, (i.tags || []).join(" ")].join(" ").toLowerCase().indexOf(q) >= 0;
  });
  if (f.verifiedOnly) items = items.filter(function (i) { return i.verified; });
  var wantOs = f.platform === "mine" ? S.myOs : f.platform;
  if (wantOs !== "all") items = items.filter(function (i) { return (i.platforms || []).indexOf(wantOs) >= 0; });
  items.sort(function (a, b) {
    return f.sort === "fresh"
      ? String(b.updatedAt || "").localeCompare(String(a.updatedAt || ""))
      : (b.stars || 0) - (a.stars || 0);
  });

  holder.innerHTML = "";
  if (!items.length) {
    holder.appendChild(el("div", "empty", esc(S.marketLoading ? t("market.loading") : t("market.nothing"))));
    return;
  }
  if (!(S.market && S.market.length) && !S.marketLoading) {
    holder.appendChild(el("div", "risk", esc(t("market.offline"))));
  }
  var grid = el("div", "grid cards");
  items.forEach(function (item) {
    var c = el("div", "repo-card");
    var score = item.score == null ? 0 : item.score;
    var cls = score >= 70 ? "good" : score >= 40 ? "mid" : "low";
    var repo = item.repo || item.id || "";
    c.innerHTML =
      '<div class="repo-head">' +
      '<div class="repo-avatar">' + esc(initials(item.name || repo)) + "</div>" +
      '<div style="min-width:0;flex:1"><div class="repo-name">' + esc(item.name || repo) + "</div>" +
      '<div class="repo-owner">' + esc(repo) + "</div></div>" +
      (item.verified ? '<span class="chip ok">✓</span>' : "") + "</div>" +
      '<div class="repo-desc">' + esc(localizedDesc(item)) + "</div>" +
      '<div style="display:flex;gap:5px;flex-wrap:wrap">' + osChips(item.platforms) + "</div>" +
      '<div class="repo-foot">' +
      '<span class="chip">★ ' + esc(item.stars ? num(item.stars) : "—") + "</span>" +
      '<span class="chip">' + esc(item.license || t("market.fromGithub")) + "</span>" +
      (item.supportCount ? '<span class="chip accent">＋' + esc(num(item.supportCount)) + "</span>" : "") +
      (item.officialMark ? '<span class="chip ok">' + esc(t("market.verified")) + "</span>" : "") +
      '<span class="score ' + cls + '">' + esc(score) + "/100</span></div>" +
      '<div style="display:flex;gap:7px;margin-top:4px;flex-wrap:wrap">' +
      '<button class="btn primary sm" data-mrepo="' + esc(repo) + '" data-mname="' + esc(item.name || repo) + '">' + esc(t("common.install")) + "</button>" +
      '<button class="btn ghost sm" data-msupport="' + esc(repo) + '" title="' + esc(t("market.supportTitle")) + '">' + esc(t("market.support")) + "</button>" +
      '<button class="btn ghost sm" data-mdetail="' + esc(repo) + '">' + esc(t("common.details")) + "</button>" +
      '<button class="btn ghost sm" data-mfix="' + esc(repo) + '" title="' + esc(t("market.fixTitle")) + '">✎</button>' +
      "</div>";
    grid.appendChild(c);
  });
  holder.appendChild(grid);
}

/* ---- предложить правку по пунктам ---- */
var SUGGEST_FIELDS = ["description", "category", "tags", "platforms", "name", "license"];

function openSuggest(repo) {
  if (!S.account.signedIn) { toast(t("err.auth"), "bad"); go("settings"); return; }
  var body = el("div");
  var fField = field({
    label: t("market.fixTitle"), id: "sgField",
    hint: SUGGEST_FIELDS.join(" · ")
  });
  fField.input.setAttribute("list", "sgFieldsList");
  body.innerHTML = '<datalist id="sgFieldsList">' + SUGGEST_FIELDS.map(function (x) { return '<option value="' + x + '">'; }).join("") + "</datalist>";
  body.appendChild(fField.wrap);
  var fValue = field({
    label: t("import.note"), id: "sgValue", textarea: true, rows: 2,
    validate: function (v) {
      if (!v.trim()) return { ok: false, message: t("err.fillFields") };
      if (fField.input.value === "description" && v.length > 400) return { ok: false, message: "≤ 400" };
      if (fField.input.value === "tags" && v.length > 120) return { ok: false, message: "≤ 120" };
      return { ok: true, message: "✓" };
    }
  });
  body.appendChild(fValue.wrap);
  var fComment = field({ label: t("opts.title", { name: repo }).split("·")[0].trim(), id: "sgComment", textarea: true, rows: 2 });
  body.appendChild(fComment.wrap);

  openModal({
    title: t("market.fixTitle"), sub: repo, body: body,
    actions: [
      { label: t("common.cancel"), kind: "ghost", onClick: closeModal },
      {
        label: t("common.save"), kind: "primary", onClick: async function () {
          if (fValue.validate && fValue.validate(fValue.input.value) === false) return;
          try {
            var r = await API.post("/api/market/suggest", {
              repo: repo,
              field: fField.input.value.trim() || "description",
              value: fValue.input.value.trim(),
              comment: fComment.input.value.trim()
            });
            toast(r.message || t("import.published"));
            closeModal();
          } catch (e) { toast(e.message, "bad"); }
        }
      }
    ]
  });
}

function openSupport(repo) {
  if (!S.account.signedIn) { toast(t("err.auth"), "bad"); go("settings"); return; }
  API.post("/api/market/submit", { repo: repo }).then(function (r) {
    toast(r.message || t("import.published"));
  }).catch(function (e) { toast(e.message, "bad"); });
}

function openDetails(repo) {
  var item = (S.market || []).filter(function (x) { return (x.repo || x.id) === repo; })[0] || { repo: repo };
  var body = el("div");
  body.innerHTML =
    '<div style="font-size:13px;line-height:1.6;color:var(--t2)">' + esc(item.description || t("market.noDesc")) + "</div>" +
    '<div style="display:flex;gap:6px;flex-wrap:wrap;margin:12px 0">' + osChips(item.platforms) + "</div>" +
    '<div class="field"><span>' + esc(t("import.repo")) + '</span><div class="hint">' + esc(repo) + "</div></div>" +
    (item.tags && item.tags.length ? '<div class="field"><span>' + esc(t("import.tags")) + '</span><div class="hint">' + esc(item.tags.join(", ")) + "</div></div>" : "");
  openModal({
    title: item.name || repo, sub: repo, body: body,
    actions: [
      { label: t("common.close"), kind: "ghost", onClick: closeModal },
      { label: t("common.install"), kind: "primary", onClick: function () { closeModal(); openRepoInstall(repo, item.name); } }
    ]
  });
}

/* установка репозитория с GitHub: сначала импорт, затем обычный диалог установки */
async function openRepoInstall(repo, name) {
  try {
    toast(t("ok.checking"));
    var prev = await API.get("/api/github/preview?repo=" + encodeURIComponent(repo));
    var slug = String(repo).split("/")[1] || repo;
    await API.post("/api/import", {
      repo: repo, assetUrl: prev.downloadUrl || "", assetName: prev.assetName || "",
      version: prev.version || "", name: name || slug, category: prev.category || "utility",
      tags: prev.tags || [], note: ""
    });
    toast(t("import.added", { name: name || slug }));
    await refresh();
    openInstallDialog([slug]);
  } catch (e) { toast(e.message, "bad"); }
}

/* ============================ диалог установки ============================ */
var LAST_INSTALL = { dirTouched: false, argsTouched: false };

function validateDir(v) {
  var s = String(v || "").trim();
  if (!s) return { ok: false, message: t("err.dirRequired") };
  var absolute = /^([a-zA-Z]:[\\/]|\\\\|\/)/.test(s);
  if (!absolute) return { ok: false, message: t("err.dirAbsolute") };
  return { ok: true, message: "✓" };
}
function validateArgs(v) {
  var q = (String(v || "").match(/"/g) || []).length;
  var sq = (String(v || "").match(/'/g) || []).length;
  if (q % 2 || sq % 2) return { ok: false, message: t("err.argsQuotes") };
  return { ok: true, message: "✓" };
}

function openInstallDialog(slugs) {
  if (!slugs || !slugs.length) return;
  var st = S.status || {};
  var multi = slugs.length > 1;
  var firstName = multi ? t("install.selected", { n: slugs.length }) : slugName(slugs[0]);
  var body = el("div");
  body.innerHTML = '<div style="font-size:12.5px;color:var(--t3);line-height:1.6;margin-bottom:14px">' +
    esc(t("install.intro")) + "</div>";

  var fDir = field({
    label: t("install.dir"), id: "insDir", value: st.installRoot || "",
    hint: t("install.dirHint"), validate: validateDir
  });
  var dirRow = el("div");
  dirRow.style.display = "flex"; dirRow.style.gap = "8px"; dirRow.style.alignItems = "flex-start";
  dirRow.appendChild(fDir.wrap);
  var pick = el("button", "btn", esc(t("install.pick")));
  pick.style.marginTop = "22px"; pick.style.flex = "0 0 auto";
  pick.addEventListener("click", async function () {
    try {
      var r = await API.post("/api/pick-folder", {});
      if (r && r.path) { fDir.input.value = r.path; fDir.validate(); }
      else if (r && r.error) toast(r.error, "bad");
    } catch (e) { toast(e.message, "bad"); }
  });
  dirRow.appendChild(pick);
  body.appendChild(dirRow);

  body.appendChild(toggleRow("install.shortcut", "install.shortcutDesc", "optShortcut", st.createShortcuts !== false));
  body.appendChild(toggleRow("install.startMenu", "install.startMenuDesc", "optStartMenu", st.createStartMenu !== false));
  body.appendChild(toggleRow("install.launchAfter", "install.launchAfterDesc", "optLaunchAfter", false));

  var fArgs = field({
    label: t("install.args"), id: "insArgs", placeholder: t("install.argsPh"),
    hint: t("install.argsDesc"), validate: validateArgs
  });
  body.appendChild(fArgs.wrap);
  body.appendChild(toggleRow("install.verify", "install.verifyDesc", "optVerify", true));

  openModal({
    title: t("install.title", { name: firstName }), sub: slugs.join(", "), body: body,
    actions: [
      { label: t("common.cancel"), kind: "ghost", onClick: closeModal },
      {
        label: t("install.start"), kind: "primary", onClick: async function () {
          var dv = validateDir(fDir.input.value);
          var av = validateArgs(fArgs.input.value);
          fDir.setState(dv.ok ? "valid" : "invalid", dv.message);
          fArgs.setState(av.ok ? "valid" : "invalid", av.message);
          if (!dv.ok) { fDir.input.focus(); return; }
          if (!av.ok) { fArgs.input.focus(); return; }
          var opts = {
            dir: fDir.input.value.trim(),
            shortcut: $("#optShortcut").getAttribute("data-on") === "true",
            startMenu: $("#optStartMenu").getAttribute("data-on") === "true",
            launchAfter: $("#optLaunchAfter").getAttribute("data-on") === "true",
            args: fArgs.input.value.trim(),
            verify: $("#optVerify").getAttribute("data-on") === "true"
          };
          try {
            await API.post("/api/install", { slugs: slugs, options: opts });
            closeModal();
            toast(t("install.queued"));
            go("downloads");
            setTimeout(function () { refresh().then(render); }, 700);
          } catch (e) { toast(e.message, "bad"); }
        }
      }
    ]
  });
}

function toggleRow(titleKey, descKey, id, on) {
  var row = el("div", "opt-row");
  row.innerHTML = '<div><div class="t">' + esc(t(titleKey)) + '</div><div class="d">' + esc(t(descKey)) + "</div></div>" +
    '<div class="toggle" id="' + id + '" data-on="' + (on ? "true" : "false") + '"></div>';
  $(".toggle", row).addEventListener("click", function (e) {
    var n = e.currentTarget;
    n.setAttribute("data-on", n.getAttribute("data-on") === "true" ? "false" : "true");
  });
  return row;
}
function slugName(slug) {
  var meta = ((S.status && S.status.catalog) || []).filter(function (c) { return c.slug === slug; })[0];
  var custom = S.status && S.status.custom && S.status.custom[slug];
  return (meta && meta.name) || (custom && custom.name) || slug;
}

/* ============================ импорт с GitHub ============================ */
var REPO_RE = /^(?:https?:\/\/)?(?:www\.)?github\.com\/([A-Za-z0-9_.-]+)\/([A-Za-z0-9_.-]+?)(?:\.git)?(?:\/.*)?$|^([A-Za-z0-9_.-]+)\/([A-Za-z0-9_.-]+)$/;

function parseRepo(raw) {
  var s = String(raw || "").trim().replace(/\s+/g, "");
  if (!s) return null;
  var m = s.match(REPO_RE);
  if (!m) return null;
  var owner = m[1] || m[3], name = m[2] || m[4];
  if (!owner || !name) return null;
  return owner + "/" + name;
}

function openImport(prefill) {
  var body = el("div");
  var preview = null;
  var fRepo = field({
    label: t("import.repo"), id: "impRepo", value: prefill || "", placeholder: "obsproject/obs-studio",
    hint: t("import.repoHint"),
    validate: function (v) {
      if (!v.trim()) return { ok: false, message: t("err.repoRequired") };
      if (!parseRepo(v)) return { ok: false, message: t("err.repoFormat") };
      return { ok: true, message: "✓ " + parseRepo(v) };
    }
  });
  body.appendChild(fRepo.wrap);

  var checkRow = el("div");
  checkRow.style.display = "flex"; checkRow.style.gap = "8px"; checkRow.style.alignItems = "center";
  var btnCheck = el("button", "btn sm", esc(t("import.check")));
  var status = el("div", "hint");
  status.id = "impStatus";
  checkRow.appendChild(btnCheck); checkRow.appendChild(status);
  body.appendChild(checkRow);

  var fName = field({ label: t("import.name"), id: "impName" });
  var fCat = field({ label: t("import.category"), id: "impCat", value: "utility" });
  var fTags = field({ label: t("import.tags"), id: "impTags" });
  var fNote = field({ label: t("import.note"), id: "impNote", textarea: true, rows: 2 });
  body.appendChild(fName.wrap); body.appendChild(fCat.wrap); body.appendChild(fTags.wrap); body.appendChild(fNote.wrap);

  var payload = function () {
    var repo = parseRepo(fRepo.input.value);
    return {
      repo: repo || fRepo.input.value.trim(),
      assetUrl: preview && preview.downloadUrl || "",
      assetName: preview && preview.assetName || "",
      version: preview && preview.version || "",
      name: fName.input.value.trim() || (repo ? repo.split("/")[1] : ""),
      category: fCat.input.value.trim() || "utility",
      tags: fTags.input.value.split(",").map(function (x) { return x.trim(); }).filter(Boolean),
      note: fNote.input.value.trim()
    };
  };

  btnCheck.addEventListener("click", async function () {
    var repo = parseRepo(fRepo.input.value);
    if (!repo) {
      var v = fRepo.validate();
      fRepo.setState("invalid", (v && v.message) || t("err.repoFormat"));
      return;
    }
    btnCheck.disabled = true;
    status.textContent = t("ok.checking");
    status.style.color = "var(--t3)";
    try {
      preview = await API.get("/api/github/preview?repo=" + encodeURIComponent(repo));
      status.textContent = t("ok.repo", {
        name: preview.name || repo,
        stars: num(preview.stars || 0),
        assets: (preview.assets || (preview.assetName ? 1 : 0))
      });
      status.style.color = "var(--ok)";
      if (preview.name) fName.input.value = preview.name;
      if (preview.tags) fTags.input.value = (preview.tags || []).join(", ");
      if (preview.category) fCat.input.value = preview.category;
    } catch (e) {
      preview = null;
      status.textContent = t("err.repoNotFound");
      status.style.color = "var(--bad)";
    }
    btnCheck.disabled = false;
  });

  openModal({
    title: t("import.title"), sub: t("import.intro"), body: body,
    actions: [
      { label: t("common.cancel"), kind: "ghost", onClick: closeModal },
      {
        label: t("import.local"), kind: "primary", onClick: async function () {
          var v = fRepo.validate();
          if (v && !v.ok) { fRepo.setState("invalid", v.message); return; }
          try {
            await API.post("/api/import", payload());
            closeModal(); toast(t("import.added", { name: payload().name }));
            await refresh(); go("library");
          } catch (e) { toast(e.message, "bad"); }
        }
      },
      {
        label: t("import.publish"), kind: "", onClick: async function () {
          if (!S.account.signedIn) { toast(t("err.auth"), "bad"); return; }
          var v = fRepo.validate();
          if (v && !v.ok) { fRepo.setState("invalid", v.message); return; }
          try {
            await API.post("/api/market/submit", payload());
            closeModal(); toast(t("import.published"));
          } catch (e) { toast(e.message, "bad"); }
        }
      }
    ]
  });
}

/* ============================ Settings ============================ */
VIEWS.settings = function (root) {
  var st = S.status || {};
  var acc = S.account || {};

  /* установка */
  root.appendChild(sectionTitle(t("settings.install"), t("settings.installSub")));
  var c1 = el("div", "card"); c1.style.padding = "18px";
  var fRoot = field({
    label: t("settings.installRoot"), id: "installRoot", value: st.installRoot || "",
    hint: t("install.dirHint"), validate: validateDir
  });
  c1.appendChild(fRoot.wrap);
  var row = el("div"); row.style.display = "flex"; row.style.gap = "8px"; row.style.marginTop = "8px";
  var bPick = el("button", "btn", esc(t("install.pick")));
  var bSave = el("button", "btn primary", esc(t("common.save")));
  var check = el("div", "hint"); check.id = "dirCheck";
  row.appendChild(bPick); row.appendChild(bSave); row.appendChild(check);
  c1.appendChild(row);
  c1.appendChild(toggleRow("settings.shortcut", "settings.shortcutDesc", "setShortcuts", st.createShortcuts !== false));
  c1.appendChild(toggleRow("settings.startMenu", "settings.startMenuDesc", "setStartMenu", st.createStartMenu !== false));
  var autoRow = toggleRow("settings.autostart", "settings.autostartDesc", "setAutostart", !!st.autoStartActive);
  c1.appendChild(autoRow);
  // Показываем реальное состояние: включён ли автозапуск в системе, а не только
  // флаг в state.json. Если система запись не приняла — честно пишем ошибку.
  var autoState = el("div", "hint");
  autoState.id = "autoStartState";
  autoState.style.margin = "-6px 0 10px";
  autoState.textContent = st.autoStartActive ? t("settings.autostartOn") : t("settings.autostartOff");
  c1.appendChild(autoState);

  var trayRow = toggleRow("settings.tray", "settings.trayDesc", "setTray", !!st.closeToTray);
  if (st.traySupported === false) trayRow.style.opacity = "0.55";
  c1.appendChild(trayRow);
  if (st.traySupported === false) {
    c1.appendChild(el("div", "hint", esc(t("settings.trayUnsupported"))));
  } else if (st.closeToTray) {
    var bHide = el("button", "btn sm", esc(t("settings.trayHideNow")));
    bHide.style.marginBottom = "10px";
    bHide.addEventListener("click", async function () {
      try { await API.post("/api/tray/hide", {}); toast(t("settings.trayHidden")); }
      catch (e) { toast(e.message, "bad"); }
    });
    c1.appendChild(bHide);
  }
  c1.appendChild(toggleRow("settings.lowSpec", "settings.lowSpecDesc", "setLowSpec", !!st.lowSpecMode));
  root.appendChild(c1);

  bPick.addEventListener("click", async function () {
    try {
      var r = await API.post("/api/pick-folder", {});
      if (r && r.path) { fRoot.input.value = r.path; fRoot.validate(); saveRoot(); }
    } catch (e) { toast(e.message, "bad"); }
  });
  async function saveRoot() {
    var v = validateDir(fRoot.input.value);
    fRoot.setState(v.ok ? "valid" : "invalid", v.message);
    if (!v.ok) return;
    try {
      var r = await API.post("/api/check-dir", { path: fRoot.input.value.trim() });
      if (r && r.ok === false) { fRoot.setState("invalid", r.error || t("err.dirNoWrite")); return; }
      if (r && r.writable === false) { fRoot.setState("invalid", t("err.dirNoWrite")); return; }
      fRoot.setState("valid", t("ok.dir"));
      await API.post("/api/settings", { installRoot: fRoot.input.value.trim() });
      toast(t("toast.saved"));
      await refresh();
    } catch (e) { toast(e.message, "bad"); }
  }
  bSave.addEventListener("click", saveRoot);

  $$("[data-toggle], .toggle", c1).forEach(function (n) {
    if (!n.id) return;
    n.addEventListener("click", async function () {
      var on = n.getAttribute("data-on") === "true";
      var map = {
        setShortcuts: "createShortcuts", setStartMenu: "createStartMenu",
        setAutostart: "autoStart", setTray: "closeToTray", setLowSpec: "lowSpecMode"
      };
      var patch = {}; patch[map[n.id]] = on;
      try {
        var res = await API.post("/api/settings", patch).catch(function (e) { throw e; });
        if (res && res.ok === false) {
          toast(res.errorKey ? t(res.errorKey) + (res.errorDetail ? " · " + res.errorDetail : "") : t("err.generic"), "bad");
        } else {
          toast(t("toast.saved"));
        }
        await refresh();
        if (n.id === "setLowSpec") document.body.classList.toggle("low-spec", on);
        if (res && res.trayRestartNeeded) toast(t("settings.trayRestart"), "bad");
      } catch (e) { toast(e.message, "bad"); }
    });
  });

  /* язык */
  root.appendChild(sectionTitle(t("settings.lang"), t("settings.langSub")));
  var c2 = el("div", "card"); c2.style.padding = "18px";
  var cur = localeInfo(S.locale);
  c2.innerHTML = '<div style="display:flex;align-items:center;gap:12px;flex-wrap:wrap">' +
    '<div class="repo-avatar" style="font-size:16px">' + esc(cur.flag) + "</div>" +
    '<div><div style="font-weight:600">' + esc(cur.native) + "</div>" +
    '<div style="font-size:11.5px;color:var(--t3)">' + esc(t("settings.langCurrent", { lang: cur.name })) + "</div></div>" +
    '<button class="btn primary" id="openLang" style="margin-left:auto">' + esc(t("settings.langPick")) + "</button></div>";
  root.appendChild(c2);
  $("#openLang", c2).addEventListener("click", openLanguagePicker);

  /* оформление + о программе */
  root.appendChild(sectionTitle(t("settings.theme"), t("settings.themeDesc")));
  var c3 = el("div", "card"); c3.style.padding = "18px";
  c3.innerHTML = '<div style="display:flex;gap:8px;flex-wrap:wrap">' +
    '<button class="btn" id="themeDark">🌙 Dark</button><button class="btn" id="themeLight">☀️ Light</button>' +
    '<button class="btn ghost" id="repair" style="margin-left:auto">' + esc(t("settings.repair")) + "</button>" +
    '<button class="btn ghost" id="health">' + esc(t("settings.health")) + "</button></div>";
  root.appendChild(c3);
  $("#themeDark", c3).addEventListener("click", function () { setTheme("dark"); });
  $("#themeLight", c3).addEventListener("click", function () { setTheme("light"); });
  $("#repair", c3).addEventListener("click", async function () {
    try { await API.post("/api/shortcut", { slug: "__all__" }); toast(t("toast.shortcuts")); }
    catch (e) { toast(e.message, "bad"); }
  });
  $("#health", c3).addEventListener("click", runDiagnostics);

  root.appendChild(sectionTitle(t("settings.about"), "v" + (st.launcherVersion || "")));
  var c4 = el("div", "card"); c4.style.padding = "18px";
  c4.innerHTML = '<div style="display:flex;gap:8px;flex-wrap:wrap;align-items:center">' +
    '<span class="chip">' + esc(t("settings.version")) + " " + esc(st.launcherVersion || "") + "</span>" +
    '<span class="chip">' + esc(st.osLabel || "") + " · " + esc(st.arch || "") + "</span>" +
    '<button class="btn sm" id="checkUpd">' + esc(t("settings.update")) + "</button>" +
    '<button class="btn ghost sm" id="refreshRel">' + esc(t("settings.refresh")) + "</button></div>";
  root.appendChild(c4);
  $("#checkUpd", c4).addEventListener("click", async function () {
    try {
      var r = await API.get("/api/check-launcher-update");
      if (r && r.available) toast(t("settings.updateFound", { v: r.remoteVer || "" }));
      else toast(t("settings.upToDate"));
    } catch (e) { toast(e.message, "bad"); }
  });
  $("#refreshRel", c4).addEventListener("click", async function () {
    try { await API.post("/api/refresh-releases", {}); await refresh(); toast(t("toast.saved")); }
    catch (e) { toast(e.message, "bad"); }
  });

  /* аккаунт */
  root.appendChild(sectionTitle(t("acc.section"), t("acc.sectionSub")));
  var c5 = el("div", "card"); c5.style.padding = "18px";
  if (acc.signedIn) {
    c5.innerHTML = '<div style="display:flex;align-items:center;gap:12px;flex-wrap:wrap">' +
      '<div class="repo-avatar">' + esc(initials(acc.name || acc.email)) + "</div>" +
      '<div><div style="font-weight:600">' + esc(acc.name || acc.email) + "</div>" +
      '<div style="font-size:12px;color:var(--t3)">' + esc(acc.email || "") + "</div></div>" +
      '<button class="btn sm" id="logout" style="margin-left:auto">' + esc(t("acc.logout")) + "</button></div>";
    root.appendChild(c5);
    $("#logout", c5).addEventListener("click", async function () {
      await API.post("/api/site/logout", {});
      await refresh(); render(); toast(t("acc.signedOut"));
    });
  } else {
    var fEmail = field({
      label: t("acc.email"), id: "accEmail", placeholder: "you@example.com",
      validate: function (v) {
        if (!v.trim()) return { ok: false, message: t("err.fillFields") };
        var okMail = /^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(v) || /^@?[A-Za-z0-9_]{3,24}$/.test(v);
        return okMail ? { ok: true, message: "✓" } : { ok: false, message: t("err.handle") };
      }
    });
    var fPass = field({
      label: t("acc.password"), id: "accPass", type: "password",
      validate: function (v) { return v.length >= 8 ? { ok: true, message: "✓" } : { ok: false, message: t("err.password") }; }
    });
    var fHandle = field({
      label: t("acc.handle"), id: "accHandle", placeholder: "alex_craft",
      validate: function (v) {
        if (!v.trim()) return null;
        return /^[A-Za-z0-9_]{3,24}$/.test(v) ? { ok: true, message: "✓" } : { ok: false, message: t("err.handle") };
      }
    });
    var fName = field({ label: t("acc.name"), id: "accNameInput" });
    c5.appendChild(fEmail.wrap); c5.appendChild(fPass.wrap);
    var g = el("div", "grid"); g.style.gridTemplateColumns = "1fr 1fr";
    g.appendChild(fHandle.wrap); g.appendChild(fName.wrap);
    c5.appendChild(g);
    var btns = el("div"); btns.style.display = "flex"; btns.style.gap = "8px"; btns.style.flexWrap = "wrap"; btns.style.marginTop = "8px";
    var bLogin = el("button", "btn primary", esc(t("acc.login")));
    var bReg = el("button", "btn", esc(t("acc.register")));
    btns.appendChild(bLogin); btns.appendChild(bReg); c5.appendChild(btns);
    c5.appendChild(el("div", "hint", esc(t("acc.deviceNote", { id: (acc.deviceId || "…") }))));
    root.appendChild(c5);

    var doAuth = async function (register) {
      var eOk = fEmail.validate().ok, pOk = fPass.validate().ok;
      if (register) { var h = fHandle.validate(); if (h === null || (h && !h.ok)) { fHandle.setState("invalid", t("err.handle")); return; } }
      if (!eOk) { fEmail.input.focus(); return; }
      if (!pOk) { fPass.input.focus(); return; }
      try {
        var r = await API.post("/api/site/login", {
          email: fEmail.input.value.trim().replace(/^@/, ""),
          password: fPass.input.value,
          register: register,
          handle: fHandle.input.value.trim(),
          name: fName.input.value.trim()
        });
        await refresh(); render();
        toast(r && r.message ? r.message : t("toast.saved"));
        (r && (r.warningKeys || r.warnings) || []).forEach(function (w) {
          toast(r.warningKeys ? t(w) : w);
        });
      } catch (e) { toast(e.message, "bad"); }
    };
    bLogin.addEventListener("click", function () { doAuth(false); });
    bReg.addEventListener("click", function () { doAuth(true); });
  }
};

/* ---------- выбор языка ---------- */
function openLanguagePicker() {
  var body = el("div");
  var search = el("input", "input lang-search");
  search.placeholder = t("settings.langSearch");
  body.appendChild(search);
  var list = el("div", "lang-list");
  list.setAttribute("role", "listbox");
  body.appendChild(list);

  function paint(q) {
    var s = String(q || "").toLowerCase().trim();
    list.innerHTML = "";
    var shown = 0;
    I18N.LOCALES.forEach(function (loc) {
      var hay = (loc.native + " " + loc.name + " " + loc.code).toLowerCase();
      if (s && hay.indexOf(s) < 0) return;
      shown++;
      var b = el("button", "lang-opt");
      b.setAttribute("role", "option");
      b.setAttribute("aria-selected", loc.code === S.locale ? "true" : "false");
      b.innerHTML = '<span class="code" style="margin:0">' + esc(loc.code.toUpperCase()) + "</span>" +
        '<span>' + esc(loc.native) + "</span>" +
        '<span class="code">' + esc(loc.name) + "</span>";
      b.addEventListener("click", function () {
        S.localeTouched = true;
        applyLocale(loc.code, true);
        closeModal();
        render();
        toast(t("toast.langChanged", { lang: loc.native }));
      });
      list.appendChild(b);
    });
    if (!shown) list.appendChild(el("div", "empty", esc(t("settings.langEmpty"))));
  }
  paint("");
  search.addEventListener("input", function (e) { paint(e.target.value); });
  openModal({
    title: t("settings.langPick"), sub: t("settings.langSub"), body: body,
    actions: [{ label: t("common.close"), kind: "ghost", onClick: closeModal }]
  });
  setTimeout(function () { search.focus(); }, 60);
}

/* ---------- тема ---------- */
function setTheme(mode) {
  S.theme = mode;
  document.documentElement.setAttribute("data-theme", mode);
  try { localStorage.setItem("artecraft.theme", mode); } catch (e) {}
}

/* ---------- диагностика ---------- */
async function runDiagnostics() {
  var problems = [];
  try {
    var r = await API.post("/api/check-dir", { path: (S.status && S.status.installRoot) || "" });
    if (!r || r.writable === false) problems.push("install dir");
  } catch (e) { problems.push("install dir"); }
  try { await API.get("/api/market/catalog"); } catch (e) { problems.push("network"); }
  if (S.status && !(S.account && S.account.signedIn)) problems.push("account");
  if (!problems.length) toast(t("toast.healthOk"));
  else toast(t("toast.healthBad", { list: problems.join(", ") }), "bad");
}

/* ============================ Downloads ============================ */
VIEWS.downloads = function (root) {
  var tasks = Object.values((S.status && S.status.tasks) || {});
  if (!tasks.length) { root.appendChild(el("div", "empty", esc(t("dl.empty")))); return; }
  tasks.forEach(function (tk) {
    var card = el("div", "task");
    var pct = Math.max(0, Math.min(100, tk.percent || 0));
    var stageKey = "dl." + (tk.stage || "queued");
    var stageText = ["dl.queued", "dl.download", "dl.extracting", "dl.done", "dl.error", "dl.canceled"].indexOf(stageKey) >= 0
      ? t(stageKey) : (tk.stage || "");
    var steps = ["queued", "download", "extracting", "done"].map(function (stg, i) {
      var order = ["queued", "download", "extracting", "done"];
      var idxNow = order.indexOf(tk.stage === "error" ? "download" : tk.stage);
      return '<span class="' + (i <= idxNow ? "on" : "") + '"></span>';
    }).join("");
    card.innerHTML =
      '<div class="task-head"><b>' + esc(tk.slug) + "</b>" +
      '<span class="chip ' + (tk.stage === "error" ? "bad" : tk.stage === "done" ? "ok" : "") + '">' + esc(stageText) + "</span>" +
      '<span class="st">' + pct.toFixed(0) + "%</span></div>" +
      '<div class="bar"><i style="width:' + pct + '%"></i></div>' +
      '<div class="steps">' + steps + "</div>" +
      '<div class="meta">' + esc(tk.messageKey ? t(tk.messageKey) : (tk.message || "")) +
      (tk.totalMB ? " · " + (tk.downloadedMB || 0).toFixed(1) + " / " + tk.totalMB.toFixed(1) + " MB" : "") +
      (tk.speedMbps ? " · " + tk.speedMbps.toFixed(1) + " MB/s" : "") + "</div>" +
      (tk.sha256 ? '<div class="meta" style="font-family:var(--mono);font-size:10.5px;word-break:break-all">' +
        esc(t("dl.sha")) + ": " + esc(tk.sha256) + "</div>" : "") +
      (tk.errorKey || tk.error
        ? '<div class="risk bad">' + esc(tk.errorKey ? t(tk.errorKey) + (tk.errorDetail ? " · " + tk.errorDetail : "") : tk.error) + "</div>"
        : "") +
      '<div class="actions">' +
      (tk.stage === "error" ? '<button class="btn sm" data-retry="' + esc(tk.slug) + '">' + esc(t("common.retry")) + "</button>" : "") +
      (["queued", "download", "extracting"].indexOf(tk.stage) >= 0
        ? '<button class="btn ghost sm" data-cancel="' + esc(tk.slug) + '">' + esc(t("dl.cancel")) + "</button>" : "") +
      "</div>";
    root.appendChild(card);
  });
};

/* ============================ параметры запуска ============================ */
async function openLaunchOptions(slug) {
  var cfg = {};
  try { cfg = await API.get("/api/launch-opts?slug=" + encodeURIComponent(slug)); } catch (e) { cfg = {}; }
  var body = el("div");
  var fArgs = field({ label: t("opts.args"), id: "loArgs", value: cfg.args || "", placeholder: t("install.argsPh"), validate: validateArgs });
  var fCwd = field({ label: t("opts.cwd"), id: "loCwd", value: cfg.cwd || "" });
  var envText = Object.keys(cfg.env || {}).map(function (k) { return k + "=" + cfg.env[k]; }).join("\n");
  var fEnv = field({ label: t("opts.env"), id: "loEnv", value: envText, textarea: true, rows: 2, hint: "KEY=value" });
  var fPost = field({ label: t("opts.postInstall"), id: "loPost", value: cfg.postInstall || "", textarea: true, rows: 2 });
  body.appendChild(fArgs.wrap); body.appendChild(fCwd.wrap); body.appendChild(fEnv.wrap); body.appendChild(fPost.wrap);

  openModal({
    title: t("opts.title", { name: slugName(slug) }), body: body,
    actions: [
      { label: t("common.cancel"), kind: "ghost", onClick: closeModal },
      {
        label: t("common.save"), kind: "primary", onClick: async function () {
          var argsOk = validateArgs(fArgs.input.value);
          fArgs.setState(argsOk.ok ? "valid" : "invalid", argsOk.message);
          if (!argsOk.ok) return;
          var env = {};
          fEnv.input.value.split("\n").forEach(function (line) {
            var i = line.indexOf("=");
            if (i > 0) env[line.slice(0, i).trim()] = line.slice(i + 1).trim();
          });
          try {
            await API.post("/api/launch-opts", {
              slug: slug,
              config: { args: fArgs.input.value.trim(), cwd: fCwd.input.value.trim(), env: env, postInstall: fPost.input.value.trim() },
              runPostInstall: false
            });
            closeModal(); toast(t("opts.saved"));
          } catch (e) { toast(e.message, "bad"); }
        }
      }
    ]
  });
}

/* ============================ события ============================ */
/* Колесо мыши над выпадающим списком в Chromium переключает пункты вместо
   прокрутки — из-за этого настройки и языки казавлись «непрокручиваемыми».
   Отдаём колесо ближайшему прокручиваемому контейнеру. */
function wireWheelPassthrough() {
  document.addEventListener("wheel", function (e) {
    var t = e.target;
    if (!t || t.tagName !== "SELECT") return;
    var sc = t.closest(".view, .modal-body, .lang-list, .rail");
    if (!sc) return;
    sc.scrollTop += e.deltaY;
    e.preventDefault();
  }, { passive: false });
}

function wireGlobal() {
  $$(".nav-item").forEach(function (b) {
    b.addEventListener("click", function () { go(b.dataset.nav); });
  });
  var si = $("#searchInput");
  si.addEventListener("input", function (e) {
    S.query = e.target.value;
    if (S.view !== "apps") go("apps"); else render();
  });
  $("#btnImport").addEventListener("click", function () { openImport(); });
  $("#btnTheme").addEventListener("click", function () {
    setTheme(document.documentElement.getAttribute("data-theme") === "light" ? "dark" : "light");
  });
  $("#langBtn").addEventListener("click", openLanguagePicker);
  $("#accountChip").addEventListener("click", function () { go("settings"); });
  document.addEventListener("keydown", function (e) {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
      e.preventDefault(); si.focus(); si.select();
    }
  });

  /* делегирование клика по карточкам */
  $("#view").addEventListener("click", function (e) {
    var n = e.target.closest("[data-install],[data-run],[data-detail],[data-uninstall],[data-launchopts],[data-wasm],[data-mrepo],[data-msupport],[data-mdetail],[data-mfix],[data-cancel],[data-retry]");
    if (!n) return;
    var d = n.dataset;
    if (d.install) return openInstallDialog([d.install]);
    if (d.detail) return openInstallDialog([d.detail]);
    if (d.launchopts) return openLaunchOptions(d.launchopts);
    if (d.run) {
      API.post(d.kind === "custom" ? "/api/custom/launch" : "/api/launch", { slug: d.run })
        .then(function () { toast(t("toast.launched", { name: slugName(d.run) })); })
        .catch(function (err) { toast(err.message, "bad"); });
      return;
    }
    if (d.wasm) { API.post("/api/open-url", { url: WASM(d.wasm) }); return; }
    if (d.uninstall) {
      var slug = d.uninstall;
      API.post(d.kind === "custom" ? "/api/custom/remove" : "/api/uninstall", { slug: slug, purge: true })
        .then(async function () { toast(t("toast.removed")); await refresh(); render(); })
        .catch(function (err) { toast(err.message, "bad"); });
      return;
    }
    if (d.mrepo) return openRepoInstall(d.mrepo, d.mname);
    if (d.msupport) return openSupport(d.msupport);
    if (d.mdetail) return openDetails(d.mdetail);
    if (d.mfix) return openSuggest(d.mfix);
    if (d.cancel) {
      API.post("/api/cancel", { slug: d.cancel }).then(function () { toast(t("dl.canceled")); refresh().then(render); });
      return;
    }
    if (d.retry) return openInstallDialog([d.retry]);
  });
}

/* ============================ init ============================ */

/* ======================= обновление лаунчера ==============================
   Два режима:
   • available — сверху появляется полоска «доступно обновление» с кнопкой;
   • locked    — «блокировка фулл»: поверх окна плашка, установка запрещена
                 (сервер тоже отвечает 409 err.updateRequired, так что обойти
                 клик из консоли нельзя).
   ======================================================================== */
var UPD = { bannerDismissed: false };

function openUpdatePage() {
  API.post("/api/open-update-page", {}).catch(function () {
    if (UPD.downloadUrl) window.open(UPD.downloadUrl, "_blank");
  });
}

function renderLauncherUpdate(su) {
  var bar = $("#updateBar");
  var lock = $("#updateLock");
  UPD.downloadUrl = su.downloadUrl || "https://getartecraft.com/download";

  if (!bar) {
    bar = el("div", "update-bar");
    bar.id = "updateBar";
    bar.style.display = "none";
    var host = $("#view") ? $("#view").parentNode : document.body;
    host.insertBefore(bar, host.firstChild);
  }
  if (!lock) {
    lock = el("div", "update-lock");
    lock.id = "updateLock";
    lock.style.display = "none";
    document.body.appendChild(lock);
  }

  if (su.locked) {
    bar.style.display = "none";
    lock.innerHTML =
      '<div class="update-lock-card">' +
      '<div class="update-lock-title">' + esc(t("update.locked")) + "</div>" +
      '<div class="update-lock-text">' + esc(t("update.hint")) + "</div>" +
      '<div class="update-lock-ver">' + esc(String(su.remoteVer || "")) + "</div>" +
      '<button class="btn primary" id="updateLockBtn">' + esc(t("update.now")) + "</button>" +
      "</div>";
    lock.style.display = "flex";
    var lb = $("#updateLockBtn");
    if (lb) lb.addEventListener("click", openUpdatePage);
    document.body.classList.add("update-locked");
    return;
  }

  lock.style.display = "none";
  document.body.classList.remove("update-locked");

  if (su.available && !UPD.bannerDismissed) {
    bar.innerHTML =
      '<span class="update-bar-dot"></span>' +
      "<b>" + esc(t("update.available")) + "</b>" +
      (su.remoteVer ? ' <span class="update-bar-ver">' + esc(String(su.remoteVer)) + "</span>" : "") +
      '<button class="btn sm primary" id="updateBarBtn">' + esc(t("update.now")) + "</button>" +
      '<button class="icon-btn update-bar-x" id="updateBarX" aria-label="close">✕</button>';
    bar.style.display = "flex";
    $("#updateBarBtn").addEventListener("click", openUpdatePage);
    $("#updateBarX").addEventListener("click", function () {
      UPD.bannerDismissed = true;
      bar.style.display = "none";
    });
  } else {
    bar.style.display = "none";
  }
}

async function init() {
  var theme = "dark";
  try { theme = localStorage.getItem("artecraft.theme") || "dark"; } catch (e) {}
  setTheme(theme);
  applyLocale(I18N.detect(), false);
  wireGlobal();
  wireWheelPassthrough();
  await refresh();
  render();
  applyLocale(S.locale, false);
  setInterval(async function () {
    await refresh();
    if (S.view === "downloads" || S.view === "library") render();
  }, 1500);
}
document.addEventListener("DOMContentLoaded", init);
