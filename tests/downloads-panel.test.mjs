import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import assert from "node:assert/strict";
import test from "node:test";

// Script-level consts are not context properties; hand them out explicitly.
const source = readFileSync(new URL("../internal/server/web/downloads-panel.js", import.meta.url), "utf8") +
  "\nthis.DOWNLOADS_COLUMNS = DOWNLOADS_COLUMNS;" +
  "\nthis.DOWNLOADS_BULK_ACTIONS = DOWNLOADS_BULK_ACTIONS;" +
  "\nthis.DOWNLOADS_MISSING_LABEL = DOWNLOADS_MISSING_LABEL;";
const context = {};
runInNewContext(source, context);
const {
  DOWNLOADS_COLUMNS, DOWNLOADS_BULK_ACTIONS, DOWNLOADS_MISSING_LABEL,
  downloadsPanelItems, downloadsStatus, downloadsRowActions, downloadsModel,
  downloadsBulk, downloadsDetail, downloadsPanelRender,
} = context;

// Values that cross the vm boundary have another realm's prototypes.
const plain = (value) => JSON.parse(JSON.stringify(value));

const hash = (c) => c.repeat(40);
const H = { dune: hash("a"), severance: hash("b"), old: hash("c"), imported: hash("d"), queued: hash("e") };

// Fixtures: one active download with target and indexer, one paused, one whose
// files are gone, one imported+verified (must never reach the panel), one queued.
const dune = {
  hash: H.dune, name: "Dune Part Two", state: "downloading", progress: 0.42,
  downloaded: 4.2e9, size: 1e10, dlspeed: 4.2e6, eta: 1500,
  indexer: "1337x", app: "Radarr", posterPath: "/cache/dune.jpg",
  targetPath: "/Volumes/NAS/Movies/Dune Part Two (2024)",
};
const severance = { hash: H.severance, name: "Severance S02E01", state: "pausedDL", progress: 0.1, downloaded: 1e9, size: 1e10, app: "Sonarr", savePath: "/downloads/tv" };
const oldShow = { hash: H.old, name: "Old Show", state: "missingFiles", progress: 0, size: 5e9, app: "Sonarr" };
const imported = {
  hash: H.imported, name: "Imported Film", status: "copied", verified: "hash",
  verifiedAt: "2026-11-12T10:00:00Z", sourcePath: "/downloads/movies/Imported Film",
  targetPath: "/Volumes/NAS/Movies/Imported Film (2020)", statusAt: "2026-11-12T09:59:00Z",
};
const queued = { hash: H.queued, name: "Queued Show", state: "queuedDL", progress: 0, size: 2e9, app: "Sonarr" };
const items = [dune, severance, oldShow, imported, queued];
const names = (list) => Array.from(list, (item) => item.name || item.title);
const titles = (rows) => Array.from(rows, (row) => row.title);

test("columns are the brief's columns in order and every row models each one", () => {
  assert.deepEqual(plain(DOWNLOADS_COLUMNS.map((c) => c.key)), ["poster", "title", "status", "size", "progress", "source", "target"]);
  assert.deepEqual(plain(DOWNLOADS_COLUMNS.map((c) => c.label)), ["", "Title", "Status", "Size", "Progress", "Source", "Target"]);
  const [row] = downloadsModel(items).rows;
  for (const key of ["poster", "title", "status", "sizeText", "progress", "source", "target"]) {
    assert.notEqual(row[key], undefined, "row is missing " + key);
  }
  assert.equal(row.poster, "/cache/dune.jpg");
  assert.equal(row.source, "1337x");
  assert.equal(row.target, "/Volumes/NAS/Movies/Dune Part Two (2024)");
  assert.equal(row.sizeText, "4.2 GB of 10 GB");
});

test("a row whose source is gone says so and offers Clear instead of a fake 0%", () => {
  const row = downloadsModel(items).rows.find((r) => r.hash === H.old);
  assert.equal(row.status.label, "Files missing — no source");
  assert.equal(row.status.label, DOWNLOADS_MISSING_LABEL);
  assert.equal(row.status.missing, true);
  assert.deepEqual(plain(row.actions), [{ key: "clear", label: "Clear" }]);
  assert.equal(row.progress.label, "—");
  assert.notEqual(row.progress.label, "0%");
  assert.equal(downloadsStatus({ state: "error" }).key, "problems");
  assert.equal(downloadsStatus({ progress: 1 }).key, "completed");
  assert.deepEqual(Array.from(downloadsRowActions(downloadsStatus(severance)), (a) => a.key), ["resume", "remove", "reveal"]);
});

