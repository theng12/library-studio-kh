# Library Studio KH — what it is

Library Studio KH is the owner's media library manager. It gives qBittorrent
downloads and Sonarr, Radarr, Lidarr and Prowlarr libraries one private interface,
and resolves official companion packages for setting up a new Mac. The media itself
stays in the stock apps and on the NAS.

## What it does

- Shows downloads with title search, status/app filters, counts and display
  sorting; sorting never changes qBittorrent's real queue.
- Matches in-flight downloads to the owning app and import history, and shows
  library progress without guessing from titles.
- Browses the library by genre, year, type, language and title, with metadata,
  collections and optional Bazarr subtitle requests; a request is not a finished
  download.
- Configures Prowlarr sources with explicit Add, Edit, Test and Save; new sources
  start disabled and testing never saves.
- Guides a fresh Apple Silicon Mac through official companion packages, checksum
  verification, folders, categories, links and connection checks.
- Optionally cleans local copies only after qBittorrent says complete, the library
  app says the whole download was imported, no queue still lists it, retention has
  passed and every connected app answered. Automatic cleanup starts off.
- Clears a download that has stopped moving: a day with no progress from
  qBittorrent removes it, blocklists that release in Sonarr or Radarr and leaves
  them to pick another. Files, history and library records stay, and a download
  reporting an error is left alone — that is usually this Mac, not the release.
- Offers Setup & help, native app settings and optional at-login startup from the
  packaged server. Settings lists and adds movie folders in Radarr and TV folders
  in Sonarr without moving existing titles; local media scanning is separate.
- Updates itself from its own signed appcast: it waits for work in flight to
  settle, installs quietly and starts the new version by itself.
- Supports Tailscale-only browser access from a phone when explicitly enabled:
  sign-in, 12-hour sessions or 30 days with "Remember this device", and session
  revocation in Settings.

## How it is built

Go service (`cmd/`, `internal/`), TypeScript settings UI (`ui-shell/`), Tauri 2
desktop shell (`src-tauri/`) and Node build scripts (`scripts/`). Tests use mocked
apps and isolated directories.

Build the signed desktop app and DMG with:

    npm run build:desktop

## Where it fits

It is a standalone app that drives the owner's existing library stack; the README
describes no Gateway or Hub role for it. A single Mac can also serve the interface
over its Tailscale address. Packaging follows the shared signed-desktop and Updater
path and its own signed appcast, but Library does not hold the Studio job queue.

## Current version

0.7.36, from the `version` field in `package.json`; the newest `CHANGELOG.md` entry
agrees.
