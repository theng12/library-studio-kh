"use strict";
// The launch token arrives in the address (#token); keep it for reloads and hide it from the address bar.
if (location.hash.length > 1) {
  sessionStorage.setItem("token", location.hash.slice(1));
  history.replaceState(null, "", location.pathname);
}
let token = sessionStorage.getItem("token") || "";
const $ = (s) => document.querySelector(s);
const ARRS = ["sonarr", "radarr", "lidarr", "prowlarr"];
const NAMES = { sonarr: "Sonarr (TV)", radarr: "Radarr (films)", lidarr: "Lidarr (music)", prowlarr: "Prowlarr (search sites)" };
const PORTS = { sonarr: 8989, radarr: 7878, lidarr: 8686, prowlarr: 9696 };
const AUTH_FORM_PATHS = new Set(["/api/auth/login", "/api/auth/setup", "/api/auth/password"]);
const AUTH = { available: false, authenticated: false, setupRequired: false, rememberDays: 30, sessionHours: 12, minPasswordLength: 6 };
let current = null;

async function api(path, body, write) {
  const headers = token ? { "X-Library-Token": token } : {};
  if (body) headers["Content-Type"] = "application/json";
  if (write || body) headers["X-Library-Request"] = "1";
  const r = await fetch(path, {
    method: write || body ? "POST" : "GET",
    headers,
    credentials: "same-origin",
    body: body ? JSON.stringify(body) : undefined,
  });
  const data = await r.json().catch(() => ({}));
  // The API guard answers 403 when a session is missing or expired; auth forms handle their own errors.
  if ((r.status === 401 || (r.status === 403 && AUTH.available)) && !AUTH_FORM_PATHS.has(path)) signedOut();
  if (!r.ok) throw new Error(data.error || (r.status === 403
    ? (AUTH.available ? "Please sign in to Library Studio KH." : "Please reopen Library Studio KH from the desktop app.")
    : "Something went wrong (" + r.status + ")."));
  return data;
}

const day = (iso) => new Date(iso).toLocaleDateString(undefined, { weekday: "short", day: "numeric", month: "short" });
function inDays(iso) {
  const d = Math.ceil((new Date(iso) - Date.now()) / 86400000);
  return d <= 0 ? "at the next check" : d === 1 ? "tomorrow" : "in " + d + " days";
}
const time = (iso) => new Date(iso).toLocaleString(undefined, { weekday: "short", hour: "numeric", minute: "2-digit" });
function gb(n) {
  return n >= 1e12 ? (n / 1e12).toFixed(1) + " TB" : Math.round(n / 1e9) + " GB";
}

function stateOf(it, cleanupOn) {
  switch (it.kind) {
    case "downloading": return ["Downloading", Math.floor(it.progress * 100) + "%"];
    case "waiting": return ["Waiting for import", it.app || ""];
    case "imported": return ["In library", cleanupOn ? "Local copy goes " + inDays(it.when) : "Local copy kept"];
    case "unmanaged": return ["Downloaded", "Not from Sonarr, Radarr or Lidarr, so it stays"];
    case "upcoming": return ["Coming up", day(it.when)];
    case "removed": return ["In library", "Local copy removed " + day(it.when)];
  }
  return [it.kind, ""];
}

function row(cells, tableID) {
  const tr = document.createElement("tr");
  const headers = tableID ? document.getElementById(tableID).querySelectorAll("thead th") : [];
  for (const [i, c] of cells.entries()) {
    const td = document.createElement("td");
    if (headers[i]) td.dataset.label = headers[i].textContent;
    if (c instanceof HTMLInputElement && c.type === "checkbox") {
      const label = document.createElement("label"); label.className = "check-target";
      label.append(c); td.append(label);
    } else if (c instanceof Node) td.append(c); else td.textContent = c;
    tr.append(td);
  }
  return tr;
}

