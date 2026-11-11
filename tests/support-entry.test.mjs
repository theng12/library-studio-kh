import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import test from "node:test";

const html = readFileSync(new URL("../internal/server/web/index.html", import.meta.url), "utf8");
const rust = readFileSync(new URL("../src-tauri/src/main.rs", import.meta.url), "utf8");
const DONATION = "https://nowpayments.io/donation/studiokh";

test("the sidebar footer offers Support above the version pill", () => {
  const nav = html.match(/<nav class="side"[\s\S]*?<\/nav>/)?.[0] || "";
  const support = nav.indexOf('data-view="support"');
  const version = nav.indexOf('data-view="whatsnew"');
  assert.ok(support > 0, "support entry is missing");
  assert.ok(version > support, "support must sit above the version pill");
  assert.match(nav.slice(support - 200, support), /class="support"/);
  assert.match(nav.slice(support, version), /Support this app/);
  assert.match(nav.slice(support, version), /<svg[^>]*aria-hidden="true"/);
});

test("every sidebar entry carries a leading icon and keeps its text label", () => {
  const nav = html.match(/<nav class="side"[\s\S]*?<\/nav>/)?.[0] || "";
  const entries = nav.match(/<button[^>]*>[\s\S]*?<\/button>/g) || [];
  const labelled = entries.filter((b) => b.includes("data-view=") || b.includes("menu-toggle"));
  assert.ok(labelled.length >= 9, "sidebar entries were not found");
  for (const entry of labelled) {
    assert.match(entry, /<svg[^>]*aria-hidden="true"/, "an entry has no icon: " + entry.slice(0, 80));
    assert.match(entry, /<span[^>]*>[^<]*/, "an entry has no visible label: " + entry.slice(0, 80));
  }
});

test("the Support page states the app is free and links the shared donation page", () => {
  assert.match(html, /Library Studio KH is free, with all features unlocked\./);
  const link = html.match(/<a class="support-donate"[^>]*>/)?.[0] || "";
  assert.ok(link.includes('href="' + DONATION + '"'), "donation link is not the shared page");
  assert.match(link, /target="_blank"/);
  assert.match(link, /rel="noopener"/);
});

test("the desktop shell opens only the allow-listed support page externally", () => {
  assert.match(rust, /const SUPPORT_URL: &str = "https:\/\/nowpayments\.io\/donation\/studiokh";/);
  assert.match(rust, /\.on_new_window\(/);
  assert.match(rust, /url\.as_str\(\) == SUPPORT_URL/);
});
