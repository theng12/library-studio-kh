"use strict";
// The launch token arrives in the address (#token); keep it for reloads and hide it from the address bar.
if (location.hash.length > 1) {
  sessionStorage.setItem("token", location.hash.slice(1));
  history.replaceState(null, "", location.pathname);
}
let token = sessionStorage.getItem("token") || "";
function clearLocalToken() { token = ""; sessionStorage.removeItem("token"); }
window.libraryClearToken = clearLocalToken;
const $ = (s) => document.querySelector(s);
const ARRS = ["sonarr", "radarr", "lidarr", "prowlarr", "bazarr", "whisparr"];
const NAMES = { sonarr: "Sonarr (TV)", radarr: "Radarr (films)", lidarr: "Lidarr (music)", prowlarr: "Prowlarr (search sites)", bazarr: "Bazarr (subtitles · optional)", whisparr: "Whisparr (optional)" };
const PORTS = { sonarr: 8989, radarr: 7878, lidarr: 8686, prowlarr: 9696, bazarr: 6767, whisparr: 6969 };
const AUTH_FORM_PATHS = new Set(["/api/auth/login", "/api/auth/setup", "/api/auth/password"]);
const AUTH = { available: false, authenticated: false, setupRequired: false, rememberDays: 30, sessionHours: 12, minPasswordLength: 6 };
let current = null;

// Appearance: System follows this Mac; Light/Dark are explicit choices. An older
// saved "default" means System.
const darkQuery = window.matchMedia("(prefers-color-scheme: dark)");
function applyAppearance(saved) {
  const choice = !saved || saved === "default" ? "system" : saved;
  document.documentElement.dataset.theme = choice === "system" ? (darkQuery.matches ? "dark" : "light") : choice;
}
darkQuery.addEventListener("change", () => {
  if ((current?.settings.appearance || "system") === "system") applyAppearance("system");
});

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
const time = (iso) => new Date(iso).toLocaleString(undefined, { weekday: "short", hour: "numeric", minute: "2-digit" });
function gb(n) {
  return n >= 1e12 ? (n / 1e12).toFixed(1) + " TB" : Math.round(n / 1e9) + " GB";
}

function torrentStateWord(state) {
  const word = (state || "").toLowerCase();
  if (word.startsWith("paused") || word.startsWith("stopped")) return "Paused";
  if (word.startsWith("queued")) return "Queued";
  if (word.startsWith("stalled")) return "Stalled";
  if (word.startsWith("checking") || word === "moving") return word === "moving" ? "Moving" : "Checking";
  if (word === "missingfiles") return "Missing files";
  return "Downloading";
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
  fillWebAddresses(st);
  const s = st.snapshot, cfg = st.settings;
  $("#version").textContent = st.version;
  const updatesVersion = $("#updates-version");
  if (updatesVersion) updatesVersion.textContent = "This Mac runs Library Studio KH " + st.version + " · checks are manual, and nothing installs without you.";
  applyAppearance(cfg.appearance || "system");
  // Setup is done once a download client and a library app are both saved.
  const configured = !!cfg.qbittorrent?.url && ["sonarr", "radarr", "lidarr"].some((a) => !!cfg[a]?.url);
  for (const el of document.querySelectorAll(".first-run")) el.hidden = configured;

  const parts = [];
  parts.push(s.freeBytes == null ? "Free space on this computer: not known yet." : "<strong>" + gb(s.freeBytes) + "</strong> free on this computer.");
  parts.push(cfg.stagedCleanupOn
    ? "Staged cleanup is <strong>on</strong> for verified imports."
    : "Local copies are <strong>kept</strong>. Staged cleanup is off.");
  if (s.library === "missing") parts.push("<span class=off>The NAS library folder can't be found.</span>");
  if (st.metadata) parts.push("<span class=off>" + st.metadata + "</span>");
  $("#summary").innerHTML = parts.join(" ");

  $("#apps").replaceChildren(...s.apps.map((a) => {
    const li = document.createElement("li");
    li.className = a.ok ? "ok" : "bad";
    li.textContent = a.name + (a.ok ? " · Connected" : " · Offline");
    li.title = a.ok ? "Connected" : a.error || "Not connected";
    li.setAttribute("aria-label", a.name + ": " + li.title);
    return li;
  }));

  const notice = $("#notice");
  notice.hidden = !s.blocked;
  notice.textContent = s.blocked || "";

  renderActivity();

  // Cleanup's own line: its policy and the truth about the library, never a
  // second free-space figure (the status table below carries that one).
  const lede = [cfg.stagedCleanupOn
    ? "Staged cleanup is <strong>on</strong> for verified imports."
    : "Local copies are <strong>kept</strong>. Staged cleanup is off."];
  if (s.library === "missing") lede.push("<span class=off>The NAS library folder can't be found.</span>");
  if (st.metadata) lede.push("<span class=off>" + st.metadata + "</span>");
  $("#cleanup-lede").innerHTML = lede.join(" ");

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

// Repeated warnings are one line with a count, never the same sentence twice.
function groupMessages(list) {
  const counts = new Map();
  for (const raw of list || []) {
    const text = String(raw || "").trim();
    if (!text) continue;
    counts.set(text, (counts.get(text) || 0) + 1);
  }
  return [...counts].map(([text, n]) => n > 1 ? text + " ×" + n : text);
}

const has = (title, q) => title.toLowerCase().includes(q.trim().toLowerCase());
const TYPE = { Sonarr: "TV", Radarr: "Film", Lidarr: "Music" };

// One line per real event, newest first. Events come from the same snapshot the
// other pages use, so Activity can never claim something the item record denies.
function activityLabel(it) {
  const status = it.status || it.kind || "";
  if (/^(moving|checking)/i.test(it.state || "")) {
    // qBittorrent is relocating a finished download into place: the same
    // Importing state the other pages show, with the operation as the detail.
    const word = torrentStateWord(it.state);
    return {
      outcome: "importing",
      label: "Importing",
      detail: (word === "Moving" ? "Moving the finished download into place" : "Checking the finished download") + (it.progress >= 0 && it.progress <= 1 ? " · " + downloadProgressText(it.progress) : ""),
      at: it.statusAt || null,
    };
  }
  if (status === "copied" || status === "moved") {
    const where = libraryVolumeLabel(it.targetVolume);
    const done = status === "moved"
      ? (where ? "Moved to " + where : "Moved off this computer")
      : (where ? "Copied to " + where : "Copied");
    if (it.verified === "hash" || it.verified === "size") {
      return {
        outcome: "verified",
        label: done + " · verified",
        detail: (it.verified === "hash" ? "SHA-256 hash match" : "Size and readback match")
          + (it.targetPath ? " · " + libraryGridBreakPath(it.targetPath) : " · location not recorded"),
        at: it.verifiedAt || it.statusAt || it.when || null,
      };
    }
    return { outcome: "problems", label: done + " · not verified yet", detail: "The library app recorded the import, but no verification proof is saved yet. Cleanup’s Preview checks the copy.", at: it.statusAt || it.when || null };
  }
  if (status === "importing") return { outcome: "importing", label: "Importing", detail: "", at: it.statusAt || null };
  if (status === "paused" || status === "resumed") {
    return { outcome: "downloading", label: status === "paused" ? "Paused by you" : "Resumed by you", detail: it.sourcePath || "", at: it.when || null };
  }
  if (status === "download-removed") {
    return { outcome: "removed", label: "Removed from qBittorrent · files kept", detail: it.sourcePath ? "Still on disk at " + it.sourcePath : "Its files were left on disk.", at: it.when || null };
  }
  if (status === "download-cleared") {
    return { outcome: "removed", label: "Cleared from qBittorrent · files were already missing", detail: "", at: it.when || null };
  }
  if (status === "cleanup-restored") {
    return { outcome: "removed", label: "Restored from Library’s _trash", detail: it.sourcePath ? "Back at " + libraryGridBreakPath(it.sourcePath) : "", at: it.when || null };
  }
  if (status === "cleanup-moved") {
    return { outcome: "removed", label: "Moved into Library’s _trash", detail: it.sourcePath ? "Returns to " + libraryGridBreakPath(it.sourcePath) : "", at: it.when || null };
  }
  if (status === "cleanup-pass") {
    return { outcome: "removed", label: "Cleanup pass", detail: it.sourcePath || "", at: it.when || null };
  }
  if (status === "download-dead-removed") {
    return { outcome: "removed", label: "Removed torrent · files stayed", detail: it.sourcePath ? "Still on disk at " + libraryGridBreakPath(it.sourcePath) : "", at: it.when || null };
  }
  if (status === "download-arr-removed") {
    return { outcome: "removed", label: "Removed from " + (it.app || "app") + " queue · files stayed", detail: it.sourcePath ? "Still on disk at " + libraryGridBreakPath(it.sourcePath) : "", at: it.when || null };
  }
  if (status === "downloading" || status === "queued" || status === "stalled") {
    const word = torrentStateWord(it.state);
    const broken = word === "Missing files" || /error/i.test(it.state || "");
    return {
      outcome: broken ? "problems" : "downloading",
      label: broken ? (word === "Missing files" ? word : "Error") : word || (status === "queued" ? "Queued" : "Downloading"),
      detail: downloadProgressText(it.progress) + (it.speed ? " · " + rate(it.speed) : ""),
      at: it.statusAt || null,
    };
  }
  if (status === "removed") return { outcome: "removed", label: "Local copy removed", detail: it.targetPath || "Removed after a verified import.", at: it.when || it.statusAt || null };
  if (status === "upcoming") return { outcome: "upcoming", label: "Coming up", detail: "", at: it.when || null };
  if (status === "missing") return { outcome: "problems", label: "Files missing", detail: "Its source is gone; open Downloads to act on it.", at: it.statusAt || null };
  if (status === "on-disk") return { outcome: "problems", label: "On disk · no import proof", detail: "The files are there, but no verified import record explains them.", at: it.statusAt || null };
  return { outcome: "problems", label: it.kind || "Recorded", detail: "", at: it.statusAt || null };
}

function realTime(value) {
  if (!value) return null;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) || date.getFullYear() < 2000 ? null : value;
}

