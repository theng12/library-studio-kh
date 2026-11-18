# What's new in Library Studio KH

## 0.7.27 — 2026-11-18

- The program files no longer contain this Mac's username or the folders it was built in, so a copy of Library gives nothing away about the Mac it came from. Every build now checks itself for those paths and stops if one gets back in.
- A sign-in from a Tailscale device is marked secure, so its browser never sends that session over a plain, unencrypted connection. The desktop app on this Mac keeps working the same way; a phone keeps its sign-in only over an HTTPS link.

## 0.7.26 — 2026-11-17

- Closing the Library window no longer quits the app. Downloads, imports and the update pause keep working in the background; only Quit stops it.
- The window comes back from the menu bar item, the Dock icon and the app menu. The menu bar item offers Show Library Studio KH and Quit Library Studio KH.
- One short line at the bottom of the window says closing it keeps Library running, and that Quit stops it.

## 0.7.25 — 2026-11-17

- Only the updater can now pause Library for an update. Before this, any program on this Mac that found the pause address could stop Library taking new work; now it has to prove it is the updater, and one that cannot is turned away without changing anything.
- The updater's key is re-read every time it is used, so a fresh key takes effect straight away with no restart of Library.

## 0.7.24 — 2026-11-17

- An update no longer leaves Library's background service stopped. Before this, Library could open without it, and the updater then reported the update as unhealthy and kept both versions while it waited for a clean try.
- If Library's background service is already saved exactly as it should be, Library now just starts it instead of replacing it, so it can no longer undo the update's own restart.
- When that background service really is down, Library's log now says so in plain words, so the reason is visible without reading the source.

## 0.7.23 — 2026-11-17

- Library now opens even when its own background service is already running. Before, it could say another Library Studio KH was running and quit without showing a window — which is what happened on this Mac with the background service set up.
- If that background service was stopped or crashed, Library now says so and starts anyway, instead of refusing to open until a file is deleted by hand.

## 0.7.22 — 2026-11-17

- Library no longer stops by itself a moment after it opens. Until now My library, Downloads and the update check all read "Load failed" a second or two after launch, with nothing in the grid.
- If the app's background program really has stopped, the page now says so in plain words and tells you to quit Library and open it again, instead of showing a browser error.
- A pause request from the Updater no longer shuts Library down, so an update can slide in without cutting off what it was doing.

## 0.7.21 — 2026-11-15

- The update hold now answers on the fixed address the Updater is told to use, so an update can ask Library to pause before it replaces it. It answers only on this computer, and if the address is busy Library still runs — it just cannot be paused for an update.

## 0.7.20 — 2026-11-15

- Library now answers the update hold, so the Updater can install a new version without cutting off work: it stops taking new jobs, gives what is running a short, bounded time to settle, and says whether a restart is safe right now. Anything still running is named so the next start can pick it up.
- The pause lasts only a couple of minutes and ends by itself, so an install that is abandoned cannot leave Library refusing to work; a restart always resumes with nothing to click.

## 0.7.19 — 2026-11-15

- Every folder field takes a typed path as well as Browse: Settings, Setup and Cleanup. A path that does not open says why under that field and keeps your saved value, so nothing ever runs against a folder that is not there.
- Every Settings field now has one plain line saying what the app does with it, and the server's address, phone link, port and session live on Settings instead of on the working pages.
- Turning staged cleanup on no longer needs a folder chosen first. The switch stays where you put it, and the page tells you which folder to set. The waits and limits show their defaults even while the switch is off, so the page is never empty.
- The Cleanup status folds away behind Show more / Show less, with the actions, the switch and any blocker kept on screen.

## 0.7.18 — 2026-11-14

