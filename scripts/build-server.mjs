// Builds the Go server as the Tauri sidecar for this machine (or TARGET=<rust triple> for another one).
import { execFileSync } from "node:child_process";
import { chmodSync, copyFileSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";

const triple = process.env.TARGET || execFileSync("rustc", ["--print", "host-tuple"]).toString().trim();
const goos = { darwin: "darwin", linux: "linux", windows: "windows" }[triple.split("-")[2]] ?? (triple.includes("windows") ? "windows" : null);
const goarch = triple.startsWith("aarch64") ? "arm64" : triple.startsWith("x86_64") ? "amd64" : null;
if (!goos || !goarch) throw new Error("Unsupported target " + triple);

mkdirSync("src-tauri/binaries", { recursive: true });
const out = `src-tauri/binaries/library-studio-server-${triple}${goos === "windows" ? ".exe" : ""}`;
// macOS needs cgo: the native folder panel is a small AppKit file in
// internal/platform. Other targets keep the plain static build and use the
// in-app directory browser instead.
execFileSync("go", ["build", "-trimpath", "-ldflags", "-s -w", "-o", out, "./cmd/library-studio"], {
  stdio: "inherit",
  env: { ...process.env, GOOS: goos, GOARCH: goarch, CGO_ENABLED: goos === "darwin" ? "1" : "0" },
});
console.log("built " + out);

// The login job runs this helper bundle, so macOS shows a named, icon-bearing
// helper instead of the bare server binary. Sign it before Tauri bundles it.
if (goos === "darwin") {
  const { version } = JSON.parse(readFileSync("package.json", "utf8"));
  const helper = "src-tauri/helper/Library Studio KH Helper.app";
  const macos = `${helper}/Contents/MacOS`;
  const resources = `${helper}/Contents/Resources`;
  mkdirSync(macos, { recursive: true });
  mkdirSync(resources, { recursive: true });
  copyFileSync(out, `${macos}/Library Studio KH Helper`);
  chmodSync(`${macos}/Library Studio KH Helper`, 0o755);
  copyFileSync("src-tauri/icons/icon.icns", `${resources}/icon.icns`);
  writeFileSync(`${helper}/Contents/Info.plist`, `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleDevelopmentRegion</key>
	<string>en</string>
	<key>CFBundleDisplayName</key>
	<string>Library Studio KH Helper</string>
	<key>CFBundleExecutable</key>
	<string>Library Studio KH Helper</string>
	<key>CFBundleIconFile</key>
	<string>icon.icns</string>
	<key>CFBundleIdentifier</key>
	<string>app.librarystudiokh.helper</string>
	<key>CFBundleInfoDictionaryVersion</key>
	<string>6.0</string>
	<key>CFBundleName</key>
	<string>Library Studio KH Helper</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleShortVersionString</key>
	<string>${version}</string>
	<key>CFBundleVersion</key>
	<string>${version}</string>
	<key>LSMinimumSystemVersion</key>
	<string>13.0</string>
	<key>LSUIElement</key>
	<true/>
	<key>NSHighResolutionCapable</key>
	<true/>
</dict>
</plist>
`);
  execFileSync("codesign", [
    "--force", "--sign", "KH Studio Local Signing", "--identifier", "app.librarystudiokh.helper",
    "--options", "runtime", helper,
  ], { stdio: "inherit" });
  console.log("built and signed " + helper);
}
