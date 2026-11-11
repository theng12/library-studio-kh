import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";

const source = readFileSync(new URL("../internal/server/web/app.js", import.meta.url), "utf8");
const render = source.slice(source.indexOf("function renderDownloads() {"), source.indexOf('for (const id of ["#dl-q"'));

// Model browser focus loss when replaceChildren disconnects the focused control.
function fixture() {
  const doc = { activeElement: null };
  const node = (type = "div") => ({
    type, value: "", children: [], dataset: {}, options: [],
    focus() { doc.activeElement = this; },
    matches(selector) { return selector === "input[type=checkbox]" && type === "checkbox"; },
    closest() { return this.card; },
    contains(el) { return el === this || this.children.some(c => c.contains(el)); },
    replaceChildren(...children) {
      if (doc.activeElement !== this && this.contains(doc.activeElement)) doc.activeElement = body;
      this.children = children; this.options = children;
    },
  });
  const body = node(); doc.activeElement = body;
  const els = Object.fromEntries(["dl", "dl-state", "dl-app", "dl-sort", "dl-q", "dl-empty", "dl-count"].map(id => ["#" + id, node()]));
  const makeCard = t => {
    const card = node(); card.dataset.hash = t.hash;
    const box = node("checkbox"), summary = node("summary");
    box.card = summary.card = card; card.children = [box, summary];
    card.querySelector = selector => selector === "summary" ? summary : box;
    return card;
  };
  const ctx = vm.createContext({
    document: doc, $: id => els[id], downloads: [{ hash: "a" }, { hash: "b" }],
    DOWNLOAD_FILTERS: [], downloadMatches: () => true, dlApp: x => x,
    mediaOption: (value, text) => Object.assign(node("option"), { value, text }), downloadRows: all => all,
    downloadCard: makeCard, updateDlButtons() {}, dlError: "",
  });
  vm.runInContext(render, ctx);
  const run = () => ctx.renderDownloads(); run();
  return { doc, els, ctx, run, list: els["#dl"] };
}

test("download refresh preserves the focused checkbox by hash after reordering", () => {
  const f = fixture(), old = f.list.children[1].querySelector("input[type=checkbox]");
  old.focus(); f.ctx.downloads.reverse(); f.run();
  assert.notEqual(f.doc.activeElement, old);
  assert.equal(f.doc.activeElement.card.dataset.hash, "b");
  assert.equal(f.list.contains(f.doc.activeElement), true);
  assert.equal(f.doc.activeElement.matches("input[type=checkbox]"), true);
  assert.match(source, /card\.dataset\.hash = t\.hash/);
});

test("download refresh preserves a focused summary", () => {
  const f = fixture(); f.list.children[0].querySelector("summary").focus(); f.run();
  assert.equal(f.doc.activeElement.type, "summary");
  assert.equal(f.doc.activeElement.card.dataset.hash, "a");
});

test("removed focused download falls back to the live list, then search when empty", () => {
  const f = fixture(); f.list.children[0].querySelector("input[type=checkbox]").focus();
  f.ctx.downloads = [{ hash: "b" }]; f.run();
  assert.equal(f.doc.activeElement, f.list);
  f.list.children[0].querySelector("input[type=checkbox]").focus();
  f.ctx.downloads = []; f.run();
  assert.equal(f.doc.activeElement, f.els["#dl-q"]);
});

test("refresh leaves focus in an editor or on the list container alone", () => {
  const f = fixture(); f.els["#dl-q"].focus(); f.run();
  assert.equal(f.doc.activeElement, f.els["#dl-q"]);
  f.list.focus(); f.run();
  assert.equal(f.doc.activeElement, f.list);
});
