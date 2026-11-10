import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import assert from "node:assert/strict";
import test from "node:test";

const web = new URL("../internal/server/web/", import.meta.url);
const app = readFileSync(new URL("app.js", web), "utf8");
const setup = readFileSync(new URL("setup.js", web), "utf8");
const html = readFileSync(new URL("index.html", web), "utf8");
const fn = (source, name) => source.match(new RegExp(`(?:async )?function ${name}\\([^]*?^\\}`, "m"))[0];
const defaults = {
  apps: ["sonarr"], ports: { qbittorrent: 8080, sonarr: 8989, radarr: 7878, lidarr: 8686, prowlarr: 9696 },
  roots: {}, categories: {},
};
function setupContext(extra = {}) {
  const c = { URL, ...extra };
  runInNewContext(setup.match(/\/\* Pure helpers[\s\S]*?\n\/\* DOM wiring \*\//)[0], c);
  return c;
}

test("Whisparr remains optional and its native folder/category/port reach the plan", () => {
  const c = setupContext();
  assert.equal(c.setupRequest({}, defaults).apps.includes("whisparr"), false);
  assert.equal(Object.hasOwn(c.setupRequest({}, defaults).ports, "whisparr"), false);
  const input = html.match(/<input[^>]+name="app\.whisparr"[^>]*>/)[0];
  assert.doesNotMatch(input, /\bchecked\b/);
  const values = { apps: { whisparr: true }, roots: { whisparr: "/chosen/library" }, categories: { whisparr: "chosen-category" }, ports: { whisparr: 7999 } };
  const request = c.setupRequest(values, defaults);
  assert.equal(request.roots.whisparr, "/chosen/library");
  assert.equal(request.categories.whisparr, "chosen-category");
  assert.equal(request.ports.whisparr, 7999);
  assert.throws(() => c.setupRequest({ ...values, ports: {} }, defaults), /Ports must/);
  assert.throws(() => c.setupRequest({ ...values, ports: { whisparr: 8989 } }, defaults), /Sonarr and Whisparr both use port/);
  assert.throws(() => c.setupRequest({ ...values, ports: { whisparr: 80 } }, defaults), /1024 to 65535/);
});

test("Whisparr form roundtrip, plan review and existing-app Settings use native controls", () => {
  const fields = new Map();
  for (const name of ["app.whisparr", "root.whisparr", "category.whisparr", "port.whisparr"]) fields.set(name, { type: name.startsWith("app.") ? "checkbox" : "text", value: "", checked: false });
  const node = (tag, text) => ({ tag, text, childNodes: [], append(...nodes) { this.childNodes.push(...nodes); } });
  const c = setupContext({ setupForm: { dataset: {} }, setupField: (name) => fields.get(name), setupMusicProfileIds: {}, setupNode: node,
    setupButton: (text) => ({ text, dataset: {}, setAttribute() {} }) });
  for (const name of ["setupSetField", "fillSetupForm", "readSetupForm", "setupReview", "setupAppActions"]) runInNewContext(fn(setup, name), c);
  c.fillSetupForm({ apps: ["whisparr"], ports: { whisparr: 7999 }, roots: { whisparr: "/chosen/library" }, categories: { whisparr: "chosen-category" } }, true);
  const read = c.readSetupForm();
  assert.equal(read.apps.whisparr, true);
  assert.equal(read.ports.whisparr, "7999");
  assert.equal(read.roots.whisparr, "/chosen/library");
  const review = c.setupReview(c.setupRequest(read, defaults)).childNodes[1].childNodes.map((n) => n.text).join(" ");
  for (const text of ["Whisparr", "/chosen/library", "7999", "chosen-category"]) assert.ok(review.includes(text), text);
  const buttons = c.setupAppActions({ id: "whisparr", state: "needs_connection", managed: false }, { active: false, open: [] }).childNodes;
  assert.equal(buttons.length, 1);
  assert.equal(buttons[0].dataset.view, "settings");
});

test("Settings renders an OFF switch and keeps keys empty while preserving saved-key hints", async () => {
  const fields = { tailscaleOn: {}, tailscalePort: {}, bazarrOn: {}, whisparrOn: {}, downloadPath: {}, libraryPath: {}, "qbittorrent.url": {}, "qbittorrent.username": {} };
  const secret = { value: "must-clear", dataset: { secret: "whisparr" } };
  for (const id of ["sonarr", "radarr", "lidarr", "prowlarr", "bazarr", "whisparr"]) {
    fields[id + ".url"] = { value: "" }; fields[id + ".apiKey"] = { value: "" };
  }
  let submit, body, rendered = "";
  const msg = {}, button = {};
  fields.querySelectorAll = () => [secret];
  fields.querySelector = () => button;
  fields.addEventListener = (_, handler) => { submit = handler; };
  const nodes = { "#form": fields, "#form-msg": msg, "#remote-status": {}, "#remote-link-box": {}, "#remote-link": {}, "#arr-fields": { insertAdjacentHTML: (_, s) => { rendered += s; } } };
  const c = { $: (s) => nodes[s], current: null, api: async (_, b) => { body = b; return {}; }, load: async () => {} };
  runInNewContext(app.slice(app.indexOf("const ARRS"), app.indexOf("const AUTH_FORM_PATHS")) + fn(app, "fillForm") + fn(app, "buildArrFields"), c);
  c.buildArrFields();
  assert.match(rendered, /name="whisparrOn"/);
  assert.match(rendered, /name="whisparr\.apiKey" type="password"[^>]+data-secret="whisparr"/);
  assert.match(rendered, /Installation and first-run compatibility remain unverified/);
  c.current = { settings: { qbittorrent: { url: "", username: "" }, downloadPath: "", libraryPath: "", whisparrOn: false, whisparr: { url: "http://mock.invalid" } }, saved: { whisparr: true } };
  c.fillForm(c.current);
  assert.equal(fields.whisparrOn.checked, false);
  assert.equal(secret.value, "");
  assert.equal(secret.placeholder, "Saved. Leave empty to keep it.");
  runInNewContext(app.slice(app.indexOf('$("#form").addEventListener("submit"'), app.indexOf("async function start()")), c);
  for (const name of ["downloadPath", "libraryPath", "qbittorrent.url", "qbittorrent.username"]) fields[name].value = "";
  fields["qbittorrent.password"] = { value: "" };
  await submit({ preventDefault() {}, target: fields });
  assert.equal(body.whisparrOn, false);
  assert.equal(body.whisparr.apiKey, "");
  assert.equal(body.whisparr.url, "http://mock.invalid");
  fields.whisparrOn.checked = true; fields["whisparr.apiKey"].value = "mock-replacement";
  await submit({ preventDefault() {}, target: fields });
  assert.equal(body.whisparrOn, true);
  assert.equal(body.whisparr.apiKey, "mock-replacement");
});

test("Help separates available controls from unverified installation and runtime", () => {
  assert.doesNotMatch(html, /LazyLibrarian, Mylar3 and Whisparr/);
  assert.match(html, /<summary>Whisparr — optional library app<\/summary>/);
  assert.match(html, /Installation and first-run compatibility remain unverified/);
  assert.match(html, /Leave the key empty to keep a saved key/);
  for (const name of ["root.whisparr", "port.whisparr", "category.whisparr"]) assert.ok(html.includes(`name="${name}"`));
});
