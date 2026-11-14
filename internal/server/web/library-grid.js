"use strict";

/*
 * My Library: poster grid, compact list and right-hand detail panel.
 *
 * Self-contained module over the `items` a snapshot or /api/browse response
 * returns. The host page owns state and calls:
 *
 *   libraryGridPage({
 *     items,          // snapshot items
 *     filters,        // applied filters: { query, type,
 *                     //   statuses: [], apps: [], genres: [], years: [] }
 *     type,           // optional override for filters.type ("" | Movies | TV | Music)
 *     draft,          // values shown in the Filters panel; defaults to filters
 *     view,           // "grid" (default) | "list"
 *     selected,       // libraryGridKey() of the item whose detail shows
 *     loading, error, problems, filtersOpen,
 *     posterURL,      // optional item -> address, when posters are served over HTTP
 *     handlers: { onSearch(value), onFilterChange(nextDraft), onToggleFilters(),
 *                 onApply(), onReset(), onView("grid"|"list"), onSelect(key|null),
 *                 onType(""|"Movies"|"TV"|"Music"), onSetLocation(detail),
 *                 onOpenLocation(detail), onRetry(), onFindAndAdd() },
 *   }) -> element
 *
 * Old single-value filters ({ status, app, genre, year }) are still accepted and
 * read as one-element arrays.
 *
 * Pure model functions need no DOM: libraryGridKey, libraryGridStatus,
 * libraryGridMeta, libraryGridPoster, libraryGridLocation,
 * libraryGridVerification, libraryGridHistory, libraryGridDetail,
 * libraryGridType, libraryGridTypeCounts, libraryGridTypeTabs,
 * libraryGridFilter, libraryGridFilterNormalize, libraryGridSort,
 * libraryGridCount, libraryGridActiveFilters, libraryGridChips,
 * libraryGridFilterOptions, libraryGridEmpty, libraryGridProblems.
 *
 * Honesty rules: a copy never reads "verified" without `verified` proof; a
 * missing location, time or metadata says so instead of going blank.
 */

// One plain status language, shared with Downloads and Activity. Proof values and
// methods live in the detail's "Proof details", never in a chip.
const LIBGRID_STATUS = {
  queued: { label: "No file yet", tone: "queued" },
  downloading: { label: "Downloading", tone: "active" },
  stalled: { label: "Stalled", tone: "problem" },
  importing: { label: "Importing", tone: "active" },
  copied: { label: "Verified", tone: "ok" },
  moved: { label: "Verified", tone: "ok" },
  missing: { label: "Missing", tone: "problem" },
  "on-disk": { label: "On disk", tone: "neutral" },
  "proof-pending": { label: "Needs proof", tone: "problem" },
  upcoming: { label: "Upcoming", tone: "neutral" },
};

// The volume as a person names it: the real disk the copy is on, never a
// guessed "NAS" label. "/" is this Mac's own disk.
function libraryVolumeLabel(volume) {
  const v = String(volume || "").trim();
  if (!v) return "";
  if (v === "/") return "Startup disk"; // older data: the boot volume, named honestly
  const parts = v.split("/").filter(Boolean);
  if (parts[0] === "Volumes" && parts[1]) return parts[1];
  return v;
}

// Paths wrap after their separators instead of breaking a folder name in half.
function libraryGridBreakPath(text) {
  return String(text || "").replaceAll("/", "/\u200b");
}

// Middle truncation keeps both the volume and the file name readable; the full
// path stays on the row's title and aria-label and in the detail pane.
function libraryGridShortPath(text, max = 52) {
  const s = String(text || "");
  if (s.length <= max) return s;
  const head = Math.max(8, Math.floor((max - 1) * 0.55));
  const tail = Math.max(8, max - 1 - head);
  return s.slice(0, head) + "…" + s.slice(s.length - tail);
}

function libraryGridKey(item) {
  return String(item.app || "Library") + ":" + String(item.id ?? item.title ?? "");
}

function libraryGridVerified(item) {
  return item.verified === "hash" || item.verified === "size";
}

function libraryGridStatus(item) {
  const def = LIBGRID_STATUS[item.status] || { label: item.status || "Status not reported", tone: "neutral" };
  if (item.status !== "copied" && item.status !== "moved") {
    return { key: item.status || "unknown", label: def.label, tone: def.tone };
  }
  // One short chip; the volume and the proof method live in the detail.
  return libraryGridVerified(item)
    ? { key: "verified", label: "Verified", tone: "ok" }
    : { key: "proof-pending", label: "Needs proof", tone: "problem" };
}

function libraryGridRuntime(minutes) {
  const m = Number(minutes);
  if (!Number.isFinite(m) || m <= 0) return "";
  const hours = Math.floor(m / 60), rest = Math.round(m % 60);
  if (!hours) return rest + " min";
  return rest ? hours + "h " + rest + "m" : hours + "h";
}

