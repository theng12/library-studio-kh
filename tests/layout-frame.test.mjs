import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import assert from "node:assert/strict";
import test from "node:test";

const pagesCss = readFileSync(new URL("../internal/server/web/pages.css", import.meta.url), "utf8");
const css = readFileSync(new URL("../internal/server/web/layout.css", import.meta.url), "utf8");
const source = readFileSync(new URL("../internal/server/web/settings-view.js", import.meta.url), "utf8");

// Minimal DOM: enough for the settings builder, nothing more.
function fakeElement(tagName) {
  return {
    tagName: tagName.toUpperCase(),
    children: [],
    attributes: new Map(),
    listeners: {},
    className: "",
    textContent: "",
    value: "",
    checked: false,
    hidden: false,
    focused: false,
    append(...kids) { for (const kid of kids) this.children.push(kid); },
    setAttribute(key, value) {
      this.attributes.set(key, String(value));
      if (key === "id") this.id = String(value);
      if (key === "class") this.className = String(value);
    },
    getAttribute(key) { return this.attributes.has(key) ? this.attributes.get(key) : null; },
    removeAttribute(key) { this.attributes.delete(key); },
    addEventListener(type, fn) { (this.listeners[type] ||= []).push(fn); },
    fire(type, event = {}) { for (const fn of this.listeners[type] || []) fn({ target: this, preventDefault() {}, ...event }); },
    focus() { this.focused = true; },
  };
}

const fakeDocument = () => ({ createElement: fakeElement });
const window = {};
runInNewContext(source, { window, document: fakeDocument() });
const view = window.librarySettingsView;

function walk(node, out = []) {
  for (const child of node.children) { out.push(child); walk(child, out); }
  return out;
}
const byClass = (root, name) => walk(root).filter((n) => n.className.split(/\s+/).includes(name));
const byTag = (root, tag) => walk(root).filter((n) => n.tagName === tag.toUpperCase());

function mount() {
  const doc = fakeDocument();
  const host = doc.createElement("div");
  const api = view.mountSettingsTabs(host, doc);
  return { host, api };
}

