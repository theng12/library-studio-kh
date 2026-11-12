import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import assert from "node:assert/strict";
import test from "node:test";

const keyboardSource = readFileSync(new URL("../internal/server/web/keyboard.js", import.meta.url), "utf8");
const appSource = readFileSync(new URL("../internal/server/web/app.js", import.meta.url), "utf8");
const indexHtml = readFileSync(new URL("../internal/server/web/index.html", import.meta.url), "utf8");

class FakeElement {
  constructor(tag = "div", props = {}) {
    this.tagName = tag.toUpperCase();
    this.id = "";
    this.type = "text";
    this.hidden = false;
    this.checked = false;
    this.disabled = false;
    this.isContentEditable = false;
    this.attributes = {};
    this.children = [];
    this.parentNode = null;
    this.textContent = "";
    this._listeners = {};
    Object.assign(this, props);
  }
  addEventListener(type, fn) { (this._listeners[type] ||= []).push(fn); }
  dispatchEvent(ev) {
    for (const fn of this._listeners[ev.type] || []) fn(ev);
    return true;
  }
  focus() { this.focused = true; if (this.ownerDocument) this.ownerDocument.activeElement = this; }
  contains(node) {
    let n = node;
    while (n) { if (n === this) return true; n = n.parentNode; }
    return false;
  }
  select() { this.selected = true; }
  setAttribute(name, value) { this.attributes[name] = String(value); }
  getAttribute(name) { return this.attributes[name] ?? null; }
  matches(sel) {
    if (sel.includes(",")) return sel.split(",").some((part) => this.matches(part.trim()));
    if (/^\[[a-z-]+\]$/.test(sel)) return sel.slice(1, -1) in this.attributes;
    const m = sel.match(/^(?:([a-z]+))?\[([a-z-]+)=(?:"([^"]*)"|([^"\]]+))\]$/);
    if (m) {
      const value = m[3] ?? m[4];
      const actual = m[2] === "type" ? this.type : this.attributes[m[2]];
      return (!m[1] || this.tagName === m[1].toUpperCase()) && actual === value;
    }
    if (sel.startsWith("#")) return this.id === sel.slice(1);
    return this.tagName === sel.toUpperCase();
  }
  closest(sel) {
    let node = this;
    while (node) {
      if (node.matches && sel.split(",").some((p) => node.matches(p.trim()))) return node;
      node = node.parentNode;
    }
    return null;
  }
  querySelectorAll(sel) {
    const out = [];
    const walk = (node) => {
      for (const child of node.children) {
        if (child.matches(sel)) out.push(child);
        walk(child);
      }
    };
    walk(this);
    return out;
  }
  querySelector(sel) { return this.querySelectorAll(sel)[0] || null; }
}

const SEARCH_IDS = ["act-q", "dl-q", "lib-q", "find-q", "media-child-q"];
const KBD_IDS = ["kbd-mod", "kbd-find", "kbd-settings", "kbd-help", "kbd-refresh", "kbd-select-all"];