function activityEntries() {
  const s = current.snapshot;
  const entries = [];
  for (const it of s.items || []) {
    const info = activityLabel(it);
    info.at = realTime(info.at);
    const app = it.app || "qBittorrent";
    entries.push({ ...info, title: it.title || "Untitled", app, detail: info.detail ? info.detail + (it.app ? " · " + it.app : "") : "", stats: it.stats || null, item: it });
  }
  for (const a of s.apps || []) {
    if (a.ok) continue;
    entries.push({ at: null, outcome: "problems", label: "Service error", title: a.name, app: a.name, detail: a.error || "It did not answer.", stats: null });
  }
  // Identical events are one row with a count, never duplicate lines.
  const seen = new Map();
  for (const entry of entries) {
    const key = [entry.title, entry.label, entry.detail, entry.at || ""].join("|");
    const hit = seen.get(key);
    if (hit) hit.count = (hit.count || 1) + 1;
    else { entry.count = 1; seen.set(key, entry); }
  }
  return [...seen.values()];
}

function renderActivity() {
  if (!current) return;
  const s = current.snapshot, q = $("#act-q").value, app = $("#act-app").value, outcome = $("#act-outcome").value;
  const appSelect = $("#act-app"), keep = app;
  const apps = [...new Set([...(s.items || []).map((it) => it.app).filter(Boolean), ...(s.apps || []).map((a) => a.name)])].sort();
  appSelect.replaceChildren(mediaOption("", "All apps"), ...apps.map((name) => mediaOption(name, name)));
  appSelect.value = apps.includes(keep) ? keep : "";

  const all = activityEntries();
  const entries = all.filter((e) => (!app || e.app === app) && (!outcome || e.outcome === outcome) && (!q.trim() || has(e.title, q)));
  entries.sort((a, b) => {
    if (!a.at && !b.at) return a.title.localeCompare(b.title);
    if (!a.at) return 1; // "Time not recorded" belongs at the end, not the top
    if (!b.at) return -1;
    return new Date(b.at) - new Date(a.at);
  });
  const empty = $("#empty");
  empty.hidden = entries.length > 0;
  let emptyText = "Nothing matches these filters.";
  if (!(s.apps || []).length) emptyText = "Nothing connected yet. Open Settings and add qBittorrent and at least one of Sonarr, Radarr or Lidarr.";
  else if (!all.length) emptyText = "Nothing has happened yet. Downloads and imports appear here as they run.";
  empty.textContent = emptyText;
  if (all.length && !entries.length) {
    const clear = document.createElement("button");
    clear.type = "button";
    clear.className = "inline";
    clear.textContent = "Clear filters";
    clear.addEventListener("click", () => {
      $("#act-q").value = "";
      $("#act-app").value = "";
      $("#act-outcome").value = "";
      renderActivity();
      $("#act-q").focus();
    });
    empty.append(" ", clear);
  }
  if (!(s.apps || []).length) {
    const openSettings = document.createElement("button");
    openSettings.type = "button";
    openSettings.className = "inline";
    openSettings.textContent = "Settings";
    openSettings.addEventListener("click", () => show("settings"));
    empty.append(document.createTextNode(" "), openSettings);
  } else if (!all.length) {
    // A connected first run: say what to do next instead of only what will happen.
    const find = document.createElement("button");
    find.type = "button";
    find.className = "inline";
    find.textContent = "Find & add something";
    find.addEventListener("click", () => show("find"));
    empty.append(document.createTextNode(" "), find);
  }
  $("#timeline").replaceChildren(...entries.map((e) => {
    const li = document.createElement("li");
    li.className = "timeline-item timeline-" + e.outcome;
    const when = document.createElement("time");
    when.className = "timeline-time";
    if (e.at) { when.dateTime = String(e.at); when.textContent = stamp(e.at); }
    else when.textContent = "Time not recorded";
    const dot = document.createElement("span");
    dot.className = "timeline-dot";
    dot.setAttribute("aria-hidden", "true");
    const main = document.createElement("div");
    main.className = "timeline-main";
    const head = document.createElement("p");
    head.className = "timeline-head";
    let title = e.title;
    if (e.stats) {
      title = document.createElement("button");
      title.type = "button"; title.className = "inline title"; title.textContent = e.title;
      title.addEventListener("click", () => openDetails(e.item));
    }
    const label = document.createElement("span");
    label.className = "timeline-label";
    label.textContent = e.label;
    head.append(title, label);
    if ((e.count || 1) > 1) {
      const count = document.createElement("span");
      count.className = "timeline-count";
      count.textContent = "×" + e.count;
      count.title = e.count + " identical events";
      head.append(count);
    }
    const detail = document.createElement("p");
    detail.className = "timeline-detail muted";
    detail.textContent = e.detail;
    const source = document.createElement("span");
    source.className = "timeline-app";
    source.textContent = e.app;
    main.append(head, detail);
    li.append(when, dot, main, source);
    return li;
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
    ["Finished", done ? stamp(s.completed) : finished ? "Not known" : "Still downloading (" + downloadProgressText(it.progress) + ")"],
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
for (const id of ["#act-q", "#act-app", "#act-outcome"]) $(id).addEventListener("input", renderActivity);
$("#act-refresh").addEventListener("click", () => load());

function titleCell(m) {
  const box = document.createElement("div");
  box.textContent = m.title + " ";
  if (m.year) { const y = document.createElement("span"); y.className = "year"; y.textContent = m.year; box.append(y); }
  if (m.overview) { const o = document.createElement("span"); o.className = "sub"; o.textContent = m.overview.length > 160 ? m.overview.slice(0, 157) + "…" : m.overview; box.append(o); }
  return box;
}

/* My library: poster grid over the reconciled inventory. The server owns the
   join between library items, import proof and cached metadata. */
// The Cards/Table choice is remembered per app, not per visit.
function storedLibraryView() {
  try { return localStorage.getItem("library-view") === "table" ? "list" : "grid"; } catch { return "grid"; }
}
function storeLibraryView(view) {
  try { localStorage.setItem("library-view", view === "list" ? "table" : "cards"); } catch {}
}

const lib = { items: [], problems: [], metadata: "", loading: false, error: "", filters: {}, draft: {}, sort: { key: "title", dir: "asc" }, selected: null, filtersOpen: false, view: storedLibraryView() };
let libraryLoading = false;

async function loadLibrary() {
  if (libraryLoading) return;
  libraryLoading = true;
  lib.loading = !lib.items.length;
  if (!lib.items.length) renderLibraryPage();
  try {
    const r = await api("/api/library");
    lib.items = Array.isArray(r.items) ? r.items : [];
    lib.problems = (r.problems || []).filter(Boolean);
    lib.metadata = r.metadata || "";
    lib.error = "";
  } catch (e) {
    lib.error = e.message;
  }
  libraryLoading = false;
  lib.loading = false;
  renderLibraryPage();
}

window.libraryReloadLibrary = () => loadLibrary();

function renderLibraryPage() {
  if (!current) return;
  const host = $("#library-host");
  const focused = document.activeElement;
  const searchFocus = focused?.id === "lib-q" ? focused.selectionStart : null;
  const rowFocus = focused?.closest?.("[data-key]")?.dataset.key || null;
  const wasFilters = focused?.classList?.contains("libgrid-filters-toggle") || false;
  const unverified = lib.items.filter((item) =>
    /^(copied|moved)$/.test(item.status || "") && !(item.verified === "hash" || item.verified === "size")).length;
  $("#browse-lede").textContent = (lib.items.length
    ? lib.items.length + (lib.items.length === 1 ? " item" : " items") + " across Sonarr, Radarr and Lidarr. Status and proof come from import records."
    : "Everything Sonarr, Radarr and Lidarr look after.")
    + (lib.metadata ? " " + lib.metadata : "")
    + (unverified ? " " + unverified + (unverified === 1 ? " imported copy has" : " imported copies have") + " no verification proof yet — open one to see whether a local copy is tracked." : "");
  const notice = $("#browse-notice");
  const groupedProblems = groupMessages(lib.problems);
  notice.hidden = !groupedProblems.length;
  notice.textContent = groupedProblems.join(" ");
  const selected = lib.items.find((item) => libraryGridKey(item) === lib.selected) || null;
  const shownSelected = libraryGridFilter(lib.items, lib.filters).find((item) => libraryGridKey(item) === lib.selected) || null;
  host.replaceChildren(libraryGridPage({
    items: lib.items,
    filters: lib.filters,
    draft: { ...lib.filters, ...lib.draft },
    view: lib.view,
    sort: lib.sort,
    selected: lib.selected,
    count: libraryGridCount(libraryGridFilter(lib.items, lib.filters).length, lib.items.length),
    loading: lib.loading,
    error: lib.error,
    problems: [],
    filtersOpen: lib.filtersOpen,
    posterURL: (item) => item.posterPath ? "/api/poster?path=" + encodeURIComponent(item.posterPath) : "",
    handlers: {
      onSearch: (value) => { lib.filters = { ...lib.filters, query: value }; renderLibraryPage(); },
      onFilterChange: (next) => { const { query, ...rest } = next || {}; lib.draft = rest; renderLibraryPage(); },
      onToggleFilters: () => { lib.filtersOpen = !lib.filtersOpen; renderLibraryPage(); },
      onApply: () => { lib.filters = { ...lib.filters, ...lib.draft }; lib.draft = {}; lib.filtersOpen = false; renderLibraryPage(); },
      onReset: () => { lib.filters = {}; lib.draft = {}; renderLibraryPage(); },
      onView: (view) => {
        lib.view = view === "list" ? "list" : "grid";
        storeLibraryView(lib.view);
        renderLibraryPage();
      },
      onSort: (key) => {
        lib.sort = { key, dir: lib.sort.key === key && lib.sort.dir === "asc" ? "desc" : "asc" };
        renderLibraryPage();
      },
      onSelect: (key) => { lib.selected = key; renderLibraryPage(); },
      onRetry: () => loadLibrary(),
      onFindAndAdd: () => show("find"),
      onVerify: async (detail) => {
        if (!detail.downloadHash) return "No local copy is tracked for this item yet, so there is nothing to check.";
        try {
          const r = await api("/api/cleanup/verify", { hash: detail.downloadHash });
          return (r.actions || []).join(" ") || "Verified.";
        } catch (e) {
          if (/isn.t tracked/i.test(e.message)) {
            return "No local copy is tracked for this item yet, so there is nothing to check. Cleanup follows imports that have a saved proof.";
          }
          throw e;
        }
      },
    },
  }));
  // Below 768 the detail pushes the list away; start it at the top with Back focused.
  if (shownSelected && window.innerWidth <= 768) {
    $(".content").scrollTo?.(0, 0);
    window.scrollTo(0, 0);
    host.querySelector(".libgrid-back")?.focus();
  }
  const openDetails = $("#lib-open-details");
  // Only when the detail panel is actually on screen; a filtered-out or closed
  // selection must not open a title the owner can no longer see.
  openDetails.hidden = !shownSelected;
  openDetails.onclick = shownSelected
    ? () => openMediaDetails({ app: shownSelected.app, id: Number(shownSelected.mediaId || shownSelected.id), title: shownSelected.title })
    : null;
  if (searchFocus !== null) {
    const search = $("#lib-q");
    search?.focus();
    search?.setSelectionRange?.(searchFocus, searchFocus);
  } else if (rowFocus) {
    host.querySelector('[data-key="' + CSS.escape(rowFocus) + '"]')?.focus();
  } else if (wasFilters) {
    host.querySelector(".libgrid-filters-toggle")?.focus();
  }
}


// Arrow keys move focus through the poster grid; Enter opens the focused card.
$("#library-host").addEventListener("keydown", (ev) => {
  if (!["ArrowRight", "ArrowLeft", "ArrowDown", "ArrowUp"].includes(ev.key) || ev.altKey || ev.metaKey || ev.ctrlKey) return;
  const cards = [...document.querySelectorAll("#library-host .libgrid-card, #library-host .libgrid-row")];
  const index = cards.indexOf(document.activeElement);
  if (index < 0) return;
  const grid = document.querySelector("#library-host .libgrid-grid");
  const columns = grid ? Math.max(1, getComputedStyle(grid).gridTemplateColumns.split(" ").length) : 1;
  const step = ev.key === "ArrowRight" ? 1 : ev.key === "ArrowLeft" ? -1 : ev.key === "ArrowDown" ? columns : -columns;
  const next = cards[index + step];
  if (next) { ev.preventDefault(); next.focus(); }
});

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
let addBusy = false;

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
  $("#add-form button.primary").disabled = addBusy || !q || !r || !metaOk;
  $("#add-form button.primary").textContent = addBusy ? "Adding…" : $("#add-search").checked ? "Add and search" : "Add to library";
}

function addMonitoringChanged() {
  const search = $("#add-search");
  search.disabled = !$("#add-monitor").checked;
  if (search.disabled) search.checked = false;
  addReady();
}

async function openAdd(m, done) {
  if (addBusy) return;
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
  $("#add-monitor").checked = true;
  $("#add-search").checked = false;
  $("#add-search").disabled = false;
  $("#add-monitor-label").textContent = m.app === "Sonarr" ? "Monitor all seasons and episodes" : m.app === "Radarr" ? "Monitor this film" : "Monitor this artist and albums";
  $("#add-search-label").textContent = m.app === "Sonarr" ? "Search for missing episodes now" : m.app === "Radarr" ? "Search for this film now" : "Search for missing albums now";
  $("#add-monitor-help").textContent = m.app + " will watch for matching releases using your quality settings. You can turn monitoring off; existing files are kept.";
  $("#add-form button.primary").textContent = "Add to library";
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

for (const id of ["#add-quality", "#add-root", "#add-meta", "#add-search"]) $(id).addEventListener("change", addReady);
$("#add-monitor").addEventListener("change", addMonitoringChanged);
$("#add-cancel").addEventListener("click", () => $("#add-dialog").close());
$("#add-dialog .close").addEventListener("click", () => $("#add-dialog").close());
$("#add-dialog").addEventListener("click", (ev) => { if (ev.target === ev.currentTarget) ev.currentTarget.close(); });

$("#add-form").addEventListener("submit", async (ev) => {
  ev.preventDefault();
  if (!pendingAdd || addBusy) return;
  const btn = $("#add-form button.primary"), msg = $("#add-msg");
  msg.hidden = true;
  const body = {
    app: pendingAdd.app,
    raw: pendingAdd.raw,
    qualityProfileId: Number($("#add-quality").value),
    rootFolderPath: $("#add-root").value,
    monitored: $("#add-monitor").checked,
    search: $("#add-monitor").checked && $("#add-search").checked,
  };
  if (pendingAdd.app === "Lidarr") body.metadataProfileId = Number($("#add-meta").value);
  const title = pendingAdd.title, done = addDone;
  addBusy = true;
  btn.disabled = true; btn.textContent = "Adding…";
  try {
    const r = await api("/api/media/add", body);
    if (typeof lib !== "undefined") lib.items = [];
    $("#add-dialog").close();
    if (done) done();
    const fm = $("#find-msg");
    fm.hidden = false;
    fm.textContent = "Added " + title + " to " + (r.folder || "the library") + ". Monitoring " + (body.monitored ? "on" : "off") + ". " + (body.search ? "Search requested; check Downloads for progress." : "No immediate search requested.");
  } catch (e) {
    msg.hidden = false;
    msg.textContent = e.message;
    btn.disabled = false;
  } finally { addBusy = false; addReady(); }
});

function fillForm(st) {
  const f = $("#form"), c = st.settings;
  f.tailscaleOn.checked = c.tailscaleOn;
  f.tailscalePort.value = c.tailscalePort || 7882;
  const remote = st.remote || {};
  $("#remote-status").textContent = remote.error || (remote.on ? "Ready for Tailscale connections." : "Tailscale access is off.");
  $("#remote-link-box").hidden = !remote.url;
  $("#remote-link").value = remote.url || "";
  const saved = c.appearance === "default" ? "system" : (c.appearance || "system");
  for (const r of f.querySelectorAll('input[name="appearance"]')) r.checked = r.value === saved;
  f.bazarrOn.checked = !!c.bazarrOn;
  f.whisparrOn.checked = !!c.whisparrOn;
  f.deadCleanupOn.checked = !!c.deadCleanupOn;
  f.deadCleanupHours.value = c.deadCleanupHours || 24;
  f.deadArrCleanupOn.checked = !!c.deadArrCleanupOn;
  f.downloadPath.value = c.downloadPath;
  f.libraryPath.value = c.libraryPath;
  for (const name of ["downloadPath", "libraryPath"]) {
    const input = (f.elements || {})[name] || f[name];
    if (!input || !input.parentElement || typeof input.insertAdjacentElement !== "function") continue;
    let hint = input.parentElement.querySelector(".path-full");
    if (!hint) {
      hint = document.createElement("p");
      hint.className = "path-full muted";
      input.insertAdjacentElement("afterend", hint);
    }
    hint.textContent = input.value ? "Full path: " + input.value : "Not set";
    input.title = input.value;
  }
  f["qbittorrent.url"].value = c.qbittorrent.url;
  f["qbittorrent.username"].value = c.qbittorrent.username;
  for (const a of ARRS) {
    f[a + ".url"].value = c[a]?.url || "";
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
      (a === "bazarr" ? `<label class="check"><input type="checkbox" name="bazarrOn"> Enable subtitle controls <span class="hint">· searches run on request; Bazarr's own tasks are separate</span></label>` : "") +
      (a === "whisparr" ? `<label class="check"><input type="checkbox" name="whisparrOn"> Enable Whisparr connection <span class="hint">· off by default; first-run support unverified</span></label>` : "") +
      `<label>Address <input name="${a}.url" placeholder="http://127.0.0.1:${PORTS[a]}" inputmode="url"></label>` +
      `<label>API key <span class="hint">· ${NAMES[a].split(" ")[0]} → Settings → General</span><input name="${a}.apiKey" type="password" autocomplete="off" data-secret="${a}"></label></fieldset>`);
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
// Escape always does the job of where it is: close the open dialog, clear a
// focused search, close the mobile menu. It never silently does nothing.
document.addEventListener("keydown", (ev) => {
  if (ev.key !== "Escape" || ev.isComposing || ev.keyCode === 229 || ev.defaultPrevented) return;
  const t = ev.target;
  const dialog = [...document.querySelectorAll("dialog[open]")].pop();
  if (dialog) {
    ev.preventDefault();
    dialog.close();
    return;
  }
  const rowMenu = [...document.querySelectorAll("details.row-menu[open]")].pop();
  if (rowMenu) {
    ev.preventDefault();
    rowMenu.open = false;
    rowMenu.querySelector("summary")?.focus();
    return;
  }

  const search = t?.closest?.('input[type="search"]');
  if (search) {
    if (search.value) {
      ev.preventDefault();
      search.value = "";
      search.dispatchEvent(new Event("input", { bubbles: true }));
    }
    return;
  }
  // Close a page-level detail pane and put focus back on the row that opened it.
  if (!$("#view-downloads").hidden && dl.openKey) {
    const key = dl.openKey;
    ev.preventDefault();
    dl.openKey = null;
    renderDownloadsPage();
    document.querySelector('#dl tr[data-key="' + CSS.escape(key) + '"] .dlp-open')?.focus();
    return;
  }
  if (!$("#view-browse").hidden && lib.selected) {
    const key = lib.selected;
    ev.preventDefault();
    lib.selected = null;
    renderLibraryPage();
    document.querySelector('#library-host [data-key="' + CSS.escape(key) + '"]')?.focus();
    return;
  }
  if (menuToggle.getAttribute("aria-expanded") !== "true") return;
  const editableHost = t?.closest?.("[contenteditable]");
  if (t?.closest?.("input, textarea, select")
    || (editableHost && editableHost.getAttribute("contenteditable") !== "false")) return;
  ev.preventDefault();
  closeMenu(); menuToggle.focus();
});
document.addEventListener("click", (ev) => {
  if (!ev.target.closest(".side")) closeMenu();
  for (const menu of document.querySelectorAll("details.row-menu[open]")) {
    if (!menu.contains(ev.target)) menu.open = false;
  }
});

// Save feedback belongs to the save that produced it, never to another tab
// (including keyboard tab changes, which settings-view reports as an event).
document.addEventListener("settings:tab", () => {
  for (const id of ["#settings-save-msg", "#form-msg"]) {
    const msg = $(id);
    if (msg) msg.textContent = "";
  }
  settingsCardsForPhone();
});

// Cmd/Ctrl+F focuses the current screen's search, on every browser and system.
document.addEventListener("keydown", (ev) => {
  if (!(ev.key === "f" || ev.key === "F") || !(ev.metaKey || ev.ctrlKey) || ev.altKey) return;
  const view = document.querySelector(".view:not([hidden])");
  const input = view?.querySelector('input[type="search"]');
  if (!input) return;
  ev.preventDefault();
  input.focus();
  input.select?.();
});

// Arrow keys walk the Downloads rows, like the library grid.
$("#dl").addEventListener("keydown", (ev) => {
  if (!["ArrowDown", "ArrowUp"].includes(ev.key) || ev.altKey || ev.metaKey || ev.ctrlKey) return;
  const buttons = [...document.querySelectorAll("#dl .dlp-open")];
  const index = buttons.indexOf(document.activeElement);
  if (index < 0) return;
  const next = buttons[index + (ev.key === "ArrowDown" ? 1 : -1)];
  if (next) { ev.preventDefault(); next.focus(); }
});

function show(view) {
  closeMenu();
  for (const v of document.querySelectorAll(".view")) v.hidden = v.id !== "view-" + view;
  for (const b of document.querySelectorAll(".side [data-view]")) {
    if (b.dataset.view === view) b.setAttribute("aria-current", "page"); else b.removeAttribute("aria-current");
  }
  document.dispatchEvent(new CustomEvent("library:view", { detail: { view } }));
  if (view === "settings" && current) { fillForm(current); loadSessions(); settingsCardsForPhone(); loadDeadSummary(); }
  if (view === "browse") loadLibrary();
  if (view === "downloads") loadDownloads();
  if (view === "sources") {
    if (!$("#source-manager").childNodes.length) mountSourceManager($("#source-manager"), loadSources);
    else refreshSourceManager();
    loadSources();
  }
  if (view === "cleanup") {
    loadCleanup();
    mountCompanionSettings($("#companion-settings"));
  }
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

/* Downloads: the reconciled table. Remove takes the torrent out of qBittorrent's
   list and never deletes its files; Reveal shows the saved folder in Finder. */
const dl = { items: [], problems: "", metadata: "", lastNotice: "", lastTone: "", filter: "all", query: "", selected: new Set(), openKey: null, loading: false, focusAfter: null };
let downloadsLoading = false;

async function loadDownloads(preserveSelection = false) {
  if (downloadsLoading) return;
  downloadsLoading = true;
  if (!preserveSelection) dl.selected.clear();
  if (!dl.items.length) renderDownloadsPage();
  try {
    const r = await api("/api/downloads");
    dl.items = (Array.isArray(r.items) ? r.items : []).map((t) => ({
      ...t,
      posterPath: t.posterPath ? "/api/poster?path=" + encodeURIComponent(t.posterPath) : "",
    }));
    dl.problems = [r.error, ...(r.problems || [])].filter(Boolean).join(" ");
    dl.metadata = r.metadata || "";
    const dead = r.dead || {};
    const byHash = new Map((dead.candidates || []).map((c) => [String(c.hash).toUpperCase(), c]));
    dl.deadOn = !!dead.on;
    dl.items = dl.items.map((item) => {
      const cand = byHash.get(String(item.hash || "").toUpperCase());
      return cand ? { ...item, deadSince: cand.firstSeen || "", removeEligibleAt: cand.eligibleAt || "", deadCleanupOn: !!dead.on } : item;
    });
  } catch (e) {
    dl.items = [];
    dl.problems = e.message;
  }
  downloadsLoading = false;
  renderDownloadsPage();
}

function setDownloadNotice(text, tone) {
  // Kept on the page state too, so the result survives the next re-render.
  dl.lastNotice = text || "";
  dl.lastTone = tone || "";
  renderDownloadNotice();
}

function renderDownloadNotice() {
  const notice = $("#dl-notice");
  const text = [...groupMessages(String(dl.problems || "").split(/(?<=\.)\s+/)), dl.lastNotice].filter(Boolean).join(" ");
  notice.hidden = !text;
  notice.textContent = text;
  if (dl.lastTone && !dl.problems) notice.dataset.tone = dl.lastTone;
  else delete notice.dataset.tone;
}

function renderDownloadsPage() {
  const host = $("#dl"), detail = $("#downloads-detail");
  const model = downloadsModel(dl.items, { filter: dl.filter, query: dl.query, selected: dl.selected });
  // Only rows the filters currently show stay selected, so a bulk action can
  // never touch a download the owner can't see.
  dl.selected = new Set(model.selected);
  const focused = document.activeElement;
  const searchFocus = focused?.id === "dl-q" ? focused.selectionStart : null;
  const rowFocus = focused?.closest?.("tr[data-key]")?.dataset.key || null;
  const wasSelectAll = focused?.hasAttribute?.("data-select-all") || false;
  const wasFilter = focused?.tagName === "SELECT" && host.contains(focused);
  const handlers = {
    filter: (key) => { dl.filter = key; dl.selected.clear(); renderDownloadsPage(); },
    query: (text) => { dl.query = text; renderDownloadsPage(); },
    select: (key, on) => { if (on) dl.selected.add(key); else dl.selected.delete(key); renderDownloadsPage(); },
    selectAll: (on) => { for (const row of model.rows) { if (on) dl.selected.add(row.key); else dl.selected.delete(row.key); } renderDownloadsPage(); },
    open: (row) => { dl.openKey = dl.openKey === row.key ? null : row.key; renderDownloadsPage(); },
    action: (row, key) => { if (key === "reveal") revealDownload(row.hash); else downloadAction([row.hash], key); },
    bulk: (key) => downloadAction([...dl.selected], key),
    emptyAction: (key) => {
      if (key === "find") { show("find"); return; }
      dl.filter = "all"; dl.query = ""; renderDownloadsPage();
    },
  };
  downloadsPanelRender(host, model, handlers);
  // Say so only when the table really needs sideways scrolling, after layout settles.
  const baseStatus = dl.problems
    ? "Some sources did not answer."
    : "Status and proof come from import records" + (dl.metadata ? " · " + dl.metadata : "");
  $("#dl-page-status").textContent = baseStatus;
  const scrollBox = host.querySelector(".table-scroll");
  if (scrollBox) {
    if (scrollBox.scrollWidth > scrollBox.clientWidth + 1) scrollBox.dataset.scrollable = "1";
    else delete scrollBox.dataset.scrollable;
  }
  requestAnimationFrame(() => {
    // Only the tablet widths, where the table is genuinely wider than its frame.
    // A hint left over from another width is removed, never left standing.
    const tablet = window.innerWidth >= 761 && window.innerWidth < 1000;
    const scrolls = tablet && scrollBox && scrollBox.scrollWidth > scrollBox.clientWidth + 1;
    $("#dl-page-status").textContent = scrolls ? baseStatus + " · Scroll sideways for more columns." : baseStatus;
  });
  renderDownloadNotice();
  const openRow = model.rows.find((row) => row.key === dl.openKey);
  // The proof opens as a modal: the table keeps its columns and the page never reflows.
  if (openRow) {
    downloadsDetailRender(detail, downloadsDetail(openRow.item, []), () => detail.close());
    if (!detail.open) {
      detail.showModal();
      [...detail.querySelectorAll("button")].find((b) => /^close$/i.test(b.textContent.trim()))?.focus();
    }
  } else if (detail.open) detail.close();
  if (searchFocus !== null) {
    const search = $("#dl-q");
    search?.focus();
    search?.setSelectionRange?.(searchFocus, searchFocus);
  } else if (rowFocus) {
    host.querySelector('tr[data-key="' + CSS.escape(rowFocus) + '"] input[type=checkbox]')?.focus();
  } else if (wasSelectAll) {
    host.querySelector("[data-select-all]")?.focus();
  } else if (wasFilter) {
    host.querySelector("select")?.focus();
  } else if (dl.focusAfter) {
    // After Remove/Clear the row is gone; focus the next row, else the search.
    const gone = dl.focusAfter;
    dl.focusAfter = null;
    host.querySelector('tr[data-key="' + CSS.escape(gone) + '"] .dlp-open')?.focus();
    if (!host.contains(document.activeElement)) (host.querySelector(".dlp-open") || $("#dl-q"))?.focus();
  }
}

// Closing the proof dialog puts focus back on the row that opened it.
$("#downloads-detail").addEventListener("close", () => {
  if (!dl.openKey) return;
  const key = dl.openKey;
  dl.openKey = null;
  renderDownloadsPage();
  // After the dialog's own focus restoration settles, land on the row.
  requestAnimationFrame(() => {
    const back = document.querySelector('#dl tr[data-key="' + CSS.escape(key) + '"] .dlp-open');
    (back || $("#dl-q"))?.focus();
  });
});

// Remove and Clear change qBittorrent's list, so they ask in Library's own
// dialog first (a native confirm is a silent no-op inside the app window).
// Files are never deleted; Keep it leaves everything as it was.
function confirmDownloadAction(action, hashes) {
  const names = hashes.map((h) => dl.items.find((i) => i.hash === h)?.name).filter(Boolean);
  const count = names.length || hashes.length;
  const verb = action === "clear" ? "Clear" : "Remove";
  const who = count === 1 ? (names[0] || "this download") : count + " downloads";
  return new Promise((resolve) => {
    const dialog = document.createElement("dialog");
    dialog.className = "confirm";
    dialog.setAttribute("aria-labelledby", "confirm-title");
    const heading = document.createElement("h2");
    heading.id = "confirm-title";
    heading.textContent = verb + " " + who + " from qBittorrent?";
    const text = document.createElement("p");
    text.textContent = action === "clear"
      ? "Its files are already missing. This only removes the entry from qBittorrent’s list."
      : "Its files stay on disk. This only removes the entry from qBittorrent’s list.";
    const actions = document.createElement("div");
    actions.className = "actions";
    const cancel = document.createElement("button");
    cancel.type = "button"; cancel.className = "small"; cancel.textContent = "Keep it";
    const go = document.createElement("button");
    go.type = "button"; go.className = "small primary"; go.textContent = verb;
    actions.append(cancel, go);
    dialog.append(heading, text, actions);
    document.body.append(dialog);
    const opener = document.activeElement; // Escape must return here, not to BODY
    let answer = false;
    cancel.addEventListener("click", () => dialog.close());
    go.addEventListener("click", () => { answer = true; dialog.close(); });
    dialog.addEventListener("close", () => {
      // Restore focus before the element is removed, and again next frame.
      const back = opener && opener.isConnected ? opener : null;
      back?.focus();
      requestAnimationFrame(() => {
        dialog.remove();
        if (back && back.isConnected) back.focus();
        resolve(answer);
      });
    });
    dialog.showModal();
  });
}

async function downloadAction(hashes, action) {
  if (!hashes.length || downloadsLoading) return;
  if ((action === "remove" || action === "clear") && !(await confirmDownloadAction(action, hashes))) return;
  const names = hashes.map((h) => dl.items.find((i) => i.hash === h)?.name).filter(Boolean);
  const who = names.length === 1 ? names[0] : (names.length || hashes.length) + " downloads";
  const folders = hashes.map((h) => dl.items.find((i) => i.hash === h)?.save_path).filter(Boolean);
  const kept = folders.length ? " at " + folders[0] : "";
  const words = {
    pause: "Paused " + who + ".",
    resume: "Resumed " + who + ".",
    remove: "Removed " + who + " from qBittorrent. Its files were kept on disk" + kept + ".",
    clear: "Cleared " + who + " from qBittorrent. Its files were already missing.",
  };
  try {
    const r = await api("/api/downloads/action", { hashes, action });
    const errs = (r.errors || r.problems || []).filter(Boolean);
    dl.selected.clear();
    dl.openKey = null;
    const proof = $("#downloads-detail");
    if (proof && proof.open) proof.close(); // a removed row's proof must not stay on screen
    dl.focusAfter = hashes[0];
    await loadDownloads();
    await load(); // Activity and every other page move on with the action
    if (action === "pause" || action === "resume") {
      const unchanged = hashes
        .map((h) => dl.items.find((i) => String(i.hash).toLowerCase() === String(h).toLowerCase()))
        .filter((it) => it && /^(paused|stopped)/i.test(it.state || "") !== (action === "pause"));
      if (unchanged.length) {
        setDownloadNotice("qBittorrent still reports " + (unchanged[0].state || "the same state") + " for " + (unchanged[0].name || "that download") + ". Nothing changed.", "warn");
        return;
      }
    }
    setDownloadNotice(errs.length ? errs.join(" ") : words[action], errs.length ? "warn" : "ok");
  } catch (e) {
    await loadDownloads();
    setDownloadNotice(e.message, "warn");
  }
}

async function revealDownload(hash) {
  if (!hash) return;
  try {
    await api("/api/downloads/reveal", { hash });
    setDownloadNotice("Revealed in Finder.");
  } catch (e) {
    setDownloadNotice(e.message);
  }
}


/* Sign-in, owner setup and sessions */
function showGate(mode) {
  $("#app-shell").hidden = true;
  $("#gate").hidden = false;
  for (const id of ["signin", "setup", "blocked", "recovery"]) $("#gate-" + id).hidden = id !== mode;
  document.title = (mode === "setup" ? "Create account" : mode === "recovery" ? "Recover access" : "Sign in") + " · Library Studio KH";
  const first = $("#gate-" + mode).querySelector("input, button");
  if (first) first.focus();
  document.dispatchEvent(new CustomEvent("library:gate", { detail: { mode } }));
}

function hideGate() {
  $("#gate").hidden = true;
  $("#app-shell").hidden = false;
  document.title = "Library Studio KH";
}

function signedOut() {
  AUTH.authenticated = false;
  showGate(AUTH.setupRequired ? "blocked" : "signin");
  document.dispatchEvent(new CustomEvent("library:signedout"));
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
  const authPanel = $("#auth-panel");
  if (authPanel) authPanel.hidden = !AUTH.available;
  $("#auth-note").textContent = AUTH.available
    ? "Browser sessions last " + AUTH.sessionHours + " hours, or " + AUTH.rememberDays + " days with Remember this device. The host desktop retains owner recovery access. Remote sign-in is encrypted by Tailscale; the page uses HTTP, not HTTPS."
    : "Sign-in is not available in this build yet. The desktop app's local access is in use.";
  await load();
  loadSessions();
}

// Dead-download switch: state plus what the next pass would do (never silent).
async function loadDeadSummary() {
  const line = $("#dead-cleanup-status");
  if (!line) return;
  try {
    const d = await api("/api/downloads/dead");
    const n = (d.candidates || []).length;
    const next = (d.candidates || []).map((c) => c.eligibleAt).filter(Boolean).sort()[0];
    const when = next ? new Date(next).toLocaleString() : "";
    line.textContent = (d.on
      ? "On · grace " + d.hours + " hours · " + (d.ready ? d.ready + " dead download" + (d.ready === 1 ? "" : "s") + " can be removed now" : n ? n + " dead download" + (n === 1 ? " is" : "s are") + " waiting out the grace" : "no dead downloads match right now")
      : "Off · " + (n ? n + " dead download" + (n === 1 ? " matches" : "s match") + " the rule; nothing is removed while off" : "no dead downloads match right now"))
      + (when && n ? " · next eligible " + when : "")
      + ". Removes the torrent entry only. Files on disk are never deleted.";
    const arrLine = $("#dead-arr-status");
    if (arrLine) {
      const arr = d.arr || {};
      const n = (arr.items || []).length;
      const r = arr.ready || 0;
      arrLine.textContent = (arr.on
        ? "On · " + (r ? r + " dead item" + (r === 1 ? "" : "s") + " can be removed from the app queues now" : n ? n + " dead item" + (n === 1 ? " is" : "s are") + " waiting in the arr queues for its grace" : "no dead items are waiting in the arr queues")
        : "Off · " + (n ? n + " dead item" + (n === 1 ? " is" : "s are") + " waiting in the arr queues; nothing is removed while off" : "no dead items are waiting in the arr queues"))
        + (arr.problems && arr.problems.length ? " · " + arr.problems.join(" ") : "")
        + ". Removes only the app's queue row; the release is never blocklisted and files, history and library records are untouched.";
    }
  } catch (e) {
    line.textContent = e.message || "The dead-download state could not be read.";
  }
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
      const device = document.createElement("span");
      device.className = "session-device";
      device.textContent = (s.device || "Browser on an unrecorded system") + (s.peer ? " · " + s.peer : "");
      const when = document.createElement("span");
      when.textContent = (s.lastSeen ? "last seen " + stamp(s.lastSeen) : "not used yet")
        + " · signed in " + stamp(s.created) + " · ends " + stamp(s.expires);
      info.append(device, when);
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
    show("help");
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

// The sign-out button is created with the Settings tabs, so this runs after the mount.
async function signOut() {
  const btn = $("#logout");
  btn.disabled = true;
  try { await api("/api/auth/logout", null, true); }
  catch (e) {
    const msg = $("#sessions-msg"); msg.hidden = false; msg.textContent = "Sign-out could not be confirmed: " + e.message;
    btn.disabled = false;
    return;
  }
  clearLocalToken();
  AUTH.authenticated = false;
  fillAuthCopy();
  btn.disabled = false;
  showGate(AUTH.setupRequired ? "blocked" : "signin");
  document.dispatchEvent(new CustomEvent("library:signedout"));
}

// Update check: read the shared KH feed, never install here. Studio Launcher KH does that.
const updateStatus = $("#update-status"), checkUpdates = $("#check-updates"), openUpdate = $("#open-update");
function updateLine(text, version) {
  updateStatus.textContent = text;
  const cardState = document.getElementById("updates-state");
  if (cardState) cardState.textContent = text;
  openUpdate.hidden = !version;
  if (version) openUpdate.setAttribute("aria-label", "Update to " + version);
  if (version) openUpdate.title = "Update to " + version;
}
checkUpdates.addEventListener("click", async () => {
  checkUpdates.disabled = true;
  updateLine("Checking...");
  try {
    const r = await api("/api/update/check");
    if (r.state === "available") updateLine("Checked just now · " + r.version + " is available", r.version);
    else if (r.state === "up_to_date") updateLine("Checked just now · up to date");
    else if (r.state === "not_offered") updateLine("Not offered by this Update Studio");
    else if (r.state === "not_checked") updateLine("Not checked" + (r.reason ? ": " + r.reason : ""));
    else updateLine("Check failed: " + (r.reason || "no answer from the update feed") + ". Try again.");
  } catch (e) {
    updateLine("Check failed: " + e.message.replace(/\.$/, "") + ". Try again.");
  } finally { checkUpdates.disabled = false; }
});
openUpdate.addEventListener("click", async () => {
  try { await api("/api/update/open", {}, true); }
  catch (e) { updateLine("Check failed: " + e.message.replace(/\.$/, "") + ". Try again."); }
});

$("#form").addEventListener("change", (ev) => {
  if (ev.target.name !== "appearance") return;
  applyAppearance(ev.target.value);
  const hint = $("#settings-card-appearance .card-body > p.muted");
  if (hint) {
    hint.textContent = ev.target.value === "system"
      ? "System follows this Mac."
      : "This library always uses the " + ev.target.value + " theme.";
  }
});

$("#form").addEventListener("submit", async (ev) => {
  ev.preventDefault();
  const f = ev.target, msg = $("#form-msg"), btn = $("#settings-save") || f.querySelector("button.primary");
  const body = {
    appearance: f.querySelector('input[name="appearance"]:checked')?.value || "system",
    tailscaleOn: f.tailscaleOn.checked, tailscalePort: Number(f.tailscalePort.value),
    deadCleanupOn: f.deadCleanupOn.checked, deadCleanupHours: Number(f.deadCleanupHours.value),
    deadArrCleanupOn: f.deadArrCleanupOn.checked,
    bazarrOn: f.bazarrOn.checked, whisparrOn: f.whisparrOn.checked,
    downloadPath: f.downloadPath.value.trim(), libraryPath: f.libraryPath.value.trim(),
    qbittorrent: { url: f["qbittorrent.url"].value.trim(), username: f["qbittorrent.username"].value.trim(), password: f["qbittorrent.password"].value },
  };
  for (const a of ARRS) body[a] = { url: f[a + ".url"].value.trim(), apiKey: f[a + ".apiKey"].value.trim() };
  // Feedback sits beside Save (the header line above it stays instructional),
  // and clears itself so it never follows the owner onto another tab.
  const beside = $("#settings-save-msg");
  let clearTimer = null;
  const say = (text) => {
    msg.textContent = text;
    if (beside) beside.textContent = text;
    if (clearTimer) clearTimeout(clearTimer);
    if (text && text !== "Saving…") clearTimer = setTimeout(() => { if (beside) beside.textContent = ""; }, 8000);
  };
  btn.disabled = true; msg.hidden = false; say("Saving…");
  try {
    const r = await api("/api/settings", body);
    say(r.problems && r.problems.length
      ? "Saved, but these settings couldn't be applied: " + r.problems.join("; ")
      : "Saved.");
    await load();
    fillForm(current);
    loadDeadSummary();
    const settings = window.librarySettingsApi;
    for (const name of ["tailscaleOn", "bazarrOn", "whisparrOn", "deadCleanupOn", "deadArrCleanupOn"]) {
      settings?.savedFeedback?.(name, "Saved", "saved");
    }
  } catch (e) { say("Not saved. " + e.message); }
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

function publicHelpCopy(source) {
  const copy = source.cloneNode(true);
  // Remove owner forms, paths and running-job actions before IDs are stripped.
  for (const node of copy.querySelectorAll("[data-owner-only]")) node.remove();
  copy.removeAttribute("id"); copy.hidden = false;
  for (const node of copy.querySelectorAll("[id]")) node.removeAttribute("id");
  return copy;
}

$("#open-public-help").addEventListener("click", () => {
  $("#public-help-content").replaceChildren(publicHelpCopy($("#view-help")));
  $("#gate").hidden = true;
  $("#public-help").hidden = false;
  $("#close-public-help").focus();
});
$("#close-public-help").addEventListener("click", () => {
  $("#public-help").hidden = true;
  start();
});

// Settings is four tabs of self-contained cards; the module owns the markup.
// Settings cards: open on desktop; on a phone one card at a time keeps the page
// within the window instead of one long scroll.
function settingsCardsForPhone() {
  const narrow = window.matchMedia("(max-width: 900px)").matches;
  for (const panel of document.querySelectorAll("#settings-tabs .tabpanel")) {
    [...panel.querySelectorAll(":scope > .settings-card")].forEach((card, i) => { card.open = !narrow || i === 0; });
  }
}
window.matchMedia?.("(max-width: 900px)").addEventListener?.("change", settingsCardsForPhone);
window.addEventListener("resize", settingsCardsForPhone);

if (window.librarySettingsView) {
  window.librarySettingsApi = window.librarySettingsView.mountSettingsTabs($("#settings-tabs"));
  settingsCardsForPhone();
  const cardCheck = $("#updates-check"), cardOpen = $("#updates-open");
  if (cardCheck) cardCheck.addEventListener("click", () => $("#check-updates").click());
  if (cardOpen) cardOpen.addEventListener("click", () => $("#open-update").click());
  const logoutBtn = $("#logout");
  if (logoutBtn) logoutBtn.addEventListener("click", signOut);
}
buildArrFields();
window.libraryKeyboard(document);
start();
let resizeTimer = null;
window.addEventListener("resize", () => {
  clearTimeout(resizeTimer);
  resizeTimer = setTimeout(() => {
    if (!$("#view-downloads").hidden) renderDownloadsPage();
  }, 250);
});

setInterval(() => {
  if (!$("#app-shell").hidden && $("#view-settings").hidden) {
    load();
    if (!$("#view-downloads").hidden) loadDownloads(true);
    if (!$("#view-browse").hidden) loadLibrary();
  }
}, 30000);
