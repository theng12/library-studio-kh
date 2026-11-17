import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const rust = readFileSync(new URL("../src-tauri/src/main.rs", import.meta.url), "utf8");
const go = readFileSync(new URL("../cmd/library-studio/main.go", import.meta.url), "utf8");

test("a server that cannot start explains itself instead of panicking", () => {
  // The shell must not refuse to open just because its own background host is
  // listening: that host is the service this app attaches to, and refusing there
  // left the app closed after an install on every Mac with the login job installed.
  assert.ok(!rust.includes("another_instance_running"), "the port probe that blocked the app is back");
  assert.ok(!rust.includes("HOST_PORTS"), "the background host's ports are probed again");
  assert.match(rust, /Err\(reason\) => report_and_exit\(&reason\)/);
  assert.ok(!rust.includes("let (child, url) = start_server()?"), "the setup hook still aborts on a server failure");
  assert.ok(!rust.match(/\.lock\(\)\.unwrap\(\)/), "a poisoned lock can still panic the exit path");
  assert.match(rust, /catch_unwind/);
});

test("the window is reachable from the menu and the Dock after it closes", () => {
  // The id and its label are one call, laid out over several lines by rustfmt.
  assert.match(rust, /"show-window",\s*"Show Library Studio KH"/);
  assert.match(rust, /fn show_main\(app: &tauri::AppHandle\)/);
  assert.match(rust, /if let Some\(window\) = app\.get_webview_window\("main"\)/);
  assert.match(rust, /open_main_window\(app, &url\)/, "a closed window is rebuilt on demand");
  assert.match(rust, /event\.id\(\) == "show-window"/);
  assert.match(rust, /RunEvent::Reopen/);
  assert.match(rust, /open_main_window\(app\.handle\(\), &url\)/, "launch opens the main window");
});

test("the shell never closes the server's pipes once it is running", () => {
  // A closed stderr read end is SIGPIPE for the server's first log line, which
  // ends it and leaves the window on a dead address.
  assert.match(rust, /let mut stderr = child\.stderr\.take\(\)/);
  assert.match(rust, /std::io::copy\(&mut pipe, &mut std::io::sink\(\)\)/, "the server's stderr is closed after launch");
});

test("the server explains a busy port in plain words", () => {
  assert.match(go, /BLOCKED %s/);
  assert.match(go, /Another Library Studio KH is already running on this Mac\./);
  assert.match(go, /errors\.Is\(err, syscall\.EADDRINUSE\)/);
});

const platform = readFileSync(new URL("../internal/platform/startup_darwin.go", import.meta.url), "utf8");
const control = readFileSync(new URL("../cmd/library-studio/host_control.go", import.meta.url), "utf8");
const uninstall = readFileSync(new URL("../scripts/uninstall.sh", import.meta.url), "utf8");

test("the background runner lives inside the app bundle", () => {
  assert.match(platform, /func packagedHelper/);
  assert.match(platform, /if packaged := packagedHelper\(exe\); packaged != "" \{/);
  assert.ok(!platform.includes("helperInstallPath"), "the helper is still copied outside the bundle");
  assert.ok(!platform.includes("func installHelper"), "the helper is still copied outside the bundle");
  assert.match(platform, /retireHelperCopy\(spec\.Home\)/);
});

test("uninstalling removes the app, its helper and the login job but keeps settings", () => {
  assert.match(platform, /func \(s \*HostService\) Uninstall\(home string\)/);
  assert.match(platform, /moveToTrash\(path, "kh\.librarystudio\.host\.plist/);
  assert.match(control, /case "uninstall":/);
  assert.match(control, /use install, start, stop, uninstall or status/);
  assert.match(uninstall, /-host-control uninstall/);
  assert.match(uninstall, /\.Trash/);
  assert.match(uninstall, /Library Studio KH\.app/);
  assert.ok(!uninstall.includes("rm -rf"), "uninstall deletes instead of retiring");
});