function libraryGridRating(value) {
  const n = Number(value);
  if (!Number.isFinite(n) || n <= 0) return "";
  return Math.round(n * 10) / 10 + "/10";
}

function libraryGridGenres(item) {
  return (item.genres || []).map((g) => String(g || "").trim()).filter(Boolean);
}

function libraryGridMeta(item) {
  const parts = [
    Number(item.year) > 0 ? String(item.year) : "",
    libraryGridRuntime(item.runtimeMinutes),
    libraryGridGenres(item).slice(0, 3).join(", "),
    libraryGridRating(item.rating),
  ].filter(Boolean);
  return parts.length ? parts.join(" · ") : "Metadata not reported";
}

// Go's zero time serialises as 0001-01-01; it means "not recorded", never a date.
function libraryGridRealTime(value) {
  if (!value) return "";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) || date.getFullYear() < 2000 ? "" : value;
}

function libraryGridTime(value) {
  const real = libraryGridRealTime(value);
  if (!real) return "";
  return new Date(real).toLocaleString(undefined, { day: "numeric", month: "short", year: "numeric", hour: "2-digit", minute: "2-digit" });
}

function libraryGridPoster(item, posterURL) {
  const src = item.posterPath ? String((typeof posterURL === "function" ? posterURL(item) : item.posterPath) || "") : "";
  const label = String(item.title || "").trim().charAt(0).toLocaleUpperCase() || "?";
  return { src, placeholder: !src, label };
}

function libraryGridLocation(item) {
  const volume = String(item.targetVolume || "").trim();
  const where = libraryVolumeLabel(volume);
  const path = String(item.targetPath || "").trim();
  const disk = where ? where + (volume === "/" ? " (/)" : "") : "";
  const text = [disk, path].filter(Boolean).join(" · ") || "No storage location recorded yet";
  return { volume, where, path, text, display: text };
}

function libraryGridVerification(item) {
  const method = item.verified === "hash" ? "SHA-256 hash match"
    : item.verified === "size" ? "Size and readback match"
    : "Not verified — no proof recorded yet";
  return { verified: libraryGridVerified(item), method, hash: String(item.proofHash || ""), at: libraryGridTime(item.verifiedAt) };
}

function libraryGridHistory(item) {
  const entries = [];
  const when = libraryGridRealTime(item.when);
  if (when) {
    entries.push({
      at: when,
      label: item.status === "moved" ? "Local copy moved off this computer"
        : item.status === "upcoming" ? "Release date" : "Recorded",
    });
  }
  const statusAt = libraryGridRealTime(item.statusAt);
  if (statusAt) entries.push({ at: statusAt, label: "Status: " + libraryGridStatus(item).label });
  const verifiedAt = libraryGridRealTime(item.verifiedAt);
  if (verifiedAt) {
    entries.push({
      at: verifiedAt,
      label: "Verified: " + (item.verified === "hash" ? "SHA-256 hash match"
        : item.verified === "size" ? "Size and readback match" : "method not recorded"),
    });
  }
  return entries.sort((a, b) => new Date(b.at) - new Date(a.at));
}

function libraryGridDetail(item, posterURL) {
  const genres = libraryGridGenres(item);
  return {
    key: libraryGridKey(item),
    title: item.title || "Untitled",
    app: item.app || "",
    // A local file has no saved proof to show; it gets a metadata match instead.
    local: item.app === "Local",
    path: String(item.targetPath || item.id || ""),
    poster: libraryGridPoster(item, posterURL),
    status: libraryGridStatus(item),
    overview: String(item.overview || "").trim(),
    year: Number(item.year) > 0 ? String(item.year) : "",
    runtime: libraryGridRuntime(item.runtimeMinutes),
    genres,
    rating: libraryGridRating(item.rating),
    location: libraryGridLocation(item),
    verification: libraryGridVerification(item),
    downloadHash: String(item.downloadHash || ""),
    localCopy: item.localCopy === true,
    source: String(item.sourcePath || "").trim(),
    history: libraryGridHistory(item),
  };
}

// The type tab a title belongs to. Local files live in the films/series folder,
// and the library's own wording puts them under Movies.
function libraryGridType(item) {
  const app = String(item?.app || "");
  if (app === "Radarr" || app === "Local") return "Movies";
  if (app === "Sonarr") return "TV";
  if (app === "Lidarr") return "Music";
  return "";
}

// Tabs count the whole library, never the filtered subset.
function libraryGridTypeCounts(items) {
  const counts = { "": 0, Movies: 0, TV: 0, Music: 0 };
  for (const item of items || []) {
    counts[""] += 1;
    const type = libraryGridType(item);
    if (type) counts[type] += 1;
  }
  return counts;
}

