# Packaging and releases

Library Studio KH is a Tauri 2 shell around one Go server, the same shape as Music Studio KH.
The Go server is pure Go (`CGO_ENABLED=0`), so it cross-compiles for every target from any machine.
Operating-system work stays in `internal/platform`, the guarded macOS setup/installation helpers and the Tauri shell. The packaged app supplies its server, client-mode switchback and macOS login-job controls; Node/Go/Homebrew are development tools, not end-user requirements.

## Mac (now)

```sh
npm ci
npm run build:desktop   # builds the Go sidecar for this Mac, then the .app and .dmg
```

Output: `src-tauri/target/release/bundle/dmg/Library Studio KH_<version>_aarch64.dmg`. Tauri signs nested code and the app with the existing `KH Studio Local Signing` identity stored on this build Mac (codesign reads it from the login Keychain at build time). Library itself never touches the Keychain: app secrets live only in the 0600 settings file. This local certificate is not notarised and needs no trust-settings change. Its private key must never leave this Mac. Stop at any password or key-access prompt and leave approval to the owner; never hand-seal the output with `codesign --deep`.

After building, run `python3 -I scripts/verify-macos-signing.py '<path-to-delivered.dmg>' --receipt '<receipt.json>'`. It verifies the app and executable requirements and starts the packaged host through a unique disposable launchd job submitted with `launchctl submit`, using isolated settings and no native connections. The test avoids importing an external plist or having launchd open ATLAS log files before starting the app. A readiness timeout retains `launchd-status.txt`; macOS unified logs identify startup errors for the unique test service. Keep the receipt; the next signed release adds `--previous-receipt '<previous-receipt.json>'` to require unchanged designated requirements across differing versions and executable bytes. These checks supplement the change-aware release gate. Installed 0.7.4 and 0.7.5 already prove stable app/helper requirements, changed executable bytes, launch health and original rollback integrity. Full Disk Access persistence remains separately deferred until the next naturally eligible app-owned purge; metadata checks are not content-access or purge proof. Follow the current shared `_orchestra/developer-handoff/SIGNING.md` for other-app rollout authority.
DMG verification uses the operating system's assigned read-only mount point and detaches its exact disk device. Keep the image, isolated launch settings and receipts on ATLAS; macOS refuses mounting inside that external worktree.
Rename to `Library Studio KH-<version>.dmg` (shared Studio KH name, no architecture suffix) before handing it to the Updater.

**Scan the artifact before publish.** The release gate ends by running `scripts/scan-release.sh '<dmg>'`, which prints the DMG's sha256 and bytes, runs `hdiutil verify`, then mounts it read-only and scans every file with the shared `kh-mirror-tools/scan-dmg.sh`. A non-zero exit blocks publish — a clean tree is not a clean artifact (issues row 20261120-138). Judge the artifact by its own contents, never by rebuilding it: a DMG is not byte-reproducible.

### Hidden packaged-webview check

The signing check above starts the packaged server, not its webview. The webview check is an **opt-in verification build only**: `cd src-tauri && cargo build --features webview-check`. The shipped release is never built with that feature (`npm run build:desktop` and `tauri build` do not enable it), so a release binary contains no check code at all. An ordinary or debug build without the feature **fails closed**: running it with `--webview-check` prints `native-webview-check: REFUSED` and exits nonzero before `app_context`, Tauri setup, HOME, host, window, menu or server work — it never falls through to a normal launch. That refusal is verified by running the feature-free binary with the flag first, before the feature build is compiled; `webview_check.rs` also holds a source test asserting the dispatch order (marker → entry → single `app_context`) and the refusal exit.

