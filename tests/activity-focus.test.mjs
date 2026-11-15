import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import assert from "node:assert/strict";
import test from "node:test";

const source = readFileSync(new URL("../internal/server/web/app.js", import.meta.url), "utf8");
const start = source.indexOf('$("#details").addEventListener("close",');
const end = source.indexOf('\n});', start) + '\n});'.length;

test("Activity close restores its opener, including a row replaced by polling", () => {
  for (const state of ["connected", "replaced", "removed"]) {
    let focused = null;
    let onClose;
    const opener = { isConnected: state === "connected", dataset: { activityKey: "download-1" }, focus() { focused = this; } };
    const replacement = { dataset: { activityKey: "download-1" }, focus() { focused = this; } };
    const search = { focus() { focused = this; } };
    const context = {
      activityDetailReturn: opener,
      $: (selector) => selector === "#act-q" ? search : { addEventListener: (name, fn) => { onClose = fn; } },
      document: { querySelectorAll: () => state === "removed" ? [] : [replacement] },
    };
    runInNewContext(source.slice(start, end), context);
    onClose();
    assert.equal(focused, state === "connected" ? opener : state === "replaced" ? replacement : search);
    assert.equal(context.activityDetailReturn, null);
  }
});
