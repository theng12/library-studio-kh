import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import assert from "node:assert/strict";
import test from "node:test";

function extras() {
  const context = {};
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
