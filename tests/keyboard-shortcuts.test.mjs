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
  select() { this.selected = true; }
  setAttribute(name, value) { this.attributes[name] = String(value); }
  getAttribute(name) { return this.attributes[name] ?? null; }
  matches(sel) {
    if (sel.startsWith("#")) return this.id === sel.slice(1);
    const m = sel.match(/^(?:([a-z]+))?\[([a-z-]+)=(?:"([^"]*)"|([^"\]]+))\]$/);
    if (m) {
      const value = m[3] ?? m[4];
      const actual = m[2] === "type" ? this.type : this.attributes[m[2]];
      return (!m[1] || this.tagName === m[1].toUpperCase()) && actual === value;
    }
    if (sel.includes(",")) return sel.split(",").some((part) => this.matches(part.trim()));
    return this.tagName === sel.toUpperCase();
  }
  closest(sel) {
    let node = this;
    const parts = sel.split(",").map((s) => s.trim());
    while (node) {
      if (parts.some((p) => node.matches(p))) return node;
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
    createRange() {
      return { selectNodeContents() {}, collapse() {} };
    },
    getSelection() {
      return { removeAllRanges() {}, addRange() {} };
    },
    dispatch(ev) { for (const fn of this._listeners[ev.type] || []) fn(ev); },
    getElementById(id) { return this.elements.get(id) || null; },
    querySelector(sel) {
      if (sel === ".view:not([hidden])") return this.views.find((v) => !v.hidden) || null;
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
    doc.views.push(section);
  }
  for (const id of [...SEARCH_IDS, ...KBD_IDS]) add(new FakeElement("input", { id }));
  if (downloads) add(downloads);
  return doc;
}

function makeList({ hidden = false, count = 0 } = {}) {
  const list = new FakeElement("div", { id: "dl", hidden });
  const boxes = [];
  for (let i = 0; i < count; i++) {
    const card = new FakeElement("article", { parentNode: list });
    const box = new FakeElement("input", { type: "checkbox", attributes: { type: "checkbox" }, parentNode: card, ownerDocument: null });
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
  if (opts.downloads) {
    // Real elements report to their document when focused.
    const wire = (node) => { node.ownerDocument = doc; for (const child of node.children) wire(child); };
    wire(opts.downloads);
  }
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

test("Cmd+F searches the active screen and selects the query", () => {
  const { doc, kb, calls } = setup({ view: "activity" });
  const ev = cmd("f");
  doc.activeElement = new FakeElement("body");
  ev.target = doc.activeElement;
  doc.dispatch(ev);
  assert.equal(ev.defaultPrevented, true);
  const input = doc.getElementById("act-q");
  assert.equal(input.focused, true);
  assert.equal(input.selected, true);
  assert.deepEqual(calls.show, []);
});

test("Ctrl+F works on Windows the same way", () => {
  const { doc, calls } = setup({ view: "downloads", platform: "Win32" });
  const ev = keyEvent({ key: "f", ctrlKey: true });
  ev.target = new FakeElement("body");
  doc.activeElement = ev.target;
  doc.dispatch(ev);
  assert.equal(ev.defaultPrevented, true);
  assert.equal(doc.getElementById("dl-q").focused, true);
  assert.deepEqual(calls.show, []);
});

test("Cmd+F is left alone on a screen with no search box", () => {
  const { doc } = setup({ view: "settings" });
  const ev = cmd("f");
  ev.target = new FakeElement("body");
  doc.dispatch(ev);
  assert.equal(ev.defaultPrevented, false);
  assert.equal(doc.getElementById("act-q").focused, undefined);
});

test("Cmd+, opens Settings only outside fields, the gate and dialogs", () => {
  const open = setup({ view: "activity" });
  const ev = cmd(",");
  ev.target = new FakeElement("body");
  open.doc.dispatch(ev);
  assert.equal(ev.defaultPrevented, true);
  assert.deepEqual(open.calls.show, ["settings"]);

  const editable = setup({ view: "activity" });
  const ev2 = cmd(",");
  ev2.target = new FakeElement("div", { isContentEditable: true });
  editable.doc.dispatch(ev2);
  assert.equal(ev2.defaultPrevented, false);
  assert.deepEqual(editable.calls.show, []);

  const gated = setup({ view: "activity", gateHidden: false });
  const ev3 = cmd(",");
  ev3.target = new FakeElement("body");
  gated.doc.dispatch(ev3);
  assert.equal(ev3.defaultPrevented, false);
  assert.deepEqual(gated.calls.show, []);

  const dialoged = setup({ view: "activity", dialogs: [new FakeElement("dialog", { open: true })] });
  const ev4 = cmd(",");
  ev4.target = new FakeElement("body");
  dialoged.doc.dispatch(ev4);
  assert.equal(ev4.defaultPrevented, false);
  assert.deepEqual(dialoged.calls.show, []);
});

test("Cmd+R refreshes only read-only lists and never the screens without one", () => {
  const downloads = setup({ view: "downloads" });
  const ev = cmd("r");
  ev.target = new FakeElement("body");
  downloads.doc.dispatch(ev);
  assert.equal(ev.defaultPrevented, true);
  assert.deepEqual(downloads.calls.loadDownloads, [true]);

  const browse = setup({ view: "browse" });
  const ev2 = cmd("r");
  ev2.target = new FakeElement("body");
  browse.doc.dispatch(ev2);
  assert.equal(ev2.defaultPrevented, true);
  assert.equal(browse.calls.loadLibrary, 1);

  const find = setup({ view: "find" });
  const ev3 = cmd("r");
  ev3.target = new FakeElement("body");
  find.doc.dispatch(ev3);
  assert.equal(ev3.defaultPrevented, false);
  assert.equal(find.calls.load, 0);
});

test("Cmd+? opens Help from outside a field", () => {
  const { doc, calls } = setup({ view: "activity" });
  const ev = cmd("?");
  ev.target = new FakeElement("body");
  doc.dispatch(ev);
  assert.equal(ev.defaultPrevented, true);
  assert.deepEqual(calls.show, ["help"]);
});

test("Cmd+Shift+/ types Help while other shifted chords stay untouched", () => {
  const help = setup({ view: "activity" });
  const ev = cmd("?", { shiftKey: true });
  press(help.doc, ev, new FakeElement("body"));
  assert.equal(ev.defaultPrevented, true);
  assert.deepEqual(help.calls.show, ["help"]);

  // A plain shifted letter is not a command chord.
  const shifted = setup({ view: "activity" });
  const ev2 = cmd("f", { shiftKey: true });
  press(shifted.doc, ev2, new FakeElement("body"));
  assert.equal(ev2.defaultPrevented, false);
  assert.equal(shifted.doc.getElementById("act-q").focused, undefined);
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
  doc.activeElement = field;
  for (const combo of [{ key: "f", ctrlKey: true }, { key: "a", ctrlKey: true }, { key: ",", metaKey: true }]) {
    const ev = keyEvent(combo);
    ev.target = field;
    doc.dispatch(ev);
    assert.equal(ev.defaultPrevented, false, JSON.stringify(combo));
  }
  assert.equal(doc.getElementById("act-q").focused, undefined);
  assert.deepEqual(calls.show, []);
});

test("a contenteditable descendant also keeps every binding", () => {
  const { doc } = setup({ view: "activity" });
  const host = new FakeElement("div", { attributes: { contenteditable: "true" }, isContentEditable: true });
  const child = new FakeElement("span", { parentNode: host, isContentEditable: true });
  for (const combo of [{ key: "f", metaKey: true }, { key: ",", metaKey: true }, { key: "a", metaKey: true }]) {
    const ev = keyEvent(combo);
    ev.target = child;
    doc.dispatch(ev);
    assert.equal(ev.defaultPrevented, false, JSON.stringify(combo));
  }
});

test("IME, key repeat and already-handled events are ignored", () => {
  for (const flags of [{ isComposing: true }, { repeat: true }, { keyCode: 229 }, { defaultPrevented: true }]) {
    const { doc, calls } = setup({ view: "activity" });
    const ev = press(doc, cmd("f", flags), new FakeElement("body"));
    // Handled before us or arriving mid-IME: the query box is never touched.
    assert.equal(doc.getElementById("act-q").focused, undefined, JSON.stringify(flags));
    assert.equal(doc.getElementById("act-q").selected, undefined, JSON.stringify(flags));
    assert.deepEqual(calls.show, []);
    if (!flags.defaultPrevented) assert.equal(ev.defaultPrevented, false, JSON.stringify(flags));
  }
});

test("Cmd+A ticks only the downloads the current filter rendered", () => {
  const { list, boxes } = makeList({ count: 2 });
  const { doc } = setup({ view: "downloads", downloads: list });
  const card = list.children[0];
  const changes = [];
  for (const box of boxes) box.addEventListener("change", () => changes.push(box));
  // A monitoring switch lives outside the downloads list and must never be touched.
  const monitor = new FakeElement("input", { type: "checkbox", checked: false });
  doc.elements.set("monitor", monitor);

  const ev = cmd("a");
  ev.target = card;
  doc.activeElement = card;
  doc.dispatch(ev);
  assert.equal(ev.defaultPrevented, true);
  assert.deepEqual(boxes.map((b) => b.checked), [true, true]);
  assert.equal(changes.length, 2);
  assert.equal(monitor.checked, false);
  // Focus stays on the card that had it.
  assert.equal(doc.activeElement, card);
});

test("Cmd+A from outside the list stays with the platform", () => {
  const { list, boxes } = makeList({ count: 2 });
  const { doc } = setup({ view: "downloads", downloads: list });
  const ev = cmd("a");
  ev.target = new FakeElement("body");
  doc.activeElement = ev.target;
  doc.dispatch(ev);
  assert.equal(ev.defaultPrevented, false);
  assert.deepEqual(boxes.map((b) => b.checked), [false, false]);
});

test("a hidden or empty downloads list falls back to the platform", () => {
  const hidden = makeList({ hidden: true, count: 2 });
  const doc1 = setup({ view: "downloads", downloads: hidden.list }).doc;
  const ev = cmd("a");
  ev.target = hidden.list;
  doc1.activeElement = hidden.list;
  doc1.dispatch(ev);
  assert.equal(ev.defaultPrevented, false);
  assert.deepEqual(hidden.boxes.map((b) => b.checked), [false, false]);

  const empty = makeList({ count: 0 });
  const doc2 = setup({ view: "downloads", downloads: empty.list }).doc;
  const ev2 = cmd("a");
  ev2.target = empty.list;
  doc2.activeElement = empty.list;
  doc2.dispatch(ev2);
  assert.equal(ev2.defaultPrevented, false);
});

test("selection focus survives a checkbox-driven update", () => {
  const { list, boxes } = makeList({ count: 3 });
  const { doc } = setup({ view: "downloads", downloads: list });
  const changes = [];
  // The real handler re-renders the list, which drops DOM focus to the body.
  for (const box of boxes) box.addEventListener("change", () => {
    changes.push(box);
    doc.activeElement = new FakeElement("body");
  });
  const card = list.children[1];
  const ev = press(doc, cmd("a"), card);
  assert.equal(ev.defaultPrevented, true);
  assert.equal(changes.length, 3);
  assert.deepEqual(boxes.map((b) => b.checked), [true, true, true]);
  // Focus returns inside the downloads list instead of being lost.
  assert.ok(doc.activeElement?.closest?.("#dl"), "focus stayed in the list");
});

test("platform labels in Setup & help match the host", () => {
  const mac = setup({ platform: "MacIntel" });
  assert.equal(mac.doc.getElementById("kbd-mod").textContent, "Command");
  assert.equal(mac.doc.getElementById("kbd-find").textContent, "Command + F");
  assert.equal(mac.doc.getElementById("kbd-select-all").textContent, "Command + A");

  const win = setup({ platform: "Win32" });
  assert.equal(win.doc.getElementById("kbd-mod").textContent, "Ctrl");
  assert.equal(win.doc.getElementById("kbd-find").textContent, "Ctrl + F");
  assert.equal(win.doc.getElementById("kbd-settings").textContent, "Ctrl + ,");
});

test("the mobile menu only takes Escape when no dialog, field or overlay did", () => {
  const listener = appSource.match(/document\.addEventListener\("keydown"[\s\S]*?\n\}\);/);
  assert.ok(listener, "expected the Escape guard in app.js");
  let handler = null;
  const calls = { closed: 0, focused: 0 };
  const context = {
    document: { addEventListener: (type, fn) => { handler = fn; } },
    menuToggle: {
      getAttribute: () => "true",
      focus() { calls.focused += 1; },
    },
    closeMenu: () => { calls.closed += 1; },
  };
  runInNewContext(listener[0], context);
  assert.equal(typeof handler, "function");

  const plain = new FakeElement("body");
  handler(keyEvent({ key: "Escape", target: plain }));
  assert.equal(calls.closed, 1);

  const dialogHost = new FakeElement("dialog");
  const inDialog = new FakeElement("div", { parentNode: dialogHost });
  handler(keyEvent({ key: "Escape", target: inDialog }));
  assert.equal(calls.closed, 1, "the dialog's own Escape must stand");

  const input = new FakeElement("input", { type: "text" });
  handler(keyEvent({ key: "Escape", target: input }));
  assert.equal(calls.closed, 1, "search fields keep Escape");

  handler(keyEvent({ key: "Escape", target: plain, defaultPrevented: true }));
  assert.equal(calls.closed, 1, "a handler that already acted wins");
});

test("the shortcut map is discoverable in the shipped markup", () => {
  assert.ok(indexHtml.includes('<script src="keyboard.js" defer></script>'));
  assert.ok(indexHtml.indexOf("keyboard.js") < indexHtml.indexOf('<script src="app.js"'), "keyboard.js loads before app.js");
  for (const id of KBD_IDS) assert.ok(indexHtml.includes('id="' + id + '"'), id);
  assert.ok(indexHtml.includes("<h2>Keyboard</h2>"));
  assert.ok((indexHtml.match(/aria-keyshortcuts="Meta\+F Control\+F"/g) || []).length === 5);
  assert.ok(indexHtml.includes('aria-keyshortcuts="Meta+A Control+A"'));
  assert.ok(indexHtml.includes('aria-keyshortcuts="Meta+,'));
  assert.ok(indexHtml.includes('aria-keyshortcuts="Meta+? Control+?"'));
});