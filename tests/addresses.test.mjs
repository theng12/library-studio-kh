import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import assert from "node:assert/strict";
import test from "node:test";

function fixture(clipboard, fallback = () => true) {
  const elements = new Map();
  const element = id => {
    if (!elements.has(id)) elements.set(id, { value: "", disabled: false, focus() {}, select() { this.selected = true; }, setSelectionRange(start, end) { this.selection = [start, end]; } });
    return elements.get(id);
  };
  const listeners = {};
  const document = { getElementById: element, execCommand: fallback, addEventListener(type, fn) { listeners[type] = fn; } };
  const context = { document, navigator: { clipboard } };
  runInNewContext(readFileSync(new URL("../internal/server/web/addresses.js", import.meta.url), "utf8"), context);
  return { context, element, listeners };
}

test("copies the full webpage URL through the modern clipboard", async () => {
  let copied;
  const { context, element } = fixture({ writeText: async value => { copied = value; } });
  const input = element("link"), message = element("message"); input.value = "http://[::1]:4567/";
  await context.copyWebAddress(input, message);
  assert.equal(copied, input.value);
  assert.equal(message.textContent, "Link copied.");
});

test("denied clipboard uses HTTP fallback; failure leaves manual selection", async () => {
  let attempts = 0;
  const { context, element } = fixture({ writeText: async () => { throw new Error("denied"); } }, () => { attempts++; return false; });
  const input = element("link"), message = element("message"); input.value = "http://example.invalid:4567/";
  await context.copyWebAddress(input, message);
  assert.equal(attempts, 1);
  assert.equal(input.selected, true);
  assert.deepEqual(input.selection, [0, input.value.length]);
  assert.match(message.textContent, /Automatic copy is unavailable/);
});

test("the Settings Copy button copies its own link into its own message, even though it renders later", async () => {
  let copied;
  const { element, listeners } = fixture({ writeText: async value => { copied = value; } });
  element("remote-link").value = "http://tail.example.invalid:7882/";
  const button = { dataset: { copyAddress: "remote-link", copyMsg: "remote-link-msg" } };
  listeners.click({ target: { closest: () => button }, preventDefault() {} });
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(copied, "http://tail.example.invalid:7882/");
  assert.equal(element("remote-link-msg").textContent, "Link copied.");
});

test("a click elsewhere copies nothing", async () => {
  let copied = false;
  const { listeners } = fixture({ writeText: async () => { copied = true; } });
  listeners.click({ target: { closest: () => null }, preventDefault() {} });
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(copied, false);
});

test("Activity no longer carries the web-address disclosure; Settings offers the copy", () => {
  const html = readFileSync(new URL("../internal/server/web/index.html", import.meta.url), "utf8");
  const settings = readFileSync(new URL("../internal/server/web/settings-view.js", import.meta.url), "utf8");
  assert.ok(!html.includes('id="web-addresses"'), "the status detail is off the Activity page");
  assert.match(settings, /id: "remote-link"[^}]*copy: "Copy phone link"/);
});
