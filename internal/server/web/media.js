"use strict";

let mediaCurrent = null;
let mediaSerial = 0;
let mediaData = null;
let releaseBusy = false;

function mediaNode(tag, text, className) {
  const n = document.createElement(tag);
  if (text != null) n.textContent = text;
  if (className) n.className = className;
  return n;
}
function mediaTable(headers, rows) {
  const wrap = mediaNode("div", null, "table-scroll");
  const table = mediaNode("table", null, "list"), head = document.createElement("thead"), tr = document.createElement("tr");
  for (const name of headers) { const th = mediaNode("th", name); th.scope = "col"; tr.append(th); }
  head.append(tr);
  const body = document.createElement("tbody"); body.append(...rows);
  table.append(head, body); wrap.append(table); return wrap;
}
function mediaPanel(title, description, body) {
  const section = mediaNode("section", null, "panel"), header = mediaNode("div", null, "panel-head");
  header.append(mediaNode("h2", title));
  if (description) header.append(mediaNode("p", description, "muted"));
  section.append(header, body); return section;
}
function mediaNotice(message) {
  $("#media-msg").hidden = !message;
  $("#media-msg").textContent = message || "";
}
function mediaContextQuery(context) {
  const q = new URLSearchParams({ app: context.app, id: context.id });
  if (context.episodeId) q.set("episodeId", context.episodeId);
  if (context.albumId) q.set("albumId", context.albumId);
  return q;
}
async function openMediaDetails(item) {
  mediaCurrent = { app: item.app, id: item.id, title: item.title };
  show("media");
  document.querySelector('[data-view="browse"]').setAttribute("aria-current", "page");
  $("#media-title").textContent = item.title;
  $("#media-meta").textContent = "Loading details…";
  $("#media-information").replaceChildren(); $("#media-children").replaceChildren();
  $("#media-releases").hidden = true; $("#media-filters").hidden = true;
  $("#media-child-q").value = "";
  $("#media-season").value = "";
  await refreshMediaDetails();
}
async function refreshMediaDetails() {
  const serial = ++mediaSerial, context = { ...mediaCurrent };
  mediaNotice("");
  $("#media-releases").hidden = true;
  try {
    const data = await api("/api/media/details?" + mediaContextQuery(context));
    if (serial !== mediaSerial) return;
    mediaData = data;
    $("#media-title").textContent = data.title;
    $("#media-meta").textContent = (TYPE[data.app] || data.app) + " · managed by " + data.app;
    const information = mediaNode("div", null, "panel");
    const dl = mediaNode("dl", null, "facts");
    facts(dl, [["State", data.status || "Not reported"], ["Files", data.haveFiles ? "In library" : "Not reported in library"],
      ["Quality profile ID", data.qualityProfileId || "Not reported"], ["Library folder", data.path || "Not reported"]]);
    const actions = mediaNode("div", null, "media-actions media-padding");
    actions.append(mediaMonitor(data.monitored, { app: data.app, id: data.id }, "Monitor this item", true));
    if (data.app === "Radarr") actions.append(mediaSearchButton("Search releases", {}, data.title));
    information.append(dl, actions); $("#media-information").replaceChildren(information);
    const seasons = $("#media-season"), selectedSeason = seasons.value;
    seasons.replaceChildren(mediaOption("", "All seasons"));
    const numbers = [...new Set((data.episodes || []).map(e => e.seasonNumber))].sort((a, b) => a - b);
    for (const n of numbers) seasons.append(mediaOption(String(n), n === 0 ? "Specials" : "Season " + n));
    if ([...seasons.options].some(o => o.value === selectedSeason)) seasons.value = selectedSeason;
    $("#media-season-label").hidden = data.app !== "Sonarr";
    $("#media-filters").hidden = data.app === "Radarr";
    renderMediaChildren();
  } catch (e) {
    if (serial !== mediaSerial) return;
    $("#media-meta").textContent = "Details unavailable";
    mediaNotice(e.message);
  }
}
function mediaDay(value) {
  if (/^\d{4}-\d{2}-\d{2}$/.test(value)) return new Date(value + "T12:00:00").toLocaleDateString(undefined, { weekday: "short", day: "numeric", month: "short" });
  return day(value);
}
function sourceDisableTime(value) {
  if (!value) return "No disable time reported";
  return (new Date(value).getTime() > Date.now() ? "Disabled until " : "Last disable ended ") + stamp(value);
}
function mediaOption(value, text) { const o = mediaNode("option", text); o.value = value; return o; }
function mediaMonitor(checked, request, label, root) {
  const box = mediaNode("label", null, "check media-monitor"), input = document.createElement("input");
  input.type = "checkbox"; input.checked = !!checked; input.setAttribute("aria-label", label);
  box.append(input, mediaNode("span", root ? "Monitored" : "Monitor"));
  input.addEventListener("change", async () => {
    input.disabled = true;
    const serial = mediaSerial;
    try {
      await api(root ? "/api/media/monitor" : "/api/media/tracking", { ...request, monitored: input.checked });
      libItems = null;
      if (serial === mediaSerial) await refreshMediaDetails();
    } catch (e) {
      input.checked = !input.checked;
      if (serial === mediaSerial) mediaNotice(e.message);
    } finally { input.disabled = false; }
  });
  return box;
}
function mediaSearchButton(label, child, title) {
  const button = mediaNode("button", label, "small media-search"); button.type = "button";
  button.disabled = releaseBusy;
  button.addEventListener("click", () => searchMediaReleases(child, title));
  return button;
}
function renderMediaChildren() {
  if (!mediaData) return;
  const d = mediaData, body = $("#media-children"), q = $("#media-child-q").value;
  body.replaceChildren();
  if (d.app === "Sonarr") {
    const seasonRows = (d.seasons || []).map(s => row([
      s.seasonNumber === 0 ? "Specials" : "Season " + s.seasonNumber,
      mediaMonitor(s.monitored, { app: d.app, id: d.id, kind: "season", seasonNumber: s.seasonNumber }, "Monitor season " + s.seasonNumber),
    ]));
    if (seasonRows.length) body.append(mediaPanel("Season monitoring", "Choose which seasons to follow. Files are never deleted by monitoring changes.", mediaTable(["Season", "Monitoring"], seasonRows)));
    const filter = $("#media-season").value;
    const episodes = (d.episodes || []).filter(e => (!filter || String(e.seasonNumber) === filter) && has(e.title, q))
      .sort((a, b) => a.seasonNumber - b.seasonNumber || a.episodeNumber - b.episodeNumber);
    const rows = episodes.map(e => row([
      "S" + String(e.seasonNumber).padStart(2, "0") + "E" + String(e.episodeNumber).padStart(2, "0") + " · " + e.title,
      e.haveFiles ? "In library" : "Missing files", e.when ? mediaDay(e.when) : "Date not reported",
      mediaMonitor(e.monitored, { app: d.app, id: d.id, kind: "episode", childId: e.id }, "Monitor " + e.title),
      mediaSearchButton("Search releases", { episodeId: e.id }, e.title),
    ]));
    body.append(mediaPanel("Episodes", episodes.length + " matching episodes", rows.length ? mediaTable(["Episode", "Files", "Date", "Monitoring", "Search"], rows) : mediaNode("p", "No episodes match or have been reported yet.", "empty")));
  } else if (d.app === "Lidarr") {
    const albums = (d.albums || []).filter(a => has(a.title, q)).sort((a, b) => (b.when || "").localeCompare(a.when || "") || byTitle(a, b));
    const rows = albums.map(a => {
      const count = Number.isFinite(a.trackCount) && Number.isFinite(a.trackFileCount) ? a.trackFileCount + " of " + a.trackCount + " tracks" : a.haveFiles ? "Files available" : "Not reported in library";
      return row([a.title, a.when ? mediaDay(a.when) : "Date not reported", count,
        mediaMonitor(a.monitored, { app: d.app, id: d.id, kind: "album", childId: a.id }, "Monitor " + a.title),
        mediaSearchButton("Search releases", { albumId: a.id }, a.title)]);
    });
    body.append(mediaPanel("Albums", albums.length + " matching albums", rows.length ? mediaTable(["Album", "Release date", "Files", "Monitoring", "Search"], rows) : mediaNode("p", "No albums match or have been reported yet.", "empty")));
  }
}
async function searchMediaReleases(child, title) {
  if (releaseBusy || !mediaCurrent) return;
  releaseBusy = true;
  const serial = mediaSerial, context = { ...mediaCurrent, ...child };
  for (const button of document.querySelectorAll(".media-search")) button.disabled = true;
  $("#media-releases").hidden = false;
  $("#media-release-title").textContent = "Releases for " + title;
  $("#media-release-msg").textContent = "Searching through " + context.app + "…";
  $("#media-release-list").replaceChildren();
  try {
    const result = await api("/api/media/releases?" + mediaContextQuery(context));
    if (serial !== mediaSerial) return;
    const items = result.items || [];
    $("#media-release-msg").textContent = items.length ? items.length + " releases. Rejected results cannot be downloaded here." : "No releases were returned by " + context.app + ".";
    const rows = items.map(release => {
      const titleBox = mediaNode("div", release.title);
      if (release.rejected) titleBox.append(mediaNode("span", (release.rejections || []).join("; ") || "Rejected by " + context.app, "sub"));
      const download = mediaNode("button", release.rejected ? "Rejected" : "Download", "small");
      download.type = "button"; download.disabled = !!release.rejected;
      download.setAttribute("aria-label", "Download " + release.title);
      download.addEventListener("click", async () => {
        download.disabled = true; download.textContent = "Checking and sending…";
        try {
          await api("/api/media/grab", { app: context.app, id: context.id, episodeId: context.episodeId, albumId: context.albumId, key: release.key });
          download.textContent = "Sent to " + context.app;
        } catch (e) {
          download.disabled = false; download.textContent = "Download";
          if (serial === mediaSerial) $("#media-release-msg").textContent = e.message;
        }
      });
      return row([titleBox, release.indexer || "Not reported", release.quality || "Not reported", release.protocol || "Not reported",
        Number.isFinite(release.size) ? bytes(release.size) : "—", Number.isFinite(release.seeders) ? release.seeders : "—",
        Number.isFinite(release.ageMinutes) ? dur(release.ageMinutes * 60) : "—", download]);
    });
    if (rows.length) $("#media-release-list").replaceChildren(mediaTable(["Release", "Source", "Quality", "Protocol", "Size", "Seeds", "Age", "Action"], rows));
  } catch (e) { if (serial === mediaSerial) $("#media-release-msg").textContent = e.message; }
  finally { releaseBusy = false; for (const button of document.querySelectorAll(".media-search")) button.disabled = false; }
}