function libraryGridTypeTabs(items, active = "") {
  const counts = libraryGridTypeCounts(items);
  return [["", "Show all"], ["Movies", "Movies"], ["TV", "TV"], ["Music", "Music"]].map(([value, label]) => ({
    value,
    label,
    count: counts[value],
    selected: value === String(active || ""),
  }));
}

// Filters arrived as one value per field before 0.7.18; the model now holds
// arrays. Both shapes normalise here so an old saved filter keeps working.
function libraryGridFilterList(value) {
  if (Array.isArray(value)) return value.map((v) => String(v)).filter(Boolean);
  const one = String(value ?? "").trim();
  return one ? [one] : [];
}

function libraryGridFilterNormalize(filters = {}) {
  const merged = (plural, single) =>
    [...new Set([...libraryGridFilterList(filters[plural]), ...libraryGridFilterList(filters[single])])];
  return {
    query: String(filters.query || ""),
    type: String(filters.type || ""),
    statuses: merged("statuses", "status"),
    apps: merged("apps", "app"),
    genres: merged("genres", "genre"),
    years: merged("years", "year"),
  };
}

function libraryGridFilter(items, filters = {}) {
  const f = libraryGridFilterNormalize(filters);
  const query = f.query.trim().toLocaleLowerCase();
  const statuses = new Set(f.statuses);
  const apps = new Set(f.apps);
  const genres = new Set(f.genres.map((g) => g.toLocaleLowerCase()));
  const years = new Set(f.years);
  return (items || []).filter((item) =>
    (!query || String(item.title || "").toLocaleLowerCase().includes(query)) &&
    (!f.type || libraryGridType(item) === f.type) &&
    // The full status text: "verified" and "not verified" are different filters.
    (!statuses.size || statuses.has(libraryGridStatus(item).label)) &&
    (!apps.size || apps.has(item.app)) &&
    (!genres.size || libraryGridGenres(item).some((g) => genres.has(g.toLocaleLowerCase()))) &&
    (!years.size || years.has(String(item.year || ""))));
}

// Title is the default order; the list's column headers can ask for status.
function libraryGridSort(items, sort = {}) {
  const key = sort.key === "status" ? "status" : "title";
  const dir = sort.dir === "desc" ? -1 : 1;
  const value = (item) => key === "status" ? libraryGridStatus(item).label : String(item.title || "");
  return (items || []).slice().sort((a, b) =>
    dir * value(a).localeCompare(value(b), undefined, { sensitivity: "base", numeric: true }));
}

function libraryGridCount(shown, total) {
  const noun = total === 1 ? "item" : "items";
  return shown === total ? total + " " + noun : shown + " of " + total + " " + noun;
}

function libraryGridActiveFilters(filters = {}) {
  const f = libraryGridFilterNormalize(filters);
  return f.statuses.length + f.apps.length + f.genres.length + f.years.length;
}

// One chip per applied value, plus the query and the type. The caller removes it
// by rebuilding the applied filter object.
function libraryGridChips(filters = {}) {
  const f = libraryGridFilterNormalize(filters);
  const chips = [];
  if (f.query.trim()) chips.push({ field: "query", value: f.query, label: "Search: " + f.query.trim() });
  if (f.type) chips.push({ field: "type", value: f.type, label: "Type: " + f.type });
  const group = (field, name, values) => {
    for (const value of values) chips.push({ field, value, label: name + ": " + value });
  };
  group("statuses", "Status", f.statuses);
  group("apps", "App", f.apps);
  group("genres", "Genre", f.genres);
  group("years", "Year", f.years);
  return chips;
}

// Every distinct value the library actually holds, with how many items carry it.
function libraryGridFilterOptions(items) {
  const statuses = new Map(), apps = new Map(), genres = new Map(), years = new Map();
  const bump = (map, key) => map.set(key, (map.get(key) || 0) + 1);
  for (const item of items || []) {
    bump(statuses, libraryGridStatus(item).label); // every distinct status, exactly as shown
    if (item.app) bump(apps, item.app);
    const seen = new Set();
    for (const genre of libraryGridGenres(item)) {
      const key = genre.toLocaleLowerCase();
      seen.add(key);
      if (!genres.has(key)) genres.set(key, { label: genre, count: 0 });
    }
    for (const key of seen) genres.get(key).count += 1;
    const year = Number(item.year);
    if (Number.isFinite(year) && year > 0) bump(years, String(year));
  }
  const byLabel = (a, b) => a.label.localeCompare(b.label, undefined, { sensitivity: "base", numeric: true });
  return {
    statuses: [...statuses].map(([value, count]) => ({ value, label: value, count })).sort(byLabel),
    apps: [...apps].map(([value, count]) => ({ value, label: value, count }))
      .sort((a, b) => a.label.localeCompare(b.label)),
    genres: [...genres.values()].map(({ label, count }) => ({ value: label, label, count })).sort(byLabel),
    years: [...years].map(([value, count]) => ({ value, label: value, count }))
      .sort((a, b) => Number(b.value) - Number(a.value)),
  };
}

