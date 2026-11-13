import test from "node:test";
import assert from "node:assert/strict";
import vm from "node:vm";
import { readFileSync } from "node:fs";

const app = readFileSync(new URL("../internal/server/web/app.js", import.meta.url), "utf8");
const html = readFileSync(new URL("../internal/server/web/index.html", import.meta.url), "utf8");

function labelHelpers() {
  const grid = readFileSync(new URL("../internal/server/web/library-grid.js", import.meta.url), "utf8");
  const source = [
    grid.match(/function libraryVolumeLabel\(volume\) \{[\s\S]*?\n\}/)[0],
    app.match(/function torrentStateWord\(state\) \{[\s\S]*?\n\}/)[0],
    app.match(/function activityLabel\(it\) \{[\s\S]*?\n\}/)[0],
  ].join("\n");
  const context = {
    downloadProgressText(value) {
      return Number.isFinite(value) && value >= 0 && value <= 1 ? Math.floor(value * 100) + "%" : "Progress not reported";
    },
    rate: (n) => n + " B/s",
  };
  vm.runInNewContext(source, context);
  return context.activityLabel;
}

test("Activity never invents progress or a finished state", () => {
  const label = labelHelpers();
  assert.match(label({ status: "downloading", state: "downloading", progress: 0 }).detail, /^0%/);
  assert.match(label({ status: "downloading", state: "downloading" }).detail, /Progress not reported/);
  assert.equal(label({ status: "queued", state: "queuedDL" }).label, "Queued");
  assert.equal(label({ status: "downloading", state: "pausedDL", progress: 0.4 }).label, "Paused");
  assert.equal(label({ status: "downloading", state: "missingFiles" }).label, "Missing files");
  assert.equal(label({ status: "downloading", state: "downloading", progress: 0.4 }).label, "Downloading");
});

test("a verified copy names its proof and location; an unproven one says so", () => {
  const label = labelHelpers();
  const verified = label({ status: "copied", verified: "hash", verifiedAt: "2026-01-02T03:04:05Z", targetPath: "/Volumes/NAS/Films/X" });
  assert.equal(verified.outcome, "verified");
  assert.match(verified.label, /Copied · verified/);
  assert.match(verified.detail, /SHA-256 hash match/);
  assert.match(verified.detail, /\/Volumes\/NAS\/Films\/X/);
  assert.match(label({ status: "copied", verified: "hash", targetVolume: "/Volumes/NAS" }).label, /Copied to NAS · verified/);
  assert.match(label({ status: "copied", verified: "hash", targetVolume: "/" }).label, /Copied to Startup disk · verified/);
  const size = label({ status: "moved", verified: "size" });
  assert.equal(size.outcome, "verified");
  assert.match(size.detail, /Size and readback match/);
  const unproven = label({ status: "copied" });
  assert.equal(unproven.outcome, "problems");
  assert.match(unproven.label, /not verified yet/);
  assert.doesNotMatch(unproven.label, / · verified/);
});

test("states that are not proof are never shown as verified", () => {
  const label = labelHelpers();
  assert.equal(label({ status: "on-disk" }).outcome, "problems");
  assert.match(label({ status: "on-disk" }).label, /no import proof/);
  assert.equal(label({ status: "missing" }).outcome, "problems");
  assert.equal(label({ status: "importing" }).label, "Importing");
  assert.equal(label({ status: "importing", state: "moving", progress: 1 }).label, "Importing", "a moving torrent reads like the other pages, never a separate word");
  assert.equal(label({ status: "upcoming", when: "2026-12-01T00:00:00Z" }).outcome, "upcoming");
  assert.equal(label({ status: "removed" }).outcome, "removed");
});

test("the Activity screen is a timeline page, not a download table", () => {
  assert.match(html, /<div class="page">[\s\S]*<h1>Activity<\/h1>/);
  assert.match(html, /<ol id="timeline" class="timeline"/);
  assert.match(html, /id="act-app"/);
  assert.match(html, /id="act-outcome"/);
  assert.doesNotMatch(html, /id="act-kind"/);
  assert.doesNotMatch(html, /<table id="list"/);
});

test("a failed state fetch shows its error instead of a successful empty dashboard", async () => {
  const elements = new Map();
  const node = () => ({ hidden: true, value: "", textContent: "", replaceChildren() {} });
  const $ = (selector) => { if (!elements.has(selector)) elements.set(selector, node()); return elements.get(selector); };
  const rendering = app.slice(app.indexOf("async function load() {"), app.indexOf("/* Downloads: the reconciled table."));
  const context = { $, AUTH: { authenticated: true }, render: () => { throw Error("render must not run after a failed fetch"); }, api: async () => { throw Error("Mock host unavailable; retry the connection"); }, current: null };
  vm.createContext(context);
  vm.runInContext(rendering, context);
  await context.load();
  assert.match($("#summary").textContent, /Mock host unavailable; retry/);
  assert.equal(context.current, null);
});