function render(st) {
  current = st;
  const s = st.snapshot, cfg = st.settings;
  $("#version").textContent = st.version;

  const parts = [];
  parts.push(s.freeBytes == null ? "Free space on this computer: not known yet." : "<strong>" + gb(s.freeBytes) + "</strong> free on this computer.");
  parts.push(cfg.cleanupOn
    ? "Local copies are removed <strong>" + cfg.cleanupDays + " days</strong> after they reach the library."
    : "Local copies are <strong>kept</strong>. Removal is switched off.");
  if (s.library === "missing") parts.push("<span class=off>The NAS library folder can't be found.</span>");
  $("#summary").innerHTML = parts.join(" ");

  const connected = s.apps.filter(a => a.ok).length;
  $("#connections-summary").textContent = s.apps.length ? connected + " of " + s.apps.length + " apps connected" : "No apps connected";
  $("#connections-summary").classList.toggle("off", connected < s.apps.length);
  $("#apps").replaceChildren(...s.apps.map((a) => {
    const li = document.createElement("li");
    li.className = a.ok ? "ok" : "bad";
    li.textContent = a.name + (a.ok ? " connected" : ": " + a.error);
    return li;
  }));

  const notice = $("#notice");
  notice.hidden = !s.blocked;
  notice.textContent = s.blocked || "";

  renderActivity();

  $("#cleanup-lede").innerHTML = parts.slice(0, 2).join(" ");
  const last = st.lastClean;
  $("#last-run").textContent = !last ? "Not checked yet since the app started."
    : last.blocked ? "Last check " + time(last.at) + ": nothing removed. " + last.blocked
    : "Last check " + time(last.at) + ": " + (last.removed === 1 ? "1 local copy" : last.removed + " local copies") + " removed."
      + (last.errors && last.errors.length ? " Problems: " + last.errors.join("; ") : "");
  const hasStock = s.settings && s.settings.length > 0;
  $("#stock-empty").hidden = hasStock;
  $("#stock").hidden = !hasStock;
  $("#stock tbody").replaceChildren(...(s.settings || []).map((x) => {
    const now = document.createElement("span");
    now.textContent = x.value || "Unknown";
    if (!x.ok) now.className = "off";
    return row([x.app, x.name, now, x.want], "stock");
  }));

  $("#releases").replaceChildren(...st.releases.map((r) => {
    const box = document.createElement("div");
    box.className = "panel";
    const h = document.createElement("h2");
    h.textContent = "Version " + r.version;
    const d = document.createElement("span");
    d.textContent = r.date;
    h.append(d);
    const ul = document.createElement("ul");
    for (const n of r.notes) { const li = document.createElement("li"); li.textContent = n; ul.append(li); }
    box.append(h, ul);
    return box;
  }));
}

const has = (title, q) => title.toLowerCase().includes(q.trim().toLowerCase());
const byTitle = (a, b) => a.title.localeCompare(b.title, undefined, { sensitivity: "base", numeric: true });
const TYPE = { Sonarr: "TV", Radarr: "Film", Lidarr: "Music" };

function renderActivity() {
  if (!current) return;
  const s = current.snapshot, q = $("#act-q").value, kind = $("#act-kind").value, sort = $("#act-sort").value;
  let items = s.items.filter((it) => (!kind || it.kind === kind) && has(it.title, q));
  if (sort === "title") items = [...items].sort(byTitle);
  if (sort === "when") items = [...items].sort((a, b) => (b.when || "").localeCompare(a.when || ""));
  const empty = $("#empty");
  empty.hidden = items.length > 0;
  if (s.apps.length > 0) empty.textContent = s.items.length ? "Nothing matches." : "Nothing downloading, in the library or coming up yet.";
  $("#list").hidden = items.length === 0;
  $("#list tbody").replaceChildren(...items.map((it) => {
    const [label, when] = stateOf(it, current.settings.cleanupOn);
    const tag = document.createElement("span");
    tag.className = "state " + it.kind;
    const word = document.createElement("span");
    word.textContent = label;
    tag.append(word);
    let title = it.title;
    if (it.stats) {
      title = document.createElement("button");
      title.type = "button"; title.className = "inline title"; title.textContent = it.title;
      title.addEventListener("click", () => openDetails(it));
    }
    return row([title, tag, when], "list");
  }));
}

function dur(s) {
  s = Math.round(s);
  if (s < 60) return s + " s";
  const d = Math.floor(s / 86400), h = Math.floor(s % 86400 / 3600), m = Math.floor(s % 3600 / 60);
  return d ? d + " d " + h + " h" : h ? h + " h " + m + " min" : m + " min";
}
function bytes(n) {
  for (const [u, v] of [["TB", 1e12], ["GB", 1e9], ["MB", 1e6], ["KB", 1e3]]) if (n >= v) return (n / v).toFixed(n >= 10 * v ? 0 : 1) + " " + u;
  return n + " B";
}
const rate = (n) => bytes(n) + "/s";
const stamp = (iso) => new Date(iso).toLocaleString(undefined, { day: "numeric", month: "short", year: "numeric", hour: "numeric", minute: "2-digit" });

function facts(dl, pairs) {
  dl.replaceChildren();
  for (const [k, v] of pairs) {
    if (v == null || v === "") continue;
    const dt = document.createElement("dt"), dd = document.createElement("dd");
    dt.textContent = k; dd.textContent = v;
    dl.append(dt, dd);
  }
}

