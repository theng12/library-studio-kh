import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import assert from "node:assert/strict";
import test from "node:test";

const web = new URL("../internal/server/web/", import.meta.url);
const setup = readFileSync(new URL("setup.js", web), "utf8");
const app = readFileSync(new URL("app.js", web), "utf8");
const html = readFileSync(new URL("index.html", web), "utf8");

function helpers() {
  const source = setup.match(/\/\* Pure helpers[\s\S]*?\n\/\* DOM wiring \*\//)[0];
  const context = { URL };
  runInNewContext(source, context);
  return context;
}

test("request keeps server defaults, selected apps and whole-number ports", () => {
  const context = helpers();
  const defaults = {
    apps: ["qbittorrent", "sonarr", "prowlarr"],
    appDir: "/Applications", downloadPath: "/Volumes/Media/Downloads",
    roots: { sonarr: "/Volumes/Media/TV", radarr: "/Volumes/Media/Films", lidarr: "/Volumes/Media/Music" },
    categories: { sonarr: "tv", radarr: "films", lidarr: "music" },
    ports: { qbittorrent: 8080, sonarr: 8989, radarr: 7878, lidarr: 8686, prowlarr: 9696 },
    renameTV: true, startAtLogin: false,
  };
  const request = context.setupRequest({ apps: { qbittorrent: false, sonarr: true, radarr: false, lidarr: false, prowlarr: false }, ports: {} }, defaults);
  assert.deepEqual(JSON.parse(JSON.stringify(request.apps)), ["sonarr"]);
  assert.equal(request.appDir, "/Applications");
  assert.equal(request.downloadPath, "/Volumes/Media/Downloads");
  assert.deepEqual(JSON.parse(JSON.stringify(request.roots)), defaults.roots);
  assert.deepEqual(JSON.parse(JSON.stringify(request.ports)), defaults.ports);
  assert.deepEqual(JSON.parse(JSON.stringify(request.categories)), defaults.categories);
  assert.equal(request.renameTV, true);
  assert.equal(request.startAtLogin, false);
  const none = { qbittorrent: false, sonarr: false, radarr: false, lidarr: false, prowlarr: false };
  assert.throws(() => context.setupRequest({ apps: none }, defaults), /Choose at least one app/);
  assert.throws(() => context.setupRequest({ apps: { sonarr: true }, ports: { sonarr: "80" } }, defaults), /1024 to 65535/);
  assert.throws(() => context.setupRequest({ apps: { sonarr: true }, ports: { sonarr: "8989.5" } }, defaults), /whole numbers/);
  const two = { qbittorrent: false, sonarr: true, radarr: true, lidarr: false, prowlarr: false };
  assert.throws(() => context.setupRequest({ apps: two, ports: { sonarr: "8989", radarr: "8989" } }, defaults), /both use port 8989/);
  assert.doesNotThrow(() => context.setupRequest({ apps: two, ports: { sonarr: "8989", radarr: "7878" } }, defaults));
});

test("unknown download size is never shown as zero", () => {
  const context = helpers();
  for (const value of [0, -1, null, undefined, NaN, Infinity, "not-a-number"]) {
    assert.equal(context.setupSize(value), "Size not published");
  }
  assert.equal(context.setupSize(1610612736), "1.5 GB");
  assert.equal(context.setupSize(500), "500 B");
});

test("job controls follow the reported state, and retry keeps the saved plan id", () => {
  const context = helpers();
  const planned = { id: "plan-7", state: "planned", apps: [{ id: "sonarr", state: "download", managed: true }] };
  const planView = context.setupJobView(planned);
  assert.equal(planView.run, true);
  assert.equal(planView.plan, false);
  assert.equal(planView.check, false);
  assert.equal(planView.edit, true);
  assert.deepEqual(JSON.parse(JSON.stringify(context.setupJobRequest(planned, "run").body)), { id: "plan-7" });

  const installing = context.setupJobView({ id: "plan-7", state: "installing", apps: [
    { id: "sonarr", state: "needs_open", managed: true },
  ] });
  assert.deepEqual(JSON.parse(JSON.stringify(installing.open)), []);
  assert.equal(installing.check, false);
  assert.equal(installing.edit, false);

  const needs = context.setupJobView({ id: "plan-7", state: "needs_approval", apps: [
    { id: "sonarr", state: "needs_open", managed: true },
    { id: "radarr", state: "needs_connection", managed: true },
    { id: "lidarr", state: "needs_sources", managed: true },
  ] });
  assert.deepEqual(JSON.parse(JSON.stringify(needs.open)), ["sonarr"]);
  assert.equal(needs.settings, true);
  assert.equal(needs.sources, true);
  assert.equal(needs.check, true);
  assert.equal(needs.edit, true);
  assert.deepEqual(JSON.parse(JSON.stringify(context.setupJobRequest(planned, "open", "sonarr").body)), { id: "plan-7", app: "sonarr" });
  assert.throws(() => context.setupJobRequest(planned, "open"), /Choose an app/);
  assert.throws(() => context.setupJobRequest(null, "run"), /no saved setup plan/);

  const blockedExisting = { id: "old", state: "blocked", managed: false };
  const blocked = context.setupJobView({ id: "plan-7", state: "blocked", apps: [blockedExisting] });
  assert.deepEqual(JSON.parse(JSON.stringify(blocked.open)), []);
  assert.equal(blocked.retry, false);
  assert.equal(blocked.check, true);
  assert.equal(context.setupExistingApp(blockedExisting), true);

  assert.equal(context.setupJobView({ id: "p", state: "failed", apps: [] }).retry, true);
  assert.equal(context.setupJobView({ id: "p", state: "interrupted", apps: [] }).retry, true);
  assert.equal(context.setupJobView({ id: "p", state: "failed", apps: [] }).check, false);
  const restart = context.setupJobView({ id: "p", state: "needs_restart", apps: [] });
  assert.equal(restart.restart, true);
  assert.equal(restart.ready, false);
  const ready = context.setupJobView({ id: "p", state: "ready", apps: [] });
  assert.equal(ready.ready, true);
  assert.equal(ready.check, true);
  assert.equal(context.setupJobView(null).plan, true);
});

test("only a real existing-app state is treated as an existing install", () => {
  const context = helpers();
  assert.equal(context.setupExistingApp({ managed: false, state: "planning" }), false);
  assert.equal(context.setupExistingApp({ managed: false, state: "download" }), false);
  assert.equal(context.setupExistingApp({ managed: false, state: "adopt" }), true);
  assert.equal(context.setupExistingApp({ managed: false, state: "needs_connection" }), true);
  assert.equal(context.setupExistingApp({ managed: false, state: "blocked" }), true);
  assert.equal(context.setupExistingApp({ managed: false, state: "not_checked", connectionDetected: true }), true);
  assert.equal(context.setupExistingApp({ managed: true, state: "adopt" }), false);
});

test("connection checks use passed/failed/unknown, never connected-as-success", () => {
  const context = helpers();
  assert.equal(context.setupCheckState("passed"), "ok");
  assert.equal(context.setupCheckState("failed"), "bad");
  for (const state of ["not_run", "not_checked", "connected", ""]) assert.equal(context.setupCheckState(state), "");
  assert.equal(context.setupCheckLabel("passed"), "Passed");
  assert.equal(context.setupCheckLabel("failed"), "Failed");
  assert.equal(context.setupCheckLabel("not_run"), "Not run");
  assert.equal(context.setupCheckLabel("not_checked"), "Not checked");
});

test("client navigation accepts only a host root and clears local access before leaving", async () => {
  const context = helpers();
  assert.equal(context.setupClientTarget("http://100.64.0.10:8080"), "http://100.64.0.10:8080/");
  assert.equal(context.setupClientTarget("https://library.example.invalid/"), "https://library.example.invalid/");
  for (const bad of ["http://host:8080/page", "http://host:8080/?x=1", "http://host:8080/#token", "http://user:secret@host:8080/", "ftp://host/"]) {
    assert.throws(() => context.setupClientTarget(bad));
  }
  const steps = [];
  let cleared = false;
  const target = await context.setupClientGo("http://100.64.0.10:8080", {
    api: async (path, body) => { steps.push(["api", path, body]); assert.equal(cleared, false); return { url: "http://100.64.0.10:8080/" }; },
    clearToken: () => { cleared = true; steps.push(["clear"]); },
    navigate: (url) => { assert.equal(cleared, true); steps.push(["go", url]); },
  });
  assert.equal(target, "http://100.64.0.10:8080/");
  assert.deepEqual(JSON.parse(JSON.stringify(steps)), [
    ["api", "/api/setup/client", { url: "http://100.64.0.10:8080/" }],
    ["clear"],
    ["go", "http://100.64.0.10:8080/"],
  ]);
  assert.equal(steps[2][1].includes("token"), false);
  assert.equal(steps[2][1].includes("#"), false);
});

test("public Help strips owner setup forms before removing ids", () => {
  const source = app.match(/function publicHelpCopy\([\s\S]*?\n\}/)[0];
  const order = [];
  const owner = { remove: () => order.push("owner") };
  const idNode = { removeAttribute: (name) => { if (name === "id") order.push("id"); } };
  const copy = {
    hidden: true,
    removeAttribute: (name) => { if (name === "id") order.push("self-id"); },
    querySelectorAll: (selector) => selector === "[data-owner-only]" ? [owner] : [idNode],
  };
  const context = {};
  runInNewContext(source, context);
  const result = context.publicHelpCopy({ cloneNode: (deep) => { assert.equal(deep, true); return copy; } });
  assert.equal(result, copy);
  assert.deepEqual(order, ["owner", "self-id", "id"]);

  const ownerStart = html.indexOf('data-owner-only');
  const ownerEnd = html.indexOf("</details>", ownerStart);
  assert.equal(ownerStart > 0, true);
  assert.equal(html.slice(ownerStart, ownerEnd).includes('id="setup-plan-form"'), true);
  assert.equal(html.slice(0, ownerStart).includes('id="setup-plan-form"'), false);
  assert.equal(html.slice(0, ownerStart).includes('id="setup-client-form"'), true);
  assert.match(app, /querySelectorAll\("\[data-owner-only\]"\)\) node\.remove\(\)/);
  assert.match(app.slice(app.indexOf('$("#open-public-help")')), /publicHelpCopy\(\$\("#view-help"\)\)/);
  assert.equal(setup.replace("window.location.assign", "").includes("location.assign"), false);
  assert.match(setup, /window\.location\.assign\(url\)/);
});

