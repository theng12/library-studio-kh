import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";

const source = readFileSync(new URL("../internal/server/web/library-grid.js", import.meta.url), "utf8");

// Minimal DOM: views only create elements, set text, attributes and listeners.
function fakeDocument() {
  const make = (tagName) => ({
    tagName, className: "", textContent: "", children: [], attributes: {}, listeners: {}, hidden: false,
    append(...kids) { this.children.push(...kids); },
    replaceChildren(...kids) { this.children = kids; },
    setAttribute(k, v) { this.attributes[k] = String(v); },
    getAttribute(k) { return this.attributes[k] ?? null; },
    addEventListener(type, fn) { (this.listeners[type] ||= []).push(fn); },
    fire(type, event = {}) { for (const fn of this.listeners[type] || []) fn(event); },
  });
  return { createElement: make };
}

function load() {
  const context = { document: fakeDocument() };
  runInNewContext(source, context);
  return context;
}

function walk(node, out = []) {
  if (!node || typeof node === "string") return out;
  out.push(node);
  for (const child of node.children) walk(child, out);
  return out;
}
const byClass = (root, cls) => walk(root).filter((n) => (n.className || "").split(/\s+/).includes(cls));
const byTag = (root, tag) => walk(root).filter((n) => n.tagName === tag);
function text(node) {
  if (node == null) return "";
  if (typeof node === "string") return node;
  return [node.textContent, ...node.children.map(text)].join(" ").trim();
}

// Realistic snapshot items: a verified hash copy, a verified move, an import in
// flight, a missing file, an active download, and an item with no metadata.
const items = [
  { app: "Sonarr", id: 2, title: "Andor", year: 2022, genres: ["Drama"], runtimeMinutes: 45, rating: 8.4,
    status: "moved", targetVolume: "NAS-1", targetPath: "/Volumes/NAS-1/Series/Andor",
    verified: "size", verifiedAt: "2026-11-11T09:00:00Z", when: "2026-11-11T09:30:00Z" },
  { app: "Radarr", id: 1, title: "Arrival", year: 2016, genres: ["Drama", "Sci-Fi"], runtimeMinutes: 116, rating: 7.9,
    status: "copied", statusAt: "2026-11-12T10:00:00Z", targetVolume: "NAS-1",
    targetPath: "/Volumes/NAS-1/Films/Arrival (2016)", verified: "hash", verifiedAt: "2026-11-12T10:05:00Z",
    posterPath: "/cache/posters/arrival.jpg", overview: "A linguist meets visitors.", sourcePath: "/downloads/Arrival" },
  { app: "Radarr", id: 3, title: "Blade Runner 2049", year: 2017, genres: ["Drama", "Sci-Fi", "Thriller", "Mystery"],
    status: "importing", posterPath: "" },
  { app: "Radarr", id: 4, title: "Coco", year: 2017, status: "missing" },
  { app: "Sonarr", id: 5, title: "Dune", year: 2021, status: "downloading", progress: 0.4 },
];
const titles = (rows) => Array.from(rows, (x) => x.title);

test("status chips follow the pipeline and never say verified without proof", () => {
  const g = load();
  assert.equal(g.libraryGridStatus({ status: "queued" }).label, "Queued");
  assert.equal(g.libraryGridStatus({ status: "downloading" }).label, "Downloading");
  assert.equal(g.libraryGridStatus({ status: "importing" }).label, "Importing");
  assert.deepEqual({ ...g.libraryGridStatus({ status: "copied", verified: "hash" }) },
    { key: "copied", label: "Copied to NAS (verified)", tone: "ok" });
  assert.deepEqual({ ...g.libraryGridStatus({ status: "moved", verified: "size" }) },
    { key: "moved", label: "Moved to NAS (verified)", tone: "ok" });
  assert.deepEqual({ ...g.libraryGridStatus({ status: "copied" }) },
    { key: "copied", label: "Copied to NAS (not verified)", tone: "problem" });
  assert.deepEqual({ ...g.libraryGridStatus({ status: "missing" }) },
    { key: "missing", label: "Missing", tone: "problem" });
  assert.equal(g.libraryGridStatus({ status: "copied", verified: "none" }).tone, "problem");
  assert.equal(g.libraryGridStatus({ status: "stalled" }).tone, "problem");
  assert.equal(g.libraryGridStatus({}).label, "Status not reported");
});