// shortcut: shows the reading from when it was opened; reopen for newer numbers.
function openDetails(it) {
  const s = it.stats, now = Date.now();
  const added = s.added ? new Date(s.added) : null, done = s.completed ? new Date(s.completed) : null;
  const took = added && done ? (done - added) / 1000 : added ? (now - added) / 1000 : 0;
  const finished = it.progress >= 1;
  $("#d-title").textContent = it.title;
  facts($("#d-facts"), [
    ["Started", added ? stamp(s.added) : "Not known"],
    ["Finished", done ? stamp(s.completed) : finished ? "Not known" : "Still downloading (" + Math.floor(it.progress * 100) + "%)"],
    [finished ? "Took" : "Running for", took ? dur(took) : "Not known"],
    ["Time left", !finished && s.eta ? "About " + dur(s.eta) : ""],
    ["Size", bytes(s.size)],
    ["Downloaded", bytes(s.downloaded)],
    ["Uploaded", bytes(s.uploaded) + " (share ratio " + s.ratio.toFixed(2) + ")"],
    ["Seeds and peers", s.seeds + " seeds, " + s.peers + " peers"],
    ["Saved in", s.savePath],
    ["Category", s.category],
    ["Sent by", it.app || "Added by hand"],
    ["qBittorrent state", s.state],
  ]);
  const avgWhole = took ? (finished ? s.size : s.downloaded) / took : 0;
  facts($("#d-speedfacts"), [
    ["Now", finished ? "" : rate(s.dlSpeed) + " down, " + rate(s.upSpeed) + " up"],
    ["Average, start to finish", avgWhole ? rate(avgWhole) + " (includes any pauses)" : "Not known"],
    ["Average while recorded", s.measured ? rate(s.avgMeasured || 0) : ""],
    ["Lowest (above 0)", s.measured ? (s.minSpeed ? rate(s.minSpeed) : "Never moved") : ""],
    ["Fastest", s.measured ? rate(s.maxSpeed || 0) : ""],
  ]);
  $("#d-measured").textContent = s.measured
    ? "Recorded for " + dur(s.measured) + (took ? " of the " + dur(took) + (finished ? " it took" : " so far") : "") + ". Library Studio KH records speed every 15 seconds while it's open."
    : "Speed history not recorded. Library Studio KH records it every 15 seconds while it's open, and wasn't open while this one downloaded.";
  $("#d-bands-h").hidden = !s.measured;
  $("#d-bands").replaceChildren(...(s.bands || []).map((b) => {
    const li = document.createElement("li");
    const pct = Math.round(b.seconds / s.measured * 100);
    const k = document.createElement("span"); k.textContent = b.label;
    const bar = document.createElement("span"); bar.className = "bar";
    const fill = document.createElement("span"); fill.style.width = Math.max(pct, 1) + "%"; bar.append(fill);
    const v = document.createElement("span"); v.textContent = dur(b.seconds) + " · " + pct + "%";
    li.append(k, bar, v);
    return li;
  }));
  $("#details").showModal();
}
$("#details .close").addEventListener("click", () => $("#details").close());
$("#details").addEventListener("click", (ev) => { if (ev.target === ev.currentTarget) ev.currentTarget.close(); });

$("#torrent-form").addEventListener("submit", async (ev) => {
  ev.preventDefault();
  const links = $("#t-links").value, files = $("#t-files").files, msg = $("#t-msg"), btn = ev.target.querySelector("button");
  msg.hidden = false;
  if (!links.trim() && !files.length) { msg.textContent = "Paste a link or choose a .torrent file first."; $("#t-links").focus(); return; }
  const fd = new FormData();
  fd.append("links", links);
  for (const f of files) fd.append("torrents", f, f.name);
  btn.disabled = true; msg.textContent = "Adding…";
  try {
    const headers = { "X-Library-Request": "1" };
    if (token) headers["X-Library-Token"] = token;
    const r = await fetch("/api/torrents", { method: "POST", headers, credentials: "same-origin", body: fd });
    const data = await r.json().catch(() => ({}));
    if (r.status === 401) { signedOut(); throw new Error("Please sign in again."); }
    if (!r.ok) throw new Error(data.error || "Something went wrong (" + r.status + ").");
    const parts = [];
    if (data.links) parts.push(data.links === 1 ? "1 link" : data.links + " links");
    if (data.files) parts.push(data.files === 1 ? "1 file" : data.files + " files");
    msg.textContent = "Added " + parts.join(" and ") + " to qBittorrent. They show in Activity.";
    ev.target.reset();
    load();
  } catch (e) { msg.textContent = e.message; }
  finally { btn.disabled = false; }
});
for (const id of ["#act-q", "#act-kind", "#act-sort"]) $(id).addEventListener("input", renderActivity);

function titleCell(m) {
  const box = document.createElement("div");
  box.textContent = m.title + " ";
  if (m.year) { const y = document.createElement("span"); y.className = "year"; y.textContent = m.year; box.append(y); }
  if (m.overview) { const o = document.createElement("span"); o.className = "sub"; o.textContent = m.overview.length > 160 ? m.overview.slice(0, 157) + "…" : m.overview; box.append(o); }
  return box;
}

let libItems = null;
async function loadLibrary() {
  $("#lib-empty").hidden = false;
  $("#lib-empty").textContent = "Loading…";
  try {
    const r = await api("/api/browse");
    libItems = r.items;
    $("#browse-notice").hidden = !r.problems || !r.problems.length;
    $("#browse-notice").textContent = (r.problems || []).join(" ");
  } catch (e) { libItems = []; $("#browse-notice").hidden = false; $("#browse-notice").textContent = e.message; }
  renderLibrary();
}

async function toggleMonitor(m, box) {
  const note = $("#browse-notice");
  box.disabled = true;
  try {
    await api("/api/media/monitor", { app: m.app, id: m.id, monitored: box.checked });
    m.monitored = box.checked;
    note.hidden = true;
  } catch (e) {
    box.checked = !box.checked;
    note.hidden = false;
    note.textContent = e.message;
  } finally { box.disabled = false; }
}

