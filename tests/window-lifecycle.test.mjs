import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import test from "node:test";

const rust = readFileSync(new URL("../src-tauri/src/main.rs", import.meta.url), "utf8");
const check = readFileSync(new URL("../src-tauri/src/webview_check.rs", import.meta.url), "utf8");
const html = readFileSync(new URL("../internal/server/web/index.html", import.meta.url), "utf8");
const css = readFileSync(new URL("../internal/server/web/style.css", import.meta.url), "utf8");

test("closing the window hides it, and no window left is not a quit", () => {
  assert.match(rust, /\.on_window_event\(hide_on_close\)/);
  assert.match(rust, /WindowEvent::CloseRequested \{ api, \.\. \}/);
  assert.match(rust, /api\.prevent_close\(\)/);
  assert.match(rust, /let _ = window\.hide\(\)/);
  // A destroyed window must not end the app either; only Quit and Restart do.
  assert.match(rust, /RunEvent::ExitRequested \{ code, api, \.\. \}/);
  // A hidden window must not end the app — except while Sparkle is installing or relaunching.
  assert.match(rust, /if code\.is_none\(\) && !sparkle::updating\(\) \{\s*api\.prevent_exit\(\)/);
});

test("the window comes back from the menu bar item, the app menu and the Dock", () => {
  assert.match(rust, /fn build_tray\(app: &tauri::App\) -> tauri::Result<\(\)>/);
  assert.match(rust, /TrayIconBuilder::with_id\("library-tray"\)/);
  assert.match(rust, /"show-window", "Show Library Studio KH"/);
  assert.match(rust, /"quit", "Quit Library Studio KH"/);
  assert.match(rust, /if let Err\(e\) = build_tray\(app\) \{/, "a missing tray must not stop the app");
  assert.match(rust, /RunEvent::Reopen/);
  assert.match(rust, /fn show_main\(app: &tauri::AppHandle\)/);
});

test("the native lifecycle check drives the shipped close and reopen paths", () => {
  assert.match(check, /\.on_window_event\(crate::hide_on_close\)/);
  assert.match(check, /crate::run_action\(&runner, "show-window"\)/);
  assert.match(check, /windowSurvivedClose/);
  assert.match(check, /hiddenAfterClose/);
  assert.match(check, /KH_HOLD_URL/, "the hold must be asked while the window is gone");
  assert.match(check, /holdWhileHidden/);
});

test("the window says one plain line about staying running", () => {
  assert.match(
    html,
    /class="persist-note">Closing this window keeps Library running\. Quit Library Studio KH to stop it\./,
  );
  assert.match(css, /\.persist-note \{[^}]*font-size: var\(--text-xs\)/);
});