test("the page frame fills the window instead of a 60rem column", () => {
  assert.match(css, /--pad-page:/);
  assert.match(css, /--gap-page:/);
  assert.match(css, /--gap-card:/);
  assert.match(css, /\.view \{ max-width: none; gap: var\(--gap-page\); \}/);
  assert.match(css, /#form \{ max-width: none; \}/);
  assert.match(css, /\.content \{[^}]*padding: var\(--pad-page\) var\(--pad-page\)/);
  assert.match(css, /\.page-head \{[^}]*display: flex/);
  assert.match(css, /\.page-status \{[^}]*overflow-wrap: anywhere/);
  assert.match(css, /\.page-actions \{ margin-left: auto/);
});

test("tables fit the width or scroll sideways with the first column pinned", () => {
  assert.match(css, /\.table-wrap \{[^}]*overflow-x: auto/);
  // A grid cell may shrink, so a wide table scrolls inside its card at 768.
  assert.match(css, /\.table-wrap \{[^}]*min-width: 0/);
  assert.match(css, /\.card-body > \*, #cleanup-status, \.libgrid-body > \* \{ min-width: 0; \}/);
  // A closed dialog is never in the layout.
  assert.match(pagesCss, /dialog:not\(\[open\]\) \{ display: none !important; \}/);
  assert.match(css, /\.page-table th:first-child, \.page-table td:first-child \{[^}]*position: sticky;[^}]*left: 0;/);
  assert.match(css, /\.page-table \{[^}]*width: 100%/);
});

test("cards, switches and tabs have their own dense styles", () => {
  assert.match(css, /\.card-grid, \.tabpanel \{[^}]*repeat\(auto-fit, minmax\(21rem, 1fr\)\)/);
  assert.match(css, /\.switch input \{[^}]*appearance: none/);
  assert.match(css, /\.switch input:checked \{ background: var\(--accent\); \}/);
  assert.match(css, /\.switch-saved\[data-state="saved"\]/);
  assert.match(css, /\.tab\[aria-selected="true"\]/);
  assert.match(css, /\.appearance label:has\(input:checked\)/);
  assert.match(css, /@media \(max-width: 900px\)/);
  assert.match(css, /@media \(max-width: 760px\)/);
});

test("Settings offers exactly the four tabs, General first and open", () => {
  const { api } = mount();
  const tabs = byClass(api.root, "tab");
  assert.deepEqual(tabs.map((t) => t.textContent), ["General", "Sessions & security", "Remote access", "Updates"]);
  assert.equal(tabs[0].getAttribute("aria-selected"), "true");
  assert.deepEqual(tabs.slice(1).map((t) => t.getAttribute("aria-selected")), ["false", "false", "false"]);
  const panels = byClass(api.root, "tabpanel");
  assert.equal(panels.length, 4);
  assert.deepEqual(panels.map((p) => p.hidden), [false, true, true, true]);
});

test("each tab is a set of self-contained cards, not one long column", () => {
  const { api } = mount();
  const panels = byClass(api.root, "tabpanel");
  const titles = panels.map((p) => byTag(p, "h2").map((h) => h.textContent));
  assert.deepEqual(titles[0], ["Appearance", "Folders", "Companion apps", "Help"]);
  assert.deepEqual(titles[1], ["Sessions & security"]);
  assert.deepEqual(titles[2], ["Phone & Tailscale access"]);
  assert.deepEqual(titles[3], ["Updates"]);
  for (const panel of panels) assert.ok(byClass(panel, "card").length === byTag(panel, "h2").length);
  assert.ok(byClass(panels[0], "card").length === 4);
});

test("appearance offers exactly System, Light and Dark, starting on System", () => {
  const { api } = mount();
  const radios = byTag(api.root, "input").filter((n) => n.getAttribute("name") === "appearance");
  assert.deepEqual(radios.map((r) => r.value), ["system", "light", "dark"]);
  assert.deepEqual(radios.map((r) => r.checked), [true, false, false]);
  assert.equal(radios[0].getAttribute("type"), "radio");
});

test("a switch carries its own saved-feedback slot", () => {
  const { api } = mount();
  const toggles = byTag(api.root, "input").filter((n) => n.getAttribute("role") === "switch");
  const toggle = toggles.find((n) => n.getAttribute("name") === "tailscaleOn");
  assert.ok(toggle, "the Tailscale switch exists");
  // Every switch has its own slot, including the dead-download action.
  assert.ok(toggles.some((n) => n.getAttribute("name") === "deadCleanupOn"));
  assert.ok(byClass(api.root, "switch-saved").some((s) => s.getAttribute("data-saved-for") === "deadCleanupOn"));
  assert.equal(toggle.getAttribute("type"), "checkbox");
  const slot = byClass(api.root, "switch-saved").find((n) => n.getAttribute("data-saved-for") === "tailscaleOn");
  assert.ok(slot, "the Tailscale switch has its own saved slot");
  assert.equal(slot.getAttribute("aria-live"), "polite");
  assert.equal(api.savedFeedback("tailscaleOn", "Saved", "saved"), true);
  assert.equal(slot.textContent, "Saved");
  assert.equal(slot.getAttribute("data-state"), "saved");
  assert.equal(api.savedFeedback("closedCaptioning", "Saved", "saved"), false);
});

test("clicking and arrowing a tab shows that panel and hides the others", () => {
  const { api, host } = mount();
  const tabs = byClass(api.root, "tab");
  tabs[2].fire("click");
  assert.equal(tabs[2].getAttribute("aria-selected"), "true");
  assert.equal(tabs[0].getAttribute("aria-selected"), "false");
  const panels = byClass(api.root, "tabpanel");
  assert.deepEqual(panels.map((p) => p.hidden), [true, true, false, true]);
  tabs[2].fire("keydown", { key: "ArrowRight" });
  assert.equal(tabs[3].getAttribute("aria-selected"), "true");
  assert.equal(tabs[3].focused, true);
  assert.ok(host.children.includes(api.root));
});

test("controls keep the names and ids app.js saves and fills", () => {
  const { api } = mount();
  const names = byTag(api.root, "input").map((n) => n.getAttribute("name")).filter(Boolean);
  for (const name of ["appearance", "downloadPath", "libraryPath", "qbittorrent.url", "qbittorrent.username", "qbittorrent.password", "tailscaleOn", "tailscalePort"]) {
    assert.ok(names.includes(name), "missing input " + name);
  }
  const ids = walk(api.root).map((n) => n.id).filter(Boolean);
  assert.equal(new Set(ids).size, ids.length, "duplicate ids in the settings view");
  for (const id of ["arr-fields", "sessions", "remote-status", "remote-link", "updates-state"]) {
    assert.ok(ids.includes(id), "missing slot #" + id);
  }
});

test("the settings module stays a plain script with no app imports", () => {
  assert.ok(!/^\s*(import|export)\s/m.test(source), "settings-view.js must not use modules");
  assert.ok(!/\brequire\(/.test(source), "settings-view.js must not require()");
  assert.ok(!/\bfetch\(/.test(source), "settings-view.js must not call the API itself");
});
