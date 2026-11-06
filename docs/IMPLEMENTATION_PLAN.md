# Library Studio KH — modular implementation

Owner-approved direction, 2026-11-06: one setup/control experience over separate stock companion apps. Adopt 1301's existing stack; no reinstall, production/network changes or automatic deletion. Optional companions remain off until selected. Readarr is excluded.

## Delivery order

1. Foundation: persistent host identity, browser sign-in, revocable remembered sessions, background Mac host, desktop attachment, offline Setup & Help, safe downloads controls and explicit media profile/root choices.
2. 1301 adoption: read-only inspection, preserved cleanup-off settings, protected connection credentials, Library-only installation/startup, authenticated remote check and real-device guide. Existing OS login/startup is preserved. No reboot or stack restart during active work.
3. Complete core: series/episode and artist/album monitoring details, release selection through stock workflows, Prowlarr health/sync visibility, import/failure recovery, file/path validation, NAS-reconnect verification.
4. Guided host/client setup: detect/reuse or install selected official companions, resumable verified installation, common settings preview, approved configuration backup/rollback, OS-specific guide. Optional Recyclarr/Unpackerr helpers only after their mutation/cleanup policies are verified.
5. Launcher/Updater registration with its lead, one owner for host startup and non-secret host metadata. Publication requires Developer's go.
6. Optional integrations: Bazarr, LazyLibrarian, Mylar3, Whisparr; each includes useful controls, setup/help, connection health and demonstrated supported workflows. Ebook management does not promise a reader/player. Do not add a generic plugin framework merely to reserve future slots.

The full owner-facing product plan is in Desktop/All YouTube Stuff/Apps/Library Studio KH/Deployment and product plan 2026-11-06/. This repository plan records implementation contracts and progress.

## First build batch: file ownership

