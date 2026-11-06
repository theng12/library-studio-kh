#!/usr/bin/env node
// Installs and manages the Library Studio KH background host as a macOS LaunchAgent.
// Library-only: it never touches other agents, network or firewall settings, and it
// never runs a live install during --self-test or --dry-run.
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, renameSync, rmSync, statSync, writeFileSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { dirname, isAbsolute, join } from "node:path";
import assert from "node:assert/strict";

const LABEL = "kh.librarystudio.host";
const DEFAULT_PORT = 7881;
const DEFAULT_HOME = join(homedir(), "Library", "Application Support", "Library Studio KH");
const DOMAIN = `gui/${process.getuid?.() ?? ""}`;

const agentDir = () => process.env.LIBRARY_STUDIO_LAUNCHAGENT_DIR || join(homedir(), "Library", "LaunchAgents");
const plistPath = () => join(agentDir(), `${LABEL}.plist`);

function usage() {
  console.log(`Library Studio KH background host (macOS only)

  node scripts/manage-host.mjs install --exe <absolute server path> [--home <config home>] [--port 7881] [--dry-run]
  node scripts/manage-host.mjs start   [--dry-run]
  node scripts/manage-host.mjs stop    [--dry-run]
  node scripts/manage-host.mjs status  [--home <config home>] [--dry-run]
  node scripts/manage-host.mjs --self-test

The server runs as a LaunchAgent named ${LABEL}. It listens on 127.0.0.1 only.
Other operating systems are not supported yet; run the server directly with -service instead.`);
}

function parseArgs(argv) {
  const out = { _: [] };
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (!arg.startsWith("--")) {
      out._.push(arg);
      continue;
    }
    const name = arg.slice(2);
    if (name === "dry-run") out.dryRun = true;
    else if (name === "self-test") out.selfTest = true;
    else if (name === "help") out.help = true;
    else {
      const key = name.replace(/-([a-z])/g, (_, c) => c.toUpperCase());
      const value = argv[++i];
      if (value === undefined) throw new Error(`--${name} needs a value`);
      out[key] = value;
    }
  }
  return out;
}

function xmlEscape(value) {
  return String(value).replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;");
}

