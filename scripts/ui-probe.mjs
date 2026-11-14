#!/usr/bin/env node
// Headless UI probe: drives the cached Chrome headless shell over CDP. No window,
// no Dock icon, no pointer or keyboard injection into the owner's desktop.
//
//   node scripts/ui-probe.mjs start --url http://127.0.0.1:PORT/
//   node scripts/ui-probe.mjs login owner pass
//   node scripts/ui-probe.mjs eval "document.title" [--viewport 375x812]
//   node scripts/ui-probe.mjs wait "document.querySelectorAll('.libgrid-card').length" 10000
//   node scripts/ui-probe.mjs measure [--viewport 1440x900]   # asserts layout, prints JSON
//   node scripts/ui-probe.mjs click "#selector" [--after "JS"] [--viewport 768x1024]   (trusted pointer)
//   node scripts/ui-probe.mjs key Escape [--after "JS"]            (trusted CDP key event)
//   node scripts/ui-probe.mjs key --text "look" --focus "#find-q"  (focus a field, then type real keys)
//   node scripts/ui-probe.mjs shot /tmp/x.png [--viewport 768x1024]
//   node scripts/ui-probe.mjs stop
//
// The viewport belongs to the CDP session, so it must be passed with each command.
// The shell is reused across calls through a pid file and port 9333.

import { spawn } from "node:child_process";
import { existsSync, readFileSync, writeFileSync, unlinkSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { join } from "node:path";

const PORT = Number(process.env.UI_PROBE_PORT || 9333);
const pidFile = join(tmpdir(), (process.env.UI_PROBE_PROFILE || "library-ui-probe") + ".pid");
const profileDir = join(tmpdir(), process.env.UI_PROBE_PROFILE || "library-ui-probe-profile");
const shell = join(homedir(), "Library/Caches/ms-playwright/chromium_headless_shell-1248/chrome-headless-shell-mac-arm64/chrome-headless-shell");

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function waitJSON(path, method = "GET", tries = 80) {
  for (let i = 0; i < tries; i++) {
    try {
      const r = await fetch(`http://127.0.0.1:${PORT}${path}`, { method });
      if (r.ok) return await r.json();
    } catch {}
    await sleep(125);
  }
  throw new Error("headless shell did not answer on port " + PORT);
}

async function target() {
  const list = await waitJSON("/json/list");
  const page = list.find((t) => t.type === "page");
  if (!page) throw new Error("no page target");
  return page;
}

let seq = 0;
async function send(ws, method, params = {}) {
  const id = ++seq;
  const answer = new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error("CDP timeout: " + method)), 20000);
    const onMessage = (event) => {
      const msg = JSON.parse(event.data);
      if (msg.id !== id) return;
      clearTimeout(timer);
      ws.removeEventListener("message", onMessage);
      msg.error ? reject(new Error(method + ": " + msg.error.message)) : resolve(msg.result);
    };
    ws.addEventListener("message", onMessage);
  });
  ws.send(JSON.stringify({ id, method, params }));
  return answer;
}

async function connect() {
  const page = await target();
  const ws = new WebSocket(page.webSocketDebuggerUrl);
  await new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error("CDP socket open timed out")), 10000);
    ws.addEventListener("open", () => { clearTimeout(timer); resolve(); }, { once: true });
    ws.addEventListener("error", () => { clearTimeout(timer); reject(new Error("CDP socket failed")); }, { once: true });
  });
  return { ws, page };
}

function takeViewport(args) {
  const i = args.indexOf("--viewport");
  if (i < 0) return { rest: args, vp: null };
  const [w, h] = String(args[i + 1] || "").split("x").map(Number);
  const rest = args.slice(0, i).concat(args.slice(i + 2));
  return { rest, vp: w > 0 && h > 0 ? { w, h } : null };
}

async function connectWith(vp) {
  const { ws } = await connect();
  if (vp) {
    const mobile = vp.w < 768;
    await send(ws, "Emulation.setDeviceMetricsOverride", { width: vp.w, height: vp.h, deviceScaleFactor: 1, mobile });
    await send(ws, "Emulation.setTouchEmulationEnabled", mobile ? { enabled: true, maxTouchPoints: 5 } : { enabled: false });
    // The new metrics apply on the next frame; measure after it settles.
    await sleep(400);
  }
  return ws;
}

async function evaluate(expression, vp = null) {
  const ws = await connectWith(vp);
  const result = await send(ws, "Runtime.evaluate", { expression, returnByValue: true, awaitPromise: true });
  ws.close();
  if (result.exceptionDetails) throw new Error(result.exceptionDetails.exception?.description || "page error");
  return result.result.value;
}