function makeDoc({ view = "activity", gateHidden = true, shellHidden = false, dialogs = [], downloads = null, platform = "MacIntel" } = {}) {
  const doc = {
    activeElement: new FakeElement("body"),
    views: [],
    dialogs,
    elements: new Map(),
    _listeners: {},
    platform,
    addEventListener(type, fn) { (this._listeners[type] ||= []).push(fn); },
    createRange() { return { selectNodeContents() {}, collapse() {} }; },
    getSelection() { return { removeAllRanges() {}, addRange() {} }; },
    dispatch(ev) { for (const fn of this._listeners[ev.type] || []) fn(ev); },
    getElementById(id) { return this.elements.get(id) || null; },
    querySelector(sel) {
      if (sel === ".view:not([hidden])") return this.views.find((v) => !v.hidden) || null;
      if (sel === "dialog[open]") return this.dialogs.find((d) => d.open) || null;
      if (sel.includes(" ")) {
        const [idSel, rest] = sel.split(" ", 2);
        const el = idSel.startsWith("#") ? this.elements.get(idSel.slice(1)) : null;
        return el ? el.querySelector(rest) : null;
      }
      if (sel.startsWith("#")) return this.elements.get(sel.slice(1)) || null;
      return null;
    },
    querySelectorAll(sel) { return sel === "dialog" ? this.dialogs : []; },
  };
  const add = (el) => { doc.elements.set(el.id, el); return el; };
  add(new FakeElement("div", { id: "gate", hidden: gateHidden }));
  add(new FakeElement("div", { id: "app-shell", hidden: shellHidden }));
  for (const name of ["activity", "downloads", "browse", "media", "find", "settings", "help"]) {
    const section = add(new FakeElement("section", { id: "view-" + name, hidden: name !== view }));
    const heading = new FakeElement("h1", { parentNode: section, ownerDocument: doc });
    section.children.push(heading);
    doc.views.push(section);
  }
  for (const id of [...SEARCH_IDS, ...KBD_IDS]) add(new FakeElement("input", { id }));
  if (downloads) {
    // Real elements report to their document when focused.
    const wire = (node) => { node.ownerDocument = doc; for (const child of node.children) wire(child); };
    wire(downloads);
    add(downloads);
  }
  return doc;
}

function makeList({ hidden = false, count = 0, disabled = [] } = {}) {
  const list = new FakeElement("div", { id: "dl", hidden });
  const boxes = [];
  for (let i = 0; i < count; i++) {
    const card = new FakeElement("article", { parentNode: list });
    const box = new FakeElement("input", { type: "checkbox", attributes: { type: "checkbox" }, parentNode: card });
    box.disabled = disabled.includes(i);
    card.children.push(box);
    list.children.push(card);
    boxes.push(box);
  }
  return { list, boxes };
}

function keyEvent(opts = {}) {
  return {
    type: "keydown", key: "", ctrlKey: false, metaKey: false, altKey: false, shiftKey: false,
    isComposing: false, defaultPrevented: false, repeat: false, keyCode: 0,
    preventDefault() { this.defaultPrevented = true; },
    ...opts,
  };
}

function press(doc, ev, target) {
  doc.activeElement = target;
  ev.target = target;
  doc.dispatch(ev);
  return ev;
}

function setup(opts = {}) {
  const doc = makeDoc(opts);
  const calls = { show: [], loadDownloads: [], loadLibrary: 0, load: 0 };
  const context = {
    window: {}, document: doc,
    navigator: { platform: opts.platform || doc.platform, userAgent: "test" },
    Element: FakeElement,
    Event: class Event { constructor(type, init = {}) { this.type = type; Object.assign(this, init); } },
    show: (v) => calls.show.push(v),
    loadDownloads: (preserve) => calls.loadDownloads.push(preserve),
    loadLibrary: () => { calls.loadLibrary += 1; },
    load: () => { calls.load += 1; },
    console,
  };
  runInNewContext(keyboardSource, context);
  const kb = context.window.libraryKeyboard(doc);
  return { doc, kb, calls };
}

const cmd = (key, extra = {}) => keyEvent({ key, metaKey: true, ...extra });

/* --- search, settings, help, refresh --- */

test("Cmd+F searches the active screen and selects the query", () => {
  const { doc, calls } = setup({ view: "activity" });
  const ev = press(doc, cmd("f"), new FakeElement("body"));
  assert.equal(ev.defaultPrevented, true);
  const input = doc.getElementById("act-q");
  assert.equal(input.focused, true);
  assert.equal(input.selected, true);
  assert.deepEqual(calls.show, []);
});

test("Ctrl+F works on Windows the same way", () => {
  const { doc, calls } = setup({ view: "downloads", platform: "Win32" });
  const ev = press(doc, keyEvent({ key: "f", ctrlKey: true }), new FakeElement("body"));
  assert.equal(ev.defaultPrevented, true);
  assert.equal(doc.getElementById("dl-q").focused, true);
  assert.deepEqual(calls.show, []);
});

