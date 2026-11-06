import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import assert from "node:assert/strict";
import test from "node:test";

function load() {
  const context = { window: {} };
  runInNewContext(readFileSync(new URL("../internal/server/web/source-manager.js", import.meta.url), "utf8"), context);
  return context;
}

test("blank and no-longer-offered selects are omitted so an edit keeps the native value", () => {
  const context = load();
  const container = {
    querySelectorAll: () => [
      { dataset: { sourceField: "categories", sourceType: "select", sourceSecret: "" }, value: "", checked: false },
      { dataset: { sourceField: "username", sourceType: "text", sourceSecret: "" }, value: "owner", checked: false },
      { dataset: { sourceField: "freeleech", sourceType: "bool", sourceSecret: "" }, value: "", checked: true },
      { dataset: { sourceField: "apiKey", sourceType: "password", sourceSecret: "1" }, value: "", checked: false },
    ],
  };
  assert.deepEqual({ ...context.collectSourceFields(container, true) }, { username: "owner", freeleech: true });
});

test("a successful add or save reloads the configured list and the existing status report", async () => {
  const context = load();
  const calls = [];
  await context.reloadAfterSourceChange(async () => calls.push("list"), async () => calls.push("status"));
  assert.deepEqual(calls, ["list", "status"]);
});

test("configured rows report availability and disabledUntil without inferring health", () => {
  const context = load();
  const enabled = context.sourceAvailability({ enabled: true });
  assert.equal(enabled.setting, "Enabled");
  assert.equal(enabled.disabledUntil, "Not reported");
  const paused = context.sourceAvailability({ enabled: true, disabledUntil: "2026-11-06T10:00:00Z" });
  assert.equal(paused.setting, "Enabled");
  assert.notEqual(paused.disabledUntil, "Not reported");
  assert.deepEqual({ ...context.sourceAvailability({ enabled: null }) }, { setting: "Not reported", disabledUntil: "Not reported" });
  assert.deepEqual({ ...context.sourceAvailability({ enabled: false }) }, { setting: "Disabled", disabledUntil: "Not reported" });
  assert.ok(!JSON.stringify([enabled, paused]).toLowerCase().includes("healthy"));
});
