// Builds the Go sidecar and the signed desktop bundle, then proves the shipped
// code carries no owner path. Rust embeds its absolute source paths unless they
// are remapped, so every build maps them away and the shipped DMG is checked.
import { execFileSync } from "node:child_process";
import { mkdtempSync, readdirSync, readFileSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { join } from "node:path";

execFileSync("node", ["scripts/build-server.mjs"], { stdio: "inherit" });

const home = homedir();
const rustflags = [
  process.env.RUSTFLAGS ?? "",
  // rustc lets the last matching prefix win, so the registry rule comes last:
  // it must also remove the ".cargo/registry" token, not just the owner name.
  `--remap-path-prefix=${home}=/home/owner`,
  `--remap-path-prefix=${home}/.cargo/registry=/rust/registry`,
].join(" ").trim();
execFileSync(join("node_modules", ".bin", "tauri"), ["build"], {
  stdio: "inherit",
  env: { ...process.env, RUSTFLAGS: rustflags },
});

// Tauri deletes the .app after wrapping it in the DMG, so the check reads the
// delivered image: the same bytes the owner would install.
const config = JSON.parse(readFileSync("src-tauri/tauri.conf.json", "utf8"));
const arch = execFileSync("rustc", ["--print", "host-tuple"]).toString().trim().split("-")[0];
const dmg = `src-tauri/target/release/bundle/dmg/${config.productName}_${config.version}_${arch}.dmg`;
const markers = [home, "_orchestra", ".cargo/registry"];
const leaks = [];
let scanned = 0;
const walk = (dir) => {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) walk(path);
    else if (entry.isFile()) {
      scanned++;
      const bytes = readFileSync(path);
      for (const marker of markers) if (bytes.includes(marker)) leaks.push(`${marker} in ${path}`);
    }
  }
};
const mount = mkdtempSync(join(tmpdir(), "library-paths-"));
try {
  execFileSync("hdiutil", ["attach", "-readonly", "-nobrowse", "-mountpoint", mount, dmg], { stdio: "ignore" });
  walk(mount);
  if (leaks.length) {
    console.error("owner paths found in the shipped app:\n  " + leaks.join("\n  "));
    process.exitCode = 1;
  } else {
    console.log(`no owner paths in the shipped app: ${scanned} files checked, ${markers.join(" / ")} absent`);
  }
} finally {
  execFileSync("hdiutil", ["detach", mount], { stdio: "ignore" });
}
