import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(fileURLToPath(new URL(".", import.meta.url)), "..");
const appDirs = ["internal", "cmd", "src-tauri/src"];

function walk(dir, out = []) {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) walk(path, out);
    else out.push(path);
  }
  return out;
}

test("the app never touches the system credential store", () => {
  const pattern = /keychain|secitem|ksecclass/i;
  const offenders = [];
  for (const dir of appDirs) {
    for (const path of walk(join(root, dir))) {
      if (!/\.(go|rs|js|mjs|ts|html|css|plist|json)$/.test(path)) continue;
      if (pattern.test(readFileSync(path, "utf8"))) offenders.push(path);
    }
  }
  assert.deepEqual(offenders, [], "credential-store references in app sources");
});

test("secrets live in one private, atomically written app-data file", () => {
  const config = readFileSync(join(root, "internal/config/config.go"), "utf8");
  assert.match(config, /os\.WriteFile\(tmp, b, 0o600\)/);
  assert.match(config, /os\.Rename\(tmp, s\.path\)/);
  assert.match(config, /filepath\.Join\(dir, "settings\.json"\)/);
});
