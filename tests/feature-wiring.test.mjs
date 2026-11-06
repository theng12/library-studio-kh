import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import assert from "node:assert/strict";
import test from "node:test";

const cleanup = readFileSync(new URL("../internal/server/web/cleanup.js", import.meta.url), "utf8");
const media = readFileSync(new URL("../internal/server/web/media.js", import.meta.url), "utf8");
const limits = cleanup.match(/const CLEANUP_NUMBERS = \{[\s\S]*?\n\};/)[0];
const policy = cleanup.match(/function cleanupPolicy\(form\) \{[\s\S]*?\n\}/)[0];
const run = cleanup.match(/function cleanupCanRun\(status\) \{[\s\S]*?\n\}/)[0];
function form(overrides = {}) {
  const values = { stagedDays: "3", managedTrashDays: "4", nativeTrashDays: "30", pressureFreeGB: "50", seedRatio: "2", seedMinutes: "2880", managedTrashPath: "", ...overrides };
  const elements = { stagedCleanupOn: { checked: false } };
  for (const [key, value] of Object.entries(values)) elements[key] = { value, focus() {}, parentElement: { textContent: key } };
  return { elements };
}

test("policy saves only cleanup fields and keeps the deliberate switch off", () => {
  const context = {};
  runInNewContext(limits + "\n" + policy, context);
  const result = context.cleanupPolicy(form());
  assert.equal(result.stagedCleanupOn, false);
  assert.equal(result.stagedDays + result.managedTrashDays, 7);
  assert.equal(result.nativeTrashDays, 30);
  assert.equal(result.pressureFreeGB, 50);
  assert.equal("qbittorrent" in result, false);
  assert.equal("tailscaleOn" in result, false);
  assert.equal("cleanupOn" in result, false);
});
test("policy supports disabling space pressure and fractional seed ratios but rejects invalid periods", () => {
  const context = {};
  runInNewContext(limits + "\n" + policy, context);
  assert.equal(context.cleanupPolicy(form({ pressureFreeGB: "0", seedRatio: "1.5" })).seedRatio, 1.5);
  for (const override of [{ stagedDays: "" }, { stagedDays: "0" }, { managedTrashDays: "1.5" }, { nativeTrashDays: "3651" }, { pressureFreeGB: "-1" }, { seedMinutes: "Infinity" }]) {
    assert.throws(() => context.cleanupPolicy(form(override)));
  }
});
test("Run stays unavailable while off or when native capability is blocked", () => {
  const context = {};
  runInNewContext(run, context);
  assert.equal(context.cleanupCanRun(null), false);
  assert.equal(context.cleanupCanRun({ enabled: false, engineAvailable: true }), false);
  assert.equal(context.cleanupCanRun({ enabled: true, engineAvailable: false }), false);
  assert.equal(context.cleanupCanRun({ enabled: true, engineAvailable: true, engineBlocked: ["Wrong volume"] }), false);
  assert.equal(context.cleanupCanRun({ enabled: true, engineAvailable: true, engineBlocked: [] }), true);
});
test("collection lookup selects the exact Radarr TMDb title rather than a similarly named result", () => {
  const context = {};
  runInNewContext(media.match(/function collectionLookup\(items, tmdbId\) \{[\s\S]*?\n\}/)[0], context);
  const exact = { app: "Radarr", raw: { tmdbId: 22 } };
  const items = [{ app: "Sonarr", raw: { tmdbId: 22 } }, { app: "Radarr", raw: { tmdbId: 23 } }, exact];
  assert.equal(context.collectionLookup(items, 22), exact);
  assert.equal(context.collectionLookup(items, 24), undefined);
});
test("collection action performs lookup and opens confirmation without an add request", async () => {
  const requests = [], opened = [];
  const movie = { app: "Radarr", raw: { tmdbId: 22 }, title: "Example sequel" };
  const context = { globalThis: {}, api: async (url) => { requests.push(url); return { items: [movie] }; }, openAdd: async (item) => opened.push(item) };
  const lookup = media.match(/function collectionLookup\(items, tmdbId\) \{[\s\S]*?\n\}/)[0];
  const action = media.match(/globalThis.movieExtrasFindAndAdd = async \(member\) => \{[\s\S]*?\n\};/)[0];
  runInNewContext(lookup + "\n" + action, context);
  await context.globalThis.movieExtrasFindAndAdd({ tmdbId: 22 });
  assert.deepEqual(requests, ["/api/find?q=tmdb%3A22"]);
  assert.deepEqual(opened, [movie]);
  movie.inLibrary = true;
  await assert.rejects(context.globalThis.movieExtrasFindAndAdd({ tmdbId: 22 }), /already in Radarr/);
  assert.equal(opened.length, 1);
});
