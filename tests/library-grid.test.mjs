import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";

const source = readFileSync(new URL("../internal/server/web/library-grid.js", import.meta.url), "utf8");

// Minimal DOM: views only create elements, set text, attributes and listeners.
function fakeDocument() {
  const make = (tagName) => ({
    tagName, className: "", textContent: "", children: [], attributes: {}, listeners: {}, hidden: false, dataset: {},
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
  const context = { document: fakeDocument(), setTimeout, clearTimeout };
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
  assert.equal(g.libraryGridStatus({ status: "queued" }).label, "No file yet");
  assert.equal(g.libraryGridStatus({ status: "downloading" }).label, "Downloading");
  assert.equal(g.libraryGridStatus({ status: "importing" }).label, "Importing");
  assert.deepEqual({ ...g.libraryGridStatus({ status: "copied", verified: "hash" }) },
    { key: "verified", label: "Verified", tone: "ok" });
  assert.deepEqual({ ...g.libraryGridStatus({ status: "moved", verified: "size" }) },
    { key: "verified", label: "Verified", tone: "ok" });
  assert.deepEqual({ ...g.libraryGridStatus({ status: "copied" }) },
    { key: "proof-pending", label: "Needs proof", tone: "problem" });
  assert.equal(g.libraryGridStatus({ status: "proof-pending" }).label, "Needs proof");
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
  assert.deepEqual(titles(g.libraryGridFilter(items, { statuses: ["Missing"] })), ["Coco"]);
  assert.deepEqual(titles(g.libraryGridFilter(items, { apps: ["Sonarr"] })), ["Andor", "Dune"]);
  assert.deepEqual(titles(g.libraryGridFilter(items, { genres: ["sci-fi"] })), ["Arrival", "Blade Runner 2049"]);
  assert.deepEqual(titles(g.libraryGridFilter(items, { years: ["2017"] })), ["Blade Runner 2049", "Coco"]);
  assert.deepEqual(titles(g.libraryGridFilter(items, { apps: ["Radarr"], years: ["2017"] })),
    ["Blade Runner 2049", "Coco"]);
  assert.deepEqual(titles(g.libraryGridFilter(items, { query: "b", apps: ["Radarr"], years: ["2017"] })),
    ["Blade Runner 2049"]);
  // Old single-value filters still read as one-element arrays.
  assert.deepEqual(titles(g.libraryGridFilter(items, { status: "Missing" })), ["Coco"]);
  assert.deepEqual(titles(g.libraryGridFilter(items, { app: "Sonarr" })), ["Andor", "Dune"]);
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
  assert.equal(g.libraryGridActiveFilters({ statuses: ["Missing", "Downloading"], years: ["2017"] }), 3);
});

test("type tabs count the whole library, with Local counted as Movies", () => {
  const g = load();
  const localItem = { app: "Local", id: "/films/Film.mkv", title: "Film", status: "on-disk" };
  const musicItem = { app: "Lidarr", id: 6, title: "Album", status: "copied", verified: "hash" };
  const all = [...items, localItem, musicItem];
  assert.deepEqual({ ...g.libraryGridTypeCounts(all) }, { "": 7, Movies: 4, TV: 2, Music: 1 });
  assert.equal(g.libraryGridType(localItem), "Movies");
  assert.deepEqual(titles(g.libraryGridFilter(all, { type: "TV" })), ["Andor", "Dune"]);
  assert.deepEqual(titles(g.libraryGridFilter(all, { type: "Movies" })),
    ["Arrival", "Blade Runner 2049", "Coco", "Film"]);
  assert.deepEqual(titles(g.libraryGridFilter(all, { type: "Music" })), ["Album"]);
  assert.deepEqual(Array.from(g.libraryGridTypeTabs(all, "TV"), (t) => [t.value, t.count, t.selected]), [
    ["", 7, false], ["Movies", 4, false], ["TV", 2, true], ["Music", 1, false],
  ]);
});

test("filter options come only from the items on screen, with counts", () => {
  const g = load();
  const options = g.libraryGridFilterOptions(items);
  assert.deepEqual(Array.from(options.statuses, (x) => x.value), ["Downloading", "Importing", "Missing", "Verified"]);
  assert.deepEqual(Array.from(options.apps, (x) => x.value), ["Radarr", "Sonarr"]);
  assert.deepEqual(Array.from(options.genres, (x) => x.value), ["Drama", "Mystery", "Sci-Fi", "Thriller"]);
  assert.deepEqual(Array.from(options.years, (x) => x.value), ["2022", "2021", "2017", "2016"]);
  const count = (list, value) => list.find((x) => x.value === value)?.count;
  assert.equal(count(options.statuses, "Verified"), 2);
  assert.equal(count(options.statuses, "Missing"), 1);
  assert.equal(count(options.apps, "Radarr"), 3);
  assert.equal(count(options.apps, "Sonarr"), 2);
  assert.equal(count(options.genres, "Drama"), 3);
  assert.equal(count(options.years, "2017"), 2);
});

test("multi-select filters combine values and one value can be removed", () => {
  const g = load();
  // Within one field the values are OR-ed; fields are AND-ed.
  assert.deepEqual(titles(g.libraryGridFilter(items, { statuses: ["Missing", "Downloading"] })), ["Coco", "Dune"]);
  assert.deepEqual(titles(g.libraryGridFilter(items, { genres: ["sci-fi", "mystery"] })),
    ["Arrival", "Blade Runner 2049"]);
  assert.deepEqual(titles(g.libraryGridFilter(items, { apps: ["Radarr"], statuses: ["Missing", "Importing"] })),
    ["Blade Runner 2049", "Coco"]);
  // Removing a chip rebuilds the applied filters without that value.
  const calls = [];
  const page = g.libraryGridPage({
    items,
    filters: { statuses: ["Missing", "Downloading"] },
    handlers: { onRemoveFilter: (next) => calls.push(next) },
  });
  const chips = byClass(page, "libgrid-chip");
  assert.deepEqual(chips.map(text), ["Status: Missing ×", "Status: Downloading ×"]);
  const remove = chips.find((chip) => text(chip).includes("Missing"));
  byClass(remove, "libgrid-chip-remove")[0].fire("click");
  assert.deepEqual([...calls[0].statuses], ["Downloading"]);
  assert.equal(calls[0].type, "");
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
  // Unfiltered Cards view shows shelves, so every title appears as a card there.
  const andor = cards.find((card) => card.getAttribute("data-key") === "Sonarr:2");
  assert.ok(andor, "every title is a card");
  assert.deepEqual(byClass(andor, "libgrid-card-title").map(text), ["Andor"]);
  assert.deepEqual(byClass(andor, "libgrid-status").map(text), ["Verified"]);
  assert.equal(byClass(andor, "libgrid-poster-placeholder").length, 1, "no posterPath means a placeholder");
  const arrival = cards.find((card) => card.getAttribute("data-key") === "Radarr:1");
  assert.equal(byTag(arrival, "img").length, 1, "a posterPath is used without a resolver");
  assert.equal(byTag(arrival, "img")[0].src, "/cache/posters/arrival.jpg");
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
    ["Verified: SHA-256 hash match", "Status: Verified"]);
  assert.equal(arrival.history[0].at, "2026-11-12T10:05:00Z");
  const missing = g.libraryGridDetail(items[3]);
  assert.equal(missing.status.label, "Missing");
  assert.equal(missing.verification.verified, false);
  assert.match(missing.verification.method, /Not verified/);
  assert.equal(missing.location.text, "No storage location recorded yet");
  assert.equal(missing.history.length, 0);
});