function buildPlist({ exe, home, port }) {
  return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>${xmlEscape(LABEL)}</string>
	<key>ProgramArguments</key>
	<array>
		<string>${xmlEscape(exe)}</string>
		<string>-service</string>
		<string>-port</string>
		<string>${port}</string>
	</array>
	<key>EnvironmentVariables</key>
	<dict>
		<key>LIBRARY_STUDIO_HOME</key>
		<string>${xmlEscape(home)}</string>
	</dict>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>ThrottleInterval</key>
	<integer>10</integer>
	<key>ProcessType</key>
	<string>Background</string>
	<key>StandardOutPath</key>
	<string>${xmlEscape(join(home, "host.log"))}</string>
	<key>StandardErrorPath</key>
	<string>${xmlEscape(join(home, "host.err.log"))}</string>
</dict>
</plist>
`;
}

function validateInstall({ exe, home, port }) {
  if (!exe) throw new Error("install needs --exe /absolute/path/to/library-studio-server");
  if (!isAbsolute(exe)) throw new Error("--exe must be an absolute path");
  if (!isAbsolute(home)) throw new Error("--home must be an absolute path");
  for (const [flag, value] of [["--exe", exe], ["--home", home]]) {
    if (/[\u0000-\u001f\u007f]/.test(value)) throw new Error(`${flag} cannot contain control characters`);
  }
  let st;
  try {
    st = statSync(exe);
  } catch {
    throw new Error(`--exe ${exe} does not exist`);
  }
  if (!st.isFile()) throw new Error(`--exe ${exe} is not a file`);
  if ((st.mode & 0o111) === 0) throw new Error(`--exe ${exe} is not executable`);
  if (!Number.isInteger(port) || port < 1024 || port > 65535) {
    throw new Error("--port must be a whole number from 1024 to 65535");
  }
  return { exe, home, port };
}

function launchctl(args) {
  try {
    execFileSync("launchctl", args, { stdio: ["ignore", "pipe", "pipe"] });
  } catch (err) {
    const detail = (err.stderr?.toString() || err.message).trim();
    throw new Error(`launchctl ${args[0]} failed: ${detail}`);
  }
}

function isLoaded() {
  try {
    execFileSync("launchctl", ["print", `${DOMAIN}/${LABEL}`], { stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
}

function requirePlist() {
  if (!existsSync(plistPath())) {
    throw new Error(`not installed: ${plistPath()} is missing. Run install --exe ... first.`);
  }
}

function install({ exe, home, port, dryRun, quiet }) {
  const settings = validateInstall({ exe, home, port });
  const plist = buildPlist(settings);
  const path = plistPath();
  const changed = !existsSync(path) || readFileSync(path, "utf8") !== plist;
  const say = quiet ? () => {} : console.log;
  if (dryRun) {
    say(`[dry-run] would write ${path} (${changed ? "changed" : "unchanged"})`);
    say(plist);
    say(`[dry-run] launchctl bootout ${DOMAIN}/${LABEL} (if loaded)`);
    say(`[dry-run] launchctl bootstrap ${DOMAIN} ${path}`);
    return;
  }
  mkdirSync(agentDir(), { recursive: true });
  if (changed) {
    const tmp = path + ".tmp";
    writeFileSync(tmp, plist, { mode: 0o644 });
    renameSync(tmp, path);
  }
  if (!changed && isLoaded()) {
    say(`Already installed and running: ${path}`);
    return;
  }
  if (isLoaded()) launchctl(["bootout", `${DOMAIN}/${LABEL}`]);
  launchctl(["bootstrap", DOMAIN, path]);
  say(`Installed and started ${LABEL}.`);
  say(`Logs: ${join(home, "host.log")} and ${join(home, "host.err.log")}`);
}

function start({ dryRun }) {
  requirePlist();
  if (dryRun) {
    console.log(`[dry-run] launchctl bootstrap ${DOMAIN} ${plistPath()} (if not loaded)`);
    console.log(`[dry-run] launchctl kickstart -k ${DOMAIN}/${LABEL} (if already loaded)`);
    return;
  }
  if (isLoaded()) {
    launchctl(["kickstart", "-k", `${DOMAIN}/${LABEL}`]);
    console.log("Restarted the background host.");
  } else {
    launchctl(["bootstrap", DOMAIN, plistPath()]);
    console.log("Started the background host.");
  }
}

function stop({ dryRun }) {
  if (!existsSync(plistPath())) {
    console.log("The background host is not installed; nothing to stop.");
    return;
  }
  if (dryRun) {
    console.log(`[dry-run] launchctl bootout ${DOMAIN}/${LABEL} (if loaded)`);
    return;
  }
  if (!isLoaded()) {
    console.log("The background host is not running.");
    return;
  }
  launchctl(["bootout", `${DOMAIN}/${LABEL}`]);
  console.log("Stopped the background host. Its plist is kept; run install to start it again.");
}

function installedHome() {
  try {
    const raw = execFileSync("plutil", ["-convert", "json", "-o", "-", plistPath()], { encoding: "utf8" });
    return JSON.parse(raw).EnvironmentVariables?.LIBRARY_STUDIO_HOME;
  } catch {
    return undefined;
  }
}

function loopback(address) {
  try {
    const url = new URL(`http://${address}`);
    return url.port !== "" && (url.hostname === "[::1]" || /^127\.\d+\.\d+\.\d+$/.test(url.hostname));
  } catch {
    return false;
  }
}

async function status({ home, dryRun }) {
  if (dryRun) {
    console.log(`[dry-run] would check ${plistPath()}`);
    console.log(`[dry-run] launchctl print ${DOMAIN}/${LABEL}`);
    console.log("[dry-run] would read the private host descriptor and check /api/health");
    return;
  }
  if (!existsSync(plistPath())) throw new Error(`not installed: ${plistPath()} is missing. Run install --exe ... first.`);
  console.log(`Plist: ${plistPath()}`);
  console.log(`LaunchAgent: ${isLoaded() ? "loaded" : "not loaded"}`);
  const configHome = home ?? installedHome() ?? DEFAULT_HOME;
  const descriptor = join(configHome, "host.json");
  if (!existsSync(descriptor)) {
    console.log("Host: not running (no private descriptor).");
    return;
  }
  const info = JSON.parse(readFileSync(descriptor, "utf8"));
  if (!loopback(info.address)) throw new Error(`${descriptor} names a non-loopback host; refusing to send its token`);
  const res = await fetch(`http://${info.address}/api/health`, {
    headers: { "X-Library-Token": info.token },
    redirect: "error",
    signal: AbortSignal.timeout(2000),
  });
  if (!res.ok) throw new Error(`the host answered ${res.status}; it may be restarting`);
  const body = await res.json();
  if (!body.ok) throw new Error("the host did not report healthy");
  console.log(`Host: healthy, version ${body.version}`);
}

