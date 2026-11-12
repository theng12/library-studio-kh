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

After building, run `python3 -I scripts/verify-macos-signing.py '<path-to-delivered.dmg>' --receipt '<receipt.json>'`. It verifies the app and executable requirements and starts the packaged host through a unique disposable launchd job submitted with `launchctl submit`, using isolated settings and no native connections. The test avoids importing an external plist or having launchd open ATLAS log files before starting the app. A readiness timeout retains `launchd-status.txt`; macOS unified logs identify startup errors for the unique test service. Keep the receipt; the next signed release adds `--previous-receipt '<previous-receipt.json>'` to require unchanged designated requirements across differing versions and executable bytes. These checks supplement the change-aware release gate. Installed 0.7.4 and 0.7.5 already prove stable app/helper requirements, changed executable bytes, launch health and original rollback integrity. Full Disk Access persistence remains separately deferred until the next naturally eligible app-owned purge; metadata checks are not content-access or purge proof. Follow the current shared `_orchestra/developer-handoff/SIGNING.md` for other-app rollout authority.
DMG verification uses the operating system's assigned read-only mount point and detaches its exact disk device. Keep the image, isolated launch settings and receipts on ATLAS; macOS refuses mounting inside that external worktree.
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

Use the existing shared runner; `test-policy/projects.json` resolves the project and ignored receipt/temp directories relative to this checkout. Keep active worktrees, build/test output and caches on the internal disk; inherit local temporary-directory and Go cache defaults. Confirm free disk against measured need before heavy work, run long jobs through `_orchestra/tools/run-detached.sh`, and use one heavy Library lane at a time.

```sh
~/Developer/portfolio/bin/testctl plan library --config test-policy/projects.json
```

Run the lanes covering the change and its callers with `testctl run library <lane> --config test-policy/projects.json`. `testctl check library <lane> --config test-policy/projects.json --json` supplies the original receipt, source commit and log hash. No new runner or bypass flag is used.

| Lane | Covered work and invalidating inputs |
| --- | --- |
| `frontend` | All Node UI and desktop-menu regressions; web assets, tests, package metadata and native menu source |
| `core` | Config, stock API, library/removal, platform and package installer tests; all their transitive Go inputs |
| `setup` | Setup plus HTTP/CLI callers; all internal Go and embedded assets, root release metadata |
| `version` | Version/What's New agreement; Go module, root Go, changelog and bundle manifests |
| `host` | Mock-only host-manager and signing-verifier checks; scripts and shell configuration |
| `fast` | Cheap global vet for macOS, Windows and Linux; always assess for a release |
| `package` | Fresh dependency install and normal signed desktop packaging on the frozen release source; `CI=true` uses Tauri's headless DMG layout in detached jobs |

The shared runner also keys lockfiles, commands, configured environment and local tool versions. A dependency/toolchain/shared-core change can invalidate several lanes; a full regression means running every configured testing lane (frontend, core, setup, version, host, fast) rather than a separate lane. Force it only when an actual dependency, toolchain or reach change requires fresh full checks; fresh packaging, signature and launch verification are always separate. Do not narrow paths to manufacture reuse. Keep covered tests, imported helpers and fixtures in the lane's inputs when adding a dependency.

Before release, freeze a clean commit, run `plan`, and retain the matching receipts and exact logs. Reused coverage must read `skipped (unchanged since <original commit>)`; keep its original receipt and log hash rather than claiming a fresh pass. Do not import a historical receipt merely because a similarly named test passed. The existing 0.7.5 full-gate receipt remains historical evidence, not a fabricated testctl receipt.

Packaging and the delivered-DMG signing/launchd check above are still required for the actual candidate. Compare against the previous signed receipt, verify the artifact's own version/hash, and use normal Updater installation/rollback verification after Developer's go. The shared release-proof bridge must validate these receipts before this policy can qualify a release; local lane acceptance alone does not authorize publication or installation.

## KH Updater feed

Follow the Updater lead's route: build the DMG, record its path, SHA-256, version and one-line description, and hand that to the Updater lead. They publish with `khu publish --description` (current + one previous) only after Developer gives the go. This repo never publishes itself.

Suggested description: "See what's downloading, what's in your library and what's coming up, and clear local copies once they're safely on the NAS."

## Installed acceptance and local installer retention

Finish the normal release flow after Updater installation and intended-machine verification by retaining the accepted installer plus its previous verified rollbacks in the canonical project's `dist/`. Use the shared portfolio `tools/release-dist.py` contract described in its README, **Release dist retention**. `npm run retain:accepted` invokes that helper; there is no second retention implementation and no build, signing or publication hook.

The release lead writes a trusted `schema: 1` pin document with `apps: [{app, project, releases}]`. Set `app` to `Library Studio KH` and `project` to the canonical real project directory, even when running the command from an ATLAS worktree. Each of the one to three `releases` entries supplies the exact accepted `version`, absolute `.dmg` `source`, verified `sha256`, positive `bytes` and non-empty `evidence` pointing to its installed/verified acceptance receipts. Sources may have any basename, including a feed's hash-prefixed name. Match the source's actual bytes to the accepted release receipt; never infer acceptance from the latest feed, the build folder or `package.json`. A missing older source is a recorded dependency, not permission to replace it with an unaccepted candidate.

Before invoking retention, check dependencies and active jobs, preserve evidence, and obtain the existing quiet point if retirement is needed. The helper has no dry-run and does not establish acceptance. Keep the newest **three** verified builds (**0.7.11, 0.7.10 and 0.7.9** as of 2026-11-12), matching the three kept in 1301's feed artifacts; older installers rotate to `_trash` automatically. After a later version is verified, pin that exact version and its two previous verified versions.

```bash
npm run retain:accepted -- /absolute/coordination/library-accepted-dist-pins.json
```

Record the pin document, helper source hash, JSON result and resulting `dist/.release-dist.json` with the release evidence, then recheck every retained hash and byte size. The helper copies or adopts identical bytes into `Library Studio KH-<version>.dmg`; it never moves the source. It retires only obsolete files that its ledger owns and whose recorded bytes still match. Unmanaged files remain untouched: this is never a whole-`dist` wipe, and pending installers are not managed pins. Re-running the same set is idempotent.

Retention is automatic (owner, 2026-11-12). `npm run retain:accepted -- <pins>` runs the shared helper and then `scripts/release-retain.py`, which moves any remaining Library installer older than the newest three into this project's `_trash/<date> library dist rotation/` (recoverable, never overwritten). No hand step: a version older than the latest three never needs a person.

Guards: a version named by the live or previous feed release (`feed/release.json`, `feed/previous-release.json`), the dist ledger, `feed/artifact-retention.json`, the updater's state or rollback records, a queued publish job, or a `base/versions` runtime is never moved. Every move is logged in the receipt with its version, SHA-256, byte size and destination; skipped versions are logged with the reason. Swap the wrapper for the shared helper once Updater ships one; the behaviour stays the same.

The mock contract check is part of the `host` lane and can run alone for this change:

```bash
python3 -I scripts/test_release_dist.py
```

Run active worktrees, tests and caches on the internal disk. Lanes inherit the local environment's temporary directory and Go cache defaults; any explicit `TMPDIR`, `GOTMPDIR` or `GOCACHE` override must also resolve to the internal disk. The policy's `tmpDir` is not a test-process environment override. This checks the current installed shared helper through the same command, using synthetic DMGs only. A shared helper change requires a fresh focused contract result and recorded helper hash; an unchanged Library receipt alone cannot qualify changed external tooling.
