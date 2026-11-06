# Library Studio KH

The owner's media library manager. It does not download or search anything itself: the stock apps do that.

- **qBittorrent** downloads.
- **Sonarr** (TV), **Radarr** (films), **Lidarr** (music) and **Prowlarr** (search sites) find and import into the library on the NAS.
- **Library Studio KH** shows one list of what's downloading, in the library and coming up, and removes the local copy a set number of days (default 7) after it reached the library.

Only use sources and content you are allowed to download.

## How removal stays safe

A local copy is removed only when all of these are true:

1. qBittorrent says the download is complete.
2. Sonarr, Radarr or Lidarr has a history entry saying the whole download was imported (`downloadFolderImported`, or Lidarr's `downloadImported`).
3. No library app still lists it in its queue (a partly imported or blocked download stays listed).
4. The import is at least N days old.
5. Every connected app answered this round. If one didn't, nothing is removed.

Downloads that no library app knows about are never touched. Removal can be switched off in Settings.

On Save, the app also sets the stock apps so they don't fight this: qBittorrent stops seeding after N days with the action "Stop torrent" (never "remove files"), and the library apps keep completed download handling on with "remove right after import" off. The dashboard shows each of these and flags any that differ.

The rule lives in `internal/library` (`Decide`, `Cleanup`) and is tested in `internal/library/library_test.go` against mocked apps.

## Use from your phone or away from home

Create the owner account from the host desktop first (password at least 12 characters). In Settings → Phone & Tailscale access, switch on **Allow access from my Tailscale devices** and save. The app detects this computer's Tailscale address and shows a normal browser link without an access key. Open it on a device connected to the same Tailscale network, then sign in. The host must remain awake with Library running; optional macOS background hosting lets it stay available when the desktop window closes. See [Background host](docs/HOST.md). Tailscale access rules must allow the displayed port (default 7882, changeable in Settings).

Remote access starts off and binds only the local Tailscale address. It accepts only Tailscale peers and requires authentication. The browser entry and owner identity survive restarts. A browser session lasts 12 hours, or 30 days when you explicitly choose Remember this device; Settings lists sessions and lets you revoke them. Local desktop recovery can reset the password and revoke all sessions. Switching access off and saving closes the remote listener immediately. The initial transport is HTTP inside Tailscale's encrypted network, not HTTPS; it does not claim Secure-cookie protection. No router forwarding or public sharing is needed. Only the numeric host address in the displayed link is accepted.

If Tailscale is disconnected when you enable access or start the app, Settings explains the problem. Connect Tailscale and save again. This app does not install, sign in to, or change Tailscale itself.

## Develop

```sh
go test ./...                    # all tests, mocks only, no real apps
go build -o bin/mock-stock ./tests/mock-stock && bin/mock-stock   # fake apps on 127.0.0.1:7890
go run ./cmd/library-studio      # prints LISTENING http://127.0.0.1:<port>/#<token>; open that address
npm ci && npm run desktop        # the desktop window
```

Packaging, Windows/Linux and the Updater route: [docs/PACKAGING.md](docs/PACKAGING.md).