test("the detail offers Set location and Open location, disabled without a path", () => {
  const g = load();
  const calls = [];
  const handlers = {
    onSetLocation: (detail) => calls.push(["set", detail.title, detail.location.path]),
    onOpenLocation: (detail) => calls.push(["open", detail.title]),
  };
  const page = g.libraryGridPage({ items, filters: {}, selected: "Radarr:1", handlers });
  const panel = byClass(page, "libgrid-detail")[0];
  const set = byClass(panel, "libgrid-set-location")[0];
  const open = byClass(panel, "libgrid-open-location")[0];
  assert.equal(set.textContent, "Set location");
  assert.equal(open.textContent, "Open location");
  assert.equal(set.type, "button");
  assert.equal(open.type, "button");
  set.fire("click");
  open.fire("click");
  assert.deepEqual(calls, [
    ["set", "Arrival", "/Volumes/NAS-1/Films/Arrival (2016)"],
    ["open", "Arrival"],
  ]);

  const noPath = g.libraryGridPage({ items, filters: {}, selected: "Radarr:4", handlers });
  const panel2 = byClass(noPath, "libgrid-detail")[0];
  const open2 = byClass(panel2, "libgrid-open-location")[0];
  assert.equal(open2.disabled, true, "Open location needs a path");
  const reason = byClass(panel2, "libgrid-location-reason")[0];
  assert.match(text(reason), /No storage location recorded yet/);
  assert.equal(open2.getAttribute("aria-describedby"), reason.id);
  assert.equal(open2.title, reason.textContent);
  byClass(panel2, "libgrid-set-location")[0].fire("click");
  assert.deepEqual(calls[2].slice(0, 2), ["set", "Coco"], "Set location is still offered");
  assert.equal(calls.length, 3, "a disabled Open location fires nothing");
});