test("Cmd+F never hijacks a search box behind a hidden filter bar", () => {
  // Current markup: on film details #media-filters stays hidden while its
  // #media-child-q input still exists in the document.
  const { doc } = setup({ view: "media" });
  const input = doc.getElementById("media-child-q");
  const filters = new FakeElement("div", { id: "media-filters", hidden: true });
  filters.setAttribute("hidden", "");
  filters.children.push(input);
  input.parentNode = filters;

  const ev = press(doc, cmd("f"), new FakeElement("body"));
  assert.equal(ev.defaultPrevented, false, "the platform keeps Cmd+F");
  assert.equal(input.focused, undefined, "an invisible field was not focused");

  // The same screen with the filter bar shown does use the shortcut.
  filters.hidden = false;
  delete filters.attributes.hidden;
  const ev2 = press(doc, cmd("f"), new FakeElement("body"));
  assert.equal(ev2.defaultPrevented, true);
  assert.equal(input.focused, true);

  // A disabled search field is not usable either.
  input.disabled = true;
  input.focused = false;
  const ev3 = press(doc, cmd("f"), new FakeElement("body"));
  assert.equal(ev3.defaultPrevented, false);
  assert.equal(input.focused, false);
});

test("Cmd+F is left alone on a screen with no search box", () => {
  const { doc } = setup({ view: "settings" });
  const ev = press(doc, cmd("f"), new FakeElement("body"));
  assert.equal(ev.defaultPrevented, false);
  assert.equal(doc.getElementById("act-q").focused, undefined);
});

test("Cmd+, opens Settings only outside fields, the gate and dialogs", () => {
  const open = setup({ view: "activity" });
  const ev = press(open.doc, cmd(","), new FakeElement("body"));
  assert.equal(ev.defaultPrevented, true);
  assert.deepEqual(open.calls.show, ["settings"]);

  const editable = setup({ view: "activity" });
  const ev2 = press(editable.doc, cmd(","), new FakeElement("div", { isContentEditable: true }));
  assert.equal(ev2.defaultPrevented, false);
  assert.deepEqual(editable.calls.show, []);

  const gated = setup({ view: "activity", gateHidden: false });
  const ev3 = press(gated.doc, cmd(","), new FakeElement("body"));
  assert.equal(ev3.defaultPrevented, false);
  assert.deepEqual(gated.calls.show, []);

  const dialoged = setup({ view: "activity", dialogs: [new FakeElement("dialog", { open: true })] });
  const ev4 = press(dialoged.doc, cmd(","), new FakeElement("body"));
  assert.equal(ev4.defaultPrevented, false);
  assert.deepEqual(dialoged.calls.show, []);
});

test("Cmd+R refreshes only read-only lists and never the screens without one", () => {
  const downloads = setup({ view: "downloads" });
  const ev = press(downloads.doc, cmd("r"), new FakeElement("body"));
  assert.equal(ev.defaultPrevented, true);
  assert.deepEqual(downloads.calls.loadDownloads, [true]);

  const browse = setup({ view: "browse" });
  const ev2 = press(browse.doc, cmd("r"), new FakeElement("body"));
  assert.equal(ev2.defaultPrevented, true);
  assert.equal(browse.calls.loadLibrary, 1);

  const find = setup({ view: "find" });
  const ev3 = press(find.doc, cmd("r"), new FakeElement("body"));
  assert.equal(ev3.defaultPrevented, false);
  assert.equal(find.calls.load, 0);
});

