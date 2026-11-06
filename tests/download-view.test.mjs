import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import assert from "node:assert/strict";
import test from "node:test";

const context = {};
runInNewContext(readFileSync(new URL("../internal/server/web/download-view.js", import.meta.url), "utf8"), context);
const { downloadMatches, downloadRows } = context;
const names = rows => Array.from(rows, x => x.name);

test("qBittorrent states remain distinguishable, including completed paused torrents", () => {
  for (const state of ["pausedDL", "stoppedDL", "pausedUP", "stoppedUP"]) assert.equal(downloadMatches({ state }, "paused"), true);
  for (const state of ["queuedDL", "queuedUP"]) assert.equal(downloadMatches({ state }, "queued"), true);
  for (const state of ["uploading", "forcedUP", "stalledUP"]) assert.equal(downloadMatches({ state }, "seeding"), true);
  assert.equal(downloadMatches({ state: "queuedDL" }, "downloading"), false);
  assert.equal(downloadMatches({ state: "stalledDL" }, "problems"), false);
  assert.equal(downloadMatches({ state: "missingFiles" }, "problems"), true);
  assert.equal(downloadMatches({ state: "stoppedUP", progress: 1 }, "completed"), true);
  assert.equal(downloadMatches({ state: "unknown" }, "completed"), false);
});

test("queue position uses reported positive priorities; missing and unqueued stay last", () => {
  const items = [{ name: "Unknown" }, { name: "Third", priority: 3 }, { name: "Finished", priority: 0 }, { name: "First", priority: 1 }];
  assert.deepEqual(names(downloadRows(items, "", "all", "queue")), ["First", "Third", "Finished", "Unknown"]);
  assert.deepEqual(items.map(x => x.name), ["Unknown", "Third", "Finished", "First"]);
});

test("title, status and owning app filters combine without guessing by title", () => {
  const items = [{ name: " Walking Dead ", state: "queuedDL", app: "Sonarr" }, { name: "walking film", state: "downloading", app: "Radarr" }, { name: "Walking direct", state: "queuedDL", app: "Unmatched" }];
  assert.deepEqual(names(downloadRows(items, "  WALKING ", "queued", "name", "Sonarr")), [" Walking Dead "]);
  assert.deepEqual(names(downloadRows(items, "absent", "all", "name")), []);
});

test("numeric sorting is descending with unknowns last, including unknown size", () => {
  const items = [{ name: "Unknown" }, { name: "Zero", size: 0, progress: 0, dlspeed: 0 }, { name: "Large", size: 100, progress: 0.7, dlspeed: 25, added_on: 20 }, { name: "Small", size: 10, progress: 0.2, dlspeed: 5, added_on: 10 }];
  for (const sort of ["size", "progress", "speed", "added"]) assert.deepEqual(names(downloadRows(items, "", "all", sort)).slice(0, 2), ["Large", "Small"]);
  assert.deepEqual(names(downloadRows(items, "", "all", "progress")), ["Large", "Small", "Zero", "Unknown"]);
});

test("app sorting uses the reported owner and has a deterministic title order", () => {
  const items = [{ name: "B", app: "Sonarr" }, { name: "Z", app: "Radarr" }, { name: "A", app: "Sonarr" }];
  assert.deepEqual(names(downloadRows(items, "", "all", "app")), ["Z", "A", "B"]);
});
