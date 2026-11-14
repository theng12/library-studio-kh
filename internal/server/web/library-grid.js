"use strict";

/*
 * My Library: poster grid, compact list and right-hand detail panel.
 *
 * Self-contained module over the `items` a snapshot or /api/browse response
 * returns. The host page owns state and calls:
 *
 *   libraryGridPage({
 *     items,          // snapshot items
 *     filters,        // applied filters: { query, status, app, genre, year }
 *     draft,          // values shown in the Filters panel; defaults to filters
 *     view,           // "grid" (default) | "list"
 *     selected,       // libraryGridKey() of the item whose detail shows
 *     loading, error, problems, filtersOpen,
 *     posterURL,      // optional item -> address, when posters are served over HTTP
 *     handlers: { onSearch(value), onFilterChange(nextDraft), onToggleFilters(),
 *                 onApply(), onReset(), onView("grid"|"list"), onSelect(key|null),
 *                 onRetry(), onFindAndAdd() },
 *   }) -> element
 *
 * Pure model functions need no DOM: libraryGridKey, libraryGridStatus,
 * libraryGridMeta, libraryGridPoster, libraryGridLocation,
 * libraryGridVerification, libraryGridHistory, libraryGridDetail,
 * libraryGridFilter, libraryGridSort, libraryGridCount,
 * libraryGridActiveFilters, libraryGridFilterOptions, libraryGridEmpty,
 * libraryGridProblems.
 *
 * Honesty rules: a copy never reads "verified" without `verified` proof; a
 * missing location, time or metadata says so instead of going blank.
 */

