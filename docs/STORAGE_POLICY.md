# Staged local-copy policy — owner specification, 2026-11-06

This module is planned, not enabled or implemented by the tracking/source batch. Preserve cleanup OFF on1301 until the staged behavior and a dry-run eligibility preview are verified.

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
