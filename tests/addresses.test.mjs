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
  const document = { getElementById: element, querySelector: element, querySelectorAll: () => [], execCommand: fallback };
  const context = { document, navigator: { clipboard } };
  runInNewContext(readFileSync(new URL("../internal/server/web/addresses.js", import.meta.url), "utf8"), context);
  return { context, element };
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

test("host and Tailscale addresses stay distinct and contain no bootstrap key", () => {
  const { context, element } = fixture(undefined);
  context.fillWebAddresses({ localUrl: "http://localhost:4567/", remote: { on: true, url: "http://tail.example.invalid:7890/" } });
  assert.equal(element("local-web-address").value, "http://localhost:4567/");
  assert.equal(element("tail-web-address").value, "http://tail.example.invalid:7890/");
  assert.equal(element('[data-copy-address="tail-web-address"]').disabled, false);
});

test("HTTP-only clipboard can succeed; unavailable Tailscale link is disabled", async () => {
  const { context, element } = fixture(undefined);
  context.fillWebAddresses({ localUrl: "http://localhost:4567/", remote: { on: false } });
  assert.equal(element("local-web-address").value, "http://localhost:4567/");
  assert.equal(element('[data-copy-address="tail-web-address"]').disabled, true);
  assert.equal(element("tail-web-address").value, "");
  const message = element("message");
  await context.copyWebAddress(element("local-web-address"), message);
  assert.equal(message.textContent, "Link copied.");
});