test("the page shows the count, the selected detail and working state actions", () => {
  const g = load();
  const calls = [];
  const handlers = { onSelect: (key) => calls.push(["select", key]), onRetry: () => calls.push(["retry"]), onReset: () => calls.push(["reset"]) };
  const selected = g.libraryGridPage({ items, filters: {}, selected: "Radarr:1", handlers });
  const panel = byClass(selected, "libgrid-detail")[0];
  assert.ok(panel, "the selected item opens the detail panel");
  assert.ok(text(panel).includes("Arrival"));
  assert.ok(text(panel).includes("Verified"));
  const proof = byClass(panel, "libgrid-proof")[0];
  assert.ok(proof, "proof values live in an on-demand block");
  assert.ok(text(proof).includes("SHA-256 hash match"));
  byClass(panel, "libgrid-close")[0].fire("click");
  assert.deepEqual(calls.shift(), ["select", null]);

  const errorPage = g.libraryGridPage({ items: [], error: "Sonarr didn't answer.", selected: null, handlers });
  const state = byClass(errorPage, "libgrid-state")[0];
  assert.ok(text(state).includes("Your library could not be loaded."));
  byClass(state, "libgrid-state-action")[0].fire("click");
  assert.deepEqual(calls.shift(), ["retry"]);

  const filtered = g.libraryGridPage({ items, filters: { query: "zzz" }, selected: null, handlers });
  const filteredState = byClass(filtered, "libgrid-state")[0];
  assert.ok(text(filteredState).includes("Nothing matches your search."));
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
  byClass(page, "libgrid-card").find((card) => card.getAttribute("data-key") === "Sonarr:2").fire("click");
  assert.deepEqual(calls.shift(), ["select", "Sonarr:2"]);

  const groups = byClass(page, "libgrid-filter-group");
  assert.equal(groups.length, 4, "one checkbox group per field");
  assert.deepEqual(groups.map((group) => text(byTag(group, "legend")[0])), ["Status", "App", "Genre", "Year"]);
  const missingBox = byTag(page, "input").find((box) => box.type === "checkbox" && box.value === "Missing");
  missingBox.checked = true;
  missingBox.fire("change");
  const change = calls.shift();
  assert.equal(change[0], "change");
  assert.deepEqual([...change[1].statuses], ["Missing"]);
  assert.equal(change[1].query, "");
  byClass(page, "libgrid-apply")[0].fire("click");
  assert.deepEqual(calls.shift(), ["apply"]);
  byClass(page, "libgrid-reset")[0].fire("click");
  assert.deepEqual(calls.shift(), ["reset"]);
});

test("the Filters panel shows a draft that differs from the applied filters", () => {
  const g = load();
  const page = g.libraryGridPage({ items, filters: {}, draft: { status: "Missing" }, filtersOpen: true, handlers: {} });
  assert.equal(byClass(page, "libgrid-count").map(text)[0], "5 items", "count follows applied filters");
  const boxes = byTag(page, "input").filter((box) => box.type === "checkbox");
  assert.equal(boxes.find((box) => box.value === "Missing").checked, true, "the draft value is ticked");
  assert.equal(boxes.find((box) => box.value === "Downloading").checked, false);
  assert.equal(byClass(page, "libgrid-filters-toggle")[0].textContent, "Filters", "no applied filters yet");
  const applied = g.libraryGridPage({ items, filters: { status: "Missing" }, filtersOpen: false, handlers: {} });
  assert.equal(byClass(applied, "libgrid-filters-toggle")[0].textContent, "Filters · 1");
  assert.equal(byClass(applied, "libgrid-count").map(text)[0], "1 of 5 items");
});

test("active chips and the result count reflect the applied filters", () => {
  const g = load();
  const calls = [];
  const filters = { query: "coco", type: "Movies", statuses: ["Missing"], apps: ["Radarr"] };
  const page = g.libraryGridPage({ items, filters, handlers: { onRemoveFilter: (next) => calls.push(next) } });
  assert.equal(byClass(page, "libgrid-count").map(text)[0], "1 of 5 items");
  const labels = byClass(page, "libgrid-chip-label").map(text);
  assert.deepEqual(labels, ["Search: coco", "Type: Movies", "Status: Missing", "App: Radarr"]);
  for (const remove of byClass(page, "libgrid-chip-remove")) {
    assert.match(remove.getAttribute("aria-label"), /^Remove /);
    assert.equal(remove.type, "button");
  }
  const typeChip = byClass(page, "libgrid-chip").find((chip) => text(chip).includes("Type: Movies"));
  byClass(typeChip, "libgrid-chip-remove")[0].fire("click");
  assert.equal(calls[0].type, "");
  assert.deepEqual([...calls[0].statuses], ["Missing"], "other applied values are kept");
});

test("type tabs are buttons with counts, aria-selected and arrow-key movement", () => {
  const g = load();
  const calls = [];
  const page = g.libraryGridPage({ items, filters: { type: "Movies" }, handlers: { onType: (v) => calls.push(v) } });
  const tabs = byClass(page, "libgrid-tab");
  assert.equal(tabs.length, 4);
  assert.deepEqual(tabs.map((tab) => tab.getAttribute("role")), ["tab", "tab", "tab", "tab"]);
  assert.deepEqual(tabs.map((tab) => tab.getAttribute("aria-label")),
    ["Show all, 5 items", "Movies, 3 items", "TV, 2 items", "Music, 0 items"]);
  assert.deepEqual(tabs.map((tab) => tab.getAttribute("aria-selected")), ["false", "true", "false", "false"]);
  assert.deepEqual(tabs.map((tab) => tab.tabIndex), [-1, 0, -1, -1], "the active tab is the only stop");
  const tablist = byTag(page, "div").find((node) => node.getAttribute("role") === "tablist");
  assert.equal(tablist.getAttribute("aria-label"), "Library type");
  let prevented = 0;
  tabs[1].fire("keydown", { key: "ArrowRight", preventDefault: () => { prevented += 1; } });
  assert.equal(prevented, 1);
  assert.deepEqual(calls, ["TV"]);
  tabs[0].fire("keydown", { key: "ArrowLeft", preventDefault() {} });
  assert.deepEqual(calls, ["TV", "Music"], "arrows wrap around the row");
  tabs[2].fire("click");
  assert.deepEqual(calls, ["TV", "Music", "TV"]);
});

