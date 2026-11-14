import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import test from "node:test";

const html = readFileSync(new URL("../internal/server/web/index.html", import.meta.url), "utf8");
const js = readFileSync(new URL("../internal/server/web/app.js", import.meta.url), "utf8");

test("the sidebar has one ordering and two standing group headers", () => {
  const nav = html.match(/<nav class="side"[\s\S]*?<\/nav>/)?.[0] || "";
  const views = ["activity", "browse", "find", "downloads", "sources", "cleanup", "settings", "help", "support", "whatsnew"];
  const at = views.map((v) => nav.indexOf('data-view="' + v + '"'));
  assert.ok(at.every((i) => i > 0), "a sidebar destination is missing");
  assert.deepEqual([...at].sort((a, b) => a - b), at, "sidebar destinations are out of order");
  assert.equal((nav.match(/class="group-head"/g) || []).length, 2, "expected the Library and System groups only");
  assert.match(nav, /class="group-head" aria-hidden="true">Library</);
  assert.match(nav, /class="group-head" aria-hidden="true">System</);
});

test("Setup & help is a first-run item that retires once setup is done", () => {
  const nav = html.match(/<nav class="side"[\s\S]*?<\/nav>/)?.[0] || "";
  assert.match(nav, /class="group-head first-run"[^>]*>Get started</);
  assert.match(nav, /<li class="first-run"><button[^>]*data-view="help"/);
  assert.match(js, /document\.querySelectorAll\("\.first-run"\)\) el\.hidden = configured/);
  assert.match(js, /cfg\.qbittorrent\?\.url && \["sonarr", "radarr", "lidarr"\]/);
});

test("Cleanup has one destination and stays reachable after setup", () => {
  const settings = html.match(/<section id="view-settings"[\s\S]*?<\/section>/)?.[0] || "";
  assert.ok(!settings.includes('data-view="cleanup"'), "Settings still duplicates the Cleanup destination");
  const view = readFileSync(new URL("../internal/server/web/settings-view.js", import.meta.url), "utf8");
  assert.match(view, /title: "Help"[\s\S]*view: "help"/);
});

test("group headers never show in the phone tab bar or menu", () => {
  const css = readFileSync(new URL("../internal/server/web/style.css", import.meta.url), "utf8");
  assert.match(css, /@media \(max-width: 760px\) \{[\s\S]*?\.menu \.group-head \{ display: none; \}/);
});
