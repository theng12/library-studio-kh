#!/usr/bin/env node
// Headless UI probe: drives the cached Chrome headless shell over CDP. No window,
// no Dock icon, no pointer or keyboard injection into the owner's desktop.
//
//   node scripts/ui-probe.mjs start --url http://127.0.0.1:PORT/
//   node scripts/ui-probe.mjs login owner pass
//   node scripts/ui-probe.mjs eval "document.title"
//   node scripts/ui-probe.mjs wait "document.querySelectorAll('.libgrid-card').length" 10000
//   node scripts/ui-probe.mjs viewport 375 812
//   node scripts/ui-probe.mjs measure            # asserts layout; prints JSON
//   node scripts/ui-probe.mjs shot /tmp/x.png
//   node scripts/ui-probe.mjs stop
//
// The shell is reused across calls through a pid file and port 9333.

import { spawn } from "node:child_process";
import { existsSync, readFileSync, writeFileSync, unlinkSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { join } from "node:path";

const PORT = Number(process.env.UI_PROBE_PORT || 9333);
const pidFile = join(tmpdir(), "library-ui-probe.pid");
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
    ws.addEventListener("open", resolve, { once: true });
    ws.addEventListener("error", () => reject(new Error("CDP socket failed")), { once: true });
  });
  return { ws, page };
}

async function evaluate(expression) {
  const { ws } = await connect();
  const result = await send(ws, "Runtime.evaluate", { expression, returnByValue: true, awaitPromise: true });
  ws.close();
  if (result.exceptionDetails) throw new Error(result.exceptionDetails.exception?.description || "page error");
  return result.result.value;
}

const MEASURE = `(() => {
  const box = (sel) => { const el = document.querySelector(sel); if (!el) return null; const r = el.getBoundingClientRect(); return { w: Math.round(r.width), h: Math.round(r.height), top: Math.round(r.top), right: Math.round(r.right) }; };
  const style = (sel, prop) => { const el = document.querySelector(sel); return el ? getComputedStyle(el)[prop] : null; };
  const visible = (el) => el && el.getBoundingClientRect().width > 0;
  const grid = [...document.querySelectorAll(".libgrid-grid")].find(visible);
  const wrap = [...document.querySelectorAll(".table-wrap, .table-scroll")].find(visible);
  return {
    width: innerWidth,
    height: innerHeight,
    docOverflow: document.documentElement.scrollWidth - innerWidth,
    content: box(".content"),
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
  };
})()`;

async function main() {
  const [cmd, ...args] = process.argv.slice(2);
  if (cmd === "start") {
    if (existsSync(pidFile)) {
      try { process.kill(Number(readFileSync(pidFile, "utf8")), 0); console.log("already running"); return; } catch {}
    }
    const child = spawn(shell, [
      "--headless", `--remote-debugging-port=${PORT}`,
      "--user-data-dir=" + join(tmpdir(), "library-ui-probe-profile"),
      "--no-first-run", "--no-default-browser-check", "--disable-gpu",
      "--disable-background-networking", "about:blank",
    ], { detached: true, stdio: "ignore" });
    child.unref();
    writeFileSync(pidFile, String(child.pid));
    await waitJSON("/json/version");
    const url = args[args.indexOf("--url") + 1] || "about:blank";
    // Reuse the existing tab so cookies and sessionStorage survive.
    const page = await target();
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
    console.log(JSON.stringify(await evaluate(script), null, 1));
    const { ws } = await connect();
    await send(ws, "Page.reload", { ignoreCache: true });
    ws.close();
    await sleep(1200);
    return;
  }
  if (cmd === "eval") {
    console.log(JSON.stringify(await evaluate(args.join(" ")), null, 1));
    return;
  }
  if (cmd === "wait") {
    const expression = args[0];
    const timeout = Number(args[1] || 10000);
    const started = Date.now();
    while (Date.now() - started < timeout) {
      try { if (await evaluate(expression)) { console.log("ready"); return; } } catch {}
      await sleep(200);
    }
    console.error("probe: timed out waiting for " + expression);
    process.exitCode = 1;
    return;
  }
  if (cmd === "viewport") {
    const width = Number(args[0] || 1440), height = Number(args[1] || 900);
    const mobile = width < 768;
    const { ws } = await connect();
    await send(ws, "Emulation.setDeviceMetricsOverride", { width, height, deviceScaleFactor: 1, mobile });
    await send(ws, "Emulation.setTouchEmulationEnabled", { enabled: mobile, maxTouchPoints: mobile ? 5 : 0 });
    ws.close();
    await sleep(300);
    console.log("viewport " + width + "x" + height + (mobile ? " (mobile)" : ""));
    return;
  }
  if (cmd === "measure") {
    const m = await evaluate(MEASURE);
    const problems = [];
    if (m.docOverflow > 1) problems.push("horizontal overflow " + m.docOverflow + "px");
    if (m.width >= 1200 && m.head && m.content && m.head.w < m.content.w - 120) problems.push("header narrower than the page: " + m.head.w + " of " + m.content.w);
    if (m.pageCount < 6) problems.push("only " + m.pageCount + " pages use the frame");
    if (m.width >= 1200 && m.gridCols && m.gridCols < 3) problems.push("library grid has " + m.gridCols + " columns at " + m.width);
    if (m.width >= 700 && m.width < 1000 && m.gridCols && m.gridCols < 2) problems.push("library grid has " + m.gridCols + " columns at " + m.width);
    if (m.width < 500 && m.gridCols > 1) problems.push("phone library grid has " + m.gridCols + " columns");
    if (m.tableW && m.wrapW && m.tableW + 2 < m.wrapW && !m.wrapScrolls) problems.push("downloads table is narrower than its frame: " + m.tableW + " of " + m.wrapW);
    if (m.sticky && m.sticky !== "sticky") problems.push("first table column is " + m.sticky + ", not sticky");
    console.log(JSON.stringify({ ...m, problems }, null, 1));
    if (problems.length) process.exitCode = 1;
    return;
  }
  if (cmd === "shot") {
    const { ws } = await connect();
    const { data } = await send(ws, "Page.captureScreenshot", { format: "png" });
    ws.close();
    writeFileSync(args[0] || join(tmpdir(), "ui-probe.png"), Buffer.from(data, "base64"));
    console.log("saved " + (args[0] || "ui-probe.png"));
    return;
  }
  console.error("usage: start --url URL | login u p | eval JS | viewport W H | measure | shot PATH | stop");
  process.exitCode = 2;
}

main().catch((e) => { console.error("probe: " + e.message); process.exitCode = 1; });