test("the metadata line is compact and honest when facts are missing", () => {
  const g = load();
  assert.equal(g.libraryGridMeta({ year: 2016, runtimeMinutes: 116, genres: ["Drama", "Sci-Fi"], rating: 7.9 }),
    "2016 · 1h 56m · Drama, Sci-Fi · 7.9/10");
  assert.equal(g.libraryGridMeta({ year: 2021, runtimeMinutes: 45 }), "2021 · 45 min");
  assert.equal(g.libraryGridMeta({ runtimeMinutes: 120 }), "2h");
  assert.equal(g.libraryGridMeta({ genres: ["A", "B", "C", "D"] }), "A, B, C");
  assert.equal(g.libraryGridMeta({}), "Metadata not reported");
  assert.equal(g.libraryGridRuntime(0), "");
  assert.equal(g.libraryGridRating(8), "8/10");
});

test("a poster placeholder appears only when posterPath is empty", () => {
  const g = load();
  assert.deepEqual({ ...g.libraryGridPoster({ title: "Dune" }) }, { src: "", placeholder: true, label: "D" });
  assert.deepEqual({ ...g.libraryGridPoster({ title: "Dune", posterPath: "/p.jpg" }) },
    { src: "/p.jpg", placeholder: false, label: "D" });
  assert.equal(g.libraryGridPoster({ title: "Dune", posterPath: "/p.jpg" },
    (item) => "/api/poster?path=" + encodeURIComponent(item.posterPath)).src, "/api/poster?path=%2Fp.jpg");
  assert.equal(g.libraryGridPoster({}).label, "?");
});

test("filters narrow the same list and never reorder or change the source", () => {
  const g = load();
  const before = items.map((x) => x.title);
  assert.deepEqual(titles(g.libraryGridFilter(items, {})), ["Andor", "Arrival", "Blade Runner 2049", "Coco", "Dune"]);
  assert.deepEqual(titles(g.libraryGridFilter(items, { query: "dune" })), ["Dune"]);
  assert.deepEqual(titles(g.libraryGridFilter(items, { status: "missing" })), ["Coco"]);
  assert.deepEqual(titles(g.libraryGridFilter(items, { app: "Sonarr" })), ["Andor", "Dune"]);
  assert.deepEqual(titles(g.libraryGridFilter(items, { genre: "sci-fi" })), ["Arrival", "Blade Runner 2049"]);
  assert.deepEqual(titles(g.libraryGridFilter(items, { year: "2017" })), ["Blade Runner 2049", "Coco"]);
  assert.deepEqual(titles(g.libraryGridFilter(items, { app: "Radarr", year: "2017" })),
    ["Blade Runner 2049", "Coco"]);
  assert.deepEqual(titles(g.libraryGridFilter(items, { query: "b", app: "Radarr", year: "2017" })),
    ["Blade Runner 2049"]);
  assert.deepEqual(items.map((x) => x.title), before);
  assert.deepEqual(titles(g.libraryGridSort([{ title: "Zed" }, { title: "abe" }, { title: "10" }, { title: "9" }])),
    ["9", "10", "abe", "Zed"]);
});

test("result count and active filters read truthfully", () => {
  const g = load();
  assert.equal(g.libraryGridCount(5, 5), "5 items");
  assert.equal(g.libraryGridCount(1, 1), "1 item");
  assert.equal(g.libraryGridCount(1, 5), "1 of 5 items");
  assert.equal(g.libraryGridCount(0, 5), "0 of 5 items");
  assert.equal(g.libraryGridActiveFilters({}), 0);
  assert.equal(g.libraryGridActiveFilters({ query: "dune" }), 0);
  assert.equal(g.libraryGridActiveFilters({ status: "missing", year: "2017" }), 2);
});

test("filter options come only from the items on screen", () => {
  const g = load();
  const options = g.libraryGridFilterOptions(items);
  assert.deepEqual(Array.from(options.statuses, (x) => x.value), ["copied", "downloading", "importing", "missing", "moved"]);
  assert.deepEqual(Array.from(options.apps, (x) => x.value), ["Radarr", "Sonarr"]);
  assert.deepEqual(Array.from(options.genres, (x) => x.value), ["Drama", "Mystery", "Sci-Fi", "Thriller"]);
  assert.deepEqual(Array.from(options.years, (x) => x.value), ["2022", "2021", "2017", "2016"]);
});

