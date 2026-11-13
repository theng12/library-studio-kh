import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import assert from "node:assert/strict";
import test from "node:test";

const source = readFileSync(new URL("../internal/server/web/library-view.js", import.meta.url), "utf8");
const context = {};
runInNewContext(source, context);
const { libraryGenres, libraryYears, libraryGenreOptions, libraryYearOptions, libraryFilter, librarySort } = context;
const NOT_REPORTED = "__not_reported";
const titles = rows => Array.from(rows, x => x.title);

const library = [
  { app: "Radarr", title: "Alpha", year: 2001, genres: ["Drama", "Mystery"], originalLanguage: "English", added: "2024-01-02T00:00:00Z" },
  { app: "Sonarr", title: "Beta", year: 2010, genres: ["drama"], originalLanguage: "French", added: "2024-03-02T00:00:00Z" },
  { app: "Lidarr", title: "Gamma", year: 0, genres: [], originalLanguage: "", added: "" },
  { app: "Radarr", title: "Delta", year: 1999, originalLanguage: "English", added: "2023-01-02T00:00:00Z" },
];

test("genre and year options come only from reported library metadata", () => {
  assert.deepEqual([...libraryGenres(library)], ["Drama", "Mystery"]);
  assert.deepEqual([...libraryYears(library)], [2010, 2001, 1999]);
  assert.deepEqual([...libraryGenreOptions(library)].map(x => ({ ...x })), [
    { value: "", label: "All genres" },
    { value: "Drama", label: "Drama" },
    { value: "Mystery", label: "Mystery" },
    { value: NOT_REPORTED, label: "Not reported" },
  ]);
  assert.deepEqual([...libraryYearOptions(library)].map(x => x.value), ["", "2010", "2001", "1999", NOT_REPORTED]);
  assert.deepEqual([...libraryGenreOptions([])].map(x => ({ ...x })), [{ value: "", label: "All genres" }, { value: NOT_REPORTED, label: "Not reported" }]);
  assert.deepEqual([...libraryYears([{ year: NaN }, { year: -5 }, { year: "2012" }])], [2012]);
});

test("all filters combine and unknown metadata stays distinguishable from reported values", () => {
  assert.deepEqual(titles(libraryFilter(library, { genre: "drama" })), ["Alpha", "Beta"]);
  assert.deepEqual(titles(libraryFilter(library, { genre: NOT_REPORTED })), ["Gamma", "Delta"]);
  assert.deepEqual(titles(libraryFilter(library, { year: "2001" })), ["Alpha"]);
  assert.deepEqual(titles(libraryFilter(library, { year: NOT_REPORTED })), ["Gamma"]);
  assert.deepEqual(titles(libraryFilter(library, { query: " a ", type: "Radarr", language: "english", genre: "Drama", year: "2001" })), ["Alpha"]);
  assert.deepEqual(titles(libraryFilter(library, { query: "Alpha", genre: "Mystery", year: "2010" })), []);
});

test("filtering and sorting never reorder or change the source array", () => {
  const before = library.map(x => x.title);
  libraryFilter(library, { genre: "Drama" });
  librarySort(library, "year");
  librarySort(library, "oldest");
  assert.deepEqual(library.map(x => x.title), before);
});

test("newest and oldest year place unknown years last with a deterministic title tie", () => {
  assert.deepEqual(titles(librarySort(library, "year")), ["Beta", "Alpha", "Delta", "Gamma"]);
  assert.deepEqual(titles(librarySort(library, "oldest")), ["Delta", "Alpha", "Beta", "Gamma"]);
  const tied = [{ title: "Zed", year: 2000 }, { title: "Abe", year: 2000 }, { title: "Unknown", year: 0 }];
  for (const sort of ["year", "oldest"]) assert.deepEqual(titles(librarySort(tied, sort)), ["Abe", "Zed", "Unknown"]);
});

test("app and recently added sorting keep unknown dates last", () => {
  assert.deepEqual(titles(librarySort(library, "app")), ["Gamma", "Alpha", "Delta", "Beta"]);
  assert.deepEqual(titles(librarySort(library, "added")), ["Beta", "Alpha", "Delta", "Gamma"]);
});

test("My library is wired to the poster grid page, not the old row list", () => {
  const html = readFileSync(new URL("../internal/server/web/index.html", import.meta.url), "utf8");
  const app = readFileSync(new URL("../internal/server/web/app.js", import.meta.url), "utf8");
  assert.match(html, /id="library-host"/);
  assert.match(html, /<script src="library-grid\.js" defer><\/script>/);
  assert.ok(html.indexOf("library-grid.js") < html.indexOf('<script src="app.js" defer>'), "grid module loads before app.js");
  assert.match(app, /libraryGridPage\(\{/);
  assert.match(app, /posterURL: \(item\) => item\.posterPath \? "\/api\/poster\?path="/);
  assert.match(app, /onApply: \(\) => \{ lib\.filters = \{ \.\.\.lib\.filters, \.\.\.lib\.draft \}/);
  assert.doesNotMatch(app, /function renderLibrary\(\)/);
});

function appSource() {
  return readFileSync(new URL("../internal/server/web/app.js", import.meta.url), "utf8");
}

test("monitoring filters distinguish true, false and unreported native states", () => {
  const rows = [
    { title: "On", app: "Sonarr", monitored: true, genres: ["Drama"], year: 2020 },
    { title: "Off", app: "Sonarr", monitored: false, genres: ["Drama"], year: 2020 },
    { title: "Unknown", app: "Sonarr", monitored: null },
    { title: "Missing", app: "Radarr" },
  ];
  assert.deepEqual(titles(libraryFilter(rows, { monitoring: "on" })), ["On"]);
  assert.deepEqual(titles(libraryFilter(rows, { monitoring: "off" })), ["Off"]);
  assert.deepEqual(titles(libraryFilter(rows, { monitoring: "unknown" })), ["Unknown", "Missing"]);
  assert.deepEqual(titles(libraryFilter(rows, { monitoring: "off", type: "Sonarr", genre: "Drama", year: "2020" })), ["Off"]);
  assert.equal(context.libraryMonitoringLabel(rows[0]), "Monitoring on");
  assert.equal(context.libraryMonitoringLabel(rows[1]), "Monitoring off");
  for (const monitored of [null, undefined, "false", 0]) assert.equal(context.libraryMonitoringLabel({ monitored }), "Monitoring not reported");
});