// The one place that decides what an empty screen says and what to do next.
function libraryGridEmpty({ items = [], shown = 0, loading = false, error = "", query = "" } = {}) {
  if (loading && !items.length) {
    return { kind: "loading", title: "Loading your library…", message: "Posters, status and storage location appear as soon as each source answers.", action: "" };
  }
  if (error) {
    return { kind: "error", title: "Your library could not be loaded.", message: String(error), action: "Try again" };
  }
  if (!items.length) {
    return { kind: "empty", title: "Nothing in your library yet.", message: "Use Find & add to add a film or series; it appears here with its status and storage location.", action: "Find & add" };
  }
  if (!shown) {
    if (String(query || "").trim()) {
      return { kind: "filtered", title: "Nothing matches your search.", message: "Clear the search to see the rest of your library.", action: "Clear search" };
    }
    return { kind: "filtered", title: "Nothing matches these filters.", message: "Reset the filters to see the rest of your library.", action: "Reset filters" };
  }
  return null;
}

function libraryGridProblems(problems) {
  const list = (problems || []).filter(Boolean);
  return list.length ? "Some sources didn't answer: " + list.join(" ") + " Showing what did." : "";
}

// ---- DOM builders ----

let libraryGridIdSeq = 0;

function libraryGridNode(tag, text, className) {
  const n = document.createElement(tag);
  if (text != null) n.textContent = text;
  if (className) n.className = className;
  return n;
}

function libraryGridStatusChip(status) {
  return libraryGridNode("span", status.label, "libgrid-status libgrid-status-" + status.tone);
}

function libraryGridPosterPlaceholder(label) {
  const span = libraryGridNode("span", label || "?", "libgrid-poster-placeholder");
  span.setAttribute("aria-hidden", "true");
  return span;
}

function libraryGridPosterBox(poster, className) {
  const box = libraryGridNode("span", null, "libgrid-poster" + (className ? " " + className : ""));
  if (poster.src) {
    const img = document.createElement("img");
    img.src = poster.src;
    img.alt = "";
    img.loading = "lazy";
    // A poster file that vanished reads as a placeholder, not a broken image.
    img.addEventListener("error", () => img.replaceWith(libraryGridPosterPlaceholder(poster.label)));
    box.append(img);
  } else box.append(libraryGridPosterPlaceholder(poster.label));
  return box;
}

function libraryGridCard(item, options = {}) {
  const key = libraryGridKey(item);
  const selected = options.selected === key;
  const card = libraryGridNode("button", null, "libgrid-card" + (selected ? " is-selected" : ""));
  card.type = "button";
  card.setAttribute("data-key", key);
  card.setAttribute("aria-pressed", selected ? "true" : "false");
  card.append(libraryGridPosterBox(libraryGridPoster(item, options.posterURL)));
  card.append(libraryGridNode("span", item.title || "Untitled", "libgrid-card-title"));
  card.append(libraryGridStatusChip(libraryGridStatus(item)));
  card.append(libraryGridNode("span", libraryGridMeta(item), "libgrid-meta"));
  if (item.status === "downloading" || item.status === "importing" || item.status === "stalled") {
    const progress = libraryGridNode("span", null, "libgrid-card-progress");
    const bar = document.createElement("progress");
    bar.max = 1;
    if (Number.isFinite(item.progress) && item.progress >= 0 && item.progress <= 1) bar.value = item.progress;
    bar.setAttribute("aria-label", "Progress for " + (item.title || "this item"));
    const caption = libraryGridNode("span", item.status === "importing"
      ? "Importing into the library"
      : Number.isFinite(item.progress) && item.progress >= 0 ? Math.floor(item.progress * 100) + "% · next: import" : "Progress not reported");
    progress.append(bar, caption);
    card.append(progress);
  }
  card.addEventListener("click", () => options.handlers?.onSelect?.(key));
  return card;
}

function libraryGridRow(item, options = {}) {
  const key = libraryGridKey(item);
  const selected = options.selected === key;
  const row = libraryGridNode("button", null, "libgrid-row" + (selected ? " is-selected" : ""));
  row.type = "button";
  row.setAttribute("data-key", key);
  row.setAttribute("aria-pressed", selected ? "true" : "false");
  row.append(libraryGridPosterBox(libraryGridPoster(item, options.posterURL), "libgrid-poster-small"));
  const main = libraryGridNode("span", null, "libgrid-row-main");
  main.append(libraryGridNode("span", item.title || "Untitled", "libgrid-card-title"));
  main.append(libraryGridNode("span", libraryGridMeta(item), "libgrid-meta"));
  row.append(main);
  const fullLocation = libraryGridLocation(item).text;
  // The row carries the full path; CSS wraps or stacks it. Shortening the text
  // at build time made the visible path depend on when the row was rendered.
  const location = libraryGridNode("span", fullLocation, "libgrid-row-location");
  location.title = fullLocation;
  location.setAttribute("aria-label", fullLocation);
  row.append(location);
  row.append(libraryGridStatusChip(libraryGridStatus(item)));
  row.addEventListener("click", () => options.handlers?.onSelect?.(key));
  return row;
}

