import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const rust = readFileSync(new URL("../src-tauri/src/main.rs", import.meta.url), "utf8");
const go = readFileSync(new URL("../cmd/library-studio/main.go", import.meta.url), "utf8");

test("a taken port or a second copy never panics the app", () => {
  assert.match(rust, /fn another_instance_running/);
  assert.match(rust, /const HOST_PORTS: \[u16; 2\] = \[7881, 7882\]/);
  assert.match(rust, /if another_instance_running\(\)/);
  assert.match(rust, /Err\(reason\) => report_and_exit\(&reason\)/);
  assert.ok(!rust.includes("let (child, url) = start_server()?"), "the setup hook still aborts on a server failure");
  assert.ok(!rust.match(/\.lock\(\)\.unwrap\(\)/), "a poisoned lock can still panic the exit path");
  assert.match(rust, /catch_unwind/);
});

test("the server explains a busy port in plain words", () => {
  assert.match(go, /BLOCKED %s/);
  assert.match(go, /Another Library Studio KH is already running on this Mac\./);
  assert.match(go, /errors\.Is\(err, syscall\.EADDRINUSE\)/);
});