test("an unproven copy can be verified in place and shows the result", async () => {
  const g = load();
  const calls = [];
  const withCopy = items.map((item) => item.title === "Dune" ? { ...item, localCopy: true } : item);
  const page = g.libraryGridPage({
    items: withCopy, filters: {}, selected: "Sonarr:5",
    handlers: { onVerify: async (detail) => { calls.push(detail.title); return "Verified Dune: every saved hash matches the file on disk."; } },
  });
  const panel = byClass(page, "libgrid-detail")[0];
  const button = byClass(panel, "libgrid-state-action")[0];
  assert.equal(button.textContent, "Verify this copy");
  button.fire("click");
  await new Promise((r) => setTimeout(r, 20));
  assert.deepEqual(calls, ["Dune"]);
  assert.match(text(panel), /every saved hash matches/);
  assert.equal(g.libraryVolumeLabel("/"), "Startup disk");
  assert.match(g.libraryGridShortPath("NAS · /Volumes/NAS/Music/Demo Artist/First Album/Some Long Name", 40), /…/);
  // Without a tracked local copy no button is offered, only the reason.
  const untracked = g.libraryGridPage({ items, filters: {}, selected: "Sonarr:5", handlers: { onVerify: async () => "should not run" } });
  const panel2 = byClass(untracked, "libgrid-detail")[0];
  assert.equal(byClass(panel2, "libgrid-state-action")[0], undefined);
  assert.match(text(panel2), /No local copy is tracked — verify when a local copy is found\./);
});

test("a phone detail offers Back, and rows keep the full path on the title", () => {
  const g = load();
  const handlers = { onSelect: (key) => handlers.closed = key };
  const page = g.libraryGridPage({ items, filters: {}, view: "list", selected: "Radarr:1", handlers });
  const panel = byClass(page, "libgrid-detail")[0];
  byClass(panel, "libgrid-back")[0].fire("click");
  assert.equal(handlers.closed, null);
  const row = byClass(page, "libgrid-row")[1];
  const location = byClass(row, "libgrid-row-location")[0];
  assert.equal(location.title, "NAS-1 · /Volumes/NAS-1/Films/Arrival (2016)");
  assert.equal(location.getAttribute("aria-label"), location.title);
});

