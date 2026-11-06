# Metadata, languages and optional subtitles

Library reads metadata from the app that owns a title. Descriptions, genres, year, runtime, network/certification and original language appear only where applicable/reported. Artist biographies use the reported overview/biography. Empty facts remain unreported; no language defaults to English.

## Language meanings

- Original language describes the title's metadata. My library can filter English, other reported languages or unknown values.
- Reported audio describes a file in Bazarr. It may differ from original language.
- Available subtitles are the languages Bazarr reports on that file. The list preserves forced and hearing-impaired labels.
- Missing English is shown only when Bazarr explicitly reports it. A missing profile or missing file is not proof that subtitles are absent.

## Bazarr controls

Bazarr is a separate optional companion; a fresh Library install does not install it automatically. Subtitle controls default off. Connect its address/API key in Settings and enable the controls. API keys are hidden from public settings responses and blank saved-key fields preserve the existing key.

Open a film or series, then expand Subtitles and file languages. Library lists only the media reported for that exact Radarr movie or Sonarr series. A wrong-parent record fails explicitly. File paths, provider download links and raw settings are not returned to the browser.

Choose an English-only profile offered by Bazarr and explicitly assign it to the title if needed. Find English subtitles requests standard full English subtitles (not forced-only or hearing-impaired variants) for one reported file through Bazarr. It never accepts an arbitrary URL. The request rechecks the file record and parent ownership before submitting.

Search requested means Bazarr accepted an asynchronous job, not that a file was downloaded. Refresh subtitle status to check the actual file report. Configured providers can still be unavailable or have no matching subtitles. The existing subtitle list remains the evidence; metadata alone never proves a subtitle download.

## Files and synchronization

Bazarr needs imported/indexed media files. If imports are deferred or its catalog has not been synced, Library shows Waiting for files. This does not prevent browsing metadata or using the main download queue.

Sync/index the media in Bazarr when files are ready. A Bazarr sync can also search for missing subtitles, so run it when ready for that work. The initial1301 setup has scheduled tasks and live-sync triggers disabled; no real subtitle search was used as a setup test. Library's enable/disable switch controls Library's subtitle access, not all independent behavior of an externally managed Bazarr instance.

## Initial1301 installation (2026-11-06)

- Bazarr1.6.2 in a private user-owned runtime under ~/Library/Application Support/Bazarr, with an independent kh.librarystudio.bazarr LaunchAgent.
- Loopback-only listener, no root/Homebrew/system-Python changes, no network/firewall/Tailscale changes.
- Official Bazarr archive and runtime-tool archives checked against published SHA256 values.
- Upstream's ARM64 directory contained Intel-only ffmpeg/ffprobe/unrar helpers. Verified native private helpers are used via this job's PATH; upstream source and other apps are unchanged.
- English profile with TVSubtitles and YIFYSubtitles configured as free providers. Configuration is not a guarantee of provider availability or matching results.
- Automatic tasks and live-sync triggers remain off. Existing downloads, stock-app settings, NAS locations and cleanup-off policy are preserved.

Real subtitle download verification awaits imported/synced media. Tests use mock APIs only; manual live setup checks do not add or modify real media.