function renderLibrary() {
  if (!libItems) return;
  const q = $("#lib-q").value, type = $("#lib-type").value, sort = $("#lib-sort").value;
  let items = libItems.filter((m) => (!type || m.app === type) && has(m.title, q));
  items.sort(sort === "added" ? (a, b) => (b.added || "").localeCompare(a.added || "")
    : sort === "year" ? (a, b) => (b.year || 0) - (a.year || 0) || byTitle(a, b) : byTitle);
  $("#browse-lede").textContent = libItems.length + (libItems.length === 1 ? " item" : " items") + " looked after by Sonarr, Radarr and Lidarr.";
  $("#lib-empty").hidden = items.length > 0;
  $("#lib-empty").textContent = libItems.length ? "Nothing matches." : "Nothing in the library yet. Use Find & add.";
  $("#lib").hidden = items.length === 0;
  $("#lib tbody").replaceChildren(...items.map((m) => {
    let monitor = "—";
    if (m.id > 0 && typeof m.monitored === "boolean") {
      monitor = document.createElement("input");
      monitor.type = "checkbox";
      monitor.checked = m.monitored;
      monitor.setAttribute("aria-label", (m.monitored ? "Stop monitoring " : "Monitor ") + m.title);
      monitor.addEventListener("change", () => toggleMonitor(m, monitor));
    }
    const title = titleCell(m);
    if (m.id > 0) {
      const open = document.createElement("button");
      open.type = "button"; open.className = "inline title"; open.textContent = m.title;
      open.setAttribute("aria-label", "Details for " + m.title);
      open.addEventListener("click", () => openMediaDetails(m));
      title.firstChild.replaceWith(open, " ");
    }
    return row([title, TYPE[m.app] || m.app, m.haveFiles ? "In library" : "Waiting for files", monitor, m.added ? day(m.added) : ""], "lib");
  }));
}
for (const id of ["#lib-q", "#lib-type", "#lib-sort"]) $(id).addEventListener("input", renderLibrary);

$("#find-form").addEventListener("submit", async (ev) => {
  ev.preventDefault();
  const q = $("#find-q").value.trim(), type = $("#find-type").value, msg = $("#find-msg");
  if (!q) { $("#find-q").focus(); return; }
  msg.hidden = false; msg.textContent = "Searching…"; $("#found").hidden = true;
  try {
    const r = await api("/api/find?q=" + encodeURIComponent(q));
    const items = r.items.filter((m) => !type || m.app === type);
    msg.textContent = (items.length ? "" : "Nothing found for “" + q + "”. ") + (r.problems || []).join(" ");
    msg.hidden = !msg.textContent;
    $("#found").hidden = items.length === 0;
    $("#found tbody").replaceChildren(...items.map((m) => {
      let action = "In your library";
      if (!m.inLibrary) {
        action = document.createElement("button");
        action.type = "button"; action.className = "small"; action.textContent = "Add";
        action.setAttribute("aria-label", "Add " + m.title);
        action.addEventListener("click", () => openAdd(m, () => {
          action.disabled = true;
          action.textContent = "In library";
        }));
      }
      return row([titleCell(m), TYPE[m.app] || m.app, action], "found");
    }));
  } catch (e) { msg.textContent = e.message; }
});

let pendingAdd = null;
let addDone = null;

function fillOptions(sel, items, label, valueKey, textKey) {
  sel.replaceChildren();
  const ph = document.createElement("option");
  ph.value = ""; ph.textContent = label; ph.disabled = true; ph.selected = true;
  sel.append(ph);
  for (const it of items) {
    const o = document.createElement("option");
    o.value = String(it[valueKey]);
    o.textContent = it[textKey] || it.name || it.path || "ID " + it.id;
    sel.append(o);
  }
  sel.disabled = items.length === 0;
}

function addReady() {
  const q = $("#add-quality").value, r = $("#add-root").value;
  const metaOk = !pendingAdd || pendingAdd.app !== "Lidarr" || !!$("#add-meta").value;
  $("#add-form button.primary").disabled = !q || !r || !metaOk;
}

async function openAdd(m, done) {
  pendingAdd = m;
  addDone = done || null;
  $("#add-title").textContent = "Add " + m.title;
  $("#add-lede").textContent = "Choose where it goes in " + (m.app || "the library") + ". These choices come live from the app.";
  $("#add-msg").hidden = true;
  $("#add-quality").replaceChildren();
  $("#add-root").replaceChildren();
  $("#add-meta").replaceChildren();
  for (const id of ["#add-quality", "#add-root", "#add-meta"]) $(id).disabled = true;
  $("#add-meta-box").hidden = m.app !== "Lidarr";
  $("#add-form button.primary").disabled = true;
  $("#add-dialog").addEventListener("close", () => { pendingAdd = null; addDone = null; }, { once: true });
  if (!$("#add-dialog").open) $("#add-dialog").showModal();
  try {
    const r = await api("/api/media/options?app=" + encodeURIComponent(m.app));
    if (pendingAdd !== m) return;
    fillOptions($("#add-quality"), r.qualityProfiles || [], "Choose a quality profile", "id", "name");
    fillOptions($("#add-root"), r.rootFolders || [], "Choose a root folder", "path", "path");
    if (m.app === "Lidarr") fillOptions($("#add-meta"), r.metadataProfiles || [], "Choose a metadata profile", "id", "name");
    if ($("#add-quality").options.length < 2 || $("#add-root").options.length < 2) {
      throw new Error("No quality profiles or root folders were returned. Set them up in " + (m.app || "the app") + ", then try again.");
    }
    addReady();
  } catch (e) {
    $("#add-msg").hidden = false;
    $("#add-msg").textContent = e.message;
  }
}

