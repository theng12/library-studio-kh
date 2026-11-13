import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const html = readFileSync(new URL("../internal/server/web/index.html", import.meta.url), "utf8");
const app = readFileSync(new URL("../internal/server/web/app.js", import.meta.url), "utf8");
const cleanup = readFileSync(new URL("../internal/server/web/cleanup.js", import.meta.url), "utf8");
const panel = readFileSync(new URL("../internal/server/web/downloads-panel.js", import.meta.url), "utf8");
const grid = readFileSync(new URL("../internal/server/web/library-grid.js", import.meta.url), "utf8");

test("every working page uses the shared frame", () => {
  for (const view of ["activity", "downloads", "browse", "find", "sources", "cleanup"]) {
    const section = html.slice(html.indexOf('id="view-' + view + '"'));
    const end = section.indexOf("</section>");
    assert.match(section.slice(0, end), /<div class="page">/, view + " has no page frame");
  }
  assert.match(html, /<link rel="stylesheet" href="layout\.css">\s*<link rel="stylesheet" href="pages\.css">/);
});

test("My library and Downloads mount their modules instead of the old renderers", () => {
  assert.match(app, /libraryGridPage\(\{/);
  assert.match(app, /downloadsModel\(dl\.items/);
  assert.match(app, /downloadsPanelRender\(host, model, handlers\)/);
  assert.match(app, /downloadsDetailRender\(detail, downloadsDetail/);
  assert.doesNotMatch(app, /function renderLibrary\(\)/);
  assert.doesNotMatch(app, /function renderDownloads\(\)/);
  assert.match(html, /id="library-host"/);
  assert.match(html, /id="dl" class="download-list"/);
  assert.match(html, /id="downloads-detail"/);
});

test("posted posters go through the safe route and never a raw path", () => {
  assert.match(app, /"\/api\/poster\?path=" \+ encodeURIComponent\(item\.posterPath\)/);
  assert.match(app, /encodeURIComponent\(t\.posterPath\)/);
  assert.match(panel, /data-select-all/);
  assert.match(grid, /search\.id = "lib-q"/);
  assert.match(panel, /search\.id = "dl-q"/);
});

test("Cleanup keeps actions visible and shows import proof", () => {
  assert.match(cleanup, /"table-wrap"/);
  assert.match(cleanup, /"page-table list"/);
  assert.match(cleanup, /"Actions"/);
  assert.match(cleanup, /cleanupProof\(item\)/);
  assert.match(cleanup, /dataset\.cleanupRestore/);
  assert.match(cleanup, /Nothing is deleted for good/);
  assert.match(cleanup, /cleanup-effect/);
  assert.match(html, /id="cleanup-proof"/);
});

test("Activity is filterable by app and outcome, newest first", () => {
  assert.match(app, /function activityLabel\(it\)/);
  assert.match(app, /function activityEntries\(\)/);
  assert.match(app, /return new Date\(b\.at\) - new Date\(a\.at\)/);
  assert.match(html, /id="act-app"/);
  assert.match(html, /id="act-outcome"/);
});
