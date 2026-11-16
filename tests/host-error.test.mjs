import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import assert from "node:assert/strict";
import test from "node:test";

const source = readFileSync(new URL("../internal/server/web/app.js", import.meta.url), "utf8");
const start = source.indexOf("function bytes(");
const end = source.indexOf("\nconst stamp", start);
const context = {};
runInNewContext(source.slice(start, end), context);

test("byte formatting rounds fractional low transfer rates", () => {
  assert.equal(context.bytes(3.4133333333333336), "3 B");
  assert.equal(context.bytes(0.4), "0 B");
  assert.equal(context.bytes(1536), "1.5 KB");
});

test("transport failures name the unreachable host; server refusals keep their cause", () => {
  for (const message of ["Failed to fetch", "NetworkError when attempting to fetch resource.", "Load failed"]) {
    assert.equal(context.plainHostError({ message }), "Could not reach the Library host on this computer. Check that it is still running.");
  }
  assert.equal(context.plainHostError({ message: "Provider API key is missing" }), "Provider API key is missing.");
  assert.equal(context.plainHostError({ message: "Provider refused the request." }), "Provider refused the request.");
  assert.equal(context.plainHostError(""), "The Library host did not answer.");
});

// Every page reads through api(), so a stopped host is named in plain words there
// instead of leaving the browser's raw "Load failed" on the library and downloads.
const apiSource = source.slice(source.indexOf("async function api("), source.indexOf("\nconst day = (iso)"));
const page = { token: "", AUTH: { available: false }, signedOut() {} };
runInNewContext(source.slice(start, end), page);
runInNewContext(apiSource, page);

test("a stopped host reaches the page as a plain sentence, not the browser's error", async () => {
  page.fetch = async () => {
    throw new TypeError("Load failed");
  };
  await assert.rejects(() => page.api("/api/state"), {
    message: "Could not reach the Library host on this computer. Check that it is still running.",
  });
});

test("a server refusal keeps its own reason", async () => {
  page.fetch = async () => ({ ok: false, status: 502, json: async () => ({ error: "qBittorrent did not answer" }) });
  await assert.rejects(() => page.api("/api/downloads"), { message: "qBittorrent did not answer" });
});
