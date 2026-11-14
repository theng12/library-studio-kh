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
  assert.match(css, /\.pill-name \{ font-size: var\(--text-xs\)/);
  assert.match(css, /\.pill-version \{[^}]*font-size: var\(--text-xs\)/);
  assert.match(css, /\.side-footer \{[^}]*gap: 6px[^}]*padding-top: 9px/);
});

test("the footer rows share one inset and never wrap", () => {
  assert.match(css, /\.pill \{[^}]*white-space: nowrap[^}]*overflow: hidden/);
  assert.match(css, /\.pill-name \{[^}]*text-overflow: ellipsis/);
  assert.match(css, /\.update-status \{[^}]*white-space: nowrap[^}]*text-overflow: ellipsis/);
  assert.match(css, /\.pillrow \{[^}]*padding: 0 0\.75rem/);
  assert.match(css, /\.update-status \{[^}]*padding: 0 0\.75rem/);
  const nav = html.match(/<nav class="side"[\s\S]*?<\/nav>/)?.[0] || "";
  const support = nav.indexOf('class="support"'), pillrow = nav.indexOf('class="pillrow"'), status = nav.indexOf('id="update-status"');
  assert.ok(support > 0 && pillrow > support && status > pillrow, "footer order changed");
});

test("Settings offers System, Light and Dark and starts on System", () => {
  const view = readFileSync(new URL("../internal/server/web/settings-view.js", import.meta.url), "utf8");
  assert.match(view, /options: \[\["system", "System", true\], \["light", "Light"\], \["dark", "Dark"\]\]/);
  assert.ok(!view.includes('"default", "Default"'), "the retired Default option is still offered");
  assert.match(js, /r\.value === saved/);
  assert.match(js, /c\.appearance === "default" \? "system"/);
  assert.match(js, /saved === "default" \? "system" : saved/);
  assert.match(js, /appearance: f\.querySelector\('input\[name="appearance"\]:checked'\)\?\.value \|\| "system"/);
});

test("Appearance reads as a segmented control and checkboxes are app controls", () => {
  assert.match(css, /#settings-card-appearance \.appearance label:has\(input:checked\)/);
  assert.match(css, /#settings-card-appearance \.appearance input \{[^}]*opacity: 0/);
  assert.match(css, /#settings-card-appearance \.appearance label:has\(input:focus-visible\)/);
  assert.match(css, /label\.check input\[type="checkbox"\] \{[^}]*width: 1\.25rem/);
  assert.match(css, /label\.check:has\(input:focus-visible\)/);
  assert.match(css, /#settings-card-appearance \.appearance \{[^}]*display: inline-flex[^}]*width: auto/);
  assert.match(css, /#settings-card-appearance \.appearance label \{[^}]*flex: 0 0 auto[^}]*min-width: 5\.5rem/);
  assert.match(css, /@media \(max-width: 760px\) \{[\s\S]*?#settings-card-appearance \.appearance label \{ min-height: 44px; \}/);
});

test("appearance reports its own saved state and the General grid has no void", () => {
  const view = readFileSync(new URL("../internal/server/web/settings-view.js", import.meta.url), "utf8");
  const app = readFileSync(new URL("../internal/server/web/app.js", import.meta.url), "utf8");
  const pages = readFileSync(new URL("../internal/server/web/pages.css", import.meta.url), "utf8");
  assert.match(view, /data-saved-for": field\.name/);
  assert.match(app, /\["tailscaleOn", "bazarrOn", "whisparrOn", "deadCleanupOn", "deadArrCleanupOn", "appearance"\]/);
  assert.match(pages, /#settings-card-appearance \{ grid-column: 1; grid-row: 1; \}/);
  assert.match(pages, /#settings-card-help \{ grid-column: 2; grid-row: 1; \}/);
  assert.match(pages, /#settings-card-dead \{ grid-column: 1 \/ span 2; grid-row: 2; \}/);
  assert.match(pages, /#settings-card-connections \{ grid-column: 3; grid-row: 1 \/ span 2; align-self: stretch; \}/);
  assert.match(pages, /#settings-card-folders \{ grid-column: 1 \/ -1; grid-row: 3; \}/);
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
  assert.match(nav, /<p id="update-status"[^>]*role="status"[^>]*aria-live="polite"[^>]*>Updates: not checked yet</);
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
