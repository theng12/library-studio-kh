import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import test from "node:test";

const html = readFileSync(new URL("../internal/server/web/index.html", import.meta.url), "utf8");
const js = readFileSync(new URL("../internal/server/web/app.js", import.meta.url), "utf8");
const css = readFileSync(new URL("../internal/server/web/style.css", import.meta.url), "utf8");

test("the footer pill shows the app name and version and opens What's New", () => {
  const nav = html.match(/<nav class="side"[\s\S]*?<\/nav>/)?.[0] || "";
  const pill = nav.match(/<button[^>]*class="pill"[^>]*>[\s\S]*?<\/button>/)?.[0] || "";
  assert.match(pill, /data-view="whatsnew"/);
  assert.match(pill, /class="pill-name">Library Studio KH</);
  assert.match(pill, /class="pill-version" id="version"/);
  assert.ok(!nav.includes("What's new"), "the old What's New link is still present");
  assert.ok(!nav.includes(">Version <"), "the old version text is still present");
});

test("the pill uses the shared footer numbers", () => {
  assert.match(css, /\.pill \{[^}]*min-height: 26px[^}]*padding: 0 7px[^}]*border-radius: 999px/);
  assert.match(css, /\.pill-name \{ font-size: 11px/);
  assert.match(css, /\.pill-version \{ font-size: 10px/);
  assert.match(css, /\.side-footer \{[^}]*gap: 6px[^}]*padding-top: 9px/);
});

test("Settings offers System, Light, Dark and Default and starts on System", () => {
  const panel = html.match(/<fieldset class="panel" id="appearance-panel">[\s\S]*?<\/fieldset>/)?.[0] || "";
  const values = [...panel.matchAll(/name="appearance" value="([a-z]+)"/g)].map((m) => m[1]);
  assert.deepEqual(values, ["system", "light", "dark", "default"]);
  assert.match(js, /r\.value === \(c\.appearance \|\| "system"\)/);
  assert.match(js, /choice === "system" \? \(darkQuery\.matches \? "dark" : "light"\)/);
  assert.match(js, /appearance: f\.querySelector\('input\[name="appearance"\]:checked'\)\?\.value \|\| "system"/);
});

test("a saved theme drives the palette instead of the OS query alone", () => {
  assert.match(css, /:root\[data-theme="dark"\] \{/);
  assert.match(css, /@media \(prefers-color-scheme: dark\) \{\s*:root:not\(\[data-theme\]\)/);
});
