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

  $("#empty").hidden = s.apps.length > 0;
  $("#list").hidden = s.items.length === 0;
  $("#list tbody").replaceChildren(...s.items.map((it) => {
    const [label, when] = stateOf(it, cfg.cleanupOn);
    const tag = document.createElement("span");
    tag.className = "state " + it.kind;
    const word = document.createElement("span");
    word.textContent = label;
    tag.append(word);
    return row([it.title, tag, when]);
  }));

  $("#stock-section").hidden = !s.settings || s.settings.length === 0;
  $("#stock tbody").replaceChildren(...(s.settings || []).map((x) => {
    const now = document.createElement("span");
    now.textContent = x.value || "Unknown";
    if (!x.ok) now.className = "off";
    return row([x.app, x.name, now, x.want]);
  }));

  $("#releases").replaceChildren(...st.releases.map((r) => {
    const box = document.createElement("div");
    const h = document.createElement("h3");
    h.textContent = r.version + " · " + r.date;
    const ul = document.createElement("ul");
    for (const n of r.notes) { const li = document.createElement("li"); li.textContent = n; ul.append(li); }
    box.append(h, ul);
    return box;
  }));
}

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
    const h = document.createElement("h2");
    h.textContent = NAMES[a];
    box.append(h);
    box.insertAdjacentHTML("beforeend",
      `<label>Address <input name="${a}.url" placeholder="http://127.0.0.1:${PORTS[a]}" inputmode="url"></label>` +
      `<label>API key <span class="saved">(Settings → General in ${NAMES[a].split(" ")[0]})</span><input name="${a}.apiKey" type="password" autocomplete="off" data-secret="${a}"></label>`);
  }
}

async function load() {
  try { render(await api("/api/state")); }
  catch (e) { $("#summary").textContent = e.message; }
}

$("#toggle").addEventListener("click", () => {
  const toSettings = $("#settings").hidden;
  if (toSettings && current) fillForm(current);
  $("#settings").hidden = !toSettings;
  $("#home").hidden = toSettings;
  $("#toggle").textContent = toSettings ? "Back to library" : "Settings";
});

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
setInterval(() => { if ($("#settings").hidden) load(); }, 30000);
