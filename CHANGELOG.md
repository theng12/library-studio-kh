# What's new in Library Studio KH

## 0.7.0 — 2026-11-07

- Guided core-app setup on Apple Silicon Macs resolves current official packages, verifies published checksums, preserves existing apps and keeps first opening under normal macOS approval.
- Set up private native connections, folders, categories and Prowlarr links with tests and saved-setting checks. New music folders use live Lidarr quality and metadata choices; TV renaming starts on and stays editable.
- Review, retry and resume setup with explicit blocked or unchecked states. Existing native settings and Library accounts are preserved, and generated credentials stay private.
- Optional packaged at-login startup needs no Node, Go or Homebrew. A client can use an existing host, with native Restart Library and Use this Mac as host controls.
- My library filters by actual genres and years, combines them with existing filters and offers newest/oldest-year sorting with unknown metadata kept distinct.
- Subtitle details accept Bazarr's current audio-language lists and display/filter every reported audio language separately from subtitle languages.

## 0.6.0 — 2026-11-07

- Copy the host and Tailscale webpage addresses from Activity. Connection pills stay visible with green or red dots, and download filters no longer hide behind a dropdown.
- Activity shows zero progress correctly and says when a percentage is unavailable.
- Films show Radarr artwork, ratings and collection lineups with owned and missing states. Find & add opens a confirmation for the exact film.
- Add, edit, test and save sources using Prowlarr’s live provider catalog; saved credentials stay private.
- Edit supported companion settings with explicit Apply controls and clear reports of any partial failure.
- Optional staged cleanup has editable retention, an eligibility preview, import evidence and local-copy restore controls. It starts off and protects NAS files, unverified downloads and unrelated Trash.

## 0.5.0 — 2026-11-06

- Film, series and artist details show descriptions and reported metadata, including genres, year, runtime, certification/network and original language where applicable.
- My library can filter original language into English, other reported languages and unknown; original language stays separate from file audio and subtitle languages.
- Optional Bazarr controls show reported subtitles/audio, English profile assignment and explicit per-file English searches. Search acceptance is shown as requested; a refreshed file report confirms availability.
- Subtitle controls stay inside an expandable section. Missing files, providers, profiles and companion outages are explicit and never block the main library.
- Existing downloads, import destinations and cleanup-off policy are preserved. Fresh-computer companion installation remains planned.

## 0.4.0 — 2026-11-06

- Downloads has title search, counted status and app filters, and sorting by queue position, app, name, progress, date, size or speed. These controls never reorder the actual downloader queue.
- Each download starts with a compact, expandable summary showing its owning app, status and progress; extra transfer and folder details stay inside.
- My library uses expandable summaries and shows current download progress matched through the owning app's queue IDs. Whole-series completion is not inferred from a season pack.
- Visible download and library views refresh automatically while preserving expanded items. Filtering clears hidden selections before pause/resume actions.
- Companion installation, richer metadata, subtitles and language controls remain planned. The installed stack on1301 is unchanged.

## 0.3.1 — 2026-11-06

- Phone navigation now uses a compact header, four everyday tabs at the bottom and a separate Menu for settings and information.
- Phone lists keep their field labels, search and filters fit the screen, and controls have larger touch targets with readable input text.
- App connections fold into a live status line. Sources puts reported health first and shows each library app's sources as a readable list.

## 0.3.0 — 2026-11-06

- Open a library item to see movie details, series seasons/episodes or artist albums, with monitoring controls for each supported level.
- Search releases deliberately and send eligible selections through Sonarr, Radarr or Lidarr. Rejected releases stay blocked and source download credentials are not exposed.
- Sources shows Prowlarr's observed configuration, reported health, application links and the source lists in connected library apps, with partial errors shown clearly.
- Setup & Help now reflects background-host availability and the new Sources screen.
- The old permanent-delete cleanup is retired. The new staged Trash policy is documented and remains off until implemented and verified.

## 0.2.1 — 2026-11-06

- Owner setup and password recovery now accept six-character passwords, including six numbers.
- Password hints and browser validation follow the minimum reported by the host. Login-attempt limits and private-access protections are unchanged.

## 0.2.0 — 2026-11-06

- Set up an owner account and sign in from a stable browser address. Remembered devices survive restarts and can be revoked.
- Run Library as a background host on macOS; the desktop connects to that host instead of starting a second server.
- Pause or resume downloads, change media monitoring, and choose live quality profiles and library folders when adding media.
- Setup & Help explains the host, companion apps, folders, monitoring, private access and troubleshooting inside Library.
- The companion installer and expanded media integrations remain planned; existing stock apps are adopted without reinstalling them.

## 0.1.0 — 2026-11-05

- First version: a blue interface with menus for activity, your library, finding and adding media, cleanup and settings. Works with your own qBittorrent, Sonarr, Radarr, Lidarr and Prowlarr.
- Browse, search and sort your library, find shows, films and artists, or send single or bulk links and .torrent files to qBittorrent.
- Click a download to see its start, finish, duration, size and share ratio. Speed is recorded every 15 seconds while the app is open, with lowest, fastest and time at each speed range.
- Local copies are removed 7 days after a library app confirms the whole download was imported. Change the wait or switch removal off. Unknown or unfinished imports stay.
- Shows free space and the cleanup settings applied to the stock apps.
- Optional Tailscale access lets you use the app from your phone or away from home. Settings shows a private phone link; keep the desktop app open and both devices connected to Tailscale.