for (const id of ["#add-quality", "#add-root", "#add-meta"]) $(id).addEventListener("change", addReady);
$("#add-cancel").addEventListener("click", () => $("#add-dialog").close());
$("#add-dialog .close").addEventListener("click", () => $("#add-dialog").close());
$("#add-dialog").addEventListener("click", (ev) => { if (ev.target === ev.currentTarget) ev.currentTarget.close(); });

$("#add-form").addEventListener("submit", async (ev) => {
  ev.preventDefault();
  if (!pendingAdd) return;
  const btn = $("#add-form button.primary"), msg = $("#add-msg");
  msg.hidden = true;
  const body = {
    app: pendingAdd.app,
    raw: pendingAdd.raw,
    qualityProfileId: Number($("#add-quality").value),
    rootFolderPath: $("#add-root").value,
  };
  if (pendingAdd.app === "Lidarr") body.metadataProfileId = Number($("#add-meta").value);
  const title = pendingAdd.title, done = addDone;
  btn.disabled = true; btn.textContent = "Adding…";
  try {
    const r = await api("/api/media/add", body);
    libItems = null;
    $("#add-dialog").close();
    if (done) done();
    const fm = $("#find-msg");
    fm.hidden = false;
    fm.textContent = "Added " + title + " to " + (r.folder || "the library") + ". It now shows in My library.";
  } catch (e) {
    msg.hidden = false;
    msg.textContent = e.message;
    btn.disabled = false;
  } finally { btn.textContent = "Add to library"; }
});

function fillForm(st) {
  const f = $("#form"), c = st.settings;
  f.tailscaleOn.checked = c.tailscaleOn;
  f.tailscalePort.value = c.tailscalePort || 7882;
  const remote = st.remote || {};
  $("#remote-status").textContent = remote.error || (remote.on ? "Ready for Tailscale connections." : "Tailscale access is off.");
  $("#remote-link-box").hidden = !remote.url;
  $("#remote-link").value = remote.url || "";
  f.cleanupOn.checked = c.cleanupOn;
  f.cleanupDays.value = c.cleanupDays;
  f.downloadPath.value = c.downloadPath;
  f.libraryPath.value = c.libraryPath;
  f["qbittorrent.url"].value = c.qbittorrent.url;
  f["qbittorrent.username"].value = c.qbittorrent.username;
  for (const a of ARRS) {
    f[a + ".url"].value = c[a].url;
  }
  for (const el of f.querySelectorAll("[data-secret]")) {
    el.value = "";
    el.placeholder = st.saved[el.dataset.secret] ? "Saved. Leave empty to keep it." : "";
  }
}

function buildArrFields() {
  const box = $("#arr-fields");
  for (const a of ARRS) {
    box.insertAdjacentHTML("beforeend",
      `<fieldset class="panel"><legend>${NAMES[a]}</legend>` +
      `<label>Address <input name="${a}.url" placeholder="http://127.0.0.1:${PORTS[a]}" inputmode="url"></label>` +
      `<label>API key <span class="hint">· in ${NAMES[a].split(" ")[0]}, Settings → General</span><input name="${a}.apiKey" type="password" autocomplete="off" data-secret="${a}"></label></fieldset>`);
  }
}

const menuToggle = $("#menu-toggle");
function closeMenu() {
  $(".side").classList.remove("menu-open");
  menuToggle.setAttribute("aria-expanded", "false");
}
menuToggle.addEventListener("click", () => {
  const open = $(".side").classList.toggle("menu-open");
  menuToggle.setAttribute("aria-expanded", String(open));
});
document.addEventListener("keydown", (ev) => {
  if (ev.key === "Escape" && menuToggle.getAttribute("aria-expanded") === "true") {
    closeMenu(); menuToggle.focus();
  }
});
document.addEventListener("click", (ev) => { if (!ev.target.closest(".side")) closeMenu(); });

function show(view) {
  closeMenu();
  for (const v of document.querySelectorAll(".view")) v.hidden = v.id !== "view-" + view;
  for (const b of document.querySelectorAll(".side [data-view]")) {
    if (b.dataset.view === view) b.setAttribute("aria-current", "page"); else b.removeAttribute("aria-current");
  }
  if (view === "settings" && current) { fillForm(current); loadSessions(); }
  if (view === "browse" && !libItems) loadLibrary();
  if (view === "downloads") loadDownloads();
  if (view === "sources") loadSources();
  if (view === "find") $("#find-q").focus();
  $(".content").scrollTo?.(0, 0);
  window.scrollTo(0, 0);
}
document.addEventListener("click", (ev) => {
  const b = ev.target.closest("[data-view]");
  if (b) show(b.dataset.view);
});