const LIBGRID_STATUS = {
  queued: { label: "Queued", tone: "queued" },
  downloading: { label: "Downloading", tone: "active" },
  stalled: { label: "Stalled", tone: "problem" },
  importing: { label: "Importing", tone: "active" },
  copied: { label: "Copied", tone: "ok" },
  moved: { label: "Moved", tone: "ok" },
  missing: { label: "Missing", tone: "problem" },
  "on-disk": { label: "On disk", tone: "neutral" },
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
  const known = LIBGRID_STATUS[item.status];
  const def = known || { label: item.status || "Status not reported", tone: "neutral" };
  if (item.status !== "copied" && item.status !== "moved") {
    return { key: item.status || "unknown", label: def.label, tone: def.tone };
  }
  const where = libraryVolumeLabel(item.targetVolume);
  const base = where ? def.label + " to " + where : def.label;
  return libraryGridVerified(item)
    ? { key: item.status, label: base + " (verified)", tone: "ok" }
    : { key: item.status, label: base + " (not verified)", tone: "problem" };
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

function libraryGridFilter(items, filters = {}) {
  const query = String(filters.query || "").trim().toLocaleLowerCase();
  return (items || []).filter((item) =>
    (!query || String(item.title || "").toLocaleLowerCase().includes(query)) &&
    // The full status text: "verified" and "not verified" are different filters.
    (!filters.status || libraryGridStatus(item).label === filters.status) &&
    (!filters.app || item.app === filters.app) &&
    (!filters.genre || libraryGridGenres(item).some((g) => g.toLocaleLowerCase() === String(filters.genre).toLocaleLowerCase())) &&
    (!filters.year || String(item.year || "") === String(filters.year)));
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
  return ["status", "app", "genre", "year"].filter((key) => String(filters[key] || "").trim()).length;
}

function libraryGridFilterOptions(items) {
  const statuses = new Map(), apps = new Set(), genres = new Map(), years = new Set();
  for (const item of items || []) {
    const status = libraryGridStatus(item);
    statuses.set(status.label, status.label); // every distinct status, exactly as shown
    if (item.app) apps.add(item.app);
    for (const genre of libraryGridGenres(item)) genres.set(genre.toLocaleLowerCase(), genre);
    const year = Number(item.year);
    if (Number.isFinite(year) && year > 0) years.add(year);
  }
  const byLabel = (a, b) => a.label.localeCompare(b.label, undefined, { sensitivity: "base", numeric: true });
  return {
    statuses: [...statuses].map(([value, label]) => ({ value, label })).sort(byLabel),
    apps: [...apps].sort((a, b) => a.localeCompare(b)).map((app) => ({ value: app, label: app })),
    genres: [...genres.values()].sort((a, b) => a.localeCompare(b, undefined, { sensitivity: "base" }))
      .map((genre) => ({ value: genre, label: genre })),
    years: [...years].sort((a, b) => b - a).map((year) => ({ value: String(year), label: String(year) })),
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
  fact("Verified", detail.verification.verified
    ? detail.verification.method + (detail.verification.at ? " · " + detail.verification.at : " · time not recorded")
    : detail.verification.method, detail.verification.verified ? null : "is-problem");
  if (detail.verification.hash) fact("Proof hash", detail.verification.hash);
  if (detail.source) fact("Source", detail.source);
  aside.append(facts);
  if (detail.location.path) {
    const copy = libraryGridNode("button", "Copy path", "libgrid-copy-path");
    copy.type = "button";
    copy.setAttribute("aria-label", "Copy the full storage path");
    copy.addEventListener("click", async () => {
      const raw = detail.location.path;
      try {
        await navigator.clipboard.writeText(raw);
        copy.textContent = "Copied";
        copy.title = raw;
      } catch {
        copy.textContent = "Copy unavailable — select the path";
      }
      setTimeout(() => { copy.textContent = "Copy path"; }, 2000);
    });
    aside.append(copy);
  }
  // 3) A title with no file is never a dead end.
  if (!detail.location.path && detail.status.key === "queued") {
    const box = libraryGridNode("div", null, "libgrid-next");
    box.append(libraryGridNode("p", "No file yet. Add a title, or check Downloads for its transfer.", "is-muted"));
    const downloads = libraryGridNode("button", "Open Downloads", "libgrid-state-action");
    downloads.type = "button";
    downloads.addEventListener("click", () => handlers.onOpenDownloads?.());
    const find = libraryGridNode("button", "Find & add", "libgrid-state-action");
    find.type = "button";
    find.addEventListener("click", () => handlers.onFindAndAdd?.());
    box.append(downloads, find);
    aside.append(box);
  }
  if (detail.localCopy || !detail.verification.verified) {
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
  bar.append(libraryGridNode("span", options.count, "libgrid-count"));

  const views = libraryGridNode("div", null, "libgrid-views");
  for (const [value, label] of [["grid", "Cards"], ["list", "Table"]]) {
    const button = libraryGridNode("button", label, "libgrid-view");
    button.type = "button";
    button.setAttribute("aria-pressed", options.view === value ? "true" : "false");
    button.addEventListener("click", () => options.handlers?.onView?.(value));
    views.append(button);
  }
  bar.append(views);
  return bar;
}

function libraryGridSelect(labelText, name, allLabel, options, value, onChange) {
  const label = libraryGridNode("label", labelText, "libgrid-filter");
  const select = document.createElement("select");
  select.setAttribute("name", name);
  select.setAttribute("aria-label", labelText);
  const choices = options.slice();
  // A filter value whose item left the list still shows instead of blanking.
  if (value && !choices.some((option) => option.value === value)) choices.push({ value, label: value });
  const all = document.createElement("option");
  all.value = "";
  all.textContent = allLabel;
  select.append(all);
  for (const option of choices) {
    const el = document.createElement("option");
    el.value = option.value;
    el.textContent = option.label;
    if (option.value === value) el.selected = true;
    select.append(el);
  }
  select.value = value || "";
  select.addEventListener("change", () => onChange(select.value));
  label.append(select);
  return label;
}

function libraryGridFilterPanel(options) {
  const panel = libraryGridNode("div", null, "libgrid-filters");
  const draft = options.draft || {};
  const choices = libraryGridFilterOptions(options.items || []);
  const changed = (name, value) => options.handlers?.onFilterChange?.({ ...draft, [name]: value });
  panel.append(libraryGridSelect("Status", "status", "All statuses", choices.statuses, draft.status || "", (v) => changed("status", v)));
  panel.append(libraryGridSelect("App", "app", "All apps", choices.apps, draft.app || "", (v) => changed("app", v)));
  panel.append(libraryGridSelect("Genre", "genre", "All genres", choices.genres, draft.genre || "", (v) => changed("genre", v)));
  panel.append(libraryGridSelect("Year", "year", "All years", choices.years, draft.year || "", (v) => changed("year", v)));
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
  const filters = options.filters || {};
  const handlers = options.handlers || {};
  const view = options.view === "list" ? "list" : "grid";
  const shown = libraryGridSort(libraryGridFilter(items, filters), options.sort);

  const page = libraryGridNode("div", null, "libgrid-page");
  page.append(libraryGridToolbar({
    filters,
    count: libraryGridCount(shown.length, items.length),
    view,
    filtersOpen: options.filtersOpen,
    handlers,
  }));
  if (options.filtersOpen) page.append(libraryGridFilterPanel({ items, draft: options.draft || filters, handlers }));

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