test("Ctrl+F5 stays a browser reload and is never hijacked", () => {
  const downloads = setup({ view: "downloads" });
  const ev = press(downloads.doc, cmd("F5"), new FakeElement("body"));
  assert.equal(ev.defaultPrevented, false);
  assert.deepEqual(downloads.calls.loadDownloads, []);
  const win = setup({ view: "downloads", platform: "Win32" });
  const ev2 = press(win.doc, keyEvent({ key: "F5", ctrlKey: true }), new FakeElement("body"));
  assert.equal(ev2.defaultPrevented, false);
  assert.deepEqual(win.calls.loadDownloads, []);
});

test("Cmd+? opens Help from outside a field", () => {
  const { doc, calls } = setup({ view: "activity" });
  const ev = press(doc, cmd("?"), new FakeElement("body"));
  assert.equal(ev.defaultPrevented, true);
  assert.deepEqual(calls.show, ["help"]);
});

test("Cmd+Shift+/ types Help while other shifted chords stay untouched", () => {
  const help = setup({ view: "activity" });
  const ev = press(help.doc, cmd("?", { shiftKey: true }), new FakeElement("body"));
  assert.equal(ev.defaultPrevented, true);
  assert.deepEqual(help.calls.show, ["help"]);

  const shifted = setup({ view: "activity" });
  const ev2 = press(shifted.doc, cmd("f", { shiftKey: true }), new FakeElement("body"));
  assert.equal(ev2.defaultPrevented, false);
  assert.equal(shifted.doc.getElementById("act-q").focused, undefined);
});

test("Caps Lock capitals still reach the map, without touching shifted chords", () => {
  // Caps Lock reports key="F"/"R"/"A" with shiftKey=false.
  const search = setup({ view: "activity" });
  const ev = press(search.doc, cmd("F"), new FakeElement("body"));
  assert.equal(ev.defaultPrevented, true);
  assert.equal(search.doc.getElementById("act-q").focused, true);

  const refresh = setup({ view: "downloads" });
  const ev2 = press(refresh.doc, cmd("R"), new FakeElement("body"));
  assert.equal(ev2.defaultPrevented, true);
  assert.deepEqual(refresh.calls.loadDownloads, [true]);

  const { list, boxes } = makeList({ count: 2 });
  const select = setup({ view: "downloads", downloads: list });
  const ev3 = press(select.doc, cmd("A"), boxes[0]);
  assert.equal(ev3.defaultPrevented, true);
  assert.deepEqual(boxes.map((b) => b.checked), [true, true]);

  // An intentionally shifted chord is still not a command.
  const shifted = setup({ view: "activity" });
  const ev4 = press(shifted.doc, cmd("F", { shiftKey: true }), new FakeElement("body"));
  assert.equal(ev4.defaultPrevented, false);
  assert.equal(shifted.doc.getElementById("act-q").focused, undefined);
});

test("Cmd+, and Cmd+? move focus to the destination screen's heading", () => {
  const settings = setup({ view: "downloads", downloads: makeList({ count: 1 }).list });
  const box = settings.doc.getElementById("dl").querySelector("input[type=checkbox]");
  const ev = press(settings.doc, cmd(","), box);
  assert.equal(ev.defaultPrevented, true);
  assert.deepEqual(settings.calls.show, ["settings"]);
  const heading = settings.doc.getElementById("view-settings").querySelector("h1");
  assert.equal(settings.doc.activeElement, heading, "focus left the hidden list for the heading");
  assert.equal(heading.getAttribute("tabindex"), "-1", "the heading can take programmatic focus");

  const help = setup({ view: "downloads", downloads: makeList({ count: 1 }).list });
  const ev2 = press(help.doc, cmd("?"), box);
  assert.equal(ev2.defaultPrevented, true);
  const helpHeading = help.doc.getElementById("view-help").querySelector("h1");
  assert.equal(help.doc.activeElement, helpHeading);
  assert.notEqual(help.doc.activeElement, help.doc.getElementById("dl").querySelector("input[type=checkbox]"));
});

/* --- modifier, editor and platform safety --- */

