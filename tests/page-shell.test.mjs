import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";

const html = readFileSync(new URL("../internal/server/web/index.html", import.meta.url), "utf8");
const css = readFileSync(new URL("../internal/server/web/pages.css", import.meta.url), "utf8");
const cleanup = readFileSync(new URL("../internal/server/web/cleanup.js", import.meta.url), "utf8");
const view = readFileSync(new URL("../internal/server/web/settings-view.js", import.meta.url), "utf8");

function fakeDoc() {
  const make = (tagName) => ({
    tagName, textContent: "", children: [], attributes: {}, listeners: {}, hidden: false,
    append(...kids) { this.children.push(...kids); },
    setAttribute(k, v) { this.attributes[k] = String(v); },
    getAttribute(k) { return this.attributes[k] ?? null; },
    addEventListener(type, fn) { (this.listeners[type] ||= []).push(fn); },
    focus() {},
  });
  return { createElement: make, dispatchEvent() {} };
}

test("Settings and Cleanup share one sticky save bar", () => {
  assert.match(css, /\.save-bar \{[^}]*position: sticky; top: 0;/);
  assert.match(html, /<div class="save-bar">\s*<button type="submit" form="form" class="primary" id="settings-save">Save<\/button>/);
  assert.match(html, /<div class="save-bar">\s*<button type="submit" form="cleanup-form" class="primary" id="cleanup-save">Save cleanup policy<\/button>/);
  assert.equal(html.match(/id="settings-save"/g).length, 1, "one Settings Save control");
  assert.equal(html.match(/id="cleanup-save"/g).length, 1, "one Cleanup Save control");
});

test("Cleanup runs inside the shared tabbed shell", () => {
  assert.match(html, /<div id="cleanup-tabs">/);
  assert.match(html, /<section class="card" id="cleanup-status-card">/);
  assert.match(cleanup, /librarySettingsView\?\.mountTabbed/);
  assert.match(cleanup, /\{ id: "policy", label: "Policy", node: \$\("#cleanup-form"\) \}/);
  assert.match(cleanup, /\{ id: "status", label: "Status & preview"/);
  assert.match(cleanup, /\{ id: "companions", label: "Companion apps"/);
});

test("mountTabbed turns existing blocks into accessible tabs", () => {
  const doc = fakeDoc();
  const context = { window: {}, document: doc };
  runInNewContext(view, context);
  const mount = context.window.librarySettingsView.mountTabbed;
  assert.equal(typeof mount, "function");

  const first = doc.createElement("div");
  const second = doc.createElement("div");
  const host = doc.createElement("div");
  const api = mount(host, [
    { id: "one", label: "One", node: first },
    { id: "two", label: "Two", node: second },
  ], "Test sections", doc);

  const root = api.root;
  const tablist = root.children[0];
  const buttons = tablist.children;
  assert.equal(buttons.length, 2);
  assert.equal(buttons[0].attributes.role, "tab");
  assert.equal(buttons[0].attributes["aria-selected"], "true");
  assert.equal(buttons[1].attributes["aria-selected"], "false");
  const panels = root.children.slice(1);
  assert.equal(panels[0].hidden, false);
  assert.equal(panels[1].hidden, true);
  assert.equal(panels[0].children[0], first, "the existing block is moved into its panel");
  assert.equal(panels[1].children[0], second);

  buttons[1].listeners.click[0]();
  assert.equal(buttons[0].attributes["aria-selected"], "false");
  assert.equal(buttons[1].attributes["aria-selected"], "true");
  assert.equal(panels[1].hidden, false);
});

test("every page uses one frame and one card gap", () => {
  for (const view of ["activity", "downloads", "browse", "media", "sources", "find", "cleanup", "settings", "help", "whatsnew", "support"]) {
    const start = html.indexOf('id="view-' + view + '"');
    assert.ok(start > 0, view + " view exists");
    const block = html.slice(start, html.indexOf("<section id=\"view-", start + 10) > 0
      ? html.indexOf("<section id=\"view-", start + 10)
      : html.indexOf("</main>", start));
    assert.match(block, /class="page"/, view + " uses the shared page frame");
    assert.match(block, /class="page-head"/, view + " uses the shared page head");
    assert.match(block, /class="page-body (stack|find-body)"/, view + " uses the shared page body");
  }
  assert.match(css, /\.page-body\.stack \{ display: grid; gap: var\(--space-4\); align-content: start; \}/);
});