// --self-test proves the plist escaping, input validation and dry-run safety without
// calling launchctl or writing to the real LaunchAgents folder.
function selfTest() {
  const dir = mkdtempSync(join(tmpdir(), "library-studio-host-"));
  const previousAgents = process.env.LIBRARY_STUDIO_LAUNCHAGENT_DIR;
  process.env.LIBRARY_STUDIO_LAUNCHAGENT_DIR = join(dir, "LaunchAgents");
  try {
    const exe = join(dir, "library-studio-server");
    writeFileSync(exe, "#!/bin/sh\n", { mode: 0o755 });
    const home = join(dir, "home & <config>");
    const plist = buildPlist({ exe, home, port: 7881 });
    assert.ok(plist.includes("&amp;"), "ampersands must be escaped");
    assert.ok(plist.includes("&lt;config&gt;"), "angle brackets must be escaped");
    assert.ok(!plist.includes("home & <config>"), "the raw path must not reach the plist");
    assert.ok(plist.includes(`<string>${exe}</string>`), "the selected executable must be used");
    assert.ok(plist.includes("<string>7881</string>"), "the selected port must be used");
    assert.ok(plist.includes(LABEL), "the label must be present");
    assert.throws(() => validateInstall({ exe: "relative/server", home, port: 7881 }), /absolute/);
    assert.throws(() => validateInstall({ exe, home: "relative", port: 7881 }), /absolute/);
    assert.throws(() => validateInstall({ exe, home, port: 80 }), /1024 to 65535/);
    assert.throws(() => validateInstall({ exe, home: home + "\n", port: 7881 }), /control characters/);
    assert.doesNotThrow(() => validateInstall({ exe, home, port: 7881 }));
    assert.ok(loopback("127.0.0.1:7881") && loopback("[::1]:7881"), "loopback addresses must be accepted");
    assert.ok(!loopback("example.com:7881") && !loopback("127.0.0.1"), "foreign or portless addresses must be refused");

    const before = existsSync(plistPath());
    install({ exe, home, port: 7881, dryRun: true, quiet: true });
    assert.equal(existsSync(plistPath()), before, "dry run must not write the plist");
    assert.ok(!existsSync(join(home, "host.json")), "dry run must not write a descriptor");

    if (process.platform === "darwin") {
      const lint = join(dir, `${LABEL}.plist`);
      writeFileSync(lint, plist);
      execFileSync("plutil", ["-lint", lint], { stdio: "ignore" });
    }
  } finally {
    if (previousAgents === undefined) delete process.env.LIBRARY_STUDIO_LAUNCHAGENT_DIR;
    else process.env.LIBRARY_STUDIO_LAUNCHAGENT_DIR = previousAgents;
    rmSync(dir, { recursive: true, force: true });
  }
  console.log("manage-host self-test: ok");
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  if (args.selfTest) return selfTest();
  const command = args._[0];
  if (!command || command === "help" || args.help) return usage();
  if (process.platform !== "darwin") {
    console.error(`manage-host: the Library Studio KH background host manager is macOS-only for now; this is ${process.platform}. Run the server directly with -service -port ${DEFAULT_PORT} instead.`);
    process.exitCode = 2;
    return;
  }
  const home = args.home ?? DEFAULT_HOME;
  const port = args.port === undefined ? DEFAULT_PORT : Number(args.port);
  switch (command) {
    case "install":
      return install({ exe: args.exe, home, port, dryRun: !!args.dryRun });
    case "start":
      return start({ dryRun: !!args.dryRun });
    case "stop":
      return stop({ dryRun: !!args.dryRun });
    case "status":
      return status({ home: args.home, dryRun: !!args.dryRun });
    default:
      throw new Error(`unknown command "${command}"; run with help to see the commands`);
  }
}

main().catch((err) => {
  console.error(`manage-host: ${err.message}`);
  process.exitCode = 1;
});