To run the check itself (never executed by packaging; only by an explicit harness step): start the isolated `scripts/test-ui.sh` fixture, then run the feature-built executable detached with `KH_TEST_URL` (the printed bare `http://127.0.0.1:<port>/` origin), `KH_TEST_HOME` (the printed `TEST_HOME`), `KH_TEST_PASSWORD` (the printed fixture password) and optionally `KH_WEBVIEW_CHECK_TIMEOUT` (seconds, 5–600, default 90 — the whole run's bound). The fixture must be test-ui.sh's layout: `home` with sibling `downloads` and `library` under one work directory. The home must be fresh: remove both `native-webview-startup.json` and `native-webview-result.json` first, or start a new fixture; the check refuses a home where either exists.

Before any Tauri/WebKit work the check **actually isolates** `HOME`, `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_CACHE_HOME`, `XDG_STATE_HOME` and `TMPDIR` into a run-owned `native-check-env-<runId>` directory, then writes a bounded startup marker, `native-webview-startup.json`, into `KH_TEST_HOME`: run nonce, PID, the executable's exact SHA-256, build flags (profile and feature set), the effective isolated paths, the fixture origin and home, and the start time. A construction watchdog armed from `KH_WEBVIEW_CHECK_TIMEOUT` writes a `phase: construction` failure receipt and exits 1 if window/WebKit construction has not completed at the deadline; the internal timer is a belt, never the substitute — the runner must bound the child PID externally from spawn through `wait()`, because a hang before the marker exists would otherwise have no stop. The runner's duties: bound that PID, `wait()` it for a clean status, treat a missing marker **or** a missing receipt as a failure and keep the marker, stderr and exit status as the evidence, and kill only the PID it started. The check spawns no child processes, never calls `pkill`, and never starts or stops the fixture or the owner's host — fixture lifecycle stays with `scripts/test-ui.sh`.

The check creates one hidden, unfocused, ephemeral webview against the fixture origin and signs in to the mock-only fixture. It writes `native-webview-result.json` in `KH_TEST_HOME` on **every** outcome it reaches — pass, failure, timeout (webview or construction) — and exits 0 only when the report is complete; partial cannot pass. Pass requires all of: the run nonce echoed back; the server's self-reported `localUrl` equal to `KH_TEST_URL` with its served `downloadPath` and `libraryPath` inside the fixture work directory (the URL↔fixture binding — file presence or the page title never counts as proof); all nine required path controls offering a chooser and accepting a typed path (`controls == chosen == typeable == 9`); a clipboard result whose feedback state is success **and** a demonstrated clipboard write with matching readback (an error message like `Could not copy` proves only the error path); and the in-app folder picker choosing exactly the directory it displayed, inside the fixture, in a Settings folder that stays typeable afterwards. The receipt keeps the raw report with every distinguished field, plus native visibility/focus, run nonce and fixture identity.

Still **not checked** by this mode: DOM clicks in a hidden webview prove no native keyboard or trusted user-gesture behavior; the native `NSOpenPanel` branch stays a separate real-window check on a permitted test machine or an agreed quiet point; clipboard readback unavailable in a hidden WKWebView makes the check **fail** with that reason — a harness limitation, never a product pass, a product defect or a waiver; and packaged-app window rendering and focus remain separate evidence.

### Window lifecycle check

`scripts/test-window-lifecycle.sh` proves the native window behaviour on a real but hidden, unfocused window with the same `--features webview-check` build (`--window-lifecycle-check`; the release build refuses that flag like `--webview-check`). It starts its own server and mock stock on an isolated home, publishes the hold on 18643 so the owner's live hold on 8643 is untouched, and proves in one receipt: the window starts hidden; the shipped close handler hides it and the window itself survives (a destroyed window would fail, so this is hide, not quit-and-rebuild); the shared Show action makes that same window visible again; and the update hold answers `200` while no window is visible. It never opens a visible window and never reads the owner's credential file.

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

### Settings survive an update

`scripts/test-settings-survive-update.sh` is the release test for the owner's settings across an update: it writes non-default values through the running headless server's own `POST /api/settings`, stops that server, starts a second build over the same isolated home and reads every value back through `GET /api/state`, printing one line per checked value and exiting non-zero if any dropped. It runs before any release that touches settings or the update path; a failure is a release blocker, not a warning. `go test ./internal/config/` holds the same guarantee at the loader: non-default values read back unchanged after reopening, opening never rewrites `settings.json`, and a partial Settings-form save keeps every omitted field.

### Update hold

`scripts/test-update-hold.sh` is the release test for the shared update hold (`_orchestra/youtube-stack/update-hold/CONTRACT.md`). It starts a real headless Library on an isolated home and proves, with live endpoints: a hold without the installer's credential is refused and pauses nothing; a held app refuses new work and says why in plain words; the hold answers with the real `in_flight`; `DELETE` resumes at once; a hold with no `DELETE` expires after `pause_s`; and a restart resumes by itself with no operator step. It drives the hold on its published fixed address `http://127.0.0.1:8643/api/maintenance/hold`, and fails loudly if that port is taken by something else. Run it before any release that touches the hold, dispatch or startup path; a failure is a release blocker. The release also passes `--hold-url http://127.0.0.1:8643/api/maintenance/hold` to `khu publish` and reads the served `holdURL` back.

The hold takes the Launcher's own credential, the same handoff Printer, Media and Hub use: a `0600` file at `~/Library/KHUpdater/state/hold-token`, read fresh on every request (so an atomic Launcher rotation needs no restart) and presented as `X-Hub-Token` or `Authorization: Bearer`. A missing, wrong, world-readable or symlinked file answers `401` and pauses nothing, and an `Origin`-bearing request is refused outright — being on loopback is not proof of being the installer. The test sets `KHU_HOLD_TOKEN_FILE` to its own file in `$TMPDIR` and never reads the owner's. `LIBRARY_STUDIO_HOLD_TOKEN_FILE` is accepted as a per-app override first.

## KH Updater feed

Follow the Updater lead's route: build the DMG, record its path, SHA-256, version and one-line description, and hand that to the Updater lead. They publish with `khu publish --description` (current + one previous) only after Developer gives the go. This repo never publishes itself.

Suggested description: "See what's downloading, what's in your library and what's coming up, and clear local copies once they're safely on the NAS."

## Installed acceptance and local installer retention

Finish the normal release flow after Updater installation and intended-machine verification by retaining the accepted installer plus its previous verified rollbacks in the canonical project's `dist/`. Use the shared portfolio `tools/release-dist.py` contract described in its README, **Release dist retention**. `npm run retain:accepted` invokes that helper; there is no second retention implementation and no build, signing or publication hook.

The release lead writes a trusted `schema: 1` pin document with `apps: [{app, project, releases}]`. Set `app` to `Library Studio KH` and `project` to the canonical real project directory, even when running the command from an ATLAS worktree. Each of the one to three `releases` entries supplies the exact accepted `version`, absolute `.dmg` `source`, verified `sha256`, positive `bytes` and non-empty `evidence` pointing to its installed/verified acceptance receipts. Sources may have any basename, including a feed's hash-prefixed name. Match the source's actual bytes to the accepted release receipt; never infer acceptance from the latest feed, the build folder or `package.json`. A missing older source is a recorded dependency, not permission to replace it with an unaccepted candidate.

Before invoking retention, check dependencies and active jobs, preserve evidence, and obtain the existing quiet point if retirement is needed. The helper has no dry-run and does not establish acceptance. Keep the newest verified build and the one before it — two artifacts, matching the owner's settled retention (2026-11-16) and the two kept on 1301's feed; older installers rotate to `_trash` with a manifest. After a later version is verified, pin that exact version and its one previous verified version.

```bash
npm run retain:accepted -- /absolute/coordination/library-accepted-dist-pins.json
```

Record the pin document, helper source hash, JSON result and resulting `dist/.release-dist.json` with the release evidence, then recheck every retained hash and byte size. The helper copies or adopts identical bytes into `Library Studio KH-<version>.dmg`; it never moves the source. It retires only obsolete files that its ledger owns and whose recorded bytes still match. Unmanaged files remain untouched: this is never a whole-`dist` wipe, and pending installers are not managed pins. Re-running the same set is idempotent.

Retention is automatic (owner, 2026-11-12). `npm run retain:accepted -- <pins>` runs the shared portfolio helper directly, which adopts the pinned pair, copies it under the shared versioned names and retires only files its ledger owns (recoverable, never overwritten). `scripts/release-retain.py` remains as the older wrapper for a manual rotation; the normal release uses the shared helper. No hand step: a version older than the latest three never needs a person.

Two is a depth, not a quota (owner, 2026-11-16): the two come from new releases only. When fewer verified versions exist, keep what exists and never restore an older installer to fill a slot. Current depth is two — 0.7.26 and 0.7.25.

Guards: a version named by the live or previous feed release (`feed/release.json`, `feed/previous-release.json`), the dist ledger, `feed/artifact-retention.json`, the updater's state or rollback records, a queued publish job, or a `base/versions` runtime is never moved. Every move is logged in the receipt with its version, SHA-256, byte size and destination; skipped versions are logged with the reason. Swap the wrapper for the shared helper once Updater ships one; the behaviour stays the same.

The mock contract check is part of the `host` lane and can run alone for this change:

```bash
python3 -I scripts/test_release_dist.py
```

Run active worktrees, tests and caches on the internal disk. Lanes inherit the local environment's temporary directory and Go cache defaults; any explicit `TMPDIR`, `GOTMPDIR` or `GOCACHE` override must also resolve to the internal disk. The policy's `tmpDir` is not a test-process environment override. This checks the current installed shared helper through the same command, using synthetic DMGs only. A shared helper change requires a fresh focused contract result and recorded helper hash; an unchanged Library receipt alone cannot qualify changed external tooling.
