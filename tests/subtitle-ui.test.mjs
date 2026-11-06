import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import assert from "node:assert/strict";
import test from "node:test";

function fixture() {
  const elements = new Map(), pending = [];
  const $ = selector => {
    if (!elements.has(selector)) elements.set(selector, { hidden: false, disabled: false, textContent: "", children: [], replaceChildren() { this.children = []; }, append(child) { this.children.push(child); }, addEventListener() {} });
    return elements.get(selector);
  };
  const context = { $, mediaSerial: 1, URLSearchParams, api: () => new Promise(resolve => pending.push(resolve)), mediaNode: (tag, text) => ({ tag, text }) };
  runInNewContext(readFileSync(new URL("../internal/server/web/subtitles.js", import.meta.url), "utf8"), context);
  return { context, $, pending };
}
const empty = { enabled: true, configured: true, version: "1.6.2", problems: [], profiles: [], providers: ["fixture"], items: [] };

test("all reported audio languages participate in display and filters", () => {
  const { context } = fixture();
  const item = { audioLanguage: "French", audioLanguages: ["French", "English"] };
  assert.equal(context.subtitleAudioLanguages(item).join(", "), "French, English");
  assert.equal(context.subtitleAudioMatches(item, "english"), true);
  assert.equal(context.subtitleAudioMatches(item, "other"), true);
  assert.equal(context.subtitleAudioMatches(item, "unknown"), false);
  assert.equal(context.subtitleAudioMatches({ audioLanguage: "English" }, "english"), true);
  assert.equal(context.subtitleAudioMatches({ audioLanguages: ["Unknown", ""] }, "unknown"), true);
  assert.equal(context.subtitleAudioMatches({}, "english"), false);
});

test("overlapping status reads render only the latest result", async () => {
  const { context, $, pending } = fixture();
  const old = context.loadMediaSubtitles({ app: "Radarr", id: 1 });
  const latest = context.loadMediaSubtitles({ app: "Radarr", id: 1 });
  pending[1](empty); await latest;
  pending[0]({ ...empty, problems: ["stale error"] }); await old;
  assert.equal($("#subtitles-content").children.length, 1);
  assert.equal($("#subtitles-message").textContent, "");
  assert.equal($("#subtitles-refresh").disabled, false);
});

test("a previous title cannot overwrite a newly opened title", async () => {
  const { context, $, pending } = fixture();
  const old = context.loadMediaSubtitles({ app: "Sonarr", id: 1 });
  context.mediaSerial = 2;
  const latest = context.loadMediaSubtitles({ app: "Radarr", id: 2 });
  pending[0]({ ...empty, problems: ["old title"] }); await old;
  assert.equal($("#subtitles-content").children.length, 0);
  pending[1](empty); await latest;
  assert.equal($("#subtitles-content").children.length, 1);
  assert.equal($("#subtitles-message").textContent, "");
});
