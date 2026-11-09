# Packaging and releases

Library Studio KH is a Tauri 2 shell around one Go server, the same shape as Music Studio KH.
The Go server is pure Go (`CGO_ENABLED=0`), so it cross-compiles for every target from any machine.
Operating-system work stays in `internal/platform`, the guarded macOS setup/installation helpers and the Tauri shell. The packaged app supplies its server, client-mode switchback and macOS login-job controls; Node/Go/Homebrew are development tools, not end-user requirements.

## Mac (now)

```sh
npm ci
npm run build:desktop   # builds the Go sidecar for this Mac, then the .app and .dmg
```

Output: `src-tauri/target/release/bundle/dmg/Library Studio KH_<version>_aarch64.dmg`. Tauri signs nested code and the app with the existing `KH Studio Local Signing` identity in this build Mac's login Keychain. This local certificate is not notarised and needs no trust-settings change. Its private key must never leave this Mac. Stop at any password or key-access prompt and leave approval to the owner; never hand-seal the output with `codesign --deep`.

After building, run `python3 -I scripts/verify-macos-signing.py '<path-to-delivered.dmg>' --receipt '<receipt.json>'`. It verifies the app and executable requirements and starts the packaged host through a unique disposable launchd job with isolated settings and no native connections. Keep the receipt; the next signed release adds `--previous-receipt '<previous-receipt.json>'` to require unchanged designated requirements across differing versions and executable bytes. These checks supplement the change-aware release gate. Installed 0.7.4 and 0.7.5 already prove stable app/helper requirements, changed executable bytes, launch health and original rollback integrity. Full Disk Access persistence remains separately deferred until the next naturally eligible app-owned purge; metadata checks are not content-access or purge proof. Follow the current shared `_orchestra/developer-handoff/SIGNING.md` for other-app rollout authority.
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

## Change-aware checks

Use the existing shared runner; `test-policy/projects.json` resolves the project and ignored receipt/temp directories relative to this checkout. Keep worktrees and build/test output on ATLAS. The lane environment explicitly records the shared ATLAS Go cache and temporary directories under `KH-Worktrees/library-studio-kh/.cache/{go,tmp}`; create those directories before the first run. If the checkout moves to another approved disk, update those config settings before running so the receipt records the new environment. Confirm free disk against measured need before heavy work, run long jobs through `_orchestra/tools/run-detached.sh`, and use one heavy Library lane at a time.

```sh
~/Developer/portfolio/bin/testctl plan library --config test-policy/projects.json
```

Run the lanes covering the change and its callers with `testctl run library <lane> --config test-policy/projects.json`. `testctl check library <lane> --config test-policy/projects.json --json` supplies the original receipt, source commit and log hash. No new runner or bypass flag is used.

| Lane | Covered work and invalidating inputs |
| --- | --- |
| `frontend` | All Node UI regressions; web assets, tests and package metadata |
| `core` | Config, stock API, library/removal, platform and package installer tests; all their transitive Go inputs |
| `setup` | Setup plus HTTP/CLI callers; all internal Go and embedded assets, root release metadata |
| `version` | Version/What's New agreement; Go module, root Go, changelog and bundle manifests |
| `host` | Mock-only host-manager and signing-verifier checks; scripts and shell configuration |
| `fast` | Cheap global vet for macOS, Windows and Linux; always assess for a release |
| `full` | Full mock regression fallback when reach cannot safely be bounded or prior full evidence is unavailable |
| `package` | Fresh dependency install and normal signed desktop packaging on the frozen release source |

The shared runner also keys lockfiles, commands, configured environment and local tool versions. A dependency/toolchain/shared-core change can invalidate several lanes; use `full` when its reach is uncertain. Do not narrow paths to manufacture reuse. Keep covered tests, imported helpers and fixtures in the lane's inputs when adding a dependency.

Before release, freeze a clean commit, run `plan`, and retain the matching receipts and exact logs. Reused coverage must read `skipped (unchanged since <original commit>)`; keep its original receipt and log hash rather than claiming a fresh pass. Do not import a historical receipt merely because a similarly named test passed. The existing 0.7.5 full-gate receipt remains historical evidence, not a fabricated testctl receipt.

Packaging and the delivered-DMG signing/launchd check above are still required for the actual candidate. Compare against the previous signed receipt, verify the artifact's own version/hash, and use normal Updater installation/rollback verification after Developer's go. The shared release-proof bridge must validate these receipts before this policy can qualify a release; local lane acceptance alone does not authorize publication or installation.

## KH Updater feed

Follow the Updater lead's route: build the DMG, record its path, SHA-256, version and one-line description, and hand that to the Updater lead. They publish with `khu publish --description` (current + one previous) only after Developer gives the go. This repo never publishes itself.

Suggested description: "See what's downloading, what's in your library and what's coming up, and clear local copies once they're safely on the NAS."
