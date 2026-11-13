import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import assert from "node:assert/strict";
import test from "node:test";

const app = readFileSync(new URL("../internal/server/web/app.js", import.meta.url), "utf8");
const source = [
  app.match(/function storedLibraryView\(\) \{[\s\S]*?\n\}/)[0],
  app.match(/function storeLibraryView\(view\) \{[\s\S]*?\n\}/)[0],
].join("\n");

function helpers(seed) {
  const store = new Map(seed ? [["library-view", seed]] : []);
  const context = { localStorage: { getItem: (k) => store.get(k) ?? null, setItem: (k, v) => store.set(k, String(v)) } };
  runInNewContext(source + "\nthis.storedLibraryView = storedLibraryView; this.storeLibraryView = storeLibraryView;", context);
  return context;
}

test("the Cards/Table choice is remembered across visits", () => {
  assert.equal(helpers().storedLibraryView(), "grid", "cards are the default");
  const ctx = helpers();
  ctx.storeLibraryView("list");
  assert.equal(ctx.storedLibraryView(), "list");
  ctx.storeLibraryView("grid");
  assert.equal(ctx.storedLibraryView(), "grid");
  // A failed storage (private mode) still yields a working default.
  const noStorage = { localStorage: { getItem: () => { throw new Error("denied"); }, setItem: () => { throw new Error("denied"); } } };
  runInNewContext(source + "\nthis.storedLibraryView = storedLibraryView; this.storeLibraryView = storeLibraryView;", noStorage);
  assert.equal(noStorage.storedLibraryView(), "grid");
  assert.doesNotThrow(() => noStorage.storeLibraryView("list"));
});
