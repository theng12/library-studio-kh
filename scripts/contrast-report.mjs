#!/usr/bin/env node
// WCAG contrast for the app's neutral ramp, status chips and primary button.
//   node scripts/contrast-report.mjs [style.css]
// Prints one line per pairing for both appearances; exits 1 if a body-text or
// chip pairing is below 4.5:1.
import { readFileSync } from "node:fs";

const file = process.argv[2] || new URL("../internal/server/web/style.css", import.meta.url);
const css = readFileSync(file, "utf8");

const block = (selector) => {
  const i = css.indexOf(selector);
  if (i < 0) throw new Error("missing " + selector);
  const start = css.indexOf("{", i);
  const end = css.indexOf("}", start);
  return css.slice(start + 1, end);
};

function tokens(text) {
  const out = {};
  for (const [, name, value] of text.matchAll(/--([a-z0-9-]+):\s*([^;]+);/g)) {
    const v = value.trim();
    const ramp = /^var\(--([a-z0-9-]+)\)$/.exec(v);
    out[name] = ramp ? out[ramp[1]] : v;
  }
  return out;
}

const hex = (v) => {
  const m = /^#([0-9a-f]{6})$/i.exec(String(v).trim());
  if (!m) throw new Error("not a hex colour: " + v);
  const n = parseInt(m[1], 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
};
const lum = ([r, g, b]) => {
  const f = (c) => { c /= 255; return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4; };
  return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b);
};
const ratio = (a, b) => {
  const [x, y] = [lum(hex(a)), lum(hex(b))].sort((m, n) => n - m);
  return (x + 0.05) / (y + 0.05);
};
// color-mix(in srgb, COLOR p%, transparent) over SURFACE = COLOR at p% on SURFACE
const tint = (color, p, surface) => {
  const [c, s] = [hex(color), hex(surface)];
  const mix = c.map((v, i) => Math.round(v * p + s[i] * (1 - p)));
  return "#" + mix.map((v) => v.toString(16).padStart(2, "0")).join("");
};

const themes = {
  light: tokens(block(":root {")),
  dark: tokens(block(':root[data-theme="dark"] {')),
};

const checks = [
  ["body on page", (t) => [t.body, t.page], 4.5],
  ["body on surface", (t) => [t.body, t.surface], 4.5],
  ["secondary on page", (t) => [t.ink2, t.page], 4.5],
  ["secondary on surface", (t) => [t.ink2, t.surface], 4.5],
  ["muted on page", (t) => [t.muted, t.page], 4.5],
  ["muted on surface", (t) => [t.muted, t.surface], 4.5],
  ["primary label on accent", (t) => [t.accentink, t.accent], 4.5],
  ["accent on accent-soft", (t) => [t.accent, t.accentsoft], 4.5],
  ["wait chip", (t) => [t.wait, tint(t.wait, 0.15, t.surface)], 4.5],
  ["ok chip", (t) => [t.connected, tint(t.connected, 0.15, t.surface)], 4.5],
  ["problem chip", (t) => [t.disconnected, tint(t.disconnected, 0.14, t.surface)], 4.5],
  ["neutral chip", (t) => [t.ink2, tint(t.muted, 0.14, t.surface)], 4.5],
  ["warning chip", (t) => [t.warnink, t.warnbg], 4.5],
  ["success banner", (t) => [t.okink, t.okbg], 4.5],
  ["border vs surface", (t) => [t.border, t.surface], 1.0],
  ["surface vs page", (t) => [t.surface, t.page], 1.0],
  ["rail vs page", (t) => [t.rail, t.page], 1.0],
];

const key = (name) => name.replace(/-/g, "");
let failed = 0;
for (const [theme, raw] of Object.entries(themes)) {
  const t = Object.fromEntries(Object.entries(raw).map(([k, v]) => [key(k), v]));
  console.log("== " + theme);
  for (const [name, pick, bar] of checks) {
    const [fg, bg] = pick(t);
    const r = ratio(fg, bg);
    const pass = r >= bar;
    if (!pass) failed++;
    console.log(`${pass ? "PASS" : "FAIL"} ${name}: ${r.toFixed(2)}:1 ${fg} on ${bg}` + (bar > 1 ? ` (bar ${bar})` : ""));
  }
}
process.exit(failed ? 1 : 0);
