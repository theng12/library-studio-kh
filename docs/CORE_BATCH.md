# Core tracking batch — 0.3.0

Owner authorized continuation after confirming the dashboard on1301. Scope: item details/monitoring, intentional release searches/grabs through the library apps, and read-only Prowlarr/source status. No downloader/indexer replacement, deletion, companion installation, real media changes during tests or production-service work. Cleanup stays off.

## Parallel ownership

- Tracking worker: NEW internal/stock/tracking*.go, internal/library/tracking*.go, internal/server/tracking*.go only. No edits to existing shared files.
- Sources worker: NEW internal/stock/sources*.go, internal/library/sources*.go, internal/server/sources*.go only. No edits to shared files.
- Lead: web UI (new media.js module plus small app/index hooks), mock fixtures, route registration, version/docs, integration/build/deployment.

Workers first invoke ponytail and then query the Hub graph /home/user/Developer/_graphs/hub/graphify-out/graph.json with --budget1500 before hand search. Relevant AGENTS sections: Layout, Rules, Verify (mock tests only). No agents/commits/branch changes, real apps, credentials, SSH, dependencies or gates. Focused tests only; keep within ~25 tool calls and one deliverable. Read this contract and the existing helper files, not the whole product plan.

## Tracking API and shapes

Server provides `registerTracking(mux *http.ServeMux)` for lead registration. All routes use existing guard/auth/CSRF.

- GET `/api/media/details?app=Sonarr|Radarr|Lidarr&id=N` -> Detail below.
- POST `/api/media/tracking` `{app,id,kind,childId,seasonNumber,monitored}` -> `{ok:true}`. Explicit monitored bool required. `kind` is `season` or `episode` for Sonarr, `album` for Lidarr. IDs positive; seasonNumber can be0 for specials. Verify child belongs to parent before writing. Never create/delete media or silently change a different parent.
- GET `/api/media/releases?app=...&id=N&episodeId=M&albumId=K` -> `{items:Release[]}`. Radarr uses movieId=N. Sonarr requires episodeId and confirms series ownership. Lidarr requires albumId and confirms artist ownership. Read details must not trigger release searches. This endpoint is called only by a deliberate Search button, never polling.
- POST `/api/media/grab` `{app,id,episodeId,albumId,key}` -> `{ok:true}`. Re-fetch stock releases for that context, match the opaque key and refuse rejected/disallowed results. POST the matched upstream release resource to the same app `/release`; do not accept arbitrary URLs or client-supplied release objects, and do not bypass app profiles/categories/imports.

Detail JSON fields: `app,id,title,monitored,haveFiles,status,path,qualityProfileId,seasons:[],episodes:[],albums:[]`. Empty collections are arrays, not null.

Season: `{seasonNumber,monitored}`.
Episode: `{id,seasonNumber,episodeNumber,title,monitored,haveFiles,when}` (`when` ISO date if known).
Album: `{id,title,monitored,haveFiles,when,trackCount,trackFileCount}` (counts pointers/omitted if unknown, not fabricated0).

Release: `{key,title,indexer,quality,protocol,size,seeders,ageMinutes,rejected,rejections:[]}`. Unknown numeric data is null/omitted. Size bytes. `key` is an opaque SHA256 digest over context+indexerId+guid; do NOT return raw guid, downloadUrl, magnetUrl, raw resources or source credentials. Internal re-fetch computes the same key. DownloadAllowed=false, rejected=true or any rejection reason should disable grabbing. Partial unsupported data/errors must be honest; no placeholder ready states.

Stock helpers use current Arr.do and the existing library.arr helper; no new generic framework. GET raw parent then PUT only its monitoring change to preserve upstream fields. Sonarr `/episode/monitor` accepts `{episodeIds:[id],monitored:bool}`; Lidarr `/album/monitor` accepts `{albumIds:[id],monitored:bool}` (verified against official source). Season monitoring updates the selected entry in the series' seasons array. Details use `/series/id` + `/episode?seriesId=id`, `/movie/id`, `/artist/id` + `/album?artistId=id`. Album statistics provide counts when present. Verify any additional API semantics against public upstream source; no live host calls in worker tests.

Tests: unknown app, invalid/omitted IDs/bool, cross-parent child refusal with no mutation, season0, field-preserving updates, no implicit release fetch, sanitized release output, rejected/stale/arbitrary key refusal, successful grab via correct app/context, upstream errors. Use mocked APIs only.

## Sources API and shapes

Server provides `registerSources(mux)` for lead registration, GET `/api/sources` only. No source tests/sync/reconfigure actions in this batch.

Return `{configured:bool,indexers:[],links:[],health:[],libraries:[],problems:[]}`.
- Indexer `{id,name,enabled,protocol,priority,disabledUntil}`; unknown fields omitted/null; enabled from actual Prowlarr config, and disabledUntil from `/indexerstatus` if available. Unknown status is not healthy.
- Link `{id,name,syncLevel}` from Prowlarr `/applications`, allowlist fields only.
- Health `{type,message}` from `/health`; sanitize messages/URLs and known API keys. Never expose raw fields config, passwords, API keys, base URLs, passkeys or download URLs.
- Library `{app,configured,indexers:[{id,name,enabled}],error}` for Sonarr/Radarr/Lidarr `/indexer`. Counts/lists are observed configuration, not proof of successful synchronization. Do not infer sync success from equal counts.
- If Prowlarr isn't configured, return configured=false, empty collections and useful problem. If individual reads fail, preserve other results and label unavailable parts. No per-source failure can read as all healthy. Error text must not leak credentials/URLs.

Use explicit methods in stock/sources.go and a small library wrapper. Tests cover whitelist/sanitization, missing connection, partial outage/status API failure, actual disabled status and no mutation requests.

## UI

Lead adds new `media.js` loaded after app.js, using existing api(), row(), helpers. My library titles open details in a normal view with a Back button. Season/episode/album monitoring controls show success/failure; release searches require explicit clicks on a movie/episode/album, show rejections and allow only eligible manual grabs. Sources view has Refresh and observed configuration/status with clear partial errors. No source configuration buttons or automatic queries. Desktop/mobile, keyboard/loading/empty/error states and existing blue identity preserved.

## Verification and deployment

Mock browser workflows, focused+full tests and three-platform vet, detached package/signature/packaged-server checks, Library-only update on1301 with identity/account/settings preserved. Read-only checks against actual APIs may verify empty lists/status; no production grab/monitor/add mutation or real download without a selected owner-approved content test. Current user sessions and six-character password policy must survive the upgrade. No feed publication.