- Local media gets real metadata: films and shows in your library folder that no library app imported are listed with a clean title and year from their file name, a Kodi .nfo sidecar, or embedded tags, with facts and a poster from the same keyless sources. A file already managed by Sonarr, Radarr or Lidarr stays that app's row, never a duplicate.
- Match metadata: a local file's details let you name it once; the match is saved and used for its facts and poster. The file itself is never renamed or moved.
- Folder fields are pickers, never typed paths: the native folder panel on the Mac, and the app's own folder browser on a phone or another computer. Paths are read-only after you choose.
- My library: type tabs with live counts (Show all, Movies, TV, Music, remembered), multi-select filters with removable chips, equal-height poster cards with shelves for Continue, Recently added, Movies, TV and Music, a table view, and a selection bar that verifies chosen copies in one go. The detail adds Set location and Open location.
- Find & add: a full-width search row, clear loading, result-count, no-result and error states, and torrent links checked line by line with the bad line named and a ready count.
- Settings and Cleanup share one tabbed shell with a sticky Save; every dialog closes from its backdrop and returns focus; the appearance control is the segmented System / Light / Dark choice again.
- Settings → General fills its columns: Appearance and Help on top, Dead downloads across the middle, Companion apps down the tall column, and Folders below. Updates says it checks on launch and on demand, which is what it does.
- Setup & help closes with sign-in, so its page never stays above the app. The Filters panel ends on a whole row with a soft fade in the gap below it instead of slicing an option in half, with Apply and Reset pinned below; on a phone Filters opens as a sheet above the navigation so Apply and Reset stay on screen. Set location points a library app (Sonarr, Radarr, Lidarr) at a title's folder; a local file uses Match metadata and Open location instead.
- Phone tap targets: Downloads row ticks are 44 px like library cards, and every Settings control reaches 44 px.
- Honest states everywhere: "Missing" only after a real check on this machine, "Needs proof" when nothing proves a copy, unknown download states say "Status not reported", and stalled or unproven downloads no longer read as success.
- Pages read a cached snapshot and refresh behind you, so My library and Activity appear immediately on launch and stay quick; the page says when it last checked.
- One page frame, spacing and type scale, phone tap targets, and measured contrast in light and dark across every page.

## 0.7.17 — 2026-11-14

- My library's list shows the full storage path at desktop widths instead of shortening it while the title column has room.
- Activity offers Clear filters whenever a search or filter is active, with or without results.
- Sources states a disconnected Prowlarr once, and Reported health says "Not checked" while it is disconnected instead of reading as clean.
- Downloads with nothing in the list show one message and a Find & add button, without selection tools or an empty table header.
- A saved copy can be verified again from My library, not only before its first proof.
- Closing a Remove or Clear confirmation returns focus to the button that opened it.

## 0.7.16 — 2026-11-13

- Every film and show gets a poster, overview, year, genres, runtime and rating from keyless public sources, cached on disk and shown when available; a missing poster shows the title's letter, never an empty box. These views and posters appear after you update to 0.7.16.
- My library is a poster grid with a remembered Cards/Table switch and a detail panel: status, real volume and path, how the copy was verified and when, the proof hash and its history. A copy never reads verified without proof.
- Downloads shows only work still in progress, with one main action per row and the rest under ⋯, plus proof and target. Imported and verified titles leave Downloads and appear in My library; Clear removes the qBittorrent entry only and never the files.
- Activity is a timestamped timeline with filters, repeated warnings grouped with counts and identical events shown once with ×N. Cleanup gains per-row actions, stage and remaining wait, and says plainly that nothing is deleted for good outside its waits.
- Optional “Remove dead downloads automatically” is off by default: after a grace of 1–168 hours, Library removes only the torrent entry for downloads whose files are missing and that have no import proof, and can separately clear the same dead item from a Sonarr/Radarr/Lidarr queue so the app stops retrying it. Files on disk are never deleted and nothing is blocklisted.
- Every page fills the window at phone, tablet and desktop widths, with measured contrast in light and dark.

## 0.7.15 — 2026-11-13

- The Appearance choice is a tidy three-part control that fits its labels in light and dark, at window and phone widths.

## 0.7.14 — 2026-11-13

- Applying a Recyclarr sync now asks in Library's own dialog; the old browser prompt was silently ignored in the app window.
- Escape closes the open dialog, clears a focused search and closes the menu, including in the desktop window.
- Reopen the Library window from the menu or the Dock after closing it.
- Activity says Paused, Queued, Stalled or Missing files instead of calling every row Downloading, app chips say Connected or Offline in words, and empty lists say what to do next.
- The footer rows line up on one inset and the version pill truncates instead of wrapping.

