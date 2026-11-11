import test from "node:test";
import assert from "node:assert/strict";
import vm from "node:vm";
import { readFileSync } from "node:fs";

const app = readFileSync(new URL("../internal/server/web/app.js", import.meta.url), "utf8");
const html = readFileSync(new URL("../internal/server/web/index.html", import.meta.url), "utf8");
const rendering = app.slice(app.indexOf("function render(st) {"), app.indexOf("function dur(s) {"));
const loading = app.slice(app.indexOf("async function load() {"), app.indexOf("/* Downloads:"));
const emptyCopy = html.match(/<p id="empty"[^>]*>(.*?)<\/p>/s)[1].replace(/<[^>]+>/g, "");
function fixture() {
  const elements = new Map();
  const node = () => ({ hidden: true, value: "", children: [], textContent: "", replaceChildren(...items) { this.children = items; }, setAttribute(k, v) { this[k] = v; } });
  const $ = selector => { if (!elements.has(selector)) elements.set(selector, node()); return elements.get(selector); };
  $("#empty").textContent = emptyCopy;
  const ctx = vm.createContext({ $, current: null, AUTH: { authenticated: true }, fillWebAddresses() {}, gb() {}, applyAppearance() {}, document: { createElement: node, querySelectorAll: () => [] }, api: async () => { throw Error("Mock host unavailable; retry the connection"); } });
  vm.runInContext(rendering + loading, ctx);
  const state = { version: "fixture", settings: { stagedCleanupOn: false }, releases: [], snapshot: { apps: [], items: [], freeBytes: null, library: "not set" } };
  return { ctx, $, state };
}

test("legacy null-app payload reproduces the reported render failure", () => {
  const { ctx, state } = fixture(); state.snapshot.apps = null;
  assert.throws(() => ctx.render(state), /map/);
});

test("fresh-host array payload renders honest not-ready guidance", () => {
  const { ctx, $, state } = fixture(); ctx.render(state);
  assert.equal($("#apps").children.length, 0);
  assert.equal($("#empty").hidden, false);
  assert.match($("#empty").textContent, /Nothing connected yet.*Settings/);
  assert.equal($("#list").hidden, true);
  assert.match($("#summary").innerHTML, /not known yet/);
});

test("failed companion stays visibly failed with the cleanup block", () => {
  const { ctx, $, state } = fixture();
  state.snapshot.apps = [{ name: "Sonarr", ok: false, error: "Mock service unavailable" }];
  state.snapshot.blocked = "Sonarr didn't answer, so nothing is removed until it does.";
  ctx.render(state);
  const row = $("#apps").children[0];
  assert.equal(row.className, "bad");
  assert.equal(row.title, "Mock service unavailable");
  assert.equal($("#notice").hidden, false);
  assert.match($("#notice").textContent, /nothing is removed/);
});

test("state fetch failure shows its error instead of a successful empty dashboard", async () => {
  const { ctx, $ } = fixture(); await ctx.load();
  assert.match($("#summary").textContent, /Mock host unavailable; retry/);
  assert.equal($("#empty").hidden, true);
  assert.equal(ctx.current, null);
});