test("only the platform's primary modifier drives the map", () => {
  const cases = [
    // Mac: Ctrl alone, or both at once, is never the app's Cmd.
    { platform: "MacIntel", combo: { key: "f", ctrlKey: true } },
    { platform: "MacIntel", combo: { key: ",", ctrlKey: true, metaKey: true } },
    { platform: "MacIntel", combo: { key: "a", ctrlKey: true, metaKey: true } },
    // Windows: Meta alone, or both at once, is never the app's Ctrl.
    { platform: "Win32", combo: { key: "f", metaKey: true } },
    { platform: "Win32", combo: { key: ",", ctrlKey: true, metaKey: true } },
  ];
  for (const { platform, combo } of cases) {
    const { doc, calls } = setup({ view: "activity", platform });
    const ev = press(doc, keyEvent(combo), new FakeElement("body"));
    assert.equal(ev.defaultPrevented, false, platform + " " + JSON.stringify(combo));
    assert.equal(doc.getElementById("act-q").focused, undefined);
    assert.deepEqual(calls.show, []);
  }
});

test("editing, Alt, Shift and AltGr combinations are never handled", () => {
  const combos = [
    { key: "c", metaKey: true }, { key: "x", metaKey: true }, { key: "v", metaKey: true },
    { key: "z", metaKey: true }, { key: "Z", metaKey: true, shiftKey: true },
    { key: "s", metaKey: true }, { key: "w", metaKey: true },
    { key: "a", metaKey: true, shiftKey: true }, { key: "f", metaKey: true, altKey: true },
    { key: "f", ctrlKey: true, altKey: true }, { key: "a", ctrlKey: true, altKey: true },
    { key: "F5", metaKey: true, altKey: true },
  ];
  for (const combo of combos) {
    const { doc, calls } = setup({ view: "downloads" });
    const ev = press(doc, keyEvent(combo), new FakeElement("body"));
    assert.equal(ev.defaultPrevented, false, JSON.stringify(combo));
    assert.deepEqual(calls.show, []);
    assert.deepEqual(calls.loadDownloads, []);
  }
});

test("a focused text field keeps Ctrl bindings instead of the app map", () => {
  // Mac Ctrl+A/Ctrl+F are line/word movement inside a field and must not be taken.
  const { doc, calls } = setup({ view: "activity" });
  const field = new FakeElement("input", { type: "text" });
  for (const combo of [{ key: "f", ctrlKey: true }, { key: "a", ctrlKey: true }, { key: ",", metaKey: true }]) {
    const ev = press(doc, keyEvent(combo), field);
    assert.equal(ev.defaultPrevented, false, JSON.stringify(combo));
  }
  assert.equal(doc.getElementById("act-q").focused, undefined);
  assert.deepEqual(calls.show, []);
});

test("every text-entry control counts as editable, a tick box does not", () => {
  const { kb } = setup({ view: "activity" });
  assert.equal(kb.editable(new FakeElement("input", { type: "text" })), true);
  assert.equal(kb.editable(new FakeElement("input", { type: "search" })), true);
  assert.equal(kb.editable(new FakeElement("input", { type: "password" })), true);
  assert.equal(kb.editable(new FakeElement("input", { type: "color" })), true);
  assert.equal(kb.editable(new FakeElement("input", { type: "file" })), true);
  assert.equal(kb.editable(new FakeElement("textarea")), true);
  assert.equal(kb.editable(new FakeElement("select")), true);
  assert.equal(kb.editable(new FakeElement("div", { isContentEditable: true })), true);
  const host = new FakeElement("div", { attributes: { contenteditable: "" }, isContentEditable: true });
  assert.equal(kb.editable(host), true);
  assert.equal(kb.editable(new FakeElement("span", { parentNode: host, isContentEditable: true })), true);
  assert.equal(kb.editable(new FakeElement("div", { attributes: { contenteditable: "false" } })), false);
  assert.equal(kb.editable(new FakeElement("input", { type: "checkbox", attributes: { type: "checkbox" } })), false);
  assert.equal(kb.editable(new FakeElement("input", { type: "radio", attributes: { type: "radio" } })), false);
  assert.equal(kb.editable(new FakeElement("div")), false);
});