// A trusted pointer click at the element's centre, not element.click(): it focuses
// and opens like a person. Shared by `click` and `key --focus` so a field can be
// focused and typed into within one session (focus clears between CLI calls).
async function clickSelector(ws, selector) {
  await send(ws, "Runtime.evaluate", { expression: `document.querySelector(${JSON.stringify(selector)})?.scrollIntoView({ block: "center", inline: "center" })` });
  await sleep(250);
  const r = await send(ws, "Runtime.evaluate", { expression: `(() => { const el = document.querySelector(${JSON.stringify(selector)}); if (!el) return null; const r = el.getBoundingClientRect(); return { x: r.x + r.width / 2, y: r.y + r.height / 2 }; })()`, returnByValue: true });
  const spot = r.result.value;
  if (!spot) throw new Error("no element for " + selector);
  await send(ws, "Input.dispatchMouseEvent", { type: "mouseMoved", x: spot.x, y: spot.y });
  await send(ws, "Input.dispatchMouseEvent", { type: "mousePressed", x: spot.x, y: spot.y, button: "left", clickCount: 1 });
  await send(ws, "Input.dispatchMouseEvent", { type: "mouseReleased", x: spot.x, y: spot.y, button: "left", clickCount: 1 });
}

// Named keys the walk needs, with the virtual-key codes CDP expects. Trusted
// Input.dispatchKeyEvent, not a synthetic DOM KeyboardEvent.
const KEYS = {
  Escape: { code: "Escape", vk: 27 }, Enter: { code: "Enter", vk: 13 }, Tab: { code: "Tab", vk: 9 },
  ArrowDown: { code: "ArrowDown", vk: 40 }, ArrowUp: { code: "ArrowUp", vk: 38 },
  ArrowLeft: { code: "ArrowLeft", vk: 37 }, ArrowRight: { code: "ArrowRight", vk: 39 },
  Backspace: { code: "Backspace", vk: 8 }, Delete: { code: "Delete", vk: 46 },
  Space: { code: "Space", vk: 32, key: " " }, Home: { code: "Home", vk: 36 }, End: { code: "End", vk: 35 },
};

async function dispatchKey(ws, name) {
  const k = KEYS[name];
  if (!k) throw new Error("unknown key " + name + " (named keys: " + Object.keys(KEYS).join(", ") + "; use --text for characters)");
  const base = { key: k.key || name, code: k.code, windowsVirtualKeyCode: k.vk, nativeVirtualKeyCode: k.vk };
  await send(ws, "Input.dispatchKeyEvent", { type: "rawKeyDown", ...base });
  await send(ws, "Input.dispatchKeyEvent", { type: "keyUp", ...base });
}

async function typeText(ws, text) {
  for (const ch of text) {
    const upper = ch.toUpperCase();
    const vk = upper.charCodeAt(0);
    const code = /^[a-z]$/i.test(ch) ? "Key" + upper : /^[0-9]$/.test(ch) ? "Digit" + ch : "";
    await send(ws, "Input.dispatchKeyEvent", { type: "keyDown", key: ch, code, windowsVirtualKeyCode: vk, nativeVirtualKeyCode: vk, text: ch, unmodifiedText: ch });
    await send(ws, "Input.dispatchKeyEvent", { type: "keyUp", key: ch, code, windowsVirtualKeyCode: vk, nativeVirtualKeyCode: vk });
  }
}

