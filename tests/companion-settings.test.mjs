import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import assert from "node:assert/strict";
import test from "node:test";

const context = {};
runInNewContext(readFileSync(new URL("../internal/server/web/companion-settings.js", import.meta.url), "utf8"), context);

test("the companion settings module loads and exposes the mount hook", () => {
  assert.equal(typeof context.mountCompanionSettings, "function");
});

test("only fields the owner changed are sent", () => {
  const baseline = { enableCompletedDownloadHandling: false, renameEpisodes: true };
  const patch = context.companionPatchFor([
    { key: "enableCompletedDownloadHandling", value: true, valid: true },
    { key: "renameEpisodes", value: true, valid: true },
  ], baseline);
  assert.deepEqual({ ...patch }, { enableCompletedDownloadHandling: true });
});

test("an invalid number stops the write before any request", () => {
  assert.throws(
    () => context.companionPatchFor([{ key: "max_ratio", value: NaN, valid: false, error: "Share ratio must be a number." }], {}),
    /Share ratio must be a number\./,
  );
});
