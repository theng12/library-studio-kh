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
  assert.throws(() => context.setupRequest({ apps: { sonarr: true }, ports: { sonarr: "0" } }, defaults), /1 to 65535/);
  assert.throws(() => context.setupRequest({ apps: { sonarr: true }, ports: { sonarr: "8989.5" } }, defaults), /whole numbers/);
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
  const planned = { id: "plan-7", state: "planned", apps: [{ id: "sonarr", state: "download" }] };
  const planView = context.setupJobView(planned);
  assert.equal(planView.run, true);
  assert.equal(planView.plan, false);
  assert.deepEqual(JSON.parse(JSON.stringify(context.setupJobRequest(planned, "run").body)), { id: "plan-7" });

  const needs = context.setupJobView({ id: "plan-7", state: "installing", apps: [
    { id: "sonarr", state: "needs_open" }, { id: "radarr", state: "needs_connection" }, { id: "lidarr", state: "needs_sources" },
  ] });
  assert.deepEqual(needs.open, ["sonarr"]);
  assert.equal(needs.settings, true);
  assert.equal(needs.sources, true);
  assert.equal(needs.check, true);
  assert.deepEqual(JSON.parse(JSON.stringify(context.setupJobRequest(planned, "open", "sonarr").body)), { id: "plan-7", app: "sonarr" });
  assert.throws(() => context.setupJobRequest(planned, "open"), /Choose an app/);
  assert.throws(() => context.setupJobRequest(null, "run"), /no saved setup plan/);

  assert.equal(context.setupJobView({ id: "p", state: "failed", apps: [] }).retry, true);
  const restart = context.setupJobView({ id: "p", state: "needs_restart", apps: [] });
  assert.equal(restart.restart, true);
  assert.equal(restart.ready, false);
  assert.equal(context.setupJobView({ id: "p", state: "ready", apps: [] }).ready, true);
  assert.equal(context.setupJobView(null).plan, true);
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