## 0.7.13 — 2026-11-12

- The background helper now runs from inside the app, so deleting Library Studio KH removes it; quitting stops the helper, its login job and its ports.
- Uninstalling removes the app, helper and login job while keeping your settings and library.

## 0.7.12 — 2026-11-12

- If another Library Studio KH is already running on this Mac, Library now says so plainly and quits cleanly instead of failing to open.

## 0.7.11 — 2026-11-12

- Appearance offers System, Light and Dark. System stays the starting choice, and an older saved "Default" is read as System.

## 0.7.10 — 2026-11-12

- Quit Library Studio KH (⌘Q) now stops the background helper too, so nothing named Library Studio KH keeps running after you quit. Reopening starts cleanly.

## 0.7.9 — 2026-11-12

- Run Library's background helper as a properly named app with its own icon, so it no longer appears as a bare background process.
- Support this app from the sidebar footer. Library is free with every feature unlocked; a one-time donation is optional.
- Find things faster with a tidier sidebar: Activity first, then Library and System groups, and the setup entry disappears once Library is set up.
- See the Library name and version in the sidebar, with What's New behind it and one plain update line beside it. When a new version exists, the update button opens Studio Launcher KH — you confirm the update there, and Library never installs anything by itself.
- Choose System, Light, Dark or Default appearance in Settings.

## 0.7.7 — 2026-11-11

- Add optional setup and readiness checks for Recyclarr, Unpackerr and Whisparr. Each stays off until you turn it on.
- Verify official companion packages before installation and keep saved connection credentials private.
- Use standard editing commands and discoverable keyboard shortcuts for search, navigation and download selection while keeping text fields and dialogs in control of focus.
- Open Activity safely before any companion apps are configured.

## 0.7.6 — 2026-11-09

- Clear verified local copies for one library app even when another app's history is unavailable. Keep active downloads, unfinished seeding and other apps' files protected.
- Explain failed import-history checks with a safe error category and response status, without exposing private connection details.

## 0.7.5 — 2026-11-08

- Keep the same macOS signing identity in the second signed update. Retained Full Disk Access and real cleanup are verified separately on the installed host before wider rollout.
- Verify the delivered DMG directly, detach its exact disk device, and read complete signer details when checking the app and its background server.

## 0.7.4 — 2026-11-08

- Sign macOS packages with the shared local code-signing identity so updates can retain a consistent application identity. Full Disk Access persistence remains subject to the two-update installation check.
- Verify packaged signatures and background startup through an isolated launchd job; compare designated requirements across signed releases.

## 0.7.3 — 2026-11-07

- Clear verified folders from macOS Trash without listing the parent Trash directory. Keep removal scoped to the exact owned item, and recheck NAS content immediately before deletion.
- Protect changed files, added content, symlinks and replaced folders during removal. Preserve existing cleanup settings and retention clocks.
- Read full verification checksums in bounded larger chunks, avoiding small network reads without caching or skipping content checks.

## 0.7.2 — 2026-11-07

- Optionally skip local staging waits when space falls below your chosen threshold. Clear oldest completed, NAS-verified copies first and stop at the measured target; existing installations keep this override off until chosen.
- Recheck current NAS content before every permanent cleanup, including normal Trash expiry. Missing, changed or unreadable copies stay protected.
- Protect downloads being checked, moved or handled by a library app. Confirm completed torrents have stopped and their metadata is removed before moving local files.
- Show the low-space override, current free space and proposed early removal clearly on Cleanup.

## 0.7.1 — 2026-11-07

- See monitoring on or off beside every library title and filter by monitoring status. Unreported native settings stay distinct from off.
- Change monitoring from the library and read the native setting back. Series details explain when the show is off even though seasons or episodes are selected.
- Adding media starts with monitoring on and Search now off. Review both choices; immediate searching is a separate opt-in, and turning monitoring off keeps existing files.
- Keep add requests single while saving, and distinguish a requested search from a downloaded or imported file.

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