function libraryGridGrid(items, options = {}) {
  const grid = libraryGridNode("div", null, "libgrid-grid");
  grid.append(...items.map((item) => libraryGridCard(item, options)));
  return grid;
}

// The list is a table with aligned columns and one sort control per column.
function libraryGridList(items, options = {}) {
  const sort = options.sort || {};
  const list = libraryGridNode("div", null, "libgrid-list");
  const head = libraryGridNode("div", null, "libgrid-list-head");
  head.setAttribute("role", "row");
  const sortButton = (key, label) => {
    const active = (sort.key || "title") === key;
    const button = libraryGridNode("button", label + (active ? (sort.dir === "desc" ? " ↓" : " ↑") : ""), "libgrid-sort");
    button.type = "button";
    button.setAttribute("role", "columnheader");
    button.setAttribute("aria-sort", active ? (sort.dir === "desc" ? "descending" : "ascending") : "none");
    button.addEventListener("click", () => options.handlers?.onSort?.(key));
    return button;
  };
  head.append(libraryGridNode("span", "", "libgrid-list-cell"));
  head.append(sortButton("title", "Title"));
  head.append(libraryGridNode("span", "Location", "libgrid-list-cell"));
  head.append(sortButton("status", "Status"));
  list.append(head);
  list.append(...items.map((item) => libraryGridRow(item, options)));
  return list;
}