async function load() {
  if (!AUTH.authenticated) return;
  try { render(await api("/api/state")); }
  catch (e) { $("#summary").textContent = e.message; }
}

/* Downloads: pause and resume only. No delete action exists here. */
const DL_STATES = {
  downloading: "Downloading", forcedDL: "Downloading", stalledDL: "Stalled", metaDL: "Fetching metadata",
  queuedDL: "Queued", checkingDL: "Checking", pausedDL: "Paused", stoppedDL: "Paused",
  uploading: "Seeding", forcedUP: "Seeding", stalledUP: "Seeding, stalled", queuedUP: "Queued to seed",
  checkingUP: "Checking", pausedUP: "Paused", stoppedUP: "Paused",
  moving: "Moving", allocating: "Allocating", error: "Error", missingFiles: "Missing files", unknown: "Unknown",
};
const dlLabel = (s) => DL_STATES[s] || s || "Unknown";
const dlPaused = (s) => /^(paused|stopped)/i.test(s || "");
let downloads = null;
let dlError = "";
const selected = new Set();

function renderDownloads() {
  const items = downloads || [];
  const empty = $("#dl-empty"), table = $("#dl");
  table.hidden = items.length === 0;
  empty.hidden = items.length > 0;
  if (!items.length) empty.textContent = dlError || "No downloads in qBittorrent right now. Add one in Find & add.";
  $("#dl-count").textContent = dlError && !items.length ? "Not available"
    : items.length ? items.length + (items.length === 1 ? " download" : " downloads") : "No downloads";
  $("#dl tbody").replaceChildren(...items.map((t) => {
    const box = document.createElement("input");
    box.type = "checkbox";
    box.checked = selected.has(t.hash);
    box.setAttribute("aria-label", "Select " + t.name);
    box.addEventListener("change", () => {
      if (box.checked) selected.add(t.hash); else selected.delete(t.hash);
      updateDlButtons();
    });
    const state = document.createElement("span");
    const bad = /error|missing/i.test(t.state || "");
    state.className = "state " + (dlPaused(t.state) ? "paused" : bad ? "error" : "downloading");
    state.textContent = dlLabel(t.state);
    const speed = dlPaused(t.state) ? "—" : rate(t.dlspeed || 0) + " down";
    return row([box, t.name, state, Math.round((t.progress || 0) * 100) + "%", t.size ? bytes(t.size) : "—", speed], "dl");
  }));
  updateDlButtons();
}

function updateDlButtons() {
  const none = selected.size === 0;
  $("#dl-pause").disabled = none;
  $("#dl-resume").disabled = none;
}

async function loadDownloads() {
  const notice = $("#dl-notice"), empty = $("#dl-empty"), table = $("#dl");
  notice.hidden = true;
  empty.hidden = false;
  empty.textContent = "Loading…";
  table.hidden = true;
  selected.clear();
  dlError = "";
  try {
    const r = await api("/api/downloads");
    downloads = Array.isArray(r.items) ? r.items : [];
    dlError = r.error || "";
    if (dlError) { notice.hidden = false; notice.textContent = dlError; }
  } catch (e) {
    downloads = [];
    dlError = e.message;
  }
  renderDownloads();
}

async function dlAction(action) {
  const hashes = [...selected];
  if (!hashes.length) return;
  const notice = $("#dl-notice");
  notice.hidden = true;
  $("#dl-pause").disabled = true;
  $("#dl-resume").disabled = true;
  try {
    const r = await api("/api/downloads/action", { hashes, action });
    const errs = (r.errors || r.problems || []).filter(Boolean);
    selected.clear();
    await loadDownloads();
    if (errs.length) { notice.hidden = false; notice.textContent = errs.join(" "); }
  } catch (e) {
    const message = e.message;
    await loadDownloads();
    notice.hidden = false;
    notice.textContent = message;
  }
}
$("#dl-pause").addEventListener("click", () => dlAction("pause"));
$("#dl-resume").addEventListener("click", () => dlAction("resume"));
$("#dl-refresh").addEventListener("click", loadDownloads);

/* Sign-in, owner setup and sessions */
function showGate(mode) {
  $("#app-shell").hidden = true;
  $("#gate").hidden = false;
  for (const id of ["signin", "setup", "blocked", "recovery"]) $("#gate-" + id).hidden = id !== mode;
  document.title = (mode === "setup" ? "Create account" : mode === "recovery" ? "Recover access" : "Sign in") + " · Library Studio KH";
  const first = $("#gate-" + mode).querySelector("input, button");
  if (first) first.focus();
}

function hideGate() {
  $("#gate").hidden = true;
  $("#app-shell").hidden = false;
  document.title = "Library Studio KH";
}

function signedOut() {
  AUTH.authenticated = false;
  showGate(AUTH.setupRequired ? "blocked" : "signin");
}

