import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const web = fileURLToPath(new URL("../internal/server/web", import.meta.url));

// Packaged webviews ignore the browser's confirm/alert/prompt APIs; the app must
// use its own dialogs so a click never silently does nothing.
test("app web code never calls the browser confirm, alert or prompt APIs", () => {
  const offenders = [];
  for (const name of readdirSync(web)) {
    if (!/\.(js|html)$/.test(name)) continue;
    const text = readFileSync(join(web, name), "utf8");
    if (/(?:window\.)?(?:confirm|alert|prompt)\s*\(/.test(text)) offenders.push(name);
  }
  assert.deepEqual(offenders, []);
});

test("the Recyclarr Apply action confirms with an in-app dialog", () => {
  const setup = readFileSync(join(web, "setup.js"), "utf8");
  assert.match(setup, /function setupConfirm\(/);
  assert.match(setup, /await setupConfirm\("Apply this Recyclarr sync now\?"/);
  assert.match(setup, /setupButton\("Keep settings"/);
  assert.match(setup, /setupButton\("Apply sync", "primary"/);
});
