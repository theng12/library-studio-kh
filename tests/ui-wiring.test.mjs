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

test("dialog closures return focus to the control that opened them", () => {
  assert.match(app, /const opener = trigger && trigger\.isConnected \? trigger : document\.activeElement/);
  assert.match(app, /downloadAction\(\[row\.hash\], key, locate\(\), locate\)/);
  assert.match(app, /refreshOpener\(\)\?\.focus\(\)/);
  assert.match(app, /const refreshOpener = \(\) =>/);
  // The Downloads detail lands focus on the title button again after teardown.
  assert.match(app, /const again = document\.querySelector\('#dl tr\[data-key="' \+ CSS\.escape\(key\) \+ '"\] \.dlp-open'\)/);
  assert.match(app, /dialog\.remove\(\)/);
});

test("Cleanup keeps actions visible and shows import proof", () => {
  assert.match(cleanup, /"table-wrap"/);
  assert.match(cleanup, /"page-table list"/);
  assert.match(cleanup, /"Actions"/);
  assert.match(cleanup, /cleanupProof\(item, proof\)/);
  // Escape/Close must return focus to the opener, never drop it to BODY.
  assert.match(cleanup, /addEventListener\("close", \(\) => \{[\s\S]*?opener/);
  assert.match(cleanup, /requestAnimationFrame\(restore\)/);
  assert.match(cleanup, /dataset\.cleanupRestore/);
  assert.match(cleanup, /Nothing is deleted for good/);
  assert.match(cleanup, /cleanup-effect/);
  assert.match(html, /id="cleanup-proof"/);
});

test("actions, saves and empty states stay honest", () => {
  const app = readFileSync(new URL("../internal/server/web/app.js", import.meta.url), "utf8");
  // A pause/resume that qBittorrent did not carry out is never reported as success.
  assert.match(app, /still reports " \+ \(unchanged\[0\]\.state/);
  // Saving repaints the live dead-download lines and marks the switches saved.
  assert.match(app, /loadDeadSummary\(\);[\s\S]*?const settings = window\.librarySettingsApi/);
  // A successful save clears the dirty status.
  assert.match(app, /pageStatus\.textContent = "Changes apply when you save\."/);
  // Folder fields carry the full value so the two locations are distinguishable.
  assert.match(app, /Full path: " \+ input\.value/);
  // A filtered-empty Activity says what to do and offers it.
  assert.match(app, /clearButton\.textContent = "Clear filters"/);
});

test("Activity offers Clear filters whenever a filter is active", () => {
  const app = readFileSync(new URL("../internal/server/web/app.js", import.meta.url), "utf8");
  assert.match(app, /clearButton\.id = "act-clear"/);
  assert.match(app, /q\.trim\(\) \|\| app \|\| outcome/);
});

test("Sources states a disconnect once and never fakes health", () => {
  const media = readFileSync(new URL("../internal/server/web/media.js", import.meta.url), "utf8");
  const manager = readFileSync(new URL("../internal/server/web/source-manager.js", import.meta.url), "utf8");
  assert.match(media, /Not checked — connect the source app to read its health\./);
  assert.doesNotMatch(manager, /Sources can be managed once Prowlarr is connected\./);
});

test("the remaining review findings are wired", () => {
  const app = readFileSync(new URL("../internal/server/web/app.js", import.meta.url), "utf8");
  const cleanup = readFileSync(new URL("../internal/server/web/cleanup.js", import.meta.url), "utf8");
  // Run cleanup asks first, naming the stages.
  assert.match(cleanup, /function cleanupRunSummary\(status\)/);
  assert.match(cleanup, /confirmCleanupRun\(\)/);
  assert.match(cleanup, /if \(await confirmCleanupRun\(\)\) cleanupAction\("run"\)/);
  // Unsaved changes are visible, and the dead helpers show the pending value.
  assert.match(app, /Unsaved changes — Save to apply\./);
  assert.match(app, /not applied yet; Save to apply\./);
  // Save belongs to General only.
  assert.match(app, /save\.hidden = \(ev\?\.detail\?\.id \|\| "general"\) !== "general"/);
  // Activity opens a real busy window and reports the filtered count politely.
  assert.match(app, /function activityFilterInput\(\)/);
  assert.match(app, /setAttribute\("aria-busy", "true"\)/);
  assert.match(app, /setTimeout\(\(\) => timeline\?\.setAttribute\("aria-busy", "false"\), 120\)/);
  assert.match(app, /countLine\.textContent/);
  assert.match(html, /id="act-count"[^>]*role="status"/);
});

test("a metadata provider outage is stated on both lists", () => {
  const app = readFileSync(new URL("../internal/server/web/app.js", import.meta.url), "utf8");
  assert.match(app, /lib\.metadata = r\.metadata \|\| ""/);
  assert.match(app, /lib\.metadata \? " " \+ lib\.metadata : ""/);
  assert.match(app, /dl\.metadata \? " · " \+ dl\.metadata : ""/);
});

test("Activity is filterable by app and outcome, newest first", () => {
  assert.match(app, /function activityLabel\(it\)/);
  assert.match(app, /function activityEntries\(\)/);
  assert.match(app, /return new Date\(b\.at\) - new Date\(a\.at\)/);
  assert.match(html, /id="act-app"/);
  assert.match(html, /id="act-outcome"/);
});