function fillAuthCopy() {
  $("#remember-days").textContent = AUTH.rememberDays;
  $("#session-hours").textContent = AUTH.sessionHours;
  for (const hint of document.querySelectorAll(".password-min")) hint.textContent = AUTH.minPasswordLength;
  for (const input of document.querySelectorAll("#setup-form input[type=password], #recovery-form input[type=password]")) input.minLength = AUTH.minPasswordLength;
  $("#recovery-link").hidden = !token;
  $("#recovery-hint").hidden = !!token;
}

async function authStatus() {
  try {
    const headers = token ? { "X-Library-Token": token } : {};
    const r = await fetch("/api/auth/status", { headers, credentials: "same-origin" });
    if (!r.ok) return null;
    const d = await r.json();
    return typeof d.authenticated === "boolean" ? d : null;
  } catch { return null; }
}

async function enterApp() {
  hideGate();
  $("#auth-panel").hidden = !AUTH.available;
  $("#auth-note").textContent = AUTH.available
    ? "Browser sessions last " + AUTH.sessionHours + " hours, or " + AUTH.rememberDays + " days with Remember this device. The host desktop retains owner recovery access. Remote sign-in is encrypted by Tailscale; the page uses HTTP, not HTTPS."
    : "Sign-in is not available in this build yet. The desktop app's local access is in use.";
  await load();
  loadSessions();
}

async function loadSessions() {
  if (!AUTH.available || !AUTH.authenticated) return;
  const box = $("#sessions"), msg = $("#sessions-msg");
  try {
    const r = await api("/api/auth/sessions");
    const items = r.items || [];
    box.replaceChildren(...items.map((s) => {
      const li = document.createElement("li");
      const info = document.createElement("div");
      const when = document.createElement("span");
      when.textContent = "Signed in " + stamp(s.created) + " · ends " + stamp(s.expires);
      info.append(when);
      if (s.current) {
        const badge = document.createElement("span");
        badge.className = "badge";
        badge.textContent = "This browser";
        info.append(badge);
      }
      li.append(info);
      if (!s.current) {
        const b = document.createElement("button");
        b.type = "button"; b.className = "small"; b.textContent = "Revoke";
        b.setAttribute("aria-label", "Revoke the session from " + stamp(s.created));
        b.addEventListener("click", async () => {
          b.disabled = true;
          try { await api("/api/auth/revoke", { id: s.id }); await loadSessions(); }
          catch (e) {
            const message = e.message;
            await loadSessions();
            msg.hidden = false;
            msg.textContent = message;
          }
        });
        li.append(b);
      }
      return li;
    }));
    if (!items.length) {
      const li = document.createElement("li");
      li.className = "muted";
      li.textContent = "No signed-in sessions.";
      box.append(li);
    }
    msg.hidden = true;
  } catch (e) { msg.hidden = false; msg.textContent = e.message; }
}

$("#signin-form").addEventListener("submit", async (ev) => {
  ev.preventDefault();
  const f = ev.target, msg = $("#signin-msg"), btn = f.querySelector("button");
  msg.hidden = true;
  const username = f.username.value.trim(), password = f.password.value;
  if (!username || !password) { msg.hidden = false; msg.textContent = "Enter your username and password."; return; }
  btn.disabled = true; btn.textContent = "Signing in…";
  try {
    await api("/api/auth/login", { username, password, remember: f.remember.checked });
    AUTH.authenticated = true;
    AUTH.setupRequired = false;
    f.password.value = "";
    await enterApp();
  } catch (e) { msg.hidden = false; msg.textContent = e.message; }
  finally { btn.disabled = false; btn.textContent = "Sign in"; }
});

$("#setup-form").addEventListener("submit", async (ev) => {
  ev.preventDefault();
  const f = ev.target, msg = $("#setup-msg"), btn = f.querySelector("button");
  msg.hidden = true;
  const username = f.username.value.trim(), password = f.password.value;
  if (!username) { msg.hidden = false; msg.textContent = "Choose a username."; f.username.focus(); return; }
  if (password.length < AUTH.minPasswordLength) { msg.hidden = false; msg.textContent = "Use at least " + AUTH.minPasswordLength + " characters."; f.password.focus(); return; }
  if (password !== f.confirm.value) { msg.hidden = false; msg.textContent = "The two passwords do not match."; f.confirm.focus(); return; }
  btn.disabled = true; btn.textContent = "Creating…";
  let created = false;
  try {
    await api("/api/auth/setup", { username, password });
    created = true;
    await api("/api/auth/login", { username, password, remember: false });
    AUTH.authenticated = true;
    AUTH.setupRequired = false;
    await enterApp();
  } catch (e) {
    if (created) {
      showGate("signin");
      const m = $("#signin-msg");
      m.hidden = false;
      m.textContent = "Account created. Sign in with your new password.";
    } else {
      msg.hidden = false;
      msg.textContent = e.message;
    }
  } finally {
    if (created) { f.password.value = ""; f.confirm.value = ""; }
    btn.disabled = false; btn.textContent = "Create account";
  }
});

