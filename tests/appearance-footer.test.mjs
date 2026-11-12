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
  assert.ok(!pill.includes("<svg"), "the reference pill has no glyph");
  assert.ok(!nav.includes("What's new"), "the old What's New link is still present");
  assert.ok(!nav.includes(">Version <"), "the old version text is still present");
});

test("the pill uses the shared footer numbers", () => {
  assert.match(css, /\.pill \{[^}]*min-height: 26px[^}]*padding: 0 7px[^}]*border-radius: 999px/);
  assert.match(css, /\.pill-name \{ font-size: 11px/);
  assert.match(css, /\.pill-version \{ font-size: 10px/);
  assert.match(css, /\.side-footer \{[^}]*gap: 6px[^}]*padding-top: 9px/);
});

test("Settings offers System, Light and Dark and starts on System", () => {
  const panel = html.match(/<fieldset class="panel" id="appearance-panel">[\s\S]*?<\/fieldset>/)?.[0] || "";
  const values = [...panel.matchAll(/name="appearance" value="([a-z]+)"/g)].map((m) => m[1]);
  assert.deepEqual(values, ["system", "light", "dark"]);
  assert.ok(!panel.includes('value="default"'), "the retired Default option is still offered");
  assert.match(js, /r\.value === saved/);
  assert.match(js, /c\.appearance === "default" \? "system"/);
  assert.match(js, /saved === "default" \? "system" : saved/);
  assert.match(js, /appearance: f\.querySelector\('input\[name="appearance"\]:checked'\)\?\.value \|\| "system"/);
});

test("a saved theme drives the palette instead of the OS query alone", () => {
  assert.match(css, /:root\[data-theme="dark"\] \{/);
  assert.match(css, /@media \(prefers-color-scheme: dark\) \{\s*:root:not\(\[data-theme\]\)/);
});

test("Check for updates is icon-only and the update button starts hidden", () => {
  const nav = html.match(/<nav class="side"[\s\S]*?<\/nav>/)?.[0] || "";
  const check = nav.match(/<button[^>]*id="check-updates"[^>]*>/)?.[0] || "";
  assert.match(check, /aria-label="Check for updates"/);
  assert.match(check, /title="Check for updates"/);
  assert.ok(!/>Check for updates</.test(nav), "the check button shows text");
  const update = nav.match(/<button[^>]*id="open-update"[^>]*>/)?.[0] || "";
  assert.match(update, /\bhidden\b/);
  assert.match(nav, /<p id="update-status"[^>]*role="status"[^>]*aria-live="polite"[^>]*>Not checked</);
});

test("the update check speaks the five contract lines and hands off to the launcher", () => {
  assert.match(js, /"Checked just now · up to date"/);
  assert.match(js, /" is available"/);
  assert.match(js, /"Not offered by this Update Studio"/);
  assert.match(js, /"Check failed: "/);
  assert.match(js, /"Checking\.\.\."/);
  assert.match(js, /"Update to " \+ version/);
  assert.match(js, /api\("\/api\/update\/open", \{\}, true\)/);
});
