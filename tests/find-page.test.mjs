import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";

const app = readFileSync(new URL("../internal/server/web/app.js", import.meta.url), "utf8");
const html = readFileSync(new URL("../internal/server/web/index.html", import.meta.url), "utf8");
const css = readFileSync(new URL("../internal/server/web/pages.css", import.meta.url), "utf8");

function helper(name) {
  const match = app.match(new RegExp("function " + name + "\\([\\s\\S]*?\\n\\}"));
  assert.ok(match, name + " must stay a plain testable helper");
  const context = {};
  runInNewContext(match[0], context);
  return context[name];
}

test("torrent links are checked line by line and a bad line is named", () => {
  const reason = helper("torrentInputReason");
  assert.equal(reason("", 0), "Paste a link or choose at least one .torrent file.");
  assert.equal(reason("magnet:?xt=urn:btih:abc", 0), "");
  assert.equal(reason("https://example.test/file.torrent", 0), "");
  assert.equal(reason("", 2), "");
  assert.match(reason("not a link", 0), /^Line 1 isn’t a magnet or web link\.$/);
  assert.match(reason("magnet:?xt=urn:btih:abc\nnonsense", 1), /^Line 2 isn’t a magnet or web link\.$/);
});

test("the ready line counts valid links and chosen files", () => {
  const ready = helper("torrentReadyText");
  assert.equal(ready("", 0), "");
  assert.equal(ready("magnet:?xt=urn:btih:abc", 0), "1 link ready.");
  assert.equal(ready("magnet:?xt=urn:btih:abc\nhttps://example.test/f.torrent", 0), "2 links ready.");
  assert.equal(ready("", 2), "2 files ready.");
  assert.equal(ready("https://example.test/f.torrent", 1), "1 link and 1 file ready.");
});

test("Find & add lays out one full-width search row and a sized type control", () => {
  assert.match(html, /class="tools find-tools"/);
  assert.match(html, /class="page-body find-body"/);
  assert.match(html, /<label class="find-type">Type <select id="find-type">/);
  assert.match(html, /<option value="Radarr">Movies<\/option>/);
  assert.match(html, /<p id="find-count" class="find-count" role="status" hidden><\/p>/);
  assert.match(css, /\.find-tools \.search \{ flex-basis: 100%; \}/);
  assert.match(css, /\.find-tools \.find-type \{ flex: 0 0 11rem; \}/);
  assert.match(css, /\.find-body \{ display: grid; gap: var\(--space-4\); \}/);
});

test("Find states are loading, results, no result and error, each with next words", () => {
  assert.match(app, /msg\.dataset\.state = "loading";/);
  assert.match(app, /msg\.dataset\.state = "empty";/);
  assert.match(app, /msg\.dataset\.state = "error";/);
  assert.match(app, /Searching Sonarr, Radarr and Lidarr for “/);
  assert.match(app, /Try a different title, or add a magnet link below\./);
  assert.match(app, /The search didn’t finish: /);
  assert.match(app, /\+ " for “" \+ q/);
  assert.match(css, /#find-msg\[data-state="error"\]/);
});