test("imported and verified rows are excluded by the documented helper, never shown as done", () => {
  const kept = downloadsPanelItems(items);
  assert.deepEqual(names(kept), ["Dune Part Two", "Severance S02E01", "Old Show", "Queued Show"]);
  const model = downloadsModel(items);
  assert.equal(model.rows.length, 4);
  assert.equal(model.rows.some((r) => r.hash === H.imported), false);
  assert.equal(model.countText, "4 of 4 downloads");
  // Imported without proof is not "done": it stays visible and flagged.
  const unproven = { hash: hash("f"), name: "Copied, unproven", status: "copied" };
  assert.deepEqual(names(downloadsPanelItems([unproven])), ["Copied, unproven"]);
  assert.equal(downloadsStatus(unproven).label, "Imported — proof missing");
  assert.equal(downloadsStatus(unproven).tone, "warn");
});

test("filters carry a live count per state", () => {
  const counts = Object.fromEntries(downloadsModel(items).filters.map((f) => [f.key, f.count]));
  assert.deepEqual(counts, {
    all: 4, downloading: 1, queued: 1, paused: 1, seeding: 0, importing: 0, completed: 0, problems: 1,
  });
  assert.deepEqual(titles(downloadsModel(items, { filter: "problems" }).rows), ["Old Show"]);
  assert.deepEqual(titles(downloadsModel(items, { filter: "paused" }).rows), ["Severance S02E01"]);
  assert.deepEqual(titles(downloadsModel(items, { query: "  DUNE " }).rows), ["Dune Part Two"]);
});

test("selection drives the bulk buttons and dies with the rows", () => {
  const rows = downloadsModel(items).rows;
  const empty = downloadsBulk(rows, new Set());
  assert.equal(empty.count, 0);
  assert.equal(empty.statusText, "No downloads selected");
  assert.deepEqual(Array.from(empty.actions, (a) => a.key), Array.from(DOWNLOADS_BULK_ACTIONS, (a) => a.key));
  assert.equal(empty.actions.every((a) => a.disabled), true);

  const chosen = downloadsBulk(rows, new Set([H.dune, H.severance, "stale-key"]));
  assert.equal(chosen.count, 2);
  assert.equal(chosen.statusText, "2 downloads selected");
  assert.equal(chosen.actions.every((a) => a.disabled === false), true);

  const model = downloadsModel(items, { selected: new Set([H.dune, H.imported]) });
  assert.deepEqual([...model.selected], [H.dune]);
  assert.equal(model.bulk.count, 1);
  assert.equal(model.bulk.statusText, "1 download selected");
});

test("empty states say what happened and name the next step", () => {
  const none = downloadsModel([]);
  assert.equal(none.rows.length, 0);
  assert.equal(none.countText, "No downloads");
  assert.match(none.empty.message, /No downloads right now/);
  assert.equal(none.empty.action, null);

  // Only imported+verified rows: Downloads is empty, not full of stale 0% rows.
  const settled = downloadsModel([imported]);
  assert.equal(settled.rows.length, 0);
  assert.match(settled.empty.message, /No downloads right now/);

  const filtered = downloadsModel(items, { filter: "seeding" });
  assert.equal(filtered.rows.length, 0);
  assert.equal(filtered.empty.message, "No downloads match this filter.");
  assert.deepEqual(plain(filtered.empty.action), { key: "show-all", label: "Show all" });

  const searched = downloadsModel(items, { query: "absent" });
  assert.equal(searched.empty.message, "No downloads match this search.");
});

test("progress, speed, ETA and size never invent values", () => {
  const rows = downloadsModel(items).rows;
  const byName = (name) => rows.find((r) => r.title === name);

  const active = byName("Dune Part Two").progress;
  assert.deepEqual({ ...active }, { known: true, value: 0.42, label: "42%", speedText: "4.2 MB/s", etaText: "25 min left" });

  const zero = byName("Queued Show").progress;
  assert.equal(zero.label, "0%");
  assert.equal(zero.speedText, "Speed not reported");
  assert.equal(zero.etaText, "ETA not reported");

  const paused = byName("Severance S02E01");
  assert.equal(paused.sizeText, "1.0 GB of 10 GB");
  assert.equal(paused.status.label, "Paused");

  for (const bad of [undefined, null, "0.5", NaN, Infinity, -1, 1.1]) {
    assert.equal(downloadsStatus({ progress: bad, state: "unknown" }).key, "other");
  }
  assert.equal(byName("Old Show").sizeText, "5.0 GB total — downloaded not reported");
});

