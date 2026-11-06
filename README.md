# Library Studio KH

The owner's media library manager. It does not download or search anything itself: the stock apps do that.

- **qBittorrent** downloads.
- **Sonarr** (TV), **Radarr** (films), **Lidarr** (music) and **Prowlarr** (search sites) find and import into the library on the NAS.
- **Library Studio KH** provides library tracking, release selection, source status and download control through one private interface. Staged local-copy cleanup is planned and stays off.

Only use sources and content you are allowed to download.

## How removal stays safe

The old permanent-delete cleanup is retired and cannot be enabled in this build. The staged policy is specified in [docs/STORAGE_POLICY.md](docs/STORAGE_POLICY.md): keep local copies for 3 days after verified import, keep them in Library's `_trash` until day 7 total, then retain them in macOS Trash for up to 30 days; below 50 GB free, only eligible Library-owned native Trash may be removed early, oldest first. That engine is not active yet.

Any future eligibility decision must still satisfy all of these requirements:

1. qBittorrent says the download is complete.
2. Sonarr, Radarr or Lidarr has a history entry saying the whole download was imported (`downloadFolderImported`, or Lidarr's `downloadImported`).
3. No library app still lists it in its queue (a partly imported or blocked download stays listed).
4. The import is at least N days old.
5. Every connected app answered this round. If one didn't, nothing is removed.

Downloads that no library app knows about are never touched. Settings saves no longer overwrite the stock seeding/retention policy. Seeding limits and staged retention are separate concerns; 1301 keeps its ratio 2 or 2,880-minute stop policy.

Eligibility classification and the retired-cleanup no-delete guard live in `internal/library` and are tested against mocked apps. Source status is read-only. Release searches/grabs run only on user actions and go through the owning library app.

## Use from your phone or away from home

Create the owner account from the host desktop first (password at least 6 characters; numbers alone are accepted). In Settings → Phone & Tailscale access, switch on **Allow access from my Tailscale devices** and save. The app detects this computer's Tailscale address and shows a normal browser link without an access key. Open it on a device connected to the same Tailscale network, then sign in. The host must remain awake with Library running; optional macOS background hosting lets it stay available when the desktop window closes. See [Background host](docs/HOST.md). Tailscale access rules must allow the displayed port (default 7882, changeable in Settings).

Remote access starts off and binds only the local Tailscale address. It accepts only Tailscale peers and requires authentication. The browser entry and owner identity survive restarts. A browser session lasts 12 hours, or 30 days when you explicitly choose Remember this device; Settings lists sessions and lets you revoke them. Local desktop recovery can reset the password and revoke all sessions. Switching access off and saving closes the remote listener immediately. The initial transport is HTTP inside Tailscale's encrypted network, not HTTPS; it does not claim Secure-cookie protection. No router forwarding or public sharing is needed. Only the numeric host address in the displayed link is accepted.

If Tailscale is disconnected when you enable access or start the app, Settings explains the problem. Connect Tailscale and save again. This app does not install, sign in to, or change Tailscale itself.

## Downloads and library progress

Downloads supports title search, status/app filters with counts, and display sorting. Sorting never changes qBittorrent's real queue order. A compact summary shows title, reported app, status and transfer progress; expand it for sizes, ETA, folder and other details. Completed includes finished downloads that are still seeding or paused, and does not mean imported.

App ownership comes from each library app's queue and available import history. Unknown/unmatched ownership stays explicit. My library matches in-flight downloads using the owning app and parent IDs, never title guesses. Its current-download percentage is not whole-series completion. Both views refresh every 30 seconds while visible; expanded items remain open. Search/status/app filter changes clear selection so hidden items cannot be paused accidentally.

A fresh Library installation currently needs separately installed and configured companions. The guided host/client installer remains planned. Item details now show reported metadata and original language; optional Bazarr controls report file audio/subtitles and request standard English subtitle searches. The existing 1301 stack is already configured independently.

See [metadata and subtitle setup](docs/SUBTITLES.md) for language meanings, profile/provider requirements and the initial1301 manual-search configuration. Subtitle downloads need imported/indexed files; a successful request is not a completed download.

## Develop

```sh
go test ./...                    # all tests, mocks only, no real apps
go build -o bin/mock-stock ./tests/mock-stock && bin/mock-stock   # fake apps on 127.0.0.1:7890
go run ./cmd/library-studio      # prints LISTENING http://127.0.0.1:<port>/#<token>; open that address
npm ci && npm run desktop        # the desktop window
```

Packaging, Windows/Linux and the Updater route: [docs/PACKAGING.md](docs/PACKAGING.md).
