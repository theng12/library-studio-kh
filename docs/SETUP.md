# Guided setup

Library keeps the media engines separate. Guided installation currently supports the official Apple Silicon macOS packages for qBittorrent, Sonarr, Radarr, Lidarr and Prowlarr. An existing-host client opens the normal Library webpage and signs in to that host. Other platform installers and optional companion installers are not implied by this five-app workflow.

## Choose this computer or an existing host

On a fresh desktop launch, choose whether this computer will host the library. A host creates its owner account here; passwords need at least six characters and may contain only numbers. A client instead enters the other computer's normal HTTP or HTTPS root address. Do not paste a bootstrap shortcut, query, page path or login into that address.

Client setup checks the host's sign-in response without forwarding local headers, cookies or credentials. Redirects are refused. The client signs in normally after navigation. The native Use this Mac as host menu returns this desktop to local mode; it never stops the remote server.

## Review selected apps and folders

Open Setup & help → Set up this computer. Select the apps, application destination, download directory, media roots, categories and ports. Ports must be distinct from 1024 through 65535. TV episode renaming starts on for new managed Sonarr profiles; the starting choice is editable. Existing naming and seeding settings are preserved.

Review setup plan inspects existing bundles and resolves missing packages from current official publications. It reads Servarr release metadata and the macOS section of qBittorrent's official download page. A package without a verifiable official digest is unavailable, rather than assigned a self-computed provenance hash.

The review shows each offered version, source and enforced download limit. Size not published is unknown, not a zero-byte download. A changed official publication requires another review before installation. A conflicting bundle is kept untouched. Applications in the standard user/system application locations are considered for adoption before another copy is downloaded.

Local folders selected for setup can be created. External/NAS destinations must be mounted and the chosen folder must exist; setup does not create a replacement local directory for an unavailable volume.

## Install, open and check

Install selected apps verifies package bytes before extraction, checks bundle identity and architecture, and installs only missing bundles. It preserves quarantine and applies a normal download quarantine to new verified bundles. It never clears quarantine, self-signs a third-party app or opens it automatically. Receipts and failed staging are recoverable.

Choose Open for each newly managed app and complete any macOS or native first-launch prompts. Library creates private loopback profiles before first opening. Generated native credentials remain in private files and are not returned in job JSON. Existing accounts and profiles are adopted where their actual loopback settings can be read; an existing qBittorrent account may need explicit connection through Settings.

Check connections tests the selected native APIs. Setup then configures or reuses the selected roots, categories, download clients and Prowlarr links. It tests native connections and reads saved settings back. A conflicting existing value is reported without replacing it. Hidden saved credentials remain not checked, rather than being reported passed because a different supplied credential worked.

A new Lidarr music root needs quality and metadata profiles. Setup offers the profiles Lidarr currently reports, waits for a choice, validates it again and verifies the saved root's profile IDs. No profile ID or quality preference is guessed.

New managed import settings enable completed-download handling and keep native removal off. Library's staged-cleanup switch and qBittorrent seeding limits remain at their existing choices. Existing companion naming/import policies are not silently reset.

## Choose a source

Prowlarr needs an enabled source before searches can work. Sources uses Prowlarr's current provider catalog. Add, test and save the chosen provider and its required account fields, then enable it if intended. Provider accounts, subscriptions and source availability are the owner's choice.

Check setup again. Readiness requires the selected native connections, accessible roots, saved links/logins and enabled-source tests to pass. This does not start a media search or download. Request accepted never means file downloaded.

## Optional startup and recovery

Start at login is off until selected. It registers only the Library host and new managed companions in the reviewed plan. Existing externally managed companion jobs are preserved. A desktop server may require quit/reopen before background handoff; check setup afterward. A saved or loaded job is not evidence of a real fresh-login test. See [background hosting](HOST.md).

The job records its actual steps privately and survives a Library restart. Retry keeps its reviewed identity and rechecks installations. Edit choices preserves the prior record and reviews a new plan. Failed writes and interrupted work remain explicit. Existing apps, account data and native settings are not discarded as an error-recovery shortcut.

A native app may need macOS approval or folder access. Complete the supported OS prompt rather than weakening security or changing privacy settings automatically. An unavailable volume, hidden native secret, source failure or blocked first launch keeps setup incomplete and explains the next action.

## Scope and checks

This flow covers the five core native macOS packages. Bazarr and the other approved optional companions remain separate integrations; choosing them is not implied by installing the core stack. Readarr remains excluded. Existing 1301 media operations and cleanup policy are not changed by publishing Library.

Automated verification uses fake vendors, fake native APIs, temporary files and fake service commands. Real first-launch approval, new-login/reboot behavior and physical-phone acceptance require their own evidence. Browser phone emulation does not establish a physical-device result.
