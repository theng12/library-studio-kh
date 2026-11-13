# Acceptance — Library Studio KH 0.7.16

Owner rule 2026-11-13: before a page is scored, this file carries the task list
with an end-to-end test per task, a capability cross-check from live data, and a
standard-practice comparison. The fuller per-claim map (file and test names,
probe evidence) is in `_orchestra/library-lead/ui-overhaul-20261113/COMPLETENESS-0.7.16.md`.

## 1. Task list — one end-to-end test per claimed behaviour

| Claimed behaviour | Test that fails if it breaks |
|---|---|
| One status and one import instant across Activity, Downloads, My library and Cleanup | `internal/library/inventory_test.go::TestInventoryJoinsProofByMediaID`, `::TestInventoryShowsActiveWorkAsImportingOrDownloading`, `internal/library/download_queue_test.go::TestDownloadQueueJoinsSavedProofByHash` |
| A verified copy never reads verified without proof | `tests/activity-state.test.mjs::a verified copy names its proof and location; an unproven one says so`, `tests/library-grid.test.mjs::detail model carries proof, location and history newest first` |
| Metadata is cache-only on the request path; prefetch fills it once | `internal/library/inventory_test.go::TestPrefetchCachesOnceThenStaysCacheOnly`, `internal/metadata/cached_test.go` |
| A provider outage is stated, never silent | `tests/ui-wiring.test.mjs::a metadata provider outage is stated on both lists` |
| Verifying a copy re-checks saved hashes in place | `internal/library/staged_cleanup_test.go::TestVerifyReChecksTheSavedHashesWithoutMovingAnything` |
| Clear/Remove touch only qBittorrent, never files or an arr queue | `internal/server/controls_test.go::TestDownloadRemoveKeepsFilesAndRevealIsGuarded` |
| Dead cleanup: off never acts; grace; metadata-only; proof/paused untouched | `internal/library/dead_downloads_test.go` |
| Arr-queue cleanup: off calls nothing; one queue DELETE with removeFromClient=false, blocklist=false | `internal/library/dead_arr_test.go` |
| Dead preview is served to the Settings card and row notes | `internal/server/controls_test.go::TestControlRoutes` (GET /api/downloads/dead) |
| Cleanup: preview is read-only, restore and Remove now are recorded, nothing is deleted for good outside its waits | `internal/library/staged_cleanup_test.go`, `internal/library/trash_removal_test.go` |
| Downloads panel: columns, one primary action + ⋯, missing rows offer Clear, no invented speed/ETA | `tests/downloads-panel.test.mjs` |
| Library cards/table remembered, filters exact, phone detail sheet with Back | `tests/library-view.test.mjs`, `tests/library-grid.test.mjs` |
| Settings: four tabs, disclosures, saved feedback clears, dead-cleanup switches | `tests/layout-frame.test.mjs`, `tests/appearance-footer.test.mjs` |
| Contrast ≥ 4.5:1 in light and dark | `tests/contrast.test.mjs` (runs `scripts/contrast-report.mjs`) |
| Keyboard: Escape returns focus, arrows in the grid, Cmd/Ctrl+F | `tests/keyboard-shortcuts.test.mjs`, `tests/ui-wiring.test.mjs::dialog closures return focus to the control that opened them` |

## 2. Capability cross-check (live routes/config vs controls)

Every owner-facing route and setting has a control, or a documented exception:

- `GET /api/state`, `/api/library`, `/api/library/detail` → Activity, My library (grid, detail, 30 s refresh).
- `GET /api/downloads`, `POST /api/downloads/action`, `/api/downloads/reveal`, `GET /api/downloads/dead` → Downloads table and actions, Settings dead-cleanup card.
- `POST /api/cleanup/{preview,run,restore,remove-now,verify}`, `GET /api/cleanup/status` → Cleanup page and proof dialog.
- `GET/POST /api/companion/settings`, `/api/browse`, `/api/find`, `/api/add`, `/api/media/*`, `/api/sources*`, `/api/setup/*`, `/api/auth/*`, `/api/torrents`, `/api/poster`, `/api/update/*` → their pages.
- Documented exceptions: `GET /api/health` and `/api/host-proof` belong to the desktop shell and Launcher readiness, not owner controls; the retired `cleanupOn`/`cleanupDays` permanent-delete policy stays off by owner safety rule.

## 3. Standard-practice comparison per page

- **Activity** (reference: Sonarr/Radarr History, qBittorrent Log): present — timeline, app/outcome filters, grouped warnings, deduped ×N, terminal outcomes with paths. Deliberately absent — per-event drawer (row carries it), live log streaming. Missing: none.
- **Downloads** (reference: qBittorrent + Sonarr Queue): present — live-only rows, status/progress/speed/ETA, source/target, selection and bulk with reasons, one primary action + ⋯, proof dialog, dead-row grace. Deliberately absent — torrent limit editing and per-file priority (qBittorrent owns them). Missing: none.
- **My library** (reference: Plex/Apple TV library, Radarr/Sonarr): present — poster grid, remembered Cards/Table, search/filters, detail with proof hash and real volume, full details, monitoring. Deliberately absent — playback and metadata editing (the arrs own both). Missing: none.
- **Cleanup** (reference: Sonarr file management, macOS Trash): present — status first, policy, preview, per-row actions, proof, waits and blockers, Restore/Remove now. Deliberately absent — a permanent-delete bypass (contradicts the owner's staged safety rule). Missing: none.
- **Settings** (reference: macOS System Settings, Sonarr Settings): present — tabs, disclosure cards, save feedback, sessions with peer, dead-download switches. Deliberately absent — a device registry (sessions name the peer and last seen instead). Missing: none.