let sourcesSerial = 0;
async function loadSources() {
  const serial = ++sourcesSerial, message = $("#sources-msg"), content = $("#sources-content"), refresh = $("#sources-refresh");
  refresh.disabled = true; message.hidden = false; message.textContent = "Reading configured sources and reported health…";
  content.replaceChildren();
  try {
    const data = await api("/api/sources");
    if (serial !== sourcesSerial) return;
    const problems = data.problems || [];
    message.hidden = data.configured && !problems.length;
    message.textContent = !data.configured ? "Prowlarr isn't connected. Add it in Settings to view its sources. " + problems.join(" ") : problems.join(" ");
    const indexerRows = (data.indexers || []).map(x => row([x.name, x.enabled == null ? "Not reported" : x.enabled ? "Enabled" : "Disabled", x.protocol || "Not reported",
      x.priority == null ? "Not reported" : x.priority, sourceDisableTime(x.disabledUntil)]));
    content.append(mediaPanel("Prowlarr sources", "Observed configuration and reported status, not a live test of every source.", indexerRows.length ? mediaTable(["Source", "Setting", "Protocol", "Priority", "Reported availability"], indexerRows) : mediaNode("p", "No source entries available.", "empty")));
    const health = mediaNode("ul", null, "source-notes");
    for (const h of data.health || []) health.append(mediaNode("li", (h.type || "Message") + ": " + h.message));
    if (!health.childNodes.length) health.append(mediaNode("li", "No health messages were reported. Check any availability errors before relying on this result."));
    content.append(mediaPanel("Reported health", "Messages supplied by the connected app.", health));
    const links = (data.links || []).map(x => row([x.name, x.syncLevel || "Not reported"]));
    content.append(mediaPanel("Application links", "A configured sync mode does not prove that the latest synchronization succeeded.", links.length ? mediaTable(["Application", "Configured sync"], links) : mediaNode("p", "No application links available.", "empty")));
    const libraries = mediaNode("div", null, "media-padding");
    for (const app of data.libraries || []) {
      libraries.append(mediaNode("h3", app.app));
      if (app.error) libraries.append(mediaNode("p", app.error, "notice"));
      else if (!app.configured) libraries.append(mediaNode("p", "Not connected", "muted"));
      else {
        const names = (app.indexers || []).map(x => x.name + (x.enabled == null ? "" : x.enabled ? " (enabled)" : " (disabled)"));
        libraries.append(mediaNode("p", names.length ? names.join(" · ") : "No source entries reported.", "source-names"));
      }
    }
    content.append(mediaPanel("Sources in each library app", "Enabled means at least one RSS or search mode is enabled. These are observed lists; equal counts alone do not prove synchronization.", libraries));
  } catch (e) { message.hidden = false; message.textContent = e.message; }
  finally { if (serial === sourcesSerial) refresh.disabled = false; }
}
$("#sources-refresh").addEventListener("click", loadSources);
$("#media-child-q").addEventListener("input", renderMediaChildren);
$("#media-season").addEventListener("change", renderMediaChildren);