test("a contenteditable descendant also keeps every binding", () => {
  const { doc } = setup({ view: "activity" });
  const host = new FakeElement("div", { attributes: { contenteditable: "true" }, isContentEditable: true });
  const child = new FakeElement("span", { parentNode: host, isContentEditable: true });
  for (const combo of [{ key: "f", metaKey: true }, { key: ",", metaKey: true }, { key: "a", metaKey: true }]) {
    const ev = press(doc, keyEvent(combo), child);
    assert.equal(ev.defaultPrevented, false, JSON.stringify(combo));
  }
});

test("IME, key repeat and already-handled events are ignored", () => {
  for (const flags of [{ isComposing: true }, { repeat: true }, { keyCode: 229 }, { defaultPrevented: true }]) {
    const { doc, calls } = setup({ view: "activity" });
    const ev = press(doc, cmd("f", flags), new FakeElement("body"));
    assert.equal(doc.getElementById("act-q").focused, undefined, JSON.stringify(flags));
    assert.equal(doc.getElementById("act-q").selected, undefined, JSON.stringify(flags));
    assert.deepEqual(calls.show, []);
    if (!flags.defaultPrevented) assert.equal(ev.defaultPrevented, false, JSON.stringify(flags));
  }
});

/* --- Cmd/Ctrl+A in the downloads list --- */

test("Cmd+A from a focused download tick box selects the visible downloads", () => {
  // The regression: the tick box is a real input, and the old guard mistook it
  // for a text field and returned before the list was selected.
  const { list, boxes } = makeList({ count: 2 });
  const { doc } = setup({ view: "downloads", downloads: list });
  const changes = [];
  for (const box of boxes) box.addEventListener("change", () => changes.push(box));
  const ev = press(doc, cmd("a"), boxes[1]);
  assert.equal(ev.defaultPrevented, true);
  assert.deepEqual(boxes.map((b) => b.checked), [true, true]);
  assert.equal(changes.length, 2);
  assert.equal(doc.activeElement, boxes[1]);
});

test("Cmd+A ticks only the downloads the current filter rendered", () => {
  const { list, boxes } = makeList({ count: 2 });
  const { doc } = setup({ view: "downloads", downloads: list });
  const changes = [];
  for (const box of boxes) box.addEventListener("change", () => changes.push(box));
  // A monitoring switch lives outside the downloads list and must never be touched.
  const monitor = new FakeElement("input", { type: "checkbox", attributes: { type: "checkbox" }, checked: false });
  doc.elements.set("monitor", monitor);

  const card = list.children[0];
  const ev = press(doc, cmd("a"), card);
  assert.equal(ev.defaultPrevented, true);
  assert.deepEqual(boxes.map((b) => b.checked), [true, true]);
  assert.equal(changes.length, 2);
  assert.equal(monitor.checked, false);
  assert.equal(doc.activeElement, card);
});

test("Cmd+A needs the Downloads view, the signed-in shell and no open dialog", () => {
  const hiddenView = makeList({ count: 2 });
  const behindView = setup({ view: "activity", downloads: hiddenView.list });
  const ev1 = press(behindView.doc, cmd("a"), hiddenView.list);
  assert.equal(ev1.defaultPrevented, false, "another screen in front");
  assert.deepEqual(hiddenView.boxes.map((b) => b.checked), [false, false]);

  const gated = makeList({ count: 2 });
  const gatedDoc = setup({ view: "downloads", gateHidden: false, downloads: gated.list });
  const ev2 = press(gatedDoc.doc, cmd("a"), gated.list);
  assert.equal(ev2.defaultPrevented, false, "sign-in gate");
  assert.deepEqual(gated.boxes.map((b) => b.checked), [false, false]);

  const dialoged = makeList({ count: 2 });
  const dialogDoc = setup({ view: "downloads", downloads: dialoged.list, dialogs: [new FakeElement("dialog", { open: true })] });
  const ev3 = press(dialogDoc.doc, cmd("a"), dialoged.list);
  assert.equal(ev3.defaultPrevented, false, "open dialog");
  assert.deepEqual(dialoged.boxes.map((b) => b.checked), [false, false]);
});

