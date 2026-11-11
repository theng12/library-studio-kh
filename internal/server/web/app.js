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

// Appearance: System follows this Mac; Light/Dark/Default are explicit choices.
const darkQuery = window.matchMedia("(prefers-color-scheme: dark)");
function applyAppearance(choice) {
  const resolved = choice === "system" ? (darkQuery.matches ? "dark" : "light") : choice === "default" ? "light" : choice;
  document.documentElement.dataset.theme = resolved;
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

function stateOf(it, cleanupOn) {
  switch (it.kind) {
    case "downloading": return ["Downloading", downloadProgressText(it.progress)];
    case "waiting": return ["Waiting for import", it.app || ""];
    case "imported": return ["In library", cleanupOn ? "Local copy: check Cleanup for verified status" : "Local copy kept"];
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
  fillWebAddresses(st);
  const s = st.snapshot, cfg = st.settings;
  $("#version").textContent = st.version;
  applyAppearance(cfg.appearance || "system");
  // Setup is done once a download client and a library app are both saved.
  const configured = !!cfg.qbittorrent?.url && ["sonarr", "radarr", "lidarr"].some((a) => !!cfg[a]?.url);
  for (const el of document.querySelectorAll(".first-run")) el.hidden = configured;

  const parts = [];
  parts.push(s.freeBytes == null ? "Free space on this computer: not known yet." : "<strong>" + gb(s.freeBytes) + "</strong> free on this computer.");
  parts.push(cfg.stagedCleanupOn
    ? "Staged cleanup is <strong>on</strong> for verified imports. Check Cleanup for eligibility and blockers."
    : "Local copies are <strong>kept</strong>. Staged cleanup is off.");
  if (s.library === "missing") parts.push("<span class=off>The NAS library folder can't be found.</span>");
  $("#summary").innerHTML = parts.join(" ");

  $("#apps").replaceChildren(...s.apps.map((a) => {
    const li = document.createElement("li");
    li.className = a.ok ? "ok" : "bad";
    li.textContent = a.name;
    li.title = a.ok ? "Connected" : a.error || "Not connected";
    li.setAttribute("aria-label", a.name + ": " + li.title);
    return li;
  }));

  const notice = $("#notice");
  notice.hidden = !s.blocked;
  notice.textContent = s.blocked || "";

  renderActivity();

  $("#cleanup-lede").innerHTML = parts.slice(0, 2).join(" ");

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
    const [label, when] = stateOf(it, current.settings.stagedCleanupOn);
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
for (const id of ["#act-q", "#act-kind", "#act-sort"]) $(id).addEventListener("input", renderActivity);

function titleCell(m) {
  const box = document.createElement("div");
  box.textContent = m.title + " ";
  if (m.year) { const y = document.createElement("span"); y.className = "year"; y.textContent = m.year; box.append(y); }
  if (m.overview) { const o = document.createElement("span"); o.className = "sub"; o.textContent = m.overview.length > 160 ? m.overview.slice(0, 157) + "…" : m.overview; box.append(o); }
  return box;
}

let libItems = null;
let libraryDownloads = [];
let libraryProgressError = "";
const openLibraryItems = new Set();
let libraryLoading = false;
async function loadLibrary() {
  if (libraryLoading) return;
  libraryLoading = true;
  if (!libItems) { $("#lib-empty").hidden = false; $("#lib-empty").textContent = "Loading…"; }
  try {
    const r = await api("/api/browse");
    libItems = r.items;
    try {
      const queue = await api("/api/downloads");
      libraryDownloads = queue.items || [];
      libraryProgressError = (queue.problems || []).join(" ");
    } catch (e) { libraryDownloads = []; libraryProgressError = "Download progress unavailable: " + e.message; }
    const problems = [...(r.problems || []), libraryProgressError].filter(Boolean);
    $("#browse-notice").hidden = !problems.length;
    $("#browse-notice").textContent = problems.join(" ");
  } catch (e) { libItems = []; $("#browse-notice").hidden = false; $("#browse-notice").textContent = e.message; }
  libraryLoading = false;
  renderLibrary();
}

async function toggleMonitor(m, box) {
  const note = $("#browse-notice"), requested = box.checked;
  let reported = null;
  box.disabled = true;
  try {
    await api("/api/media/monitor", { app: m.app, id: m.id, monitored: requested });
    const current = await api("/api/media/details?" + new URLSearchParams({ app: m.app, id: m.id }));
    reported = typeof current.monitored === "boolean" ? current.monitored : null;
    if (reported !== requested) throw new Error(m.app + " did not confirm the monitoring change. Refresh to check its current setting.");
    m.monitored = reported;
    note.hidden = true;
    renderLibrary();
  } catch (e) {
    m.monitored = reported;
    renderLibrary();
    note.hidden = false;
    note.textContent = e.message;
  } finally { box.disabled = false; }
}

function setLibraryOptions(select, options) {
  const keep = select.value;
  select.replaceChildren(...options.map(({ value, label }) => {
    const option = document.createElement("option");
    option.value = value; option.textContent = label;
    return option;
  }));
  select.value = options.some((o) => o.value === keep) ? keep : "";
}

function fillLibraryOptions(items) {
  const genre = $("#lib-genre"), year = $("#lib-year");
  // Rebuild only when the reported metadata set changes, so a chosen filter survives refreshes.
  const key = libraryGenres(items).join("\u0001") + "|" + libraryYears(items).join(",");
  if (genre.dataset.metadataKey === key) return;
  setLibraryOptions(genre, libraryGenreOptions(items));
  setLibraryOptions(year, libraryYearOptions(items));
  genre.dataset.metadataKey = key;
}

function renderLibrary() {
  if (!libItems) return;
  fillLibraryOptions(libItems);
  const q = $("#lib-q").value, type = $("#lib-type").value, sort = $("#lib-sort").value;
  const language = $("#lib-language").value, genre = $("#lib-genre").value, year = $("#lib-year").value;
  const monitoring = $("#lib-monitoring").value;
  const items = librarySort(libraryFilter(libItems, { query: q, type, language, genre, year, monitoring }), sort);
  $("#browse-lede").textContent = libItems.length + (libItems.length === 1 ? " item" : " items") + " looked after by Sonarr, Radarr and Lidarr.";
  $("#lib-empty").hidden = items.length > 0;
  $("#lib-empty").textContent = libItems.length ? "Nothing matches." : "Nothing in the library yet. Use Find & add.";
  $("#lib").hidden = items.length === 0;
  $("#lib").replaceChildren(...items.map((m) => {
    const key = m.app + ":" + m.id, details = mediaNode("details", null, "library-item");
    details.open = openLibraryItems.has(key);
    let extrasLoaded = false;
    const extras = m.app === "Radarr" ? mediaNode("div") : null;
    const loadExtras = () => {
      if (!details.open || !extras || extrasLoaded || !details.isConnected) return;
      extrasLoaded = true;
      loadMovieExtras(m, extras);
    };
    details.addEventListener("toggle", () => {
      if (details.open) { openLibraryItems.add(key); loadExtras(); }
      else openLibraryItems.delete(key);
    });
    if (details.open) queueMicrotask(loadExtras);
    const summary = mediaNode("summary", null, "download-summary");
    summary.append(mediaNode("span", m.title + (m.year ? " (" + m.year + ")" : ""), "download-title"));
    summary.append(mediaNode("span", (TYPE[m.app] || m.app) + " · " + (m.haveFiles ? "Files available" : "Waiting for files") + " · " + libraryMonitoringLabel(m), "download-status"));
    const matches = libraryDownloads.filter(t => t.app === m.app && (t.mediaIds || []).includes(m.id));
    if (matches.length) {
      const active = downloadRows(matches, "", "downloading", "queue")[0] || downloadRows(matches, "", "all", "queue").find(t => Number.isFinite(t.progress) && t.progress < 1);
      summary.append(mediaNode("span", matches.length + (matches.length === 1 ? " matched download" : " matched downloads") + (active ? " · " + dlLabel(active.state) + " transfer" : ""), "muted"));
      if (active) summary.append(progressBlock(active));
    } else summary.append(mediaNode("span", libraryProgressError ? "Download progress unavailable" : "No matched downloads in the current queue", "muted"));
    const body = mediaNode("div", null, "library-detail");
    if (m.overview) body.append(mediaNode("p", m.overview, "library-overview"));
    body.append(mediaNode("p", "Original language: " + (m.originalLanguage || "Not reported") + ". Downloaded audio and subtitles may differ.", "muted"));
    body.append(mediaNode("p", "Genres: " + (m.genres && m.genres.length ? m.genres.join(", ") : "Not reported") + ".", "muted"));
    if (m.added) body.append(mediaNode("p", "Added " + day(m.added), "muted"));
    if (typeof m.monitored === "boolean") {
      const label = mediaNode("label", null, "check media-monitor"), monitor = document.createElement("input");
      monitor.type = "checkbox"; monitor.checked = m.monitored;
      monitor.setAttribute("aria-label", "Monitor " + m.title);
      monitor.addEventListener("change", () => toggleMonitor(m, monitor));
      label.append(monitor, mediaNode("span", "Monitor for releases")); body.append(label);
    }
    if (matches.length) {
      const list = mediaNode("ul", null, "source-list");
      for (const t of matches) list.append(mediaNode("li", t.name + " · " + dlLabel(t.state) + " · " + downloadProgressText(t.progress)));
      body.append(list);
    }
    const open = mediaNode("button", "Open " + (m.app === "Sonarr" ? "series" : m.app === "Radarr" ? "film" : "artist") + " details", "small");
    open.type = "button"; open.addEventListener("click", () => openMediaDetails(m)); body.append(open);
    if (extras) body.append(extras);
    details.append(summary, body); return details;
  }));
}
for (const id of ["#lib-q", "#lib-type", "#lib-genre", "#lib-year", "#lib-sort", "#lib-language", "#lib-monitoring"]) $(id).addEventListener("input", renderLibrary);

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
    libItems = null;
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
  for (const r of f.querySelectorAll('input[name="appearance"]')) r.checked = r.value === (c.appearance || "system");
  f.bazarrOn.checked = !!c.bazarrOn;
  f.whisparrOn.checked = !!c.whisparrOn;
  f.downloadPath.value = c.downloadPath;
  f.libraryPath.value = c.libraryPath;
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
      (a === "bazarr" ? `<label class="check"><input type="checkbox" name="bazarrOn"> Enable subtitle controls</label><p class="muted">English searches run when you request them in Library. Bazarr's own background tasks are controlled separately.</p>` : "") +
      (a === "whisparr" ? `<label class="check"><input type="checkbox" name="whisparrOn"> Enable Whisparr connection</label><p class="muted">Off by default. Save an address and API key to connect an existing app. Installation and first-run compatibility remain unverified. Check Activity for connection health; saving alone does not prove it is running.</p>` : "") +
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
// Escape falls back to the mobile menu only when no open dialog, editable or
// IME composition has already taken it.
document.addEventListener("keydown", (ev) => {
  if (ev.key !== "Escape" || ev.isComposing || ev.keyCode === 229 || ev.defaultPrevented) return;
  if (menuToggle.getAttribute("aria-expanded") !== "true") return;
  const t = ev.target;
  const editableHost = t?.closest?.("[contenteditable]");
  if (document.querySelector("dialog[open]") || t?.closest?.("dialog, input, textarea, select")
    || (editableHost && editableHost.getAttribute("contenteditable") !== "false")) return;
  closeMenu(); menuToggle.focus();
});
document.addEventListener("click", (ev) => { if (!ev.target.closest(".side")) closeMenu(); });

function show(view) {
  closeMenu();
  for (const v of document.querySelectorAll(".view")) v.hidden = v.id !== "view-" + view;
  for (const b of document.querySelectorAll(".side [data-view]")) {
    if (b.dataset.view === view) b.setAttribute("aria-current", "page"); else b.removeAttribute("aria-current");
  }
  document.dispatchEvent(new CustomEvent("library:view", { detail: { view } }));
  if (view === "settings" && current) { fillForm(current); loadSessions(); }
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

/* Downloads: pause and resume only. No delete action exists here. */
const DL_STATES = {
  downloading: "Downloading", forcedDL: "Downloading", stalledDL: "Stalled", metaDL: "Fetching metadata",
  queuedDL: "Queued", checkingDL: "Checking", pausedDL: "Paused", stoppedDL: "Paused",
  uploading: "Seeding", forcedUP: "Seeding", stalledUP: "Seeding, stalled", queuedUP: "Queued to seed",
  checkingUP: "Checking", pausedUP: "Paused", stoppedUP: "Paused",
  moving: "Moving", allocating: "Allocating", error: "Error", missingFiles: "Missing files", unknown: "Unknown",
};
const dlLabel = (s) => DL_STATES[s] || s || "Unknown";
const dlApp = app => ({ Unmatched: "qBittorrent · unmatched", Unknown: "App not verified" }[app] || app || "App not verified");
const dlPaused = (s) => /^(paused|stopped)/i.test(s || "");
let downloads = null;
let dlError = "";
const selected = new Set();

const openDownloads = new Set();
function progressBlock(t) {
  const box = mediaNode("div", null, "download-progress");
  const progress = document.createElement("progress"); progress.max = 1;
  progress.setAttribute("aria-label", "Download progress");
  const known = Number.isFinite(t.progress) && t.progress >= 0 && t.progress <= 1;
  if (known) progress.value = t.progress;
  box.append(progress, mediaNode("span", known ? downloadProgressText(t.progress) : "Not reported"));
  return box;
}
function downloadCard(t) {
  const card = mediaNode("article", null, "download-item");
  card.dataset.hash = t.hash;
  const select = mediaNode("label", null, "check-target");
  const box = document.createElement("input"); box.type = "checkbox"; box.checked = selected.has(t.hash);
  box.setAttribute("aria-label", "Select " + t.name); select.append(box);
  box.addEventListener("change", () => { if (box.checked) selected.add(t.hash); else selected.delete(t.hash); updateDlButtons(); });
  const details = document.createElement("details"); details.open = openDownloads.has(t.hash);
  details.addEventListener("toggle", () => { if (details.open) openDownloads.add(t.hash); else openDownloads.delete(t.hash); });
  const summary = mediaNode("summary", null, "download-summary");
  summary.append(mediaNode("span", t.name, "download-title"));
  const line = mediaNode("span", null, "download-status");
  line.append(mediaNode("span", dlApp(t.app)), mediaNode("span", dlLabel(t.state)));
  if (!dlPaused(t.state)) line.append(mediaNode("span", rate(t.dlspeed || 0) + " down"));
  summary.append(line, progressBlock(t));
  const info = mediaNode("dl", null, "facts");
  const date = n => n > 0 ? stamp(new Date(n * 1000).toISOString()) : "Not reported";
  facts(info, [["Queue position", t.priority == null ? "Not reported" : t.priority > 0 ? t.priority : "Not queued"],
    ["Downloaded", bytes(t.downloaded || 0) + " of " + (t.size > 0 ? bytes(t.size) : "size not reported")],
    ["Time remaining", t.eta >= 0 && t.eta < 8640000 ? dur(t.eta) : "Not available"],
    ["Upload speed", rate(t.upspeed || 0)], ["Uploaded", bytes(t.uploaded || 0)],
    ["Ratio", Number.isFinite(t.ratio) ? t.ratio.toFixed(2) : "Not reported"], ["Seeds / peers", t.num_seeds + " / " + t.num_leechs],
    ["Category", t.category || "None"], ["Save folder", t.save_path || "Not reported"], ["Added", date(t.added_on)], ["Completed", date(t.completion_on)]]);
  details.append(summary, info); card.append(select, details); return card;
}
function renderDownloads() {
  const all = downloads || [], filter = $("#dl-state").value;
  const status = $("#dl-state");
  status.replaceChildren(...DOWNLOAD_FILTERS.map(([key, name]) => mediaOption(key, name + " (" + all.filter(t => downloadMatches(t, key)).length + ")")));
  status.value = filter;
  const app = $("#dl-app"), chosenApp = app.value;
  app.replaceChildren(mediaOption("", "All apps"), ...[...new Set(all.map(t => t.app || "Unknown"))].sort().map(name => mediaOption(name, dlApp(name) + " (" + all.filter(t => (t.app || "Unknown") === name).length + ")")));
  if ([...app.options].some(o => o.value === chosenApp)) app.value = chosenApp;
  const sort = $("#dl-sort");
  const items = downloadRows(all, $("#dl-q").value, status.value, sort.value, app.value);
  const empty = $("#dl-empty"), list = $("#dl"), focused = document.activeElement;
  const hadFocus = list.contains(focused), focusedHash = focused.closest?.(".download-item")?.dataset.hash;
  list.hidden = items.length === 0; empty.hidden = items.length > 0;
  if (!items.length) empty.textContent = dlError || (all.length ? "No downloads match these filters." : "No downloads in qBittorrent right now. Add one in Find & add.");
  $("#dl-count").textContent = dlError && !all.length ? "Not available" : items.length + " of " + all.length + " downloads";
  list.replaceChildren(...items.map(downloadCard));
  // Refresh replaces cards; keep keyboard focus on the same download when it remains.
  if (hadFocus && !list.contains(document.activeElement)) {
    const card = [...list.children].find(c => c.dataset.hash === focusedHash);
    const target = card?.querySelector(focused.matches("input[type=checkbox]") ? "input[type=checkbox]" : "summary");
    (target || (items.length ? list : $("#dl-q"))).focus();
  }
  updateDlButtons();
}
for (const id of ["#dl-q", "#dl-state", "#dl-app"]) $(id).addEventListener("input", () => { selected.clear(); renderDownloads(); });
$("#dl-sort").addEventListener("input", renderDownloads);

function updateDlButtons() {
  $("#dl-selected").textContent = selected.size ? selected.size + " selected" : "No items selected";
  const none = selected.size === 0;
  $("#dl-pause").disabled = none;
  $("#dl-resume").disabled = none;
}

let downloadsLoading = false;
async function loadDownloads(preserveSelection = false) {
  if (downloadsLoading) return;
  downloadsLoading = true;
  const notice = $("#dl-notice"), empty = $("#dl-empty"), table = $("#dl");
  notice.hidden = true;
  if (!preserveSelection || downloads === null) {
    empty.hidden = false;
    empty.textContent = "Loading…";
    table.hidden = true;
  }
  if (!preserveSelection) selected.clear();
  dlError = "";
  try {
    const r = await api("/api/downloads");
    downloads = Array.isArray(r.items) ? r.items : [];
    dlError = [r.error, ...(r.problems || [])].filter(Boolean).join(" ");
    for (const hash of selected) if (!downloads.some(t => t.hash === hash)) selected.delete(hash);
    if (dlError) { notice.hidden = false; notice.textContent = dlError; }
  } catch (e) {
    downloads = [];
    dlError = e.message;
  }
  downloadsLoading = false;
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
$("#dl-refresh").addEventListener("click", () => loadDownloads());

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

$("#logout").addEventListener("click", async () => {
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
});

// Update check: read the shared KH feed, never install here. Studio Launcher KH does that.
const updateStatus = $("#update-status"), checkUpdates = $("#check-updates"), openUpdate = $("#open-update");
function updateLine(text, version) {
  updateStatus.textContent = text;
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
  if (ev.target.name === "appearance") applyAppearance(ev.target.value);
});

$("#form").addEventListener("submit", async (ev) => {
  ev.preventDefault();
  const f = ev.target, msg = $("#form-msg"), btn = f.querySelector("button.primary");
  const body = {
    appearance: f.querySelector('input[name="appearance"]:checked')?.value || "system",
    tailscaleOn: f.tailscaleOn.checked, tailscalePort: Number(f.tailscalePort.value),
    bazarrOn: f.bazarrOn.checked, whisparrOn: f.whisparrOn.checked,
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

buildArrFields();
window.libraryKeyboard(document);
start();
setInterval(() => {
  if (!$("#app-shell").hidden && $("#view-settings").hidden) {
    load();
    if (!$("#view-downloads").hidden) loadDownloads(true);
    if (!$("#view-browse").hidden) loadLibrary();
  }
}, 30000);
