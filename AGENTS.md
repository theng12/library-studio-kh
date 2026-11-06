# Library Studio KH — agent guide

Direct instructions from the owner (theng12) override this file. If it disagrees with the code, the code wins and this file gets fixed.

## What this is

A thin desktop layer over the stock qBittorrent, Sonarr, Radarr, Lidarr and Prowlarr apps, using their official APIs. Never add a downloader or indexer of our own. See [README.md](README.md) for the removal safety rule; never weaken it.

## Layout

| Path | What |
| --- | --- |
| `cmd/library-studio/` | Go entry point: dashboard server on 127.0.0.1, hourly cleanup |
| `internal/stock/` | API clients for the stock apps |
| `internal/library/` | Dashboard list, removal rule, stock-app settings |
| `internal/config/` | Settings file in the OS config folder |
| `internal/platform/` | The only OS-specific code |
| `internal/server/` | HTTP API and the embedded dashboard (`web/`, plain HTML/JS, no build step) |
| `src-tauri/` | Tauri 2 shell that starts the Go sidecar |
| `tests/mock-stock/` | Fake stock apps for manual looks |
| `changelog.go`, `CHANGELOG.md` | Version and What's New (single source) |

## Verify before claiming done

```sh
go vet ./... && GOOS=windows go vet ./... && GOOS=linux go vet ./... && go test ./...
node --test tests/download-view.test.mjs
npm ci && npm run build:desktop
```

Tests use mocks only. Never call real stock apps, the NAS or the 1301 Mac from a test.

## Rules

- ponytail on every coding task; impeccable before UI edits; graphify before hand searches when a graph exists.
- Commit as `112568204+theng12@users.noreply.github.com`.
- No `rm`: move files to `~/Developer/_trash/library-studio-kh/`.
- Paths are settings, never hard-coded. Optional features off by default. Legacy permanent-delete cleanup is retired; the owner-confirmed staged policy in docs/STORAGE_POLICY.md stays off until implemented and verified.
- Releases go through the Updater lead after Developer says go ([docs/PACKAGING.md](docs/PACKAGING.md)).