test("Cmd+A from outside the list stays with the platform", () => {
  const { list, boxes } = makeList({ count: 2 });
  const { doc } = setup({ view: "downloads", downloads: list });
  const ev = press(doc, cmd("a"), new FakeElement("body"));
  assert.equal(ev.defaultPrevented, false);
  assert.deepEqual(boxes.map((b) => b.checked), [false, false]);
});

test("a hidden or empty downloads list falls back to the platform", () => {
  const hidden = makeList({ hidden: true, count: 2 });
  const doc1 = setup({ view: "downloads", downloads: hidden.list }).doc;
  const ev = press(doc1, cmd("a"), hidden.list);
  assert.equal(ev.defaultPrevented, false);
  assert.deepEqual(hidden.boxes.map((b) => b.checked), [false, false]);

  const empty = makeList({ count: 0 });
  const doc2 = setup({ view: "downloads", downloads: empty.list }).doc;
  const ev2 = press(doc2, cmd("a"), empty.list);
  assert.equal(ev2.defaultPrevented, false);
});

test("disabled tick boxes are never ticked", () => {
  const { list, boxes } = makeList({ count: 3, disabled: [1] });
  const { doc } = setup({ view: "downloads", downloads: list });
  const changes = [];
  for (const box of boxes) box.addEventListener("change", () => changes.push(box));
  const ev = press(doc, cmd("a"), boxes[0]);
  assert.equal(ev.defaultPrevented, true);
  assert.deepEqual(boxes.map((b) => b.checked), [true, false, true]);
  assert.equal(changes.length, 2);

  const all = makeList({ count: 2, disabled: [0, 1] });
  const allDoc = setup({ view: "downloads", downloads: all.list }).doc;
  const ev2 = press(allDoc, cmd("a"), all.boxes[0]);
  assert.equal(ev2.defaultPrevented, false, "nothing selectable means the platform keeps Cmd+A");
  assert.deepEqual(all.boxes.map((b) => b.checked), [false, false]);
});

test("focus survives a card rebuild that replaces the tick boxes", () => {
  const { list, boxes } = makeList({ count: 3 });
  const { doc } = setup({ view: "downloads", downloads: list });
  // A change handler re-renders the list with brand new cards and tick boxes,
  // and the old nodes are left disconnected.
  for (const box of boxes) box.addEventListener("change", () => {
    if (box !== boxes[0]) return;
    const replacements = makeList({ count: 3 });
    list.children = replacements.list.children;
    for (const node of list.children) {
      node.parentNode = list;
      const wire = (n) => { n.ownerDocument = doc; for (const c of n.children) wire(c); };
      wire(node);
    }
    doc.activeElement = new FakeElement("body");
  });
  const ev = press(doc, cmd("a"), list.children[1]);
  assert.equal(ev.defaultPrevented, true);
  assert.ok(list.querySelectorAll("input[type=checkbox]").length === 3, "new cards are in place");
  assert.ok(doc.activeElement?.closest?.("#dl"), "focus landed on a live node in the list");
  assert.ok(list.contains(doc.activeElement), "the focused node is still connected");
});

test("a refresh that leaves focus inside the list does not move it", () => {
  const { list, boxes } = makeList({ count: 2 });
  const { doc } = setup({ view: "downloads", downloads: list });
  const ev = press(doc, cmd("a"), boxes[0]);
  assert.equal(ev.defaultPrevented, true);
  assert.equal(doc.activeElement, boxes[0], "focus was not disturbed");
});

