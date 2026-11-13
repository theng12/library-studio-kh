# Library Studio KH — agent guide

Direct instructions from the owner (theng12) override this file. If it disagrees with the code, the code wins and this file gets fixed.

## Efficient delegation and parallel work (owner, 2026-11-10)

Canonical: [_orchestra/developer-handoff/LEAD-RULES.md](/home/user/Developer/_orchestra/developer-handoff/LEAD-RULES.md) → "Efficient delegation and parallel work", with "Who does what", "Every task, at all cost" and "Hard limits"; that file wins. Use the global efficient-orchestration skill; app-specific commands and holds stay here.

- Delegate complete deliverables: a useful, inexpensive, suitable worker owns one bounded, non-overlapping deliverable (named files/worktree/base commit), its implementation and routine fix loop; the lead plans, checks the actual work, integrates and accepts. Developer keeps portfolio priorities, cross-app/protected decisions and final release/live acceptance; app leads accept worker results within their brief. Existing model routing stays unchanged.
- Independent app leads work in parallel; never park one app's independent work for another app's schedule. Serialize only real conflicts: a shared writer/worktree, or shared heavy tests and target actions. Shared heavy tests use the existing KHApps `testctl` schedule; device, security, paid/public, Production and quiet-point holds remain.
- Give workers realistic fixtures and early permitted read-only checks; verify a small result before handoff. Reuse source-bound unchanged passing results; rerun changed or failed checks and affected callers, using a full gate when reach requires it. This section adds no approval gate: existing release gates and holds still apply.

## What this is

A media management application over separate official qBittorrent, Sonarr, Radarr, Lidarr and Prowlarr apps, using their supported APIs. Guided setup downloads and verifies official companion packages; never implement a media downloader or indexer of our own. Cover installation, configuration, readiness and recovery rather than stopping at wrappers. See [README.md](README.md) for the removal safety rule; never weaken it.

## Layout

| Path | What |
| --- | --- |
| `cmd/library-studio/` | Go entry point: dashboard server on 127.0.0.1, hourly cleanup |
| `internal/stock/` | API clients for the stock apps |
| `internal/library/` | Dashboard list, removal rule, stock-app settings |
| `internal/config/` | Settings file in the OS config folder |
| `internal/platform/` | The only OS-specific code |
| `internal/server/` | HTTP API and the embedded dashboard (`web/`, plain HTML/JS, no build step) |
| `src-tauri/` | Tauri 2 shell that starts the Go sidecar |
| `tests/mock-stock/` | Fake stock apps for manual looks |
| `changelog.go`, `CHANGELOG.md` | Version and What's New (single source) |

## Verify before claiming done

Use the shared `~/Developer/portfolio/bin/testctl` with this repo's `test-policy/projects.json`; see [docs/PACKAGING.md](docs/PACKAGING.md#change-aware-checks). Start with `testctl plan library --config test-policy/projects.json`, run the changed lanes and their callers, then the cheap global `fast` checks. Reuse unchanged passing receipts with their original commit; report them as skipped, never freshly passed. A full regression means running every configured testing lane (frontend, core, setup, version, host, fast); force it only when an actual dependency, toolchain or reach change requires fresh full checks. Fresh packaging, signature and launch verification are always separate from lane execution. A release also needs a fresh packaged launch/signature/update check and Developer's go; a lane receipt alone is not a release proof.

Tests use mocks only. Never call real stock apps, the NAS or the 1301 Mac from a test.

## Rules

- ponytail on every coding task; impeccable before UI edits; graphify before hand searches when a graph exists.
- Commit as `112568204+theng12@users.noreply.github.com`.
- No `rm`: move files to `~/Developer/_trash/library-studio-kh/`.
- Paths are settings, never hard-coded. Optional features off by default. Legacy permanent-delete cleanup is retired; the owner-confirmed staged policy in docs/STORAGE_POLICY.md stays off until implemented and verified.
- Releases go through the Updater lead after Developer says go ([docs/PACKAGING.md](docs/PACKAGING.md)).
