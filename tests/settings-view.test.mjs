import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import assert from "node:assert/strict";
import test from "node:test";

const settings = readFileSync(new URL("../internal/server/web/settings-view.js", import.meta.url), "utf8");
const picker = readFileSync(new URL("../internal/server/web/folder-picker.js", import.meta.url), "utf8");
const app = readFileSync(new URL("../internal/server/web/app.js", import.meta.url), "utf8");

// Minimal DOM: enough for the settings builder, nothing more.
function fakeElement(tagName) {
  return {
    tagName: tagName.toUpperCase(),
    children: [],
    attributes: new Map(),
    className: "",
    textContent: "",
    value: "",
    hidden: false,
    append(...kids) { for (const kid of kids) this.children.push(kid); },
    setAttribute(key, value) { this.attributes.set(key, String(value)); },
    getAttribute(key) { return this.attributes.has(key) ? this.attributes.get(key) : null; },
    addEventListener() {},
  };
}

function mount() {
  const doc = { createElement: fakeElement };
  const host = doc.createElement("div");
  const context = { window: {}, document: doc };
  runInNewContext(settings, context);
  context.window.librarySettingsView.mountSettingsTabs(host, doc);
  return host;
}

function walk(node, out = []) {
  for (const child of node.children) { out.push(child); walk(child, out); }
  return out;
}
const byId = (root, id) => walk(root).find((n) => n.getAttribute("id") === id);

test("Settings path fields accept a typed path and keep the chooser", () => {
  const host = mount();
  for (const id of ["settings-downloadpath", "settings-librarypath"]) {
    const input = byId(host, id);
    assert.ok(input, id + " renders");
    assert.equal(input.getAttribute("readonly"), null, id + " is typeable");
    assert.equal(input.getAttribute("data-path-picker"), "", id + " still opens the folder chooser");
  }
  assert.doesNotMatch(settings, /readonly: field\.kind === "path"/, "the old readonly rule is gone");
  assert.doesNotMatch(picker, /input\.readOnly = true/, "the picker no longer locks a path field");
  assert.match(picker, /async function checkPath\(/, "the picker checks a typed path");
  assert.match(picker, /checkPath,\s*\n\s*\};/, "the check is exported for Save");
  assert.match(app, /window\.folderPicker\?\.checkPath/, "Save runs that check per folder");
});

test("every input field carries one plain hint under it", () => {
  const context = { window: {} };
  runInNewContext(settings, context);
  const cards = context.window.librarySettingsView.SETTINGS_CARDS;
  const inputKinds = new Set(["radios", "switch", "text", "password", "number", "path"]);
  let checked = 0;
  for (const tab of Object.values(cards)) {
    for (const card of tab) {
      for (const field of card.fields) {
        if (!inputKinds.has(field.kind)) continue;
        checked++;
        const name = card.id + "/" + (field.name || field.kind);
        assert.equal(typeof field.hint, "string", name + " has a hint");
        assert.ok(field.hint.trim().length > 0, name + " hint is not empty");
        assert.ok(!field.hint.includes("\n"), name + " hint is one short line");
      }
    }
  }
  assert.ok(checked >= 10, "expected every settings input field, saw " + checked);
  // Rendered: the hint sits with its own input and is linked as its description.
  const host = mount();
  const input = byId(host, "settings-downloadpath");
  const label = walk(host).find((n) => n.children.includes(input));
  const hint = label.children.find((n) => n.getAttribute("id") === "settings-downloadpath-hint");
  assert.ok(hint, "the download folder hint renders under its field");
  assert.match(hint.textContent, /free space/);
  assert.equal(input.getAttribute("aria-describedby"), "settings-downloadpath-hint");
});

test("the server status card renders its stable ids on Settings", () => {
  const host = mount();
  assert.ok(byId(host, "settings-card-server"), "settings-card-server exists");
  for (const id of ["server-address", "server-phone-link", "server-port", "server-session"]) {
    assert.ok(byId(host, id), id + " exists on the status card");
  }
  // Filled from the state payload the Settings view already receives.
  assert.match(app, /\$\("#server-address"\)/, "the local address is filled from state");
  assert.match(app, /\$\("#server-phone-link"\)/, "the phone address is filled from state");
  assert.match(app, /\$\("#server-port"\)/, "the port is filled from state");
  assert.match(app, /\$\("#server-session"\)/, "the session id is filled from the sessions list");
});

test("an invalid typed path never blocks the rest of the save", async () => {
  const start = app.indexOf('$("#form").addEventListener("submit"');
  const end = app.indexOf("async function start()", start);
  const submitSource = app.slice(start, end);
  const fields = {
    tailscaleOn: { checked: false }, tailscalePort: { value: "7882" },
    deadCleanupOn: { checked: false }, deadCleanupHours: { value: "24" },
    deadArrCleanupOn: { checked: false },
    bazarrOn: { checked: false }, whisparrOn: { checked: false },
    downloadPath: { id: "settings-downloadpath", value: "not/absolute" },
    libraryPath: { id: "settings-librarypath", value: "" },
    "qbittorrent.url": { value: "http://127.0.0.1:8080" },
    "qbittorrent.username": { value: "owner" },
    "qbittorrent.password": { value: "" },
  };
  for (const a of ["sonarr", "radarr", "lidarr", "prowlarr", "bazarr", "whisparr"]) {
    fields[a + ".url"] = { value: "" };
    fields[a + ".apiKey"] = { value: "" };
  }
  fields.querySelector = () => ({ disabled: false, textContent: "" });
  const saveButton = { disabled: false, textContent: "" };
  const msg = { textContent: "", hidden: true };
  const beside = { textContent: "" };
  const reasonLine = { textContent: "", dataset: {} };
  const nodes = { "#form-msg": msg, "#settings-save-msg": beside, "#settings-save": saveButton };
  let submit;
  const context = {
    URL, ARRS: ["sonarr", "radarr", "lidarr", "prowlarr", "bazarr", "whisparr"],
    window: { folderPicker: { checkPath: async (v) => (v.trim() && !v.trim().startsWith("/") ? "Type a full folder path, starting with /." : "") } },
    document: { getElementById: (id) => (id === "settings-downloadpath-picker-status" ? reasonLine : null) },
    current: { settings: { downloadPath: "/saved/downloads", libraryPath: "" } },
    api: async (_url, body) => { context.sent = body; return {}; },
    load: async () => {}, fillForm: () => {}, loadDeadSummary: () => {},
    setTimeout: () => 0, clearTimeout: () => {},
    applyAppearance: () => {},
  };
  context.$ = (selector) => (selector === "#form"
    ? { addEventListener: (type, fn) => { if (type === "submit") submit = fn; } }
    : nodes[selector]);
  runInNewContext(submitSource, context);
  await submit({ preventDefault() {}, target: fields });
  // The bad folder keeps its saved value; every other setting still saves.
  assert.equal(context.sent.downloadPath, "/saved/downloads");
  assert.equal(context.sent.libraryPath, "");
  assert.equal(context.sent.tailscalePort, 7882);
  // The reason sits beside that field, and the save line stays a success note.
  assert.match(reasonLine.textContent, /full folder path/);
  assert.equal(reasonLine.dataset.invalid, "1");
  assert.match(msg.textContent, /One folder kept its saved value/);
  assert.match(msg.textContent, /reason sits under it/);
});
