# Staged local-copy policy — owner specification, 2026-11-06

The staged engine and Cleanup controls are implemented in 0.6.0. It starts off; deployment preserves cleanup OFF on1301. Preview is read-only and available while off. Enabling the saved policy is a deliberate action. Native Trash support is macOS only; other platforms report that the engine is unavailable.

## Editable defaults confirmed by the owner

- Start the retention clock at the whole download's verified successful library import, never merely torrent completion.
- Keep the local download in its normal staging location for 3 days.
- At age3days, move its local copy into Library's managed `_trash`.
- At age7days TOTAL from verified import, move it into macOS Trash. This is4 additional days in managed `_trash`, not7 additional days.
- Normally retain it in macOS Trash for30 more days. Thus ordinary final retirement would be about37days after verified import, subject to successful stage transitions.
- If actual free space on the filesystem holding the local trash drops below50GB, permanently remove eligible Library-owned native-Trash entries before their30-day deadline, oldest first, and remeasure space after each removal. Stop as soon as actual free space is at least50GB; do not empty all Trash. Display units explicitly.
- Oldest first means the earliest verified import/retention timestamp, with native-Trash entry time as a tie-breaker. Do not sort by title, media release year or arbitrary filesystem mtime.
- Seed until ratio2 OR2,880minutes, whichever comes first, then stop/pause. Two days and2,880minutes describe the same time limit, not separate policies. Use qBittorrent's supported seeding-time counter and show that meaning in settings.

## Scope and safeguards

This affects local staging copies only, never the NAS library copy or other users'/apps' Trash. Unfinished, failed, partly imported and untracked/unverified downloads remain protected. A pressure condition does not turn an unverified import into an eligible deletion.

Keep stop-seeding, staged retirement and native-Trash expiry separate. Do not run the old qBittorrent permanent-delete cleanup as a substitute. Existing stage clocks must survive restarts, retries and app updates without being reset or accelerated.

Use supported qBittorrent operations for a managed location change; avoid raw moves while the torrent is still using files. Native-Trash transfer must use the platform's supported behavior and handle the actual filesystem. A cross-filesystem move that cannot free the intended disk must be explained rather than reported as freed space.

Track ownership and original import time persistently. Before deleting, validate that the entry is still the exact Library-owned item in native Trash on the expected volume, not a manually restored/renamed/replaced unrelated file or a symlink target. Never glob/empty all of macOS Trash or change the system-wide30-day setting.

If eligible items cannot restore the threshold, show remaining free space and blockers; do not reach into live downloads, NAS media or unrelated Trash. Never infer reclaimed bytes from file sizes alone (links/clones may retain blocks); read actual free space.

## Required UI and proof

Show the3-day staging wait,7-day total deadline,30-day native-Trash retention,50GB pressure threshold and seeding settings. Let the user change/switch off policies. Show stage, remaining time, proposed action, ownership/verified-import evidence, blocked reason, restore path and permanent-deletion meaning. New automated destructive behavior requires a deliberate switch, not migration from the old default.

Tests cover boundaries at3/7/30days, restart/retry clocks, oldest-first order, low-space stop condition, wrong-volume/no-reclaimed-space, insufficient eligible data, offline/unknown imports, seeding limits, manual restoration/replacement and unrelated Trash exclusion. Validate native Trash on an isolated test directory before live use. No real-owner downloads or Trash are test fixtures.

## Explicit low-space override (owner, 2026-11-07)

The pressure threshold is editable; 1301's approved target is 200 GB. `pressureBypassRetention` starts off so existing installations retain their original staging waits. When deliberately enabled, a pressure run considers Library-owned staging, managed `_trash` and native-Trash records in oldest verified-import order, skips retention waits, and stops as soon as actual measured free space reaches the threshold. Normal 3-day plus 4-day staging and 30-day native-Trash retention remain editable and unchanged.

Only completed whole imports qualify. Fresh NAS bytes and local contents must match the recorded hashes before removal. A current queue entry, incomplete download, recheck, move, unsafe path or unavailable proof blocks that item. Completed seeding may be stopped early only for this explicit pressure rule, with native stop and metadata-removal readback before moving files through the owned trash stages. NAS media and unrelated Trash are never deleted. Every expiry deletion also rechecks current NAS content; a file changing during hashing is rejected.

For an owned directory in macOS Trash, remove only its verified entries through a directory-scoped handle. Do not enumerate or request broader access to the parent Trash directory. Validate local contents first, then perform the fresh NAS check immediately before removal. Any inaccessible, changed or unknown item remains protected; a missing saved Trash path is not counted as reclaimed space. Full checksums use bounded 4 MiB reads rather than small per-call network reads; content is never accepted from a stale checksum cache.