$("#recovery-form").addEventListener("submit", async (ev) => {
  ev.preventDefault();
  const f = ev.target, msg = $("#recovery-msg"), btn = f.querySelector("button.primary");
  msg.hidden = true;
  const username = f.username.value.trim(), password = f.password.value;
  if (!username) { msg.hidden = false; msg.textContent = "Enter your username."; f.username.focus(); return; }
  if (password.length < AUTH.minPasswordLength) { msg.hidden = false; msg.textContent = "Use at least " + AUTH.minPasswordLength + " characters."; f.password.focus(); return; }
  if (password !== f.confirm.value) { msg.hidden = false; msg.textContent = "The two passwords do not match."; f.confirm.focus(); return; }
  btn.disabled = true; btn.textContent = "Saving…";
  try {
    await api("/api/auth/password", { username, password });
    await api("/api/auth/login", { username, password, remember: false });
    AUTH.authenticated = true;
    AUTH.setupRequired = false;
    f.password.value = ""; f.confirm.value = "";
    await enterApp();
  } catch (e) {
    msg.hidden = false;
    msg.textContent = e.message;
  } finally { btn.disabled = false; btn.textContent = "Set new password"; }
});

$("#show-recovery").addEventListener("click", () => showGate("recovery"));
$("#cancel-recovery").addEventListener("click", () => showGate("signin"));
$("#gate-retry").addEventListener("click", start);

$("#logout").addEventListener("click", async () => {
  const btn = $("#logout");
  btn.disabled = true;
  try { await api("/api/auth/logout", null, true); }
  catch (e) {
    const msg = $("#sessions-msg"); msg.hidden = false; msg.textContent = "Sign-out could not be confirmed: " + e.message;
    btn.disabled = false;
    return;
  }
  token = "";
  sessionStorage.removeItem("token");
  AUTH.authenticated = false;
  fillAuthCopy();
  btn.disabled = false;
  showGate(AUTH.setupRequired ? "blocked" : "signin");
});

$("#form").addEventListener("submit", async (ev) => {
  ev.preventDefault();
  const f = ev.target, msg = $("#form-msg"), btn = f.querySelector("button.primary");
  const days = Number(f.cleanupDays.value);
  if (!Number.isInteger(days) || days < 0 || days > 3650) {
    msg.hidden = false; msg.textContent = "Days must be a whole number from 0 to 3650."; f.cleanupDays.focus(); return;
  }
  const body = {
    tailscaleOn: f.tailscaleOn.checked, tailscalePort: Number(f.tailscalePort.value),
    cleanupOn: f.cleanupOn.checked, cleanupDays: days,
    downloadPath: f.downloadPath.value.trim(), libraryPath: f.libraryPath.value.trim(),
    qbittorrent: { url: f["qbittorrent.url"].value.trim(), username: f["qbittorrent.username"].value.trim(), password: f["qbittorrent.password"].value },
  };
  for (const a of ARRS) body[a] = { url: f[a + ".url"].value.trim(), apiKey: f[a + ".apiKey"].value.trim() };
  btn.disabled = true; msg.hidden = false; msg.textContent = "Saving…";
  try {
    const r = await api("/api/settings", body);
    msg.textContent = r.problems && r.problems.length
      ? "Saved, but these settings couldn't be applied:" + r.problems.join("; ")
      : "Saved.";
    await load();
    fillForm(current);
  } catch (e) { msg.textContent = e.message; }
  finally { btn.disabled = false; }
});

async function start() {
  const st = await authStatus();
  if (st) {
    AUTH.available = true;
    AUTH.authenticated = !!st.authenticated;
    AUTH.setupRequired = !!st.setupRequired;
    if (Number.isFinite(st.rememberDays)) AUTH.rememberDays = st.rememberDays;
    if (Number.isFinite(st.sessionHours)) AUTH.sessionHours = st.sessionHours;
    if (Number.isInteger(st.minPasswordLength) && st.minPasswordLength > 0) AUTH.minPasswordLength = st.minPasswordLength;
  } else {
    AUTH.available = true;
    AUTH.authenticated = false;
    fillAuthCopy();
    showGate("signin");
    $("#signin-msg").hidden = false;
    $("#signin-msg").textContent = "The host's sign-in service could not be reached. Check the host connection and try again.";
    return;
  }
  fillAuthCopy();
  if (!AUTH.authenticated) { showGate(AUTH.setupRequired ? "blocked" : "signin"); return; }
  if (AUTH.setupRequired) { showGate("setup"); return; }
  await enterApp();
}

$("#open-public-help").addEventListener("click", () => {
  const copy = $("#view-help").cloneNode(true);
  copy.removeAttribute("id"); copy.hidden = false;
  for (const node of copy.querySelectorAll("[id]")) node.removeAttribute("id");
  $("#public-help-content").replaceChildren(copy);
  $("#gate").hidden = true;
  $("#public-help").hidden = false;
  $("#close-public-help").focus();
});
$("#close-public-help").addEventListener("click", () => {
  $("#public-help").hidden = true;
  start();
});

buildArrFields();
start();
setInterval(() => {
  if (!$("#app-shell").hidden && $("#view-settings").hidden) {
    load();
    if (!$("#view-downloads").hidden) loadDownloads();
  }
}, 30000);