test("the module stands alone: no imports and no app.js helpers", () => {
  assert.doesNotMatch(source, /\bimport\s+[^(]|\brequire\s*\(/);
  assert.doesNotMatch(source, /app\.js|mediaNode|\$\("|libraryFilter\b|libraryGenreOptions|librarySort\b/);
  assert.match(source, /function libraryGridPage\(/);
});

test("the list row carries the full location at every width and stacks it on a phone", () => {
  const longPath = "/Volumes/NAS-1/Films/A Very Long Folder Name For An Arrival Film (2016)/Arrival (2016) 2160p.mkv";
  const longItems = items.map((item) => item.title === "Arrival" ? { ...item, targetPath: longPath } : item);
  const g = load();
  for (const width of [1440, 375]) {
    g.window = { innerWidth: width };
    const page = g.libraryGridPage({ items: longItems, filters: {}, view: "list", handlers: {} });
    const row = byClass(page, "libgrid-row").find((r) => text(r).includes("Arrival"));
    const loc = byClass(row, "libgrid-row-location")[0];
    assert.match(loc.textContent, /A Very Long Folder Name/);
    assert.doesNotMatch(loc.textContent, /…/);
    assert.equal(loc.textContent, "NAS-1 · " + longPath);
  }
  // The phone layout gives the title the width and puts the path on its own line.
  const css = readFileSync(new URL("../internal/server/web/pages.css", import.meta.url), "utf8");
  assert.match(css, /\.libgrid-row \{ grid-template-columns: 2\.5rem minmax\(0, 1fr\); row-gap: 0\.2rem; \}/);
  assert.match(css, /\.libgrid-row-location \{ grid-column: 2; grid-row: 3;/);
});

test("a saved copy offers Verify even after it is verified", async () => {
  const g = load();
  const withCopy = items.map((item) => item.title === "Arrival" ? { ...item, localCopy: true } : item);
  const page = g.libraryGridPage({
    items: withCopy, filters: {}, selected: "Radarr:1",
    handlers: { onVerify: async () => "Verified again: every saved hash matches." },
  });
  const panel = byClass(page, "libgrid-detail")[0];
  const button = byClass(panel, "libgrid-state-action")[0];
  assert.equal(button.textContent, "Verify this copy");
  button.fire("click");
  await new Promise((resolve) => setTimeout(resolve, 20));
  assert.match(text(panel), /Verified again/);
  // A verified item with no saved record has nothing to re-check.
  const noCopy = g.libraryGridPage({ items, filters: {}, selected: "Radarr:1", handlers: {} });
  assert.equal(byClass(byClass(noCopy, "libgrid-detail")[0], "libgrid-state-action")[0], undefined);
});

test("paths stay raw, offer Copy path, and a no-file title has a next step", async () => {
  const g = load();
  const copied = [];
  g.navigator = { clipboard: { writeText: async (text) => copied.push(text) } };
  const page = g.libraryGridPage({ items, filters: {}, selected: "Radarr:1", handlers: {} });
  const panel = byClass(page, "libgrid-detail")[0];
  assert.doesNotMatch(text(panel), /​/);
  const field = byTag(panel, "input")[0];
  assert.equal(field.value, "/Volumes/NAS-1/Films/Arrival (2016)");
  assert.equal(field.readOnly, true);
  const copy = byClass(panel, "libgrid-copy-path")[0];
  assert.equal(copy.textContent, "Copy path");
  copy.fire("click");
  await new Promise((resolve) => setTimeout(resolve, 20));
  assert.deepEqual(copied, ["/Volumes/NAS-1/Films/Arrival (2016)"]);
  assert.equal(byClass(panel, "libgrid-path-msg")[0].textContent, "Copied.");

  const queuedItem = { app: "Sonarr", id: 9, title: "Waiting Show", status: "queued", localCopy: false };
  let openedDownloads = 0, openedFind = 0;
  const qp = g.libraryGridPage({
    items: [queuedItem], filters: {}, selected: "Sonarr:9",
    handlers: { onOpenDownloads: () => { openedDownloads += 1; }, onFindAndAdd: () => { openedFind += 1; } },
  });
  const qPanel = byClass(qp, "libgrid-detail")[0];
  assert.match(text(qPanel), /No file yet\. Add a title, or check Downloads for its transfer\./);
  const actions = byClass(qPanel, "libgrid-state-action");
  assert.deepEqual(actions.map((b) => b.textContent), ["Open Downloads", "Find & add"]);
  actions[0].fire("click");
  actions[1].fire("click");
  assert.equal(openedDownloads, 1);
  assert.equal(openedFind, 1);

  const css = readFileSync(new URL("../internal/server/web/pages.css", import.meta.url), "utf8");
  const shared = /grid-template-columns: 2\.5rem minmax\(15rem, 1\.35fr\) minmax\(0, 1\.4fr\) 10rem;/;
  const rowRule = css.slice(css.indexOf(".libgrid-row {"), css.indexOf(".libgrid-row {") + 200);
  const headRule = css.slice(css.indexOf(".libgrid-list-head {"), css.indexOf(".libgrid-list-head {") + 200);
  assert.match(rowRule, shared);
  assert.match(headRule, shared);
  assert.doesNotMatch(css, /\.libgrid-body:has\(> \.libgrid-detail\) \.libgrid-row/);
  assert.match(css, /\.tabpanel > \.card:only-of-type \{ grid-column: 1 \/ -1; \}/);
  assert.match(css, /\.libgrid-list-head \{ display: none; \}/);
});

test("a checkable missing file names the check; an uncheckable one needs proof", () => {
  const g = load();
  const missing = {
    app: "Radarr", id: 8, title: "Gone Film", status: "missing", localCopy: false,
    targetVolume: "NAS-1", targetPath: "/Volumes/NAS-1/Films/Gone Film", verified: "hash",
    verifiedAt: "2026-11-12T10:05:00Z",
  };
  const page = g.libraryGridPage({ items: [missing], filters: {}, selected: "Radarr:8", handlers: {} });
  const panel = byClass(page, "libgrid-detail")[0];
  assert.match(text(panel), /Checked on this machine — not found/);
  assert.match(text(panel), /Not found at \/Volumes\/NAS-1\/Films\/Gone Film/);
  const actions = byClass(panel, "libgrid-state-action").map((b) => b.textContent);
  assert.deepEqual(actions, ["Open Downloads", "Find & add"]);

  const pending = { app: "Radarr", id: 9, title: "Pending Film", status: "proof-pending", localCopy: false };
  const pendingPage = g.libraryGridPage({ items: [pending], filters: {}, selected: "Radarr:9", handlers: {} });
  const pendingPanel = byClass(pendingPage, "libgrid-detail")[0];
  assert.match(text(pendingPanel), /no path could be checked/);
  assert.deepEqual(byClass(pendingPanel, "libgrid-state-action").map((b) => b.textContent), ["Open Downloads", "Find & add"]);

  // Plain copy: no raw hash in the visible facts; it sits in Proof details.
  const arrival = items[1];
  const arrivalPage = g.libraryGridPage({ items, filters: {}, selected: "Radarr:1", handlers: {} });
  const factsBlock = byClass(arrivalPage, "libgrid-detail")[0];
  const facts = byClass(factsBlock, "libgrid-facts")[0];
  assert.doesNotMatch(text(facts), new RegExp(String(arrival.proofHash || "ffffffff"), "i"));
  assert.match(text(factsBlock), /Proof details/);
});

test("torrent input validity has one reason, and the Add button follows it", () => {
  const app2 = readFileSync(new URL("../internal/server/web/app.js", import.meta.url), "utf8");
  const reason = app2.match(/function torrentInputReason\(links, count\) \{[\s\S]*?\n\}/)[0];
  const context = {};
  runInNewContext(reason, context);
  assert.equal(context.torrentInputReason("", 0), "Paste a link or choose at least one .torrent file.");
  assert.equal(context.torrentInputReason("magnet:?xt=urn:btih:x", 0), "");
  assert.equal(context.torrentInputReason("", 1), "");
});

test("a local file offers a metadata match and no import proof", () => {
  const g = load();
  const path = "/Volumes/Media/Film (2020).mkv";
  const localItem = { app: "Local", id: path, targetPath: path, title: "Film", year: 2020, status: "on-disk" };
  const calls = [];
  const page = g.libraryGridPage({
    items: [localItem], filters: {}, selected: g.libraryGridKey(localItem),
    handlers: { onMatch: (detail) => calls.push(detail) },
  });
  const panel = byClass(page, "libgrid-detail")[0];
  assert.ok(text(panel).includes("Checked on this machine — found"));
  assert.doesNotMatch(text(panel), /Needs proof/);
  assert.equal(byClass(panel, "libgrid-verify-msg").length, 0, "a local file has nothing to verify against an import");
  const match = byClass(panel, "libgrid-match")[0];
  assert.ok(match, "Match metadata is offered for a local file");
  match.fire("click");
  assert.equal(calls.length, 1);
  assert.equal(calls[0].path, path);
});

test("shelves group Continue, the most recent, and each type", () => {
  const g = load();
  const shelves = g.libraryGridShelves(items);
  assert.deepEqual(Array.from(shelves, (s) => [s.id, s.items.length, s.label]), [
    ["continue", 2, "Continue"],
    ["recent", 2, "Recently added"],
    ["movies", 3, "Movies"],
    ["tv", 2, "TV"],
  ]);
  assert.deepEqual(titles(shelves[0].items), ["Blade Runner 2049", "Dune"]);
  assert.deepEqual(titles(shelves[1].items), ["Arrival", "Andor"]);
  // Shelves overlap on purpose: an in-flight title still counts in its type.
  assert.ok(shelves[3].items.some((i) => i.title === "Dune"));
  // An empty shelf is omitted.
  assert.equal(shelves.find((s) => s.id === "music"), undefined);
  // A Local file counts as Movies.
  const withLocal = [...items, { app: "Local", id: "/f.mkv", title: "Film", status: "on-disk" }];
  assert.ok(g.libraryGridShelves(withLocal).find((s) => s.id === "movies").items.some((i) => i.title === "Film"));
});

test("Recently added ranks by the newest real time and stops at 12", () => {
  const g = load();
  const dated = Array.from({ length: 14 }, (_, i) => ({
    app: "Radarr", id: i, title: "Film " + String(i + 1).padStart(2, "0"), status: "copied", verified: "hash",
    statusAt: "2026-11-" + String(i + 1).padStart(2, "0") + "T10:00:00Z",
  }));
  const goZero = { app: "Radarr", id: 7, title: "Go Zero", status: "copied", verified: "hash", statusAt: "0001-01-01T00:00:00Z" };
  const noTime = { app: "Radarr", id: 99, title: "No Date", status: "on-disk" };
  const recent = g.libraryGridShelves([...dated, goZero, noTime]).find((s) => s.id === "recent");
  assert.equal(recent.items.length, 12, "capped at 12");
  assert.equal(recent.items[0].title, "Film 14", "newest first");
  assert.equal(recent.items[11].title, "Film 03", "twelve back");
  // Go's zero time and a missing time never outrank a real one.
  const mixed = g.libraryGridShelves([dated[0], goZero, noTime, dated[1]]).find((s) => s.id === "recent");
  assert.deepEqual(titles(mixed.items), ["Film 02", "Film 01"]);
  // With no real dates at all, the shelf is omitted rather than guessed.
  const undated = g.libraryGridShelves([goZero, noTime]);
  assert.equal(undated.find((s) => s.id === "recent"), undefined);
});

test("shelves appear only on the unfiltered Show all Cards view", () => {
  const g = load();
  assert.equal(g.libraryGridShowShelves("grid", {}), true);
  assert.equal(g.libraryGridShowShelves("list", {}), false);
  assert.equal(g.libraryGridShowShelves("grid", { type: "Movies" }), false);
  assert.equal(g.libraryGridShowShelves("grid", { query: "dune" }), false);
  assert.equal(g.libraryGridShowShelves("grid", { statuses: ["Missing"] }), false);
  assert.equal(g.libraryGridShowShelves("grid", { apps: ["Radarr"] }), false);
  assert.equal(g.libraryGridShowShelves("grid", { genres: ["Drama"] }), false);
  assert.equal(g.libraryGridShowShelves("grid", { years: ["2017"] }), false);

  const shelvesPage = g.libraryGridPage({ items, filters: {}, view: "grid", handlers: {} });
  assert.equal(byClass(shelvesPage, "libgrid-shelves").length, 1);
  assert.equal(byClass(shelvesPage, "libgrid-grid").length, 0, "no flat grid in shelf mode");
  assert.deepEqual(byClass(shelvesPage, "libgrid-shelf-title").map(text),
    ["Continue 2", "Recently added 2", "Movies 3", "TV 2"]);
  const flat = g.libraryGridPage({ items, filters: { query: "e" }, view: "grid", handlers: {} });
  assert.equal(byClass(flat, "libgrid-shelf").length, 0, "a search shows the flat list");
  assert.equal(byClass(flat, "libgrid-grid").length, 1);
  const table = g.libraryGridPage({ items, filters: {}, view: "list", handlers: {} });
  assert.equal(byClass(table, "libgrid-shelf").length, 0, "the Table view stays flat");
  assert.equal(byClass(table, "libgrid-list").length, 1);
});

test("poster frames are 2:3 with a designed placeholder, not a giant letter", () => {
  const css = readFileSync(new URL("../internal/server/web/library-ia.css", import.meta.url), "utf8");
  assert.match(css, /\.libgrid-poster \{[^}]*aspect-ratio: 2 \/ 3;/);
  assert.match(css, /\.libgrid-poster img \{[^}]*object-fit: cover;/);
  assert.match(css, /\.libgrid-grid \{[^}]*grid-auto-rows: 1fr;/);
  assert.match(css, /\.libgrid-card \.libgrid-meta \{[^}]*line-clamp: 1;/);
  assert.match(css, /\.libgrid-poster-glyph/);
  assert.match(css, /\.libgrid-poster-label/);
  assert.match(css, /\.libgrid-match-hint/);
  assert.match(css, /@media \(prefers-reduced-motion: reduce\)/);

  const g = load();
  const page = g.libraryGridPage({ items, filters: { apps: ["Radarr"] }, view: "grid", handlers: {} });
  const placeholder = byClass(page, "libgrid-poster-placeholder")[0];
  assert.deepEqual(byClass(placeholder, "libgrid-poster-glyph").map(text), ["B"], "the small initial");
  assert.deepEqual(byClass(placeholder, "libgrid-poster-label").map(text), ["No poster"]);
  const local = g.libraryGridPage({
    items: [{ app: "Local", id: "/f.mkv", title: "Film", status: "on-disk" }],
    filters: {}, view: "grid", handlers: { onSelect: (key) => { local.picked = key; } },
  });
  const hint = byClass(local, "libgrid-match-hint")[0];
  assert.ok(hint, "a local file without a poster carries the Match hint");
  assert.equal(text(hint), "Match");
  // The card itself still opens the detail, which owns the Match action.
  byClass(local, "libgrid-card")[0].fire("click");
  assert.equal(local.picked, "Local:/f.mkv");
});

test("a selection checkbox sits beside the card button, never inside it", () => {
  const g = load();
  const calls = [];
  const page = g.libraryGridPage({
    items, filters: { apps: ["Sonarr"] }, view: "grid", selection: ["Sonarr:2"],
    handlers: { onToggleSelect: (key) => calls.push(key) },
  });
  const cards = byClass(page, "libgrid-card");
  assert.equal(cards.length, 2);
  for (const card of cards) assert.equal(byTag(card, "input").length, 0, "no tick inside the button");
  const select = byClass(page, "libgrid-select")[0];
  assert.equal(select.type, "checkbox");
  assert.equal(select.getAttribute("aria-label"), "Select Andor");
  assert.equal(select.checked, true, "Sonarr:2 is ticked");
  const wrapper = byClass(page, "libgrid-item").find((node) => node.children.includes(cards[0]));
  assert.ok(wrapper.children.includes(select), "the wrapper holds button and tick");
  assert.match(wrapper.className, /is-selected-item/);
  assert.doesNotMatch(cards[0].className, /is-selected-item/, "the pick state lives on the wrapper");
  select.fire("change");
  assert.deepEqual(calls, ["Sonarr:2"], "ticking never opens the detail");

  const listPage = g.libraryGridPage({ items, filters: { apps: ["Sonarr"] }, view: "list", handlers: {} });
  for (const row of byClass(listPage, "libgrid-row")) assert.equal(byTag(row, "input").length, 0);
  assert.equal(byClass(listPage, "libgrid-select").length, 2, "rows get ticks too");
});

test("the toolbar selects all shown and the bar runs the bulk actions", () => {
  const g = load();
  const calls = [];
  const handlers = {
    onToggleSelect: (key) => calls.push(["toggle", key]),
    onSelectAll: (keys) => calls.push(["all", ...keys]),
    onClearSelection: () => calls.push(["clear"]),
    onBulkAction: (id, keys) => calls.push([id, ...keys]),
  };
  const page = g.libraryGridPage({
    items, filters: { apps: ["Sonarr"] }, view: "grid", selection: ["Sonarr:2"],
    selectionActions: [{ id: "verify", label: "Verify selected" }], handlers,
  });
  const bar = byClass(page, "libgrid-selection")[0];
  assert.ok(bar, "a live selection shows one bar");
  assert.match(text(bar), /1 selected/);
  const bulk = byClass(bar, "libgrid-bulk")[0];
  assert.equal(bulk.textContent, "Verify selected");
  assert.equal(bulk.disabled, false);
  bulk.fire("click");
  assert.deepEqual(calls.shift(), ["verify", "Sonarr:2"]);
  byClass(bar, "libgrid-selection-clear")[0].fire("click");
  assert.deepEqual(calls.shift(), ["clear"]);

  const all = byClass(page, "libgrid-select-all")[0];
  assert.equal(all.getAttribute("aria-label"), "Select all shown");
  all.checked = true;
  all.fire("change");
  assert.deepEqual(calls.shift(), ["all", "Sonarr:2", "Sonarr:5"], "every shown key");
  all.checked = false;
  all.fire("change");
  assert.deepEqual(calls.shift(), ["clear"], "unticking clears the selection");

  // Nothing selected: no bar, but the select-all stays as the way in.
  const none = g.libraryGridPage({
    items, filters: {}, selection: [],
    selectionActions: [{ id: "verify", label: "Verify selected" }], handlers,
  });
  assert.equal(byClass(none, "libgrid-selection").length, 0);
  assert.equal(byClass(none, "libgrid-select-all").length, 1);
});

test("Escape clears the selection, then closes the detail; a field keeps Escape", () => {
  const g = load();
  const calls = [];
  let prevented = 0;
  const page = g.libraryGridPage({
    items, filters: {}, selection: ["Sonarr:2"],
    handlers: { onClearSelection: () => calls.push(["clear"]), onSelect: (key) => calls.push(["select", key]) },
  });
  page.fire("keydown", { key: "Escape", target: { tagName: "DIV" }, preventDefault: () => { prevented += 1; } });
  assert.deepEqual(calls, [["clear"]], "the selection goes first");
  assert.equal(prevented, 1, "the host's own Escape branch is skipped");

  // A focused field keeps its own Escape (the host clears a search box).
  page.fire("keydown", { key: "Escape", target: { tagName: "INPUT" }, preventDefault: () => { prevented += 1; } });
  assert.equal(calls.length, 1);

  // An open dialog is never touched from the library page.
  g.document.querySelector = () => ({});
  page.fire("keydown", { key: "Escape", target: { tagName: "DIV" }, preventDefault: () => { prevented += 1; } });
  assert.equal(calls.length, 1);
  g.document.querySelector = undefined;

  // With nothing selected, Escape closes the detail.
  const detail = g.libraryGridPage({
    items, filters: {}, selected: "Radarr:1", selection: [],
    handlers: { onSelect: (key) => calls.push(["select", key]) },
  });
  detail.fire("keydown", { key: "Escape", target: { tagName: "DIV" }, preventDefault: () => { prevented += 1; } });
  assert.deepEqual(calls.at(-1), ["select", null]);
  assert.equal(prevented, 2, "only the two handled Escapes were consumed");
});

test("type-ahead focuses the first card whose title starts with the letters", () => {
  const g = load();
  assert.equal(g.libraryGridTypeAhead(items, "bl"), 2);
  assert.equal(g.libraryGridTypeAhead(items, "BL"), 2, "case-insensitive");
  assert.equal(g.libraryGridTypeAhead(items, ""), -1);
  assert.equal(g.libraryGridTypeAhead(items, "zz"), -1);

  const page = g.libraryGridPage({ items, filters: {}, view: "grid", handlers: {} });
  let focused = null;
  for (const card of byClass(page, "libgrid-card")) card.focus = () => { focused = card; };
  page.fire("keydown", { key: "b", target: { tagName: "DIV" } });
  assert.equal(focused?.getAttribute("data-key"), "Radarr:3", "Blade Runner 2049");
  focused = null;
  page.fire("keydown", { key: "a", target: { tagName: "INPUT" } });
  assert.equal(focused, null, "a field keeps typing to itself");
  page.fire("keydown", { key: "b", metaKey: true, target: { tagName: "DIV" } });
  assert.equal(focused, null, "a shortcut never types ahead");
});

test("the grid paints the first 60 cards and Show more appends the next page", () => {
  const g = load();
  const many = Array.from({ length: 130 }, (_, i) => ({
    app: "Radarr", id: i, title: "Film " + String(i).padStart(3, "0"), status: "copied", verified: "hash",
  }));
  const first = g.libraryGridBatch(many, 60);
  assert.equal(first.items.length, 60);
  assert.equal(first.rest.length, 70);
  assert.equal(first.hasMore, true);
  const all = g.libraryGridBatch(many, 200);
  assert.equal(all.items.length, 130);
  assert.equal(all.hasMore, false);

  // An active filter keeps the flat grid, which batches.
  const page = g.libraryGridPage({ items: many, filters: { apps: ["Radarr"] }, view: "grid", handlers: {} });
  assert.equal(byClass(page, "libgrid-card").length, 60, "first paint stays small");
  const more = byClass(page, "libgrid-show-more")[0];
  assert.ok(more, "the rest is one button away");
  assert.equal(more.textContent, "Show more");
  more.fire("click");
  assert.equal(byClass(page, "libgrid-card").length, 120);
  byClass(page, "libgrid-show-more")[0].fire("click");
  assert.equal(byClass(page, "libgrid-card").length, 130);
  assert.equal(byClass(page, "libgrid-show-more")[0].hidden, true, "nothing left to show");
  const shown = byClass(page, "libgrid-card").map((card) => byClass(card, "libgrid-card-title")[0].textContent);
  assert.deepEqual(shown.slice(0, 2), ["Film 000", "Film 001"], "sorting order is kept");
  assert.equal(shown[129], "Film 129");
});
