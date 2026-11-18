# Library Studio KH

The owner's media library manager. Media downloads and searches run in the stock apps. Library also resolves and downloads verified official companion packages for setup.

- **qBittorrent** downloads.
- **Sonarr** (TV), **Radarr** (films), **Lidarr** (music) and **Prowlarr** (search sites) find and import into the library on the NAS.
- **Library Studio KH** provides library tracking, artwork and movie collections, release selection, source management, native app settings and download control through one private interface. Optional staged local-copy cleanup starts off.

Only use sources and content you are allowed to download.

## How removal stays safe

The old permanent-delete cleanup is retired. The optional staged policy is implemented and specified in [docs/STORAGE_POLICY.md](docs/STORAGE_POLICY.md): keep local copies for 3 days after verified import, keep them in the local download folder's `_trash` until day 7 total, then retain them in macOS Trash for up to 30 days; below the editable free-space threshold, eligible Library-owned native Trash may be removed early, oldest first. An explicit pressure override can also bypass staging waits for completed NAS-verified copies; it starts off. The periods, threshold and local managed folder are editable on Cleanup. Preview changes nothing; automatic cleanup starts off and deployment does not turn it on.

Eligibility must satisfy all of these requirements:

1. qBittorrent says the download is complete.
2. Sonarr, Radarr or Lidarr has a history entry saying the whole download was imported (`downloadFolderImported`, or Lidarr's `downloadImported`).
3. No library app still lists it in its queue (a partly imported or blocked download stays listed).
4. The import is at least N days old, unless the owner explicitly enabled skipping retention waits under pressure.
5. Every connected app answered this round. If one didn't, nothing is removed.

Downloads that no library app knows about are never touched. Settings saves no longer overwrite the stock seeding/retention policy. Seeding limits and staged retention are separate concerns; the normal ratio/time policy is preserved. The explicit low-space override may stop completed seeding early, confirms the torrent has stopped and its metadata is removed, and never touches an incomplete or checking torrent.

Eligibility and lifecycle checks live in `internal/library` and use mocked apps and isolated directories in tests. The engine verifies every imported video and local file, rejects unsafe paths and unknown proofs, and records clocks and ownership across restarts. Cleanup shows blockers, proposed actions and restore paths; every permanent deletion rechecks the current library content and exact local copy, including at native Trash expiry. Missing, changed or unreadable NAS content blocks deletion. Native Trash cleanup is unavailable outside macOS.

Sources supports explicit Add/Edit/Test/Save through Prowlarr's live schema. New sources start disabled; testing never saves. Companion settings apply only supported fields and report partial success. Release searches/grabs and collection additions run only on user actions through the owning library app.

## Use from your phone or away from home

Create the owner account from the host desktop first (password at least 6 characters; numbers alone are accepted). In Settings → Phone & Tailscale access, switch on **Allow access from my Tailscale devices** and save. The app detects this computer's Tailscale address and shows a normal browser link without an access key. Open it on a device connected to the same Tailscale network, then sign in. The host must remain awake with Library running; optional macOS background hosting lets it stay available when the desktop window closes. See [Background host](docs/HOST.md). Tailscale access rules must allow the displayed port (default 7882, changeable in Settings).

Remote access starts off and binds only the local Tailscale address. It accepts only Tailscale peers and requires authentication. The browser entry and owner identity survive restarts. A browser session lasts 12 hours, or 30 days when you explicitly choose Remember this device; Settings lists sessions and lets you revoke them. Local desktop recovery can reset the password and revoke all sessions. Switching access off and saving closes the remote listener immediately. Tailnet traffic is encrypted by WireGuard end to end. The session cookie is marked Secure whenever the app is served over HTTPS, so it is never sent over an unencrypted connection. No router forwarding or public sharing is needed. Only the numeric host address in the displayed link is accepted.

If Tailscale is disconnected when you enable access or start the app, Settings explains the problem. Connect Tailscale and save again. This app does not install, sign in to, or change Tailscale itself.

## Downloads and library progress

Downloads supports title search, status/app filters with counts, and display sorting. Sorting never changes qBittorrent's real queue order. A compact summary shows title, reported app, status and transfer progress; expand it for sizes, ETA, folder and other details. Completed includes finished downloads that are still seeding or paused, and does not mean imported.

App ownership comes from each library app's queue and available import history. Unknown/unmatched ownership stays explicit. My library matches in-flight downloads using the owning app and parent IDs, never title guesses. Its current-download percentage is not whole-series completion. Both views refresh every 30 seconds while visible; expanded items remain open. Search/status/app filter changes clear selection so hidden items cannot be paused accidentally.

Setup & help guides a fresh Apple Silicon Mac through current official companion packages, checksum verification, explicit macOS first opening, private profiles, native folders/categories/links and connection checks. It preserves existing installations and reports conflicting or unverified settings. Optional at-login startup uses the packaged server without Node, Go or Homebrew. An existing-host client uses a normal webpage URL and the native menu can switch back to this Mac. See [guided setup](docs/SETUP.md).

My library combines live genre/year choices with type, original language and title search, and sorts by newest or oldest year. Missing metadata stays selectable as Not reported. Item details show reported metadata; optional Bazarr controls display all reported audio languages separately from subtitle languages and request standard English subtitle searches. The existing 1301 stack remains independently configured.

See [metadata and subtitle setup](docs/SUBTITLES.md) for language meanings, profile/provider requirements and the initial1301 manual-search configuration. Subtitle downloads need imported/indexed files; a successful request is not a completed download.

## Develop

```sh
go test ./...                    # all tests, mocks only, no real apps
go build -o bin/mock-stock ./tests/mock-stock && bin/mock-stock   # fake apps on 127.0.0.1:7890
go run ./cmd/library-studio      # prints LISTENING http://127.0.0.1:<port>/#<token>; open that address
npm ci && npm run desktop        # the desktop window
```

Packaging, Windows/Linux and the Updater route: [docs/PACKAGING.md](docs/PACKAGING.md).
