# Packaging and releases

Library Studio KH is a Tauri 2 shell around one Go server, the same shape as Music Studio KH.
The Go server is pure Go (`CGO_ENABLED=0`), so it cross-compiles for every target from any machine.
Operating-system work stays in `internal/platform`, the guarded macOS setup/installation helpers and the Tauri shell. The packaged app supplies its server, client-mode switchback and macOS login-job controls; Node/Go/Homebrew are development tools, not end-user requirements.

## Mac (now)

```sh
npm ci
npm run build:desktop   # builds the Go sidecar for this Mac, then the .app and .dmg
```

Output: `src-tauri/target/release/bundle/dmg/Library Studio KH_<version>_aarch64.dmg`. Ad-hoc signed, not notarised (same as the other Studio KH apps).
Rename to `Library Studio KH-<version>.dmg` (shared Studio KH name, no architecture suffix) before handing it to the Updater.

## Windows and Linux (plan)

Tauri can't cross-build installers, so each is built on its own OS, the way Music Studio KH's pilot did it:

1. Add a GitHub Actions workflow with a manual trigger and two jobs on free hosted runners: `windows-latest` (NSIS `.exe`) and `ubuntu-24.04` (`.deb` and `.AppImage`). Each job runs `npm ci && npm run build:desktop` with Go, Rust and Node installed. Linux also needs `libwebkit2gtk-4.1-dev libgtk-3-dev librsvg2-dev`.
2. Smoke test per job: `go test ./...`, then start the built sidecar, read its `LISTENING` line and fetch `/` and `/api/state` with the token. Linux can also open the installed app under Xvfb like Music's desktop E2E.
3. First releases are labelled "build-only, not runtime-tested" for Windows until someone opens it on a real Windows PC; say so in CHANGELOG.md.
4. No signing certificates or paid services. Windows SmartScreen will warn on the unsigned installer.

Paths: settings live in the OS config folder (`~/Library/Application Support`, `%AppData%`, `~/.config`) under `Library Studio KH`. Download and library folders are settings, so Windows drive letters and Linux mount points work unchanged.

## Version and What's New

`CHANGELOG.md` is the single source. Its top `## <version> — <date>` heading is the version shown in the app; `go test ./...` fails if `package.json`, `src-tauri/tauri.conf.json` or `src-tauri/Cargo.toml` disagree. Keep 1–6 plain bullets per version, newest first.

## KH Updater feed

Follow the Updater lead's route: build the DMG, record its path, SHA-256, version and one-line description, and hand that to the Updater lead. They publish with `khu publish --description` (current + one previous) only after Developer gives the go. This repo never publishes itself.

Suggested description: "See what's downloading, what's in your library and what's coming up, and clear local copies once they're safely on the NAS."
