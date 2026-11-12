import test from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const script = fileURLToPath(new URL("../scripts/release-retain.py", import.meta.url));
const APP = "Library Studio KH";

function fixture(versions, { ledger = null, trashFiles = [] } = {}) {
  const root = mkdtempSync(join(tmpdir(), "library-rotate-"));
  const dist = join(root, "dist"), trash = join(root, "trash");
  mkdirSync(dist);
  mkdirSync(trash);
  for (const version of versions) writeFileSync(join(dist, `${APP}-${version}.dmg`), version);
  if (ledger) writeFileSync(join(dist, ".release-dist.json"), JSON.stringify(ledger));
  for (const name of trashFiles) writeFileSync(join(trash, name), "older");
  return { root, dist, trash };
}

function rotate({ root, dist, trash }, extra = []) {
  const out = execFileSync("python3", ["-I", script, "--rotate-only", "--dist", dist, "--trash", trash, "--prefix", APP, ...extra], { encoding: "utf8" });
  return JSON.parse(out).rotation;
}

test("dist rotation keeps the two newest installers and moves older ones", () => {
  const f = fixture(["1.2.0", "1.2.1", "1.2.2"]);
  const rotation = rotate(f);
  assert.deepEqual(rotation.kept, [`${APP}-1.2.2.dmg`, `${APP}-1.2.1.dmg`]);
  assert.equal(rotation.moved.length, 1);
  assert.equal(rotation.moved[0].version, "1.2.0");
  assert.match(rotation.moved[0].sha256, /^[0-9a-f]{64}$/);
  assert.ok(rotation.moved[0].bytes > 0);
  assert.equal(rotation.moved[0].to, join(f.trash, `${APP}-1.2.0.dmg`));
  assert.deepEqual(readdirSync(f.dist).sort(), [`${APP}-1.2.1.dmg`, `${APP}-1.2.2.dmg`]);
});

test("a name already in the trash is never overwritten", () => {
  const f = fixture(["1.2.0", "1.2.1", "1.2.2"], { trashFiles: [`${APP}-1.2.0.dmg`] });
  const rotation = rotate(f);
  assert.equal(rotation.moved[0].to, join(f.trash, `${APP}-1.2.0-2.dmg`));
  assert.deepEqual(readdirSync(f.trash).sort(), [`${APP}-1.2.0-2.dmg`, `${APP}-1.2.0.dmg`]);
});

test("files that are not Library installers are left alone", () => {
  const f = fixture(["1.2.1", "1.2.2"]);
  writeFileSync(join(f.dist, "notes.txt"), "keep me");
  const rotation = rotate(f);
  assert.deepEqual(rotation.moved, []);
  assert.deepEqual(readdirSync(f.dist).sort(), [`${APP}-1.2.1.dmg`, `${APP}-1.2.2.dmg`, "notes.txt"]);
});

test("a version named by the ledger is never moved", () => {
  const ledger = { apps: { [APP]: { files: { [`${APP}-1.2.0.dmg`]: { version: "1.2.0", sha256: "x", bytes: 1 } } } } };
  const f = fixture(["1.2.0", "1.2.1", "1.2.2"], { ledger });
  const rotation = rotate(f);
  assert.deepEqual(rotation.moved, []);
  assert.equal(rotation.skipped[0].version, "1.2.0");
  assert.equal(rotation.skipped[0].kept_because, "dist ledger");
  assert.ok(readdirSync(f.dist).includes(`${APP}-1.2.0.dmg`));
});

test("the default destination is the project _trash with the date and what", () => {
  const f = fixture(["1.2.0", "1.2.1", "1.2.2"]);
  const out = execFileSync("python3", ["-I", script, "--rotate-only", "--dist", f.dist, "--project", f.root, "--prefix", APP], { encoding: "utf8" });
  const rotation = JSON.parse(out).rotation;
  assert.match(rotation.trash, /_trash\/\d{4}-\d{2}-\d{2} library dist rotation$/);
  assert.equal(rotation.moved.length, 1);
  assert.equal(readFileSync(rotation.moved[0].to, "utf8"), "1.2.0");
});