test("owner results are stale-guarded, editable while inactive, and copy never promises an OS prompt", () => {
  assert.match(setup, /let setupGeneration = 0;/);
  assert.match(setup, /function resetSetup\(\) \{\n  setupGeneration\+\+;/);
  assert.match(setup, /const generation = setupGeneration;/);
  assert.match(setup, /if \(generation !== setupGeneration\) return;/);
  assert.match(setup, /document\.addEventListener\("library:gate"[\s\S]*?stopSetupPoll\(\)/);
  assert.match(setup, /function setupClearLocalToken\(\)/);
  assert.equal(/function clearLocalToken\(\)/.test(setup), false);
  assert.match(setup, /Choose Open, then complete any macOS approval prompts\. Check connections afterward\./);
  assert.equal(setup.includes("Library never opens an app for you"), false);
  assert.match(setup, /setupButton\("Edit choices"/);
  assert.match(setup, /fillSetupForm\(setupJob\.request \|\| setupDefaults \|\| \{\}, true\)/);
  assert.match(setup, /if \(!\(setupEditing && !nextJob\)\) setupJob = nextJob;/);
  assert.match(html, /id="setup-cancel-edit" hidden/);
  assert.equal((html.match(/type="number" min="1024" max="65535"/g) || []).length, 5);
});
