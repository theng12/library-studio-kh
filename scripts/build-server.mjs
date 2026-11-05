// Builds the Go server as the Tauri sidecar for this machine (or TARGET=<rust triple> for another one).
import { execFileSync } from "node:child_process";
import { mkdirSync } from "node:fs";

const triple = process.env.TARGET || execFileSync("rustc", ["--print", "host-tuple"]).toString().trim();
const goos = { darwin: "darwin", linux: "linux", windows: "windows" }[triple.split("-")[2]] ?? (triple.includes("windows") ? "windows" : null);
const goarch = triple.startsWith("aarch64") ? "arm64" : triple.startsWith("x86_64") ? "amd64" : null;
if (!goos || !goarch) throw new Error("Unsupported target " + triple);

mkdirSync("src-tauri/binaries", { recursive: true });
const out = `src-tauri/binaries/library-studio-server-${triple}${goos === "windows" ? ".exe" : ""}`;
execFileSync("go", ["build", "-trimpath", "-ldflags", "-s -w", "-o", out, "./cmd/library-studio"], {
  stdio: "inherit",
  env: { ...process.env, GOOS: goos, GOARCH: goarch, CGO_ENABLED: "0" },
});
console.log("built " + out);
