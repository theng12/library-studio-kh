import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import test from "node:test";

const source = readFileSync(new URL("../src-tauri/src/main.rs", import.meta.url), "utf8");

function callBlock(marker) {
  const start = source.indexOf(marker);
  assert.notEqual(start, -1, `missing ${marker}`);
  let depth = 0;
  for (let i = source.indexOf("(", start); i < source.length; i++) {
    const char = source[i];
    if (char === '"') {
      for (i++; source[i] !== '"'; i++) if (source[i] === "\\") i++;
    } else if (char === "(") depth++;
    else if (char === ")" && --depth === 0) return source.slice(start, i + 1);
  }
  throw new Error(`unbalanced ${marker}`);
}

function predefinedItems(block) {
  return [...block.matchAll(/PredefinedMenuItem::(\w+)\(/g)].map(match => match[1]);
}

test("Edit menu keeps the standard responder-based editing items", () => {
  const edit = callBlock('Submenu::with_items(\n        app,\n        "Edit"');
  assert.deepEqual(predefinedItems(edit), ["undo", "redo", "separator", "cut", "copy", "paste", "select_all"]);
});

test("Window menu uses the Tauri window submenu id and conventional window commands", () => {
  const window = callBlock("Submenu::with_id_and_items(\n        app,\n        tauri::menu::WINDOW_SUBMENU_ID");
  assert.match(window, /"Window"/);
  assert.deepEqual(predefinedItems(window), ["minimize", "maximize", "separator", "close_window"]);
});

test("app menu keeps its own local actions and all three submenus are installed", () => {
  assert.match(source, /MenuItem::with_id\(app, "use-local-host", "Use this Mac as host"/);
  assert.match(source, /MenuItem::with_id\(app, "restart", "Restart Library"/);
  const installed = [...source.matchAll(/Menu::with_items\(\s*app,\s*&\[([^\]]*)\]\s*,?\s*\)/g)].at(-1)[1];
  assert.match(installed, /&app_menu/);
  assert.match(installed, /&edit_menu/);
  assert.match(installed, /&window_menu/);
});

test("editing commands stay with the focused responder, not the app menu handler", () => {
  const handlerStart = source.indexOf(".on_menu_event");
  const handler = source.slice(handlerStart, source.indexOf(".build(", handlerStart));
  assert.deepEqual([...handler.matchAll(/event\.id\(\) == "([^"]+)"/g)].map(match => match[1]), ["use-local-host", "restart"]);
  assert.doesNotMatch(source, /\.accelerator\(|keydown|execCommand|on_keyboard/i);
});