function libraryGridDetailPanel(detail, handlers = {}) {
  const aside = libraryGridNode("aside", null, "libgrid-detail");
  aside.setAttribute("aria-label", "Details for " + detail.title);
  const head = libraryGridNode("div", null, "libgrid-detail-head");
  // On a phone the detail replaces the list, so its first control is Back.
  const back = libraryGridNode("button", "Back", "libgrid-back");
  back.type = "button";
  back.setAttribute("aria-label", "Back to the list");
  back.addEventListener("click", () => handlers.onSelect?.(null));
  head.append(back);
  head.append(libraryGridNode("h2", detail.title, "libgrid-detail-title"));
  const close = libraryGridNode("button", "Close", "libgrid-close");
  close.type = "button";
  close.setAttribute("aria-label", "Close details");
  close.addEventListener("click", () => handlers.onSelect?.(null));
  head.append(close);
  aside.append(head);
  aside.append(libraryGridPosterBox(detail.poster));
  aside.append(libraryGridStatusChip(detail.status));
  aside.append(libraryGridNode("p", detail.overview || "No overview available from the metadata source.",
    "libgrid-overview" + (detail.overview ? "" : " is-muted")));

  const facts = document.createElement("dl");
  facts.className = "libgrid-facts";
  const fact = (name, value, className) => {
    facts.append(libraryGridNode("dt", name), libraryGridNode("dd", value, className || null));
  };
  if (detail.year) fact("Year", detail.year);
  if (detail.runtime) fact("Runtime", detail.runtime);
  if (detail.genres.length) fact("Genres", detail.genres.join(", "));
  if (detail.rating) fact("Rating", detail.rating);
  if (!detail.year && !detail.runtime && !detail.genres.length && !detail.rating) fact("Metadata", "Not reported by the source");
  if (detail.app) fact("Added through", detail.app);
  fact("Location", detail.location.display || detail.location.text, detail.location.path ? null : "is-problem");
  if (detail.location.path) {
    const missing = detail.status.key === "missing";
    fact("File check", missing ? "Checked on this machine — not found" : "Checked on this machine — found", missing ? "is-problem" : null);
  }
  if (!detail.local) {
    fact("Proof", detail.verification.verified
      ? "Verified" + (detail.verification.at ? " · " + detail.verification.at : "")
      : "Needs proof — no verification recorded yet", detail.verification.verified ? null : "is-problem");
  }
  aside.append(facts);
  // Storage controls only report the request; the host owns the dialog and the
  // file-system work, so this module never fetches or opens anything.
  const locationActions = libraryGridNode("div", null, "libgrid-detail-actions");
  const setLocation = libraryGridNode("button", "Set location", "libgrid-set-location");
  setLocation.type = "button";
  setLocation.addEventListener("click", () => handlers.onSetLocation?.(detail));
  const openLocation = libraryGridNode("button", "Open location", "libgrid-open-location");
  openLocation.type = "button";
  locationActions.append(setLocation, openLocation);
  if (detail.location.path) {
    openLocation.addEventListener("click", () => handlers.onOpenLocation?.(detail));
  } else {
    // A disabled control still says why, so it is not a silent dead end.
    openLocation.disabled = true;
    const reason = libraryGridNode("p", "No storage location recorded yet — set one first.", "libgrid-location-reason");
    reason.id = "libgrid-location-reason-" + (++libraryGridIdSeq);
    openLocation.setAttribute("aria-describedby", reason.id);
    openLocation.title = reason.textContent;
    locationActions.append(reason);
  }
  aside.append(locationActions);
  if (!detail.local && (detail.verification.verified || detail.verification.hash || detail.source)) {
    // Proof values are detail on demand, not banner copy.
    const proof = document.createElement("details");
    proof.className = "libgrid-proof";
    const summary = document.createElement("summary");
    summary.textContent = "Proof details";
    const list = document.createElement("dl");
    list.className = "libgrid-facts";
    const row = (name, value) => list.append(libraryGridNode("dt", name), libraryGridNode("dd", value));
    row("Method", detail.verification.method);
    if (detail.verification.hash) row("Hash", detail.verification.hash);
    if (detail.source) row("Source", detail.source);
    proof.append(summary, list);
    aside.append(proof);
  }
  if (detail.local) {
    const match = libraryGridNode("button", "Match metadata", "libgrid-state-action libgrid-match");
    match.type = "button";
    match.setAttribute("aria-label", "Match metadata for " + detail.title);
    match.addEventListener("click", () => handlers.onMatch?.(detail));
    aside.append(match);
  }
  if (detail.location.path) {
    const box = libraryGridNode("div", null, "libgrid-path-box");
    const field = document.createElement("input");
    field.type = "text";
    field.readOnly = true;
    field.value = detail.location.path;
    field.setAttribute("aria-label", "Full storage path (selectable)");
    const copy = libraryGridNode("button", "Copy path", "libgrid-copy-path");
    copy.type = "button";
    copy.setAttribute("aria-label", "Copy the full storage path");
    const message = libraryGridNode("p", "", "libgrid-path-msg");
    message.setAttribute("role", "status");
    copy.addEventListener("click", async () => {
      const raw = field.value;
      try {
        await navigator.clipboard.writeText(raw);
        message.textContent = "Copied.";
        message.dataset.state = "ok";
      } catch (e) {
        message.textContent = "Could not copy: " + (e?.message || "clipboard unavailable") + ". Select the path above.";
        message.dataset.state = "error";
        field.select?.();
      }
    });
    box.append(field, copy, message);
    aside.append(box);
  }
  // A title that is not checkable in the library is never a dead end.
  const nextKey = detail.status.key;
  const noPath = !detail.location.path;
  if ((nextKey === "queued" && noPath) || (nextKey === "proof-pending" && noPath) || nextKey === "missing") {
    const box = libraryGridNode("div", null, "libgrid-next");
    const message = nextKey === "missing"
      ? "Not found at " + (detail.location.path || "the recorded path") + ". Check Downloads, or add the title again."
      : nextKey === "proof-pending"
        ? "The app reports a file, but no path could be checked. Locate it in Downloads, or add it again."
        : "No file yet. Add a title, or check Downloads for its transfer.";
    box.append(libraryGridNode("p", message, "is-muted"));
    const downloads = libraryGridNode("button", "Open Downloads", "libgrid-state-action");
    downloads.type = "button";
    downloads.addEventListener("click", () => handlers.onOpenDownloads?.());
    const find = libraryGridNode("button", "Find & add", "libgrid-state-action");
    find.type = "button";
    find.addEventListener("click", () => handlers.onFindAndAdd?.());
    box.append(downloads, find);
    aside.append(box);
  }
  if (!detail.local && (detail.localCopy || !detail.verification.verified)) {
    // A real verification of this copy — offered whenever a saved record tracks
    // it, including a re-check of an already-verified copy. Without a record the
    // control is replaced by the reason.
    const check = libraryGridNode("button", "Verify this copy", "libgrid-state-action");
    check.type = "button";
    const result = libraryGridNode("p", "", "libgrid-verify-msg");
    result.setAttribute("role", "status");
    if (!detail.localCopy) {
      // No tracked copy: say so plainly instead of showing a control that cannot act.
      result.textContent = "No local copy is tracked — verify when a local copy is found.";
    } else {
      if (detail.verification.verified) result.textContent = "Re-checks the saved hashes against the file on disk.";
      check.addEventListener("click", async () => {
        check.disabled = true;
        result.textContent = "Checking the saved proof…";
        try {
          result.textContent = await handlers.onVerify?.(detail) || "The check finished without a result.";
        } catch (e) {
          result.textContent = e.message || "The check could not run.";
        } finally {
          check.disabled = false;
        }
      });
    }
    if (detail.localCopy) aside.append(check, result);
    else aside.append(result);
  }

  const history = libraryGridNode("div", null, "libgrid-history-block");
  history.append(libraryGridNode("h3", "History", "libgrid-history-title"));
  if (detail.history.length) {
    const list = document.createElement("ol");
    list.className = "libgrid-history";
    for (const entry of detail.history) {
      const li = document.createElement("li");
      const time = document.createElement("time");
      time.setAttribute("datetime", String(entry.at));
      time.textContent = libraryGridTime(entry.at) || String(entry.at);
      li.append(time, libraryGridNode("span", entry.label));
      list.append(li);
    }
    history.append(list);
  } else history.append(libraryGridNode("p", "No earlier events are recorded. Current state: " + detail.status.label + ".", "is-muted"));
  aside.append(history);
  return aside;
}