test("loading, empty, error and filtered states each name the next step", () => {
  const g = load();
  assert.deepEqual({ ...g.libraryGridEmpty({ loading: true }) }, {
    kind: "loading", title: "Loading your library…",
    message: "Posters, status and storage location appear as soon as each source answers.", action: "",
  });
  const error = g.libraryGridEmpty({ error: "Sonarr didn't answer." });
  assert.equal(error.kind, "error");
  assert.equal(error.message, "Sonarr didn't answer.");
  assert.equal(error.action, "Try again");
  const empty = g.libraryGridEmpty({ items: [] });
  assert.equal(empty.kind, "empty");
  assert.equal(empty.action, "Find & add");
  assert.match(empty.message, /Find & add/);
  const filtered = g.libraryGridEmpty({ items, shown: 0 });
  assert.equal(filtered.kind, "filtered");
  assert.equal(filtered.action, "Reset filters");
  assert.equal(g.libraryGridEmpty({ items, shown: 5 }), null);
  assert.equal(g.libraryGridEmpty({ items, shown: 5, loading: true }), null, "a refresh keeps the current list");
});

test("the grid renders one card per item with poster, title, chip and meta", () => {
  const g = load();
  const page = g.libraryGridPage({ items, filters: {}, view: "grid", selected: null, handlers: {} });
  const cards = byClass(page, "libgrid-card");
  assert.equal(cards.length, 5);
  assert.equal(cards[0].getAttribute("data-key"), "Sonarr:2");
  assert.deepEqual(byClass(cards[0], "libgrid-card-title").map(text), ["Andor"]);
  assert.deepEqual(byClass(cards[0], "libgrid-status").map(text), ["Moved to NAS (verified)"]);
  assert.equal(byClass(cards[0], "libgrid-poster-placeholder").length, 1, "no posterPath means a placeholder");
  assert.equal(byTag(cards[1], "img").length, 1, "a posterPath is used without a resolver");
  assert.equal(byTag(cards[1], "img")[0].src, "/cache/posters/arrival.jpg");
  assert.ok(byClass(page, "libgrid-count").map(text)[0].includes("5 items"));
  assert.deepEqual(byClass(page, "libgrid-view").map((b) => b.getAttribute("aria-pressed")), ["true", "false"]);
  assert.equal(byClass(page, "libgrid-detail").length, 0, "no detail panel without a selection");
});

test("the list renders compact rows with the storage location", () => {
  const g = load();
  const page = g.libraryGridPage({ items, filters: {}, view: "list", selected: null, handlers: {} });
  const rows = byClass(page, "libgrid-row");
  assert.equal(rows.length, 5);
  assert.deepEqual(byClass(rows[1], "libgrid-row-location").map(text), ["NAS-1 · /Volumes/NAS-1/Films/Arrival (2016)"]);
  assert.deepEqual(byClass(rows[3], "libgrid-row-location").map(text), ["No storage location recorded yet"]);
});

test("detail model carries proof, location and history newest first", () => {
  const g = load();
  const arrival = g.libraryGridDetail(items[1]);
  assert.equal(arrival.key, "Radarr:1");
  assert.equal(arrival.year, "2016");
  assert.equal(arrival.runtime, "1h 56m");
  assert.equal(arrival.rating, "7.9/10");
  assert.deepEqual([...arrival.genres], ["Drama", "Sci-Fi"]);
  assert.equal(arrival.location.volume, "NAS-1");
  assert.equal(arrival.location.path, "/Volumes/NAS-1/Films/Arrival (2016)");
  assert.equal(arrival.verification.verified, true);
  assert.equal(arrival.verification.method, "SHA-256 hash match");
  assert.ok(arrival.verification.at, "the verification time is shown");
  assert.deepEqual(Array.from(arrival.history, (entry) => entry.label),
    ["Verified: SHA-256 hash match", "Status: Copied to NAS (verified)"]);
  assert.equal(arrival.history[0].at, "2026-11-12T10:05:00Z");
  const missing = g.libraryGridDetail(items[3]);
  assert.equal(missing.status.label, "Missing");
  assert.equal(missing.verification.verified, false);
  assert.match(missing.verification.method, /Not verified/);
  assert.equal(missing.location.text, "No storage location recorded yet");
  assert.equal(missing.history.length, 0);
});

