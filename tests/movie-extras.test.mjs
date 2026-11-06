import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import assert from "node:assert/strict";
import test from "node:test";

function extras(initial = {}) {
  const context = { URLSearchParams, ...initial };
  runInNewContext(readFileSync(new URL("../internal/server/web/movie-extras.js", import.meta.url), "utf8"), context);
  return context;
}

test("library state never treats isExisting as a file", () => {
  const m = extras();
  assert.equal(m.movieExtrasLibraryState({ haveFile: true, inLibrary: true }), "Owned");
  assert.equal(m.movieExtrasLibraryState({ haveFile: false, inLibrary: true }), "In library · no file");
  assert.equal(m.movieExtrasLibraryState({ haveFile: false, inLibrary: false }), "Missing");
  assert.equal(m.movieExtrasLibraryState({ haveFile: null, inLibrary: false, isExisting: true }), "Status unknown");
  assert.equal(m.movieExtrasLibraryState({}), "Status unknown");
});

test("release labels distinguish released, in cinemas and announced", () => {
  const m = extras();
  assert.equal(m.movieExtrasRelease({ status: "released", year: 2018 }), "Released 2018");
  assert.equal(m.movieExtrasRelease({ status: "inCinemas", year: 2023 }), "In cinemas 2023");
  assert.equal(m.movieExtrasRelease({ status: "announced", year: 2027 }), "Announced 2027");
  assert.equal(m.movieExtrasRelease({ status: "tba", year: 2028 }), "Announced 2028");
  assert.equal(m.movieExtrasRelease({ status: "deleted" }), "Removed from Radarr");
  assert.equal(m.movieExtrasRelease({}), "Not reported");
});

test("collection summary reports unknown instead of zero when the catalog failed", () => {
  const m = extras();
  assert.equal(m.movieExtrasSummary({ ownedCount: 1, noFileCount: 2, missingCount: 3 }, true), "1 owned · 2 in library without a file · 3 missing");
  assert.match(m.movieExtrasSummary({}, false), /unknown/);
});

test("artwork keeps the bootstrap token in a header, never the URL", () => {
  const m = extras({ token: "bootstrap-token", sessionStorage: { getItem: () => "" } });
  const req = m.movieExtrasArtworkRequest({ id: 13 });
  assert.equal(req.url, "/api/media/artwork?app=Radarr&id=13&name=poster.jpg");
  assert.equal(req.init.credentials, "same-origin");
  assert.equal(req.init.headers["X-Library-Token"], "bootstrap-token");
  assert.equal(req.init.headers["X-Library-Request"], "1");
  assert.ok(!/token|bootstrap/i.test(req.url));
});

test("artwork falls back to the stored session token and omits an empty header", () => {
  const stored = extras({ sessionStorage: { getItem: k => k === "token" ? "stored-token" : null } });
  assert.equal(stored.movieExtrasArtworkRequest({ id: 13 }).init.headers["X-Library-Token"], "stored-token");
  const none = extras();
  assert.equal("X-Library-Token" in none.movieExtrasArtworkRequest({ id: 13 }).init.headers, false);
});

test("concurrent expanded rows keep independent stale tokens", () => {
  const m = extras();
  const a = { isConnected: true }, b = { isConnected: true };
  const a1 = m.movieExtrasBegin(a);
  const a2 = m.movieExtrasBegin(a);
  const b1 = m.movieExtrasBegin(b);
  assert.equal(m.movieExtrasIsCurrent(a1.state, a1.request, a), false);
  assert.equal(m.movieExtrasIsCurrent(a2.state, a2.request, a), true);
  assert.equal(m.movieExtrasIsCurrent(b1.state, b1.request, b), true);

  const a3 = m.movieExtrasBegin(a); // rebuilding row A must not touch row B
  assert.equal(m.movieExtrasIsCurrent(a2.state, a2.request, a), false);
  assert.equal(m.movieExtrasIsCurrent(a3.state, a3.request, a), true);
  assert.equal(m.movieExtrasIsCurrent(b1.state, b1.request, b), true);

  b.isConnected = false;
  assert.equal(m.movieExtrasIsCurrent(b1.state, b1.request, b), false);
});

test("poster object URLs are released per row, never cross-row", () => {
  const revoked = [];
  const m = extras({ URL: { createObjectURL: () => "blob:new", revokeObjectURL: url => revoked.push(url) } });
  const first = { request: 1, posterURL: "blob:one" }, second = { request: 1, posterURL: "blob:two" };
  m.movieExtrasClearPoster(first);
  assert.deepEqual(revoked, ["blob:one"]);
  assert.equal(first.posterURL, "");
  assert.equal(second.posterURL, "blob:two");
  m.movieExtrasClearPoster(second);
  assert.deepEqual(revoked, ["blob:one", "blob:two"]);
});
