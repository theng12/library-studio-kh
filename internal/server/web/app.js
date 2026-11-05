"use strict";
// The launch token arrives in the address (#token); keep it for reloads and hide it from the address bar.
if (location.hash.length > 1) {
  sessionStorage.setItem("token", location.hash.slice(1));
  history.replaceState(null, "", location.pathname);
}
const token = sessionStorage.getItem("token") || "";
const $ = (s) => document.querySelector(s);
const ARRS = ["sonarr", "radarr", "lidarr", "prowlarr"];
const NAMES = { sonarr: "Sonarr (TV)", radarr: "Radarr (films)", lidarr: "Lidarr (music)", prowlarr: "Prowlarr (search sites)" };
const PORTS = { sonarr: 8989, radarr: 7878, lidarr: 8686, prowlarr: 9696 };
let current = null;

async function api(path, body) {
  const r = await fetch(path, {
    method: body ? "POST" : "GET",
    headers: { "X-Library-Token": token, "Content-Type": "application/json" },
    body: body ? JSON.stringify(body) : undefined,
  });
  const data = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(data.error || (r.status === 403 ? "Please reopen Library Studio KH." : "Something went wrong (" + r.status + ")."));
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

function row(cells) {
  const tr = document.createElement("tr");
  for (const c of cells) {
    const td = document.createElement("td");
    if (c instanceof Node) td.append(c); else td.textContent = c;
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
    return row([x.app, x.name, now, x.want]);
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
    return row([title, tag, when]);
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
    const r = await fetch("/api/torrents", { method: "POST", headers: { "X-Library-Token": token }, body: fd });
    const data = await r.json().catch(() => ({}));
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
  $("#lib tbody").replaceChildren(...items.map((m) =>
    row([titleCell(m), TYPE[m.app] || m.app, m.haveFiles ? "In library" : "Waiting for files", m.added ? day(m.added) : ""])));
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
        action.addEventListener("click", async () => {
          action.disabled = true; action.textContent = "Adding…";
          try {
            const res = await api("/api/add", { app: m.app, raw: m.raw });
            action.replaceWith("Added to " + res.folder);
            libItems = null;
          } catch (e) { action.disabled = false; action.textContent = "Add"; msg.hidden = false; msg.textContent = e.message; }
        });
      }
      return row([titleCell(m), TYPE[m.app] || m.app, action]);
    }));
  } catch (e) { msg.textContent = e.message; }
});

function fillForm(st) {
  const f = $("#form"), c = st.settings;
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

function show(view) {
  for (const v of document.querySelectorAll(".view")) v.hidden = v.id !== "view-" + view;
  for (const b of document.querySelectorAll(".side [data-view]")) {
    if (b.dataset.view === view) b.setAttribute("aria-current", "page"); else b.removeAttribute("aria-current");
  }
  if (view === "settings" && current) fillForm(current);
  if (view === "browse" && !libItems) loadLibrary();
  if (view === "find") $("#find-q").focus();
  $(".content").scrollTo?.(0, 0);
  window.scrollTo(0, 0);
}
document.addEventListener("click", (ev) => {
  const b = ev.target.closest("[data-view]");
  if (b) show(b.dataset.view);
});

async function load() {
  try { render(await api("/api/state")); }
  catch (e) { $("#summary").textContent = e.message; }
}

$("#form").addEventListener("submit", async (ev) => {
  ev.preventDefault();
  const f = ev.target, msg = $("#form-msg"), btn = f.querySelector("button");
  const days = Number(f.cleanupDays.value);
  if (!Number.isInteger(days) || days < 0 || days > 3650) {
    msg.hidden = false; msg.textContent = "Days must be a whole number from 0 to 3650."; f.cleanupDays.focus(); return;
  }
  const body = {
    cleanupOn: f.cleanupOn.checked, cleanupDays: days,
    downloadPath: f.downloadPath.value.trim(), libraryPath: f.libraryPath.value.trim(),
    qbittorrent: { url: f["qbittorrent.url"].value.trim(), username: f["qbittorrent.username"].value.trim(), password: f["qbittorrent.password"].value },
  };
  for (const a of ARRS) body[a] = { url: f[a + ".url"].value.trim(), apiKey: f[a + ".apiKey"].value.trim() };
  btn.disabled = true; msg.hidden = false; msg.textContent = "Saving…";
  try {
    const r = await api("/api/settings", body);
    msg.textContent = r.problems && r.problems.length
      ? "Saved here, but some apps didn't take the cleanup settings: " + r.problems.join("; ")
      : "Saved.";
    await load();
    fillForm(current);
  } catch (e) { msg.textContent = e.message; }
  finally { btn.disabled = false; }
});

buildArrFields();
load();
setInterval(() => { if ($("#view-settings").hidden) load(); }, 30000);