// Type tabs count the whole library; the active tab is the only one in the Tab
// order, and left/right arrows move between them.
function libraryGridTabsRow(options) {
  const row = libraryGridNode("div", null, "libgrid-tabs");
  row.setAttribute("role", "tablist");
  row.setAttribute("aria-label", "Library type");
  const tabs = libraryGridTypeTabs(options.items || [], options.type);
  const buttons = tabs.map((tab) => {
    const button = libraryGridNode("button", null, "libgrid-tab");
    button.type = "button";
    button.setAttribute("role", "tab");
    button.setAttribute("aria-selected", tab.selected ? "true" : "false");
    button.tabIndex = tab.selected ? 0 : -1;
    button.setAttribute("aria-label", tab.label + ", " + tab.count + (tab.count === 1 ? " item" : " items"));
    button.append(
      libraryGridNode("span", tab.label, "libgrid-tab-label"),
      libraryGridNode("span", String(tab.count), "libgrid-tab-count"),
    );
    button.addEventListener("click", () => options.handlers?.onType?.(tab.value));
    row.append(button);
    return button;
  });
  buttons.forEach((button, index) => {
    button.addEventListener("keydown", (event) => {
      const step = event.key === "ArrowRight" ? 1 : event.key === "ArrowLeft" ? -1 : 0;
      if (!step) return;
      event.preventDefault?.();
      const next = (index + step + tabs.length) % tabs.length;
      buttons[next].focus?.();
      options.handlers?.onType?.(tabs[next].value);
    });
  });
  return row;
}

function libraryGridToolbar(options) {
  const bar = libraryGridNode("div", null, "libgrid-toolbar");
  const search = document.createElement("input");
  search.type = "search";
  search.id = "lib-q";
  search.className = "libgrid-search";
  search.value = options.filters?.query || "";
  search.placeholder = "Search your library…";
  search.setAttribute("aria-label", "Search your library");
  search.setAttribute("aria-keyshortcuts", "Meta+F Control+F");
  search.addEventListener("input", () => options.handlers?.onSearch?.(search.value));
  bar.append(search);

  const active = libraryGridActiveFilters(options.filters);
  const filters = libraryGridNode("button", active ? "Filters · " + active : "Filters", "libgrid-filters-toggle");
  filters.type = "button";
  filters.setAttribute("aria-expanded", options.filtersOpen ? "true" : "false");
  filters.addEventListener("click", () => options.handlers?.onToggleFilters?.());
  bar.append(filters);

  const views = libraryGridNode("div", null, "libgrid-views");
  for (const [value, label] of [["grid", "Cards"], ["list", "Table"]]) {
    const button = libraryGridNode("button", label, "libgrid-view");
    button.type = "button";
    button.setAttribute("aria-pressed", options.view === value ? "true" : "false");
    button.addEventListener("click", () => options.handlers?.onView?.(value));
    views.append(button);
  }
  bar.append(views);

  // Result count and one removable chip per applied value, on its own line.
  const line = libraryGridNode("div", null, "libgrid-filterline");
  line.append(libraryGridNode("span", options.count, "libgrid-count"));
  for (const chip of libraryGridChips(options.filters)) {
    const el = libraryGridNode("span", null, "libgrid-chip");
    el.append(libraryGridNode("span", chip.label, "libgrid-chip-label"));
    const remove = libraryGridNode("button", "×", "libgrid-chip-remove");
    remove.type = "button";
    remove.setAttribute("aria-label", "Remove " + chip.label);
    remove.addEventListener("click", () => {
      const next = libraryGridFilterNormalize(options.filters);
      if (chip.field === "query") next.query = "";
      else if (chip.field === "type") next.type = "";
      else next[chip.field] = next[chip.field].filter((value) => value !== chip.value);
      options.handlers?.onRemoveFilter?.(next);
    });
    el.append(remove);
    line.append(el);
  }
  bar.append(line);
  return bar;
}

