import test from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, readdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const script = fileURLToPath(new URL("../scripts/release-retain.py", import.meta.url));

function run(dist, trash) {
  const out = execFileSync("python3", ["-I", script, "--rotate-only", "--dist", dist, "--trash", trash, "--prefix", "Library Studio KH"], { encoding: "utf8" });
  return JSON.parse(out).rotation;
}

test("dist rotation keeps the two newest installers and trashes older ones", () => {
  const root = mkdtempSync(join(tmpdir(), "library-rotate-"));
  const dist = join(root, "dist"), trash = join(root, "trash");
  mkdirSync(dist);
  mkdirSync(trash);
  for (const version of ["0.7.9", "0.7.10", "0.7.11"]) {
    writeFileSync(join(dist, `Library Studio KH-${version}.dmg`), version);
  }
  const rotation = run(dist, trash);
  assert.deepEqual(rotation.kept, ["Library Studio KH-0.7.11.dmg", "Library Studio KH-0.7.10.dmg"]);
  assert.equal(rotation.moved.length, 1);
  assert.match(rotation.moved[0].to, /\.Trash|trash/);
  assert.deepEqual(readdirSync(dist).sort(), ["Library Studio KH-0.7.10.dmg", "Library Studio KH-0.7.11.dmg"]);
});

test("a name already in the trash is never overwritten", () => {
  const root = mkdtempSync(join(tmpdir(), "library-rotate-"));
  const dist = join(root, "dist"), trash = join(root, "trash");
  mkdirSync(dist);
  mkdirSync(trash);
  for (const version of ["0.7.8", "0.7.9", "0.7.10"]) {
    writeFileSync(join(dist, `Library Studio KH-${version}.dmg`), version);
  }
  writeFileSync(join(trash, "Library Studio KH-0.7.8.dmg"), "older");
  const rotation = run(dist, trash);
  assert.equal(rotation.moved[0].to, join(trash, "Library Studio KH-0.7.8-2.dmg"));
  assert.deepEqual(readdirSync(trash).sort(), ["Library Studio KH-0.7.8-2.dmg", "Library Studio KH-0.7.8.dmg"]);
});

test("files that are not Library installers are left alone", () => {
  const root = mkdtempSync(join(tmpdir(), "library-rotate-"));
  const dist = join(root, "dist"), trash = join(root, "trash");
  mkdirSync(dist);
  mkdirSync(trash);
  writeFileSync(join(dist, ".release-dist.json"), "{}");
  writeFileSync(join(dist, "notes.txt"), "keep me");
  for (const version of ["0.7.10", "0.7.11"]) {
    writeFileSync(join(dist, `Library Studio KH-${version}.dmg`), version);
  }
  const rotation = run(dist, trash);
  assert.deepEqual(rotation.moved, []);
  assert.deepEqual(readdirSync(dist).sort(), [".release-dist.json", "Library Studio KH-0.7.10.dmg", "Library Studio KH-0.7.11.dmg", "notes.txt"]);
});
