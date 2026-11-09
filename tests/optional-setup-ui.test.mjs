import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import assert from "node:assert/strict";
import test from "node:test";

const setup = readFileSync(new URL("../internal/server/web/setup.js", import.meta.url), "utf8");
const html = readFileSync(new URL("../internal/server/web/index.html", import.meta.url), "utf8");
const pure = setup.match(/\/\* Pure helpers[\s\S]*?\n\/\* DOM wiring \*\//)[0];
const fn = (name) => setup.match(new RegExp(`(?:async )?function ${name}\\([^]*?^\\}`, "m"))[0];

function context(extra = {}) {
  const result = { URL, ...extra };
  runInNewContext(pure, result);
  return result;
}

const defaults = {
  apps: ["sonarr"], appDir: "/Applications", downloadPath: "/downloads",
  roots: {}, categories: {},
  ports: { qbittorrent: 8080, sonarr: 8989, radarr: 7878, lidarr: 8686, prowlarr: 9696 },
};

test("optional helpers are unchecked and only enter a plan when selected", () => {
  const c = context();
  assert.deepEqual(Array.from(c.setupRequest({}, defaults).apps), ["sonarr"]);
  const request = c.setupRequest({ apps: { recyclarr: true, unpackerr: true } }, defaults);
  assert.deepEqual(Array.from(request.apps), ["sonarr", "recyclarr", "unpackerr"]);
  assert.equal(Object.hasOwn(request.ports, "recyclarr"), false);
  assert.equal(Object.hasOwn(request.ports, "unpackerr"), false);
  for (const id of ["recyclarr", "unpackerr"]) {
    const input = html.match(new RegExp(`<input[^>]+name="app\\.${id}"[^>]*>`))[0];
    assert.doesNotMatch(input, /\bchecked\b/);
  }
});

test("sync controls require a configured idle Recyclarr and do not point to unrelated Settings", () => {
  const c = context({
    setupNode: () => ({ childNodes: [], append(...nodes) { this.childNodes.push(...nodes); } }),
    setupButton: (text) => ({ text, setAttribute() {} }),
  });
  runInNewContext(fn("setupAppActions"), c);
  const view = { active: false, open: [] };
  const labels = (app, v = view) => c.setupAppActions(app, v)?.childNodes.map((node) => node.text) || [];
  assert.deepEqual(Array.from(labels({ id: "recyclarr", state: "configured" })), ["Preview sync", "Apply sync"]);
  for (const state of ["needs_config", "download", "not_checked"]) {
    assert.equal(labels({ id: "recyclarr", state }).length, 0);
  }
  assert.equal(labels({ id: "recyclarr", state: "configured" }, { active: true, open: [] }).length, 0);
  assert.equal(labels({ id: "unpackerr", state: "configured", managed: false }).length, 0);
});

test("cancelled Apply never sends a request; Preview and confirmed Apply are distinct", async () => {
  const calls = [];
  let approved = false;
  let prompts = 0;
  const c = context({
    setupJob: { id: "plan" }, setupBusy: false, setupGeneration: 0,
    window: { confirm: () => { prompts++; return approved; } },
    setupApi: async (path, body) => { calls.push({ path, body }); return { id: "plan" }; },
    setupSay() {}, setupRememberMusicProfiles() {}, renderSetupJob() {}, scheduleSetupPoll() {},
  });
  runInNewContext(fn("setupRun"), c);
  await c.setupRun("apply", "recyclarr");
  assert.equal(prompts, 1);
  assert.equal(calls.length, 0);
  assert.equal(c.setupBusy, false);
  await c.setupRun("preview", "recyclarr");
  assert.equal(prompts, 1);
  assert.equal(calls[0].path, "/api/setup/optional");
  assert.equal(calls[0].body.action, "preview");
  assert.equal(calls[0].body.confirm, false);
  approved = true;
  await c.setupRun("apply", "recyclarr");
  assert.equal(prompts, 2);
  assert.equal(calls[1].body.action, "apply");
  assert.equal(calls[1].body.confirm, true);
});
