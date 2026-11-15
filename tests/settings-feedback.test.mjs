import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import assert from "node:assert/strict";
import test from "node:test";

const source = readFileSync(new URL("../internal/server/web/app.js", import.meta.url), "utf8");
const start = source.indexOf('document.addEventListener("settings:tab",');
const end = source.indexOf('\n});', start) + '\n});'.length;

test("switching Settings tabs preserves unsaved status and clears old save feedback", () => {
  const nodes = {
    "#settings-status": { textContent: "Unsaved changes — Save to apply." },
    "#settings-save-msg": { textContent: "Saved." },
    "#form-msg": { textContent: "Saved." },
  };
  let onTab;
  let layouts = 0;
  runInNewContext(source.slice(start, end), {
    document: { addEventListener: (name, handler) => { onTab = handler; } },
    $: (selector) => nodes[selector],
    settingsCardsForPhone: () => { layouts++; },
  });
  onTab({ detail: { id: "general" } });
  assert.equal(nodes["#settings-status"].textContent, "Unsaved changes — Save to apply.");
  assert.equal(nodes["#settings-save-msg"].textContent, "");
  assert.equal(nodes["#form-msg"].textContent, "");
  assert.equal(layouts, 1);
});
