# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

One owner-operator (theng12) managing a personal media library. Works at this Mac's desktop and, when away, from a phone or another device over Tailscale. The job: start downloads, search and find titles, import them into the library on the NAS, and keep local disks tidy without losing anything.

## Product Purpose

Library Studio KH is a private control room over an existing stack of official apps: qBittorrent for downloads; Sonarr (TV), Radarr (films), Lidarr (music) and Prowlarr (search sites) for finding, importing and managing the library on the NAS. It exists so the owner can see and act on the whole flow — downloads, imports, library browsing, sources, companion-app settings and safe local-copy cleanup — from one interface instead of several. Success means nothing is lost, nothing is removed without proof, and the state of the library is honest at a glance.

## Positioning

One private interface over stock, official applications — never its own downloader, indexer or media source. The mechanism a neighboring tool could not truthfully copy: every removal decision is provable and previewable (complete download, whole-download import history, no library app still queuing it, every connected app answered this round), and unsafe or unknown ownership is never actioned.

## Operating Context

- The host is this Mac; the library lives on a NAS; library and download apps run as separate official services on the same network.
- Remote use is HTTP inside the owner's Tailscale network, with an owner account and revocable browser sessions; access starts off.
- Downloads keep their own seeding/ratio policy; staged local-copy cleanup is optional and starts off.
- The app is packaged as a signed macOS desktop app (Tauri 2 shell + Go sidecar) with an optional background host; the same web interface opens in a phone browser over Tailscale.
- A guided setup path installs and verifies official companion packages and preserves existing installations.

## Capabilities and Constraints

- Downloads: search, filter, sort, pause/resume; real queue order is never changed by display sorting.
- My library: browse and search the library, live genre/year choices, item details, optional subtitle controls.
- Find & add: search releases and add monitored titles through the owning library app, only on explicit user action.
- Sources: Prowlarr-backed add/edit/test/save with its live schema; new sources start disabled; testing never saves.
- Cleanup: staged policy with preview, blockers and restore paths; missing, changed or unreadable library content blocks deletion; permanent deletion rechecks at Trash expiry.
- Settings: stock-app connections, folders, phone/Tailscale access, appearance, sessions.
- Setup & help: guided first-run setup and readiness checks.
- Constraints: communicates only with the local/Tailscale-bound host and the stock apps' supported APIs; no router forwarding; secrets are never returned to the browser; tests use mocks only.

## Brand Commitments

Name: Library Studio KH. Voice: plain, calm, specific — states what happened and why, never dresses up a failure. Part of the KH app family: same sidebar/footer conventions, Support page and update hand-off.

## Evidence on Hand

Real content is the owner's own library and media; nothing here is sample marketing. No testimonials, benchmarks or customer claims exist, and future work must not invent them.

## Product Principles

1. Honest states: "not checked" never reads as passed, "no data" never as zero, and failures say why.
2. Prove before removing: every cleanup decision is verifiable, previewable and restorable.
3. The stock apps stay authoritative; Library shows and acts through their supported APIs, never a private reimplementation.
4. Optional things start off and never block the main job.
5. One plain interface for the whole flow; the owner should not have to know which app is answering.

## Accessibility & Inclusion

Keep text and controls legible in light and dark, support keyboard navigation and visible focus, and keep every control's accessible name. The phone layout is a first-class surface.