/* --- Escape precedence --- */

test("Escape acts in its own context and never silently does nothing", () => {
  const listener = appSource.match(/document\.addEventListener\("keydown"[\s\S]*?\n\}\);/);
  assert.ok(listener, "expected the Escape handler in app.js");
  let handler = null;
  const calls = { menu: 0, dialog: 0, cleared: 0 };
  const context = {
    document: {
      addEventListener: (type, fn) => { handler = fn; },
      querySelectorAll: (sel) => (sel === "dialog[open]" ? context.document.openDialogs : []),
      openDialogs: [],
    },
    menuToggle: { getAttribute: () => "true", focus() {} },
    closeMenu: () => { calls.menu += 1; },
    Event: class { constructor(type, options) { this.type = type; this.bubbles = options?.bubbles; } },
  };
  runInNewContext(listener[0], context);
  assert.equal(typeof handler, "function");

  const body = new FakeElement("body");
  handler(keyEvent({ key: "Escape", target: body }));
  assert.equal(calls.menu, 1, "menu closes when nothing else acted");

  const dialog = { close: () => { calls.dialog += 1; } };
  context.document.openDialogs = [dialog];
  const ev = keyEvent({ key: "Escape", target: body });
  handler(ev);
  assert.equal(calls.dialog, 1, "Escape closes the open dialog");
  assert.equal(ev.defaultPrevented, true);
  assert.equal(calls.menu, 1, "the dialog path does not also close the menu");
  context.document.openDialogs = [];

  const search = new FakeElement("input", { type: "search", value: "bluey" });
  search.dispatchEvent = (event) => { if (event.type === "input") calls.cleared += 1; return true; };
  handler(keyEvent({ key: "Escape", target: search }));
  assert.equal(search.value, "", "Escape clears the focused search");
  assert.equal(calls.cleared, 1, "clearing re-runs the filter");
  assert.equal(calls.menu, 1, "clearing a search does not also close the menu");

  search.value = "";
  handler(keyEvent({ key: "Escape", target: search }));
  assert.equal(calls.menu, 1, "an empty search keeps Escape in the field");

  const host = new FakeElement("div", { attributes: { contenteditable: "" }, isContentEditable: true });
  handler(keyEvent({ key: "Escape", target: host }));
  assert.equal(calls.menu, 1, "a contenteditable host keeps Escape");

  handler(keyEvent({ key: "Escape", target: body, isComposing: true }));
  assert.equal(calls.menu, 1, "IME composition keeps Escape");
  handler(keyEvent({ key: "Escape", target: body, keyCode: 229 }));
  assert.equal(calls.menu, 1, "the legacy IME key code keeps Escape");
  handler(keyEvent({ key: "Escape", target: body, defaultPrevented: true }));
  assert.equal(calls.menu, 1, "a handler that already acted wins");
});

/* --- discoverability --- */

test("the shortcut map is discoverable in the shipped markup", () => {
  assert.ok(indexHtml.includes('<script src="keyboard.js" defer></script>'));
  assert.ok(indexHtml.indexOf("keyboard.js") < indexHtml.indexOf('<script src="app.js"'), "keyboard.js loads before app.js");
  for (const id of KBD_IDS) assert.ok(indexHtml.includes('id="' + id + '"'), id);
  assert.ok(indexHtml.includes("<h2>Keyboard</h2>"));
  assert.ok((indexHtml.match(/aria-keyshortcuts="Meta\+F Control\+F"/g) || []).length === 5);
  assert.ok(indexHtml.includes('aria-keyshortcuts="Meta+A Control+A"'));
  assert.ok(indexHtml.includes('aria-keyshortcuts="Meta+,'), "Settings menu");
  assert.ok(indexHtml.includes('aria-keyshortcuts="Meta+? Control+?"'), "help menu");
  assert.ok(/id="dl"[^>]*tabindex="0"/.test(indexHtml), "the list can receive focus");
});