// A checkbox group built only from values the library actually holds.
function libraryGridCheckGroup(title, name, choices, selected, onChange) {
  const group = document.createElement("fieldset");
  group.className = "libgrid-filter-group";
  const legend = document.createElement("legend");
  legend.textContent = title;
  group.append(legend);
  if (!choices.length) {
    group.append(libraryGridNode("p", "No values in this library.", "is-muted"));
    return group;
  }
  for (const choice of choices) {
    const label = libraryGridNode("label", null, "libgrid-check");
    const box = document.createElement("input");
    box.type = "checkbox";
    box.name = name;
    box.value = choice.value;
    box.checked = selected.includes(choice.value);
    box.addEventListener("change", () => {
      const next = box.checked
        ? [...selected, choice.value]
        : selected.filter((value) => value !== choice.value);
      onChange(next);
    });
    label.append(box, libraryGridNode("span", choice.label, "libgrid-check-label"));
    if (Number.isFinite(choice.count)) {
      label.append(libraryGridNode("span", String(choice.count), "libgrid-check-count"));
    }
    group.append(label);
  }
  return group;
}

function libraryGridFilterPanel(options) {
  const panel = libraryGridNode("div", null, "libgrid-filters");
  panel.setAttribute("role", "group");
  panel.setAttribute("aria-label", "Filters");
  const draft = libraryGridFilterNormalize(options.draft || {});
  const choices = libraryGridFilterOptions(options.items || []);
  const changed = (name, values) => options.handlers?.onFilterChange?.({ ...draft, [name]: values });
  panel.append(libraryGridCheckGroup("Status", "statuses", choices.statuses, draft.statuses, (v) => changed("statuses", v)));
  panel.append(libraryGridCheckGroup("App", "apps", choices.apps, draft.apps, (v) => changed("apps", v)));
  panel.append(libraryGridCheckGroup("Genre", "genres", choices.genres, draft.genres, (v) => changed("genres", v)));
  panel.append(libraryGridCheckGroup("Year", "years", choices.years, draft.years, (v) => changed("years", v)));
  const actions = libraryGridNode("div", null, "libgrid-filter-actions");
  const apply = libraryGridNode("button", "Apply", "libgrid-apply");
  apply.type = "button";
  apply.addEventListener("click", () => options.handlers?.onApply?.());
  const reset = libraryGridNode("button", "Reset", "libgrid-reset");
  reset.type = "button";
  reset.addEventListener("click", () => options.handlers?.onReset?.());
  actions.append(apply, reset);
  panel.append(actions);
  return panel;
}

function libraryGridStateBlock(state, handlers = {}) {
  const box = libraryGridNode("div", null, "libgrid-state libgrid-state-" + state.kind);
  box.append(libraryGridNode("h3", state.title, "libgrid-state-title"));
  box.append(libraryGridNode("p", state.message, "libgrid-state-message"));
  if (state.action) {
    const button = libraryGridNode("button", state.action, "libgrid-state-action");
    button.type = "button";
    button.addEventListener("click", () => {
      if (state.kind === "error") handlers.onRetry?.();
      else if (state.kind === "empty") handlers.onFindAndAdd?.();
      else handlers.onReset?.();
    });
    box.append(button);
  }
  return box;
}

function libraryGridPage(options = {}) {
  const items = Array.isArray(options.items) ? options.items : [];
  const filters = libraryGridFilterNormalize(options.filters || {});
  if (options.type != null) filters.type = String(options.type || "");
  const handlers = options.handlers || {};
  const view = options.view === "list" ? "list" : "grid";
  const shown = libraryGridSort(libraryGridFilter(items, filters), options.sort);

  const page = libraryGridNode("div", null, "libgrid-page");
  page.append(libraryGridTabsRow({ items, type: filters.type, handlers }));
  const toolbar = libraryGridToolbar({
    filters,
    count: libraryGridCount(shown.length, items.length),
    view,
    filtersOpen: options.filtersOpen,
    handlers,
  });
  // The panel lives inside the toolbar so it can pop over it on wide screens.
  if (options.filtersOpen) {
    toolbar.append(libraryGridFilterPanel({
      items,
      draft: options.draft ? libraryGridFilterNormalize(options.draft) : filters,
      handlers,
    }));
  }
  page.append(toolbar);

  const problems = libraryGridProblems(options.problems);
  if (problems) page.append(libraryGridNode("p", problems, "libgrid-problems is-problem"));

  const body = libraryGridNode("div", null, "libgrid-body");
  const results = libraryGridNode("div", null, "libgrid-results");
  const empty = libraryGridEmpty({ items, shown: shown.length, loading: options.loading, error: options.error, query: filters.query });
  if (empty) results.append(libraryGridStateBlock(empty, handlers));
  else results.append(view === "list" ? libraryGridList(shown, { ...options, handlers }) : libraryGridGrid(shown, { ...options, handlers }));
  body.append(results);

  const selected = shown.find((item) => libraryGridKey(item) === options.selected);
  if (selected) body.append(libraryGridDetailPanel(libraryGridDetail(selected, options.posterURL), handlers));
  page.append(body);
  return page;
}
