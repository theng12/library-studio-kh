import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import test from "node:test";

const picker = readFileSync(new URL("../internal/server/web/folder-picker.js", import.meta.url), "utf8");
const html = readFileSync(new URL("../internal/server/web/index.html", import.meta.url), "utf8");
const settings = readFileSync(new URL("../internal/server/web/settings-view.js", import.meta.url), "utf8");
const nativeCheck = readFileSync(new URL("../src-tauri/src/webview_check.js", import.meta.url), "utf8");

function helper(name) {
  // The pure helpers may call each other, so load them together.
  const functions = picker.match(/^function folderPicker\w+\([\s\S]*?\n\}/gm) || [];
  assert.ok(functions.some((fn) => fn.startsWith("function " + name + "(")),
    name + " must stay a plain testable helper");
  // Evaluate in this realm so returned objects compare with deepStrictEqual.
  return new Function(functions.join("\n") + "\nreturn " + name + ";")();
}

test("breadcrumb builds absolute folder segments", () => {
  const breadcrumb = helper("folderPickerBreadcrumb");
  assert.deepEqual(breadcrumb("/"), [{ name: "/", path: "/" }]);
  assert.deepEqual(breadcrumb("/Users/me/Movies/"), [
    { name: "/", path: "/" },
    { name: "Users", path: "/Users" },
    { name: "me", path: "/Users/me" },
    { name: "Movies", path: "/Users/me/Movies" },
  ]);
  assert.deepEqual(breadcrumb("/Volumes/NAS"), [
    { name: "/", path: "/" },
    { name: "Volumes", path: "/Volumes" },
    { name: "NAS", path: "/Volumes/NAS" },
  ]);
  assert.deepEqual(breadcrumb(""), []);
  assert.deepEqual(breadcrumb("relative/path"), []);
});

test("breadcrumb walks Windows drives and network shares", () => {
  const breadcrumb = helper("folderPickerBreadcrumb");
  assert.deepEqual(breadcrumb("C:\\Users\\me\\Movies"), [
    { name: "C:\\", path: "C:\\" },
    { name: "Users", path: "C:\\Users" },
    { name: "me", path: "C:\\Users\\me" },
    { name: "Movies", path: "C:\\Users\\me\\Movies" },
  ]);
  assert.deepEqual(breadcrumb("\\\\NAS\\Media\\Films"), [
    { name: "\\\\NAS\\Media", path: "\\\\NAS\\Media" },
    { name: "Films", path: "\\\\NAS\\Media\\Films" },
  ]);
});

test("a selection is a usable folder only when it is absolute", () => {
  const valid = helper("folderPickerValidSelection");
  assert.equal(valid({ path: "/" }), true);
  assert.equal(valid({ path: "/Users/me" }), true);
  assert.equal(valid({ path: "C:\\Users" }), true);
  assert.equal(valid({ path: "\\\\NAS\\Media" }), true);
  assert.equal(valid({ path: "" }), false);
  assert.equal(valid({ path: "relative" }), false);
  assert.equal(valid({}), false);
  assert.equal(valid(null), false);
});

test("every path field is read-only and picker-driven", () => {
  const fields = [...html.matchAll(/<input[^>]*\bname="(?:managedTrashPath|appDir|downloadPath|root\.[a-z]+)"[^>]*>/g)].map((m) => m[0]);
  assert.equal(fields.length, 7, "Cleanup 1 + Setup 6 path fields are expected");
  for (const field of fields) {
    assert.match(field, /data-path-picker/, field);
    assert.match(field, /\breadonly\b/, field);
  }
});

test("Settings folder fields are rendered as path pickers", () => {
  assert.match(settings, /\{ kind: "path", name: "downloadPath"/);
  assert.match(settings, /\{ kind: "path", name: "libraryPath"/);
  assert.match(settings, /"data-path-picker": field\.kind === "path"/);
  assert.match(settings, /readonly: field\.kind === "path"/);
});

test("packaged webview check requires every chooser and read-only path", () => {
  for (const name of ["appDir", "downloadPath", "root.sonarr", "root.radarr", "root.lidarr", "root.whisparr"]) {
    assert.equal(nativeCheck.includes(name), true, "missing native check for " + name);
  }
  assert.equal(nativeCheck.includes('document.querySelector("#settings-downloadpath + .folder-picker-choose").click()'), true);
  assert.equal(nativeCheck.includes("if (report.pathControls.some(item => !item.readOnly || !item.chooser))"), true);
  assert.equal(nativeCheck.includes('name => \'#view-help input[name="\' + name + \'"]\')'), true);
  assert.equal(nativeCheck.includes('["appDir", "downloadPath", "root.sonarr", "root.radarr", "root.lidarr", "root.whisparr"].map(name => \'#view-help input[name="\' + name + \'"]\')'), true);
  assert.equal(nativeCheck.includes('report.pathControls = expected.map'), true);
});

test("the picker loads before app and its stylesheet follows the layout", () => {
  assert.ok(html.indexOf("folder-picker.js") < html.indexOf("app.js"), "folder-picker.js must load before app.js");
  assert.ok(html.indexOf("folder-picker.css") > html.indexOf("layout.css"), "folder-picker.css must load after layout.css");
  assert.doesNotMatch(picker, /\bimport\s|\brequire\(/, "no new JS dependency");
});

test("the chooser returns focus to the control that opened it", () => {
  assert.match(picker, /let pickOpener = null/);
  assert.match(picker, /pickOpener = choose;/);
  // A synthetic click leaves focus on the body, so the caller's control wins.
  assert.match(picker, /pickOpener = opener \|\| \(document\.activeElement !== document\.body/);
  assert.match(picker, /function restoreOpener\(\)/);
  assert.match(picker, /pickOpener = null;\n    browser = null;/);
  // The dialog's own focus restore can run after ours; assert again next frame.
  assert.match(picker, /requestAnimationFrame\(\(\) => \{ if \(target\.isConnected\) target\.focus\(\); \}\);/);
  const close = picker.match(/function closeDialog\(\) \{[\s\S]*?\n  \}/)[0];
  assert.match(close, /restoreOpener\(\)/);
  const closeEvent = picker.match(/dialog\.addEventListener\("close",[\s\S]*?\n    \}\);/)[0];
  assert.match(closeEvent, /restoreOpener\(\)/, "a backdrop close restores focus too");
});