const MEASURE = `(() => {
  const box = (sel) => { const el = document.querySelector(sel); if (!el) return null; const r = el.getBoundingClientRect(); return { w: Math.round(r.width), h: Math.round(r.height), top: Math.round(r.top), right: Math.round(r.right) }; };
  const style = (sel, prop) => { const el = document.querySelector(sel); return el ? getComputedStyle(el)[prop] : null; };
  const visible = (el) => el && el.getBoundingClientRect().width > 0;
  const grid = [...document.querySelectorAll(".libgrid-grid")].find(visible);
  const wrap = [...document.querySelectorAll(".table-wrap, .table-scroll")].find(visible);
  const poster = [...document.querySelectorAll(".libgrid-poster")].find(visible);
  const posterBox = poster ? poster.getBoundingClientRect() : null;
  const saveBar = [...document.querySelectorAll(".save-bar")].find(visible);
  const hitBox = (el) => {
    if (el.tagName === "INPUT" && (el.type === "checkbox" || el.type === "radio")) {
      const label = el.closest("label");
      if (label) return label.getBoundingClientRect();
    }
    return el.getBoundingClientRect();
  };
  const small = [...document.querySelectorAll("button, [role=button], input[type=checkbox], summary")]
    .filter(visible)
    .filter((el) => hitBox(el).height < 43.5);
  return {
    width: innerWidth,
    height: innerHeight,
    docOverflow: document.documentElement.scrollWidth - innerWidth,
    content: box("main.content"),
    head: box(".view:not([hidden]) .page-head"),
    pageCount: document.querySelectorAll(".view .page").length,
    gridCols: grid ? getComputedStyle(grid).gridTemplateColumns.split(" ").length : 0,
    cardW: grid ? Math.round(grid.firstElementChild?.getBoundingClientRect().width || 0) : 0,
    tableW: box(".downloads-table")?.w || 0,
    wrapW: box(".table-wrap, .table-scroll")?.w || 0,
    wrapScrolls: wrap ? wrap.scrollWidth > wrap.clientWidth + 1 : null,
    sticky: style(".page-table th", "position"),
    settingsBody: box("#view-settings .page-body")?.h || 0,
    dlDetail: box("#downloads-detail:not([hidden])")?.w || 0,
    shelfRows: [...document.querySelectorAll(".libgrid-shelf-row")].filter(visible).length,
    posterRatio: posterBox && posterBox.width ? Math.round((posterBox.height / posterBox.width) * 100) / 100 : 0,
    selectionBar: box(".libgrid-selection")?.w || 0,
    saveBar: saveBar ? getComputedStyle(saveBar).position : "",
    smallTargets: innerWidth <= 760 ? small.length : 0,
    smallTargetSample: innerWidth <= 760 ? small.slice(0, 6).map((el) => el.className || el.tagName) : [],
  };
})()`;

