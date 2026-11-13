import { spawnSync } from "node:child_process";
import assert from "node:assert/strict";
import test from "node:test";
import { fileURLToPath } from "node:url";

// The palette is only "verified" when the ratios pass; this keeps it that way.
test("neutral ramp, chips and primary button meet WCAG AA in both appearances", () => {
  const script = fileURLToPath(new URL("../scripts/contrast-report.mjs", import.meta.url));
  const run = spawnSync(process.execPath, [script], { encoding: "utf8" });
  assert.equal(run.status, 0, run.stdout + run.stderr);
  assert.match(run.stdout, /== light/);
  assert.match(run.stdout, /== dark/);
  assert.doesNotMatch(run.stdout, /FAIL/);
});
