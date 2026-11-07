import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import assert from "node:assert/strict";
import test from "node:test";

const app = readFileSync(new URL("../internal/server/web/app.js", import.meta.url), "utf8");
const helper = name => app.match(new RegExp(`(?:async )?function ${name}\\([^]*?\\n\\}`))[0];

function addContext(api) {
  const elements = new Map();
  const $ = selector => {
    if (!elements.has(selector)) elements.set(selector, { value: "", checked: false, disabled: false, hidden: false, textContent: "", options: [], replaceChildren() { this.options = []; }, addEventListener() {}, close() {}, showModal() { this.open = true; } });
    return elements.get(selector);
  };
  $("#add-quality").value = "7";
  $("#add-root").value = "/nas/tv";
  $("#add-monitor").checked = true;
  const context = { $, api, pendingAdd: { app: "Sonarr", title: "Example", raw: { tvdbId: 42 } }, addDone: null, addBusy: false, libItems: [], submit: null };
  $("#add-form").addEventListener = (_event, fn) => { context.submit = fn; };
  const submit = app.match(/\$\("#add-form"\)\.addEventListener\("submit", async \(ev\) => \{[\s\S]*?\n\}\);/)[0];
  runInNewContext(helper("addReady") + "\n" + helper("addMonitoringChanged") + "\n" + submit, context);
  return context;
}
const event = { preventDefault() {} };

test("new adds send monitoring and immediate search as separate explicit choices", async () => {
  for (const [monitored, search, expectedSearch] of [[true, false, false], [true, true, true], [false, true, false]]) {
    const requests = [];
    const context = addContext(async (url, body) => { requests.push({ url, body }); return { folder: "/nas/tv" }; });
    context.$("#add-monitor").checked = monitored;
    context.$("#add-search").checked = search;
    await context.submit(event);
    assert.equal(requests.length, 1);
    assert.equal(requests[0].url, "/api/media/add");
    assert.equal(requests[0].body.monitored, monitored);
    assert.equal(requests[0].body.search, expectedSearch);
    assert.equal(requests[0].body.qualityProfileId, 7);
    assert.equal(context.addBusy, false);
    assert.match(context.$("#find-msg").textContent, monitored ? /Monitoring on/ : /Monitoring off/);
    assert.match(context.$("#find-msg").textContent, expectedSearch ? /Search requested/ : /No immediate search/);
  }
});

test("turning monitoring off clears and disables search; turning it on does not opt into downloads", () => {
  const context = addContext(async () => ({}));
  context.$("#add-monitor").checked = false;
  context.$("#add-search").checked = true;
  context.addMonitoringChanged();
  assert.equal(context.$("#add-search").checked, false);
  assert.equal(context.$("#add-search").disabled, true);
  context.$("#add-monitor").checked = true;
  context.addMonitoringChanged();
  assert.equal(context.$("#add-search").disabled, false);
  assert.equal(context.$("#add-search").checked, false);
});

test("changing choices cannot enable a duplicate submission while an add is pending", async () => {
  let resolve, calls = 0;
  const context = addContext(() => { calls++; return new Promise(r => { resolve = r; }); });
  const pending = context.submit(event);
  context.$("#add-search").checked = true;
  context.addReady();
  assert.equal(context.$("#add-form button.primary").disabled, true);
  await context.submit(event);
  assert.equal(calls, 1);
  resolve({ folder: "/nas/tv" });
  await pending;
  assert.equal(context.addBusy, false);
});

test("opening another add resets stale monitoring and search choices to safe defaults", async () => {
  const context = addContext(async () => ({ qualityProfiles: [{ id: 7 }], rootFolders: [{ path: "/nas/tv" }] }));
  context.fillOptions = (select, items) => { select.options = [{}, ...items]; };
  context.$("#add-monitor").checked = false;
  context.$("#add-search").checked = true;
  runInNewContext(helper("openAdd"), context);
  await context.openAdd({ app: "Sonarr", title: "Another show" });
  assert.equal(context.$("#add-monitor").checked, true);
  assert.equal(context.$("#add-search").checked, false);
  assert.equal(context.$("#add-search").disabled, false);
  assert.equal(context.$("#add-monitor-label").textContent, "Monitor all seasons and episodes");
});

test("list monitoring changes use native readback and retain reported state on mismatch", async () => {
  for (const [reported, expected, error] of [[true, true, false], [false, false, true], [undefined, null, true]]) {
    const note = {}, requests = [];
    let renders = 0;
    const context = { URLSearchParams, $: () => note, renderLibrary: () => renders++, api: async (url, body) => { requests.push({ url, body }); return url.includes("details") ? { monitored: reported } : { ok: true }; } };
    runInNewContext(helper("toggleMonitor"), context);
    const item = { app: "Sonarr", id: 7, monitored: false };
    await context.toggleMonitor(item, { checked: true });
    assert.equal(item.monitored, expected);
    assert.equal(requests.length, 2);
    assert.match(requests[1].url, /\/api\/media\/details\?app=Sonarr&id=7/);
    assert.equal(note.hidden, !error);
    assert.equal(renders, 1);
    if (error) assert.match(note.textContent, /did not confirm/);
  }
});

test("a failed write is never presented as a confirmed monitoring change", async () => {
  const note = {};
  const context = { $: () => note, renderLibrary() {}, api: async () => { throw new Error("Sonarr unavailable"); } };
  runInNewContext(helper("toggleMonitor"), context);
  const item = { app: "Sonarr", id: 7, monitored: false };
  await context.toggleMonitor(item, { checked: true });
  assert.equal(item.monitored, null);
  assert.equal(note.hidden, false);
  assert.equal(note.textContent, "Sonarr unavailable");
});