test("the page shows the count, the selected detail and working state actions", () => {
  const g = load();
  const calls = [];
  const handlers = { onSelect: (key) => calls.push(["select", key]), onRetry: () => calls.push(["retry"]), onReset: () => calls.push(["reset"]) };
  const selected = g.libraryGridPage({ items, filters: {}, selected: "Radarr:1", handlers });
  const panel = byClass(selected, "libgrid-detail")[0];
  assert.ok(panel, "the selected item opens the detail panel");
  assert.ok(text(panel).includes("Arrival"));
  assert.ok(text(panel).includes("SHA-256 hash match"));
  byClass(panel, "libgrid-close")[0].fire("click");
  assert.deepEqual(calls.shift(), ["select", null]);

  const errorPage = g.libraryGridPage({ items: [], error: "Sonarr didn't answer.", selected: null, handlers });
  const state = byClass(errorPage, "libgrid-state")[0];
  assert.ok(text(state).includes("Your library could not be loaded."));
  byClass(state, "libgrid-state-action")[0].fire("click");
  assert.deepEqual(calls.shift(), ["retry"]);

  const filtered = g.libraryGridPage({ items, filters: { query: "zzz" }, selected: null, handlers });
  const filteredState = byClass(filtered, "libgrid-state")[0];
  assert.ok(text(filteredState).includes("Nothing matches these filters."));
  byClass(filteredState, "libgrid-state-action")[0].fire("click");
  assert.deepEqual(calls.shift(), ["reset"]);
});

test("search, view, selection and the Filters panel call their handlers", () => {
  const g = load();
  const calls = [];
  const handlers = {
    onSearch: (v) => calls.push(["search", v]),
    onView: (v) => calls.push(["view", v]),
    onSelect: (k) => calls.push(["select", k]),
    onToggleFilters: () => calls.push(["toggle"]),
    onFilterChange: (next) => calls.push(["change", { ...next }]),
    onApply: () => calls.push(["apply"]),
    onReset: () => calls.push(["reset"]),
  };
  const page = g.libraryGridPage({ items, filters: {}, draft: { status: "" }, filtersOpen: true, handlers });
  byClass(page, "libgrid-search")[0].value = "dune";
  byClass(page, "libgrid-search")[0].fire("input");
  assert.deepEqual(calls.shift(), ["search", "dune"]);
  byClass(page, "libgrid-filters-toggle")[0].fire("click");
  assert.deepEqual(calls.shift(), ["toggle"]);
  byClass(page, "libgrid-view")[1].fire("click");
  assert.deepEqual(calls.shift(), ["view", "list"]);
  byClass(page, "libgrid-card")[0].fire("click");
  assert.deepEqual(calls.shift(), ["select", "Sonarr:2"]);

  const selects = byTag(page, "select");
  assert.equal(selects.length, 4);
  const statusSelect = selects[0];
  statusSelect.value = "missing";
  statusSelect.fire("change");
  assert.deepEqual(calls.shift(), ["change", { status: "missing" }]);
  byClass(page, "libgrid-apply")[0].fire("click");
  assert.deepEqual(calls.shift(), ["apply"]);
  byClass(page, "libgrid-reset")[0].fire("click");
  assert.deepEqual(calls.shift(), ["reset"]);
});

test("the Filters panel shows a draft that differs from the applied filters", () => {
  const g = load();
  const page = g.libraryGridPage({ items, filters: {}, draft: { status: "missing" }, filtersOpen: true, handlers: {} });
  assert.equal(byClass(page, "libgrid-count").map(text)[0], "5 items", "count follows applied filters");
  assert.equal(byTag(page, "select")[0].value, "missing");
  assert.equal(byClass(page, "libgrid-filters-toggle")[0].textContent, "Filters", "no applied filters yet");
  const applied = g.libraryGridPage({ items, filters: { status: "missing" }, filtersOpen: false, handlers: {} });
  assert.equal(byClass(applied, "libgrid-filters-toggle")[0].textContent, "Filters · 1");
  assert.equal(byClass(applied, "libgrid-count").map(text)[0], "1 of 5 items");
});

test("the module stands alone: no imports and no app.js helpers", () => {
  assert.doesNotMatch(source, /\bimport\s+[^(]|\brequire\s*\(/);
  assert.doesNotMatch(source, /app\.js|mediaNode|\$\("|libraryFilter\b|libraryGenreOptions|librarySort\b/);
  assert.match(source, /function libraryGridPage\(/);
});