| Module | Owner | Files |
| --- | --- | --- |
| Auth/session foundation and final integration | Lead | internal/server/auth*.go, server.go, remote.go, version/changelog |
| Stock controls | Scoped worker | internal/stock/actions*.go, media.go/media_test.go, internal/library/controls*.go, internal/server/controls*.go |
| Setup/help and browser controls | Scoped worker | internal/server/web/* only |
| Mac background host lifecycle | Scoped worker | cmd/library-studio/*, internal/server/health*.go, scripts/manage-host.mjs, docs/HOST.md |

Workers share this checkout with disjoint file ownership. No branch switching or worker commits; lead integrates and commits once. No worker contacts real apps, NAS, 1301, Gateway, Updater or Launcher. All automated tests use mocks. First steps for every worker: invoke ponytail, then query the Hub graph at /home/user/Developer/_graphs/hub/graphify-out/graph.json with --budget 1500 before hand searching. Applicable AGENTS sections: Layout, Verify before claiming done, Rules. UI also follows impeccable.

## Auth contract

Desktop bootstrap retains X-Library-Token for local owner access. Identity persists privately in the config home; ordinary browser entry has no token fragment. Username/password use a salted established KDF. Cookie sessions use random secrets, private server-side hashed records, expiry and revocation. Default browser session expires after 12h; explicit Remember device lasts 30 days (displayed). Session expiry controls can become configurable in a subsequent settings module. Never expose a raw credential in logs, results, guides or Launcher metadata.

- GET /api/auth/status: unauthenticated allowed; {authenticated, setupRequired, rememberDays:30, sessionHours:12}. Bootstrap token counts as authenticated.
- POST /api/auth/setup: bootstrap-token AND loopback peer required, only before an account exists; {username,password}, min 12 characters displayed by UI.
- POST /api/auth/login: {username,password,remember}; sets HttpOnly SameSite cookie; {ok:true}.
- POST /api/auth/logout: revokes current cookie session; {ok:true}.
- GET /api/auth/sessions: authenticated; {items:[{id,created,expires,current}]} without raw session tokens.
- POST /api/auth/revoke: authenticated {id}; removes that session.
- POST /api/auth/password: local bootstrap owner recovery {username,password}; revokes sessions.
- All browser writes send X-Library-Request: 1, including multipart upload. Legacy valid bootstrap-token writes remain compatible. No cross-site credentialed writes or permissive CORS. Wrong/missing login returns useful JSON without a credential. Local/remote Host and peer protections remain in force.

Auth is still private Tailnet HTTP in the initial batch; do not falsely claim HTTPS/Secure-cookie protection. HTTPS/Tailscale-Serve changes require checking existing routes and separate authorization. The transport remains Tailnet-encrypted and the deployment limitation is documented.

## Stock control contract

Worker implements func (s *Server) registerControls(mux *http.ServeMux); lead calls it from Handler.

- GET /api/downloads: {items:[]stock.Torrent}; absent qBittorrent or upstream errors are explicit.
- POST /api/downloads/action: {hashes:[...],action:"pause"|"resume"}; validate action/hash boundary, qBittorrent 5 start/stop with legacy fallback only on 404. No delete action.
- GET /api/media/options?app=Sonarr|Radarr|Lidarr: {qualityProfiles:[{id,name}],rootFolders:[{id,path}],metadataProfiles:[{id,name}]}.
- POST /api/media/add: {app,raw,qualityProfileId,rootFolderPath,metadataProfileId}; validate selection against live upstream options before add. Return {folder}. Metadata profile required only for Lidarr. Preserve legacy /api/add until lead can migrate it safely.
- POST /api/media/monitor: {app,id,monitored}; read the current item before PUT, preserve unrelated fields, return {ok:true}.
- Media output gains id, monitored and qualityProfileId; current browsing remains compatible.

Front end consumes these routes. A failed API check must never look successful. Partial bulk operations report the upstream error honestly. Helpers stay in existing stock/library/server layers; no new dependency or downloader.

## Host contract

- Add -service mode for one background server, with fixed configurable loopback port (default 7881), suppressed credential-bearing LISTENING output, and private runtime information in config home.
- GET /api/health returns only {ok:true,version}; keep it authenticated through the existing guard. Worker exposes func (s *Server) health(w,r); lead registers route.
- Existing desktop -with-shell should attach to an existing validated service using private runtime information and bootstrap auth rather than spawning another real server. Its disposable helper may wait for shell stdin while the service stays alive. No change needed to Rust if this works through current sidecar protocol.
- Without an enabled service, preserve the current desktop lifecycle. Never attach to arbitrary remote or unverified listeners. Detect a configured-but-unreachable service and give a useful error; do not silently create duplicates.
- scripts/manage-host.mjs installs/starts/stops/checks a Library-only macOS LaunchAgent. Explicit server/home/port paths; plist escaping; no network/firewall changes, passwords, stack jobs or production service operations. Do not run live install commands in worker tests. Supported OS scope is macOS initially; other platforms give a clear unsupported message.

## UI contract

Keep blue identity, simple sidebar, accessible phone layout. Add offline Setup & Help with host/client explanations, role of every companion, per-step why/required/optional/check/fix, storage/NAS, monitoring/quality, sources, cleanup, startup/login/sleep, private remote access and troubleshooting. Show future installation/integration functions as planned, never working buttons.

Show sign-in when unauthenticated. If setupRequired and bootstrap-authenticated, show local account setup. Remote users cannot create the owner account. Remember device is optional; session list/revoke/logout supported. Desktop bootstrap remains usable for recovery. Header token from an existing launch hash is removed from the address as before.

Add Downloads view with safe pause/resume and selection; My library monitoring toggles; Find/add profile/root selection from live options before committing. Handle loading/error/empty/disabled states. Distinguish sampled speed coverage from exact historical data. Remote status uses a normal non-secret browser URL once auth exists, and keeps account setup/recovery guidance local.

## Integration acceptance and limits

- Unit tests: persistent identity, login/logout/revoke/expiry, missing token, foreign peers/hosts and write-CSRF defense; mock official API action/selection/monitoring cases; host runtime/plist validation.
- Browser: initial account setup, normal sign-in, remember device, logout, guide, queue actions and add options at desktop and phone widths. Live browser auth is checked without printing credentials.
- Release: Go vet Mac/Windows/Linux; Go tests; JavaScript syntax; npm ci; detached Mac DMG; disk-image/signature/packaged-server smoke; correct CHANGELOG version and clean commit email.
- 1301 read-only checks do not mutate the stock stack. Deployment sets cleanup off before connected launch. No real download/import, reboot, NAS credential storage or upstream updates without the relevant authorization/quiet point.
- First batch does not claim all roadmap items complete. Store remaining modules and test gaps in the work list. Retain one current and one previous shipped artifact; retire older owner files through the assigned app folder policy.