test("detail pane carries proof, target, verification and history", () => {
  const detail = downloadsDetail(imported, [{ at: "2026-11-12T09:59:00Z", text: "Import started" }]);
  assert.deepEqual(Array.from(detail.sections, (s) => s.key), ["proof", "target", "verification", "history"]);
  const section = (key) => detail.sections.find((s) => s.key === key);
  assert.deepEqual(plain(section("target").facts), [["Volume", "Not reported"], ["Path", "/Volumes/NAS/Movies/Imported Film (2020)"]]);
  assert.deepEqual(Array.from(section("verification").facts, (f) => f[0]), ["Method", "Verified"]);
  assert.equal(section("verification").facts[0][1], "SHA-256 hash");
  assert.match(section("verification").facts[1][1], /2026/);
  assert.equal(section("history").events.length, 1);
  assert.equal(section("history").events[0].text, "Import started");
  assert.match(section("history").events[0].when, /2026/);
  assert.equal(detail.facts.find((f) => f[0] === "Size")[1], "Size not reported");
  assert.equal(detail.facts.find((f) => f[0] === "Source")[1], "App not verified");

  const missing = downloadsDetail(oldShow);
  assert.equal(missing.sections.find((s) => s.key === "verification").facts[0][1], "Not verified");
  assert.equal(missing.sections.find((s) => s.key === "verification").facts[1][1], "Not yet");
  assert.equal(missing.sections.find((s) => s.key === "history").empty, "No history reported yet.");
  assert.equal(missing.sections.find((s) => s.key === "proof").facts[0][1], DOWNLOADS_MISSING_LABEL);
  assert.equal(downloadsDetail(dune).sections.find((s) => s.key === "target").facts[1][1], dune.targetPath);
});

/* Minimal fake DOM: enough nodes for the render functions, no window, no browser. */
function fakeDocument() {
  return {
    createElement(tag) {
      const node = {
        tag, className: "", textContent: "", hidden: false, children: [], dataset: {}, attrs: {}, listeners: {},
        append(...kids) { node.children.push(...kids); },
        replaceChildren(...kids) { node.children = [...kids]; },
        setAttribute(key, value) { node.attrs[key] = value; },
        addEventListener(type, fn) { node.listeners[type] = fn; },
        classList: { add(name) { node.className = (node.className + " " + name).trim(); } },
      };
      return node;
    },
  };
}
function walk(node, visit) {
  for (const child of node.children || []) { visit(child); walk(child, visit); }
}
function findAll(node, pred) {
  const found = [];
  walk(node, (n) => { if (pred(n)) found.push(n); });
  return found;
}
function findOne(node, pred) {
  return findAll(node, pred)[0];
}
function fakeRoot() {
  return { children: [], classList: { add() {} }, replaceChildren(...kids) { this.children = [...kids]; }, append(...kids) { this.children.push(...kids); } };
}
const hasClass = (name) => (n) => String(n.className).split(" ").includes(name);

test("render builds the columns' header and the missing row's Clear action", () => {
  context.document = fakeDocument();
  const root = fakeRoot();
  downloadsPanelRender(root, downloadsModel(items), {});

  const headers = findAll(root, (n) => n.tag === "th").map((th) => th.textContent);
  assert.deepEqual(headers, ["", "", "Title", "Status", "Size", "Progress", "Source", "Target", "Actions"]);

  const missingRow = findOne(root, (n) => n.tag === "tr" && n.dataset.key === H.old);
  assert.equal(missingRow.className, "dlp-row dlp-missing");
  assert.deepEqual(
    findAll(missingRow, (n) => n.tag === "button" && n.dataset.action).map((b) => [b.dataset.action, b.textContent]),
    [["clear", "Clear"]]);
  assert.equal(findAll(missingRow, hasClass("dlp-poster-empty")).length, 1);

  const activeRow = findOne(root, (n) => n.tag === "tr" && n.dataset.key === H.dune);
  assert.deepEqual(findAll(activeRow, (n) => n.tag === "button" && n.dataset.action).map((b) => b.dataset.action), ["pause", "remove", "reveal"]);
  assert.equal(findOne(activeRow, (n) => n.tag === "img").src, "/cache/dune.jpg");

  const bulk = findOne(root, hasClass("dlp-bulk"));
  const bulkButtons = findAll(bulk, (n) => n.tag === "button");
  assert.equal(bulkButtons.length, 3);
  assert.equal(bulkButtons.every((b) => b.disabled), true);

  const selectedRoot = fakeRoot();
  downloadsPanelRender(selectedRoot, downloadsModel(items, { selected: new Set([H.dune]) }), {});
  const enabled = findAll(findOne(selectedRoot, hasClass("dlp-bulk")), (n) => n.tag === "button");
  assert.equal(enabled.every((b) => b.disabled === false), true);
});

test("render shows the empty state with its next-step action", () => {
  context.document = fakeDocument();
  const root = fakeRoot();
  downloadsPanelRender(root, downloadsModel(items, { filter: "seeding" }), {});
  const empty = findOne(root, hasClass("dlp-empty"));
  assert.equal(empty.hidden, false);
  assert.equal(empty.textContent, "No downloads match this filter.");
  assert.deepEqual(findAll(empty, (n) => n.tag === "button").map((b) => b.textContent), ["Show all"]);
  assert.equal(findAll(root, (n) => n.tag === "tr").length, 1, "only the header row remains");

  const noRows = fakeRoot();
  downloadsPanelRender(noRows, downloadsModel([]), {});
  const blank = findOne(noRows, hasClass("dlp-empty"));
  assert.match(blank.textContent, /No downloads right now/);
  assert.equal(findAll(blank, (n) => n.tag === "button").length, 0);
});