async function main() {
  const [cmd, ...rawArgs] = process.argv.slice(2);
  const { rest: args, vp } = takeViewport(rawArgs);
  if (cmd === "start") {
    if (existsSync(pidFile)) {
      try {
        process.kill(Number(readFileSync(pidFile, "utf8")), 0);
        // Reuse the browser but always go to the requested page; a stale tab
        // would otherwise make later checks read another instance.
        const wanted = args[args.indexOf("--url") + 1];
        if (wanted && wanted !== "about:blank") {
          const { ws } = await connect();
          await send(ws, "Page.navigate", { url: wanted });
          ws.close();
          await sleep(800);
        }
        console.log("already running");
        return;
      } catch {}
    }
    const child = spawn(shell, [
      "--headless", `--remote-debugging-port=${PORT}`,
      "--user-data-dir=" + profileDir,
      "--no-first-run", "--no-default-browser-check", "--disable-gpu",
      "--disable-background-networking", "about:blank",
    ], { detached: true, stdio: "ignore" });
    child.unref();
    writeFileSync(pidFile, String(child.pid));
    await waitJSON("/json/version");
    const url = args[args.indexOf("--url") + 1] || "about:blank";
    const { ws } = await connect();
    await send(ws, "Page.enable");
    await send(ws, "Page.navigate", { url });
    ws.close();
    await sleep(800);
    console.log("started " + child.pid + " " + url);
    return;
  }
  if (cmd === "stop") {
    if (existsSync(pidFile)) {
      try { process.kill(Number(readFileSync(pidFile, "utf8"))); } catch {}
      unlinkSync(pidFile);
    }
    console.log("stopped");
    return;
  }
  if (cmd === "login") {
    const [user, pass] = args;
    const script = `(async () => {
      const post = async (path, body) => {
        const r = await fetch(path, { method: "POST", headers: { "Content-Type": "application/json", "X-Library-Request": "1" }, credentials: "same-origin", body: JSON.stringify(body) });
        return { status: r.status, body: await r.json().catch(() => ({})) };
      };
      const setup = await post("/api/auth/setup", { username: ${JSON.stringify(user)}, password: ${JSON.stringify(pass)} });
      const login = await post("/api/auth/login", { username: ${JSON.stringify(user)}, password: ${JSON.stringify(pass)}, remember: false });
      return { setup: setup.status, login: login.status, error: login.body.error || "" };
    })()`;
    console.log(JSON.stringify(await evaluate(script, vp), null, 1));
    const ws = await connectWith(vp);
    await send(ws, "Page.reload", { ignoreCache: true });
    ws.close();
    await sleep(1200);
    return;
  }
  if (cmd === "eval") {
    console.log(JSON.stringify(await evaluate(args.join(" "), vp), null, 1));
    return;
  }
  if (cmd === "wait") {
    const expression = args[0];
    const timeout = Number(args[1] || 10000);
    const started = Date.now();
    while (Date.now() - started < timeout) {
      try { if (await evaluate(expression, vp)) { console.log("ready"); return; } } catch {}
      await sleep(250);
    }
    throw new Error("timed out waiting for " + expression);
  }
  if (cmd === "measure") {
    const m = await evaluate(MEASURE, vp);
    const problems = [];
    if (m.docOverflow > 1) problems.push("horizontal overflow " + m.docOverflow + "px");
    if (m.width >= 1200 && m.head && m.content && m.head.w < m.content.w - 120) problems.push("header narrower than the page: " + m.head.w + " of " + m.content.w);
    if (m.pageCount < 6) problems.push("only " + m.pageCount + " pages use the frame");
    if (m.width >= 1200 && m.gridCols && m.gridCols < 3) problems.push("library grid has " + m.gridCols + " columns at " + m.width);
    if (m.width >= 700 && m.width < 1000 && m.gridCols && m.gridCols < 2) problems.push("library grid has " + m.gridCols + " columns at " + m.width);
    if (m.width < 500 && m.gridCols > 3) problems.push("phone library grid has " + m.gridCols + " columns");
    if (m.tableW && m.wrapW && m.tableW + 2 < m.wrapW && !m.wrapScrolls) problems.push("downloads table is narrower than its frame: " + m.tableW + " of " + m.wrapW);
    if (m.sticky && m.sticky !== "sticky") problems.push("first table column is " + m.sticky + ", not sticky");
    if (m.posterRatio && (m.posterRatio < 1.4 || m.posterRatio > 1.6)) problems.push("poster frame is not 2:3 (h/w " + m.posterRatio + ")");
    if (m.saveBar && m.saveBar !== "sticky") problems.push("save bar is " + m.saveBar + ", not sticky");
    console.log(JSON.stringify({ ...m, problems }, null, 1));
    if (problems.length) process.exitCode = 1;
    return;
  }
  if (cmd === "click") {
    const selector = args[0];
    if (!selector) throw new Error("click needs a CSS selector");
    const ws = await connectWith(vp);
    try {
      await clickSelector(ws, selector);
    } catch (e) { ws.close(); throw e; }
    // Read the settled state before the session (and its viewport) goes away;
    // clearing the emulation resizes the page and would reset the UI.
    const after = args.includes("--after") ? args[args.indexOf("--after") + 1] : "";
    await sleep(600);
    if (after) {
      const out = await send(ws, "Runtime.evaluate", { expression: after, returnByValue: true, awaitPromise: true });
      if (out.exceptionDetails) { ws.close(); throw new Error(out.exceptionDetails.exception?.description || "after error"); }
      console.log(JSON.stringify(out.result.value, null, 1));
    }
    ws.close();
    if (!after) console.log("clicked " + selector);
    return;
  }
  if (cmd === "key") {
    const textIdx = args.indexOf("--text");
    const focusIdx = args.indexOf("--focus");
    const afterIdx = args.indexOf("--after");
    const after = afterIdx >= 0 ? args[afterIdx + 1] : "";
    const ws = await connectWith(vp);
    try {
      // Optional trusted click first, so typing lands in the field in this session.
      if (focusIdx >= 0) await clickSelector(ws, args[focusIdx + 1]);
      if (textIdx >= 0) await typeText(ws, args[textIdx + 1] || "");
      else {
        const name = args[0];
        if (!name || name.startsWith("--")) throw new Error("key needs a name (Escape, Enter, Tab, ...) or --text \"...\"");
        await dispatchKey(ws, name);
      }
      await sleep(400);
      if (after) {
        const out = await send(ws, "Runtime.evaluate", { expression: after, returnByValue: true, awaitPromise: true });
        if (out.exceptionDetails) throw new Error(out.exceptionDetails.exception?.description || "after error");
        console.log(JSON.stringify(out.result.value, null, 1));
      } else {
        console.log(textIdx >= 0 ? "typed " + JSON.stringify(args[textIdx + 1] || "") : "key " + args[0]);
      }
    } finally { ws.close(); }
    return;
  }
  if (cmd === "shot") {
    const ws = await connectWith(vp);
    const { data } = await send(ws, "Page.captureScreenshot", { format: "png" });
    ws.close();
    writeFileSync(args[0] || join(tmpdir(), "ui-probe.png"), Buffer.from(data, "base64"));
    console.log("saved " + (args[0] || "ui-probe.png"));
    return;
  }
  console.error("usage: start --url URL | login u p | eval JS | wait EXPR MS | measure | click SEL | key NAME|--text TXT [--focus SEL] | shot PATH | stop  (all accept --viewport WxH; click/key accept --after JS)");
  process.exitCode = 2;
}

setTimeout(() => { console.error("probe: hard timeout"); process.exit(1); }, 45000).unref();
main().catch((e) => { console.error("probe: " + e.message); process.exit(1); });
