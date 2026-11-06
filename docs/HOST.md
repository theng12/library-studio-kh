# Background host

Library can run with the desktop window or as an optional macOS user login job. Both keep the local dashboard on loopback. Settings controls the separate optional Tailscale listener.

Desktop-only is the starting mode. Closing the window stops its server. A background host keeps running when the window closes, and the desktop attaches to it. Setup does not change an existing startup arrangement unless the owner selects startup.

## Packaged startup

In Setup & help, select Start at login when reviewing a setup plan. Library registers only its own host and the new Library-managed companions selected in that plan. Adopted or externally managed companion jobs remain unchanged. Each companion opens through the normal macOS application launcher; setup first requires explicit opening and native readiness. No service bypasses macOS approval.

The server included inside Library Studio KH also provides local controls, with no Node, Go or Homebrew prerequisite:

```bash
library-studio-server -host-control install -port 7881
```

```bash
library-studio-server -host-control status
```

```bash
library-studio-server -host-control start
```

```bash
library-studio-server -host-control stop
```

Use the installed sidecar's full path if it is not on PATH. `-host-home` selects the Library configuration home. Only the local install CLI accepts `-exe` as a trusted packaging override; server requests never supply an executable. Status inspects the saved installation rather than treating default flags as desired settings.

The host job is `kh.librarystudio.host`. Companion jobs are `kh.librarystudio.companion.<app-id>`. Definitions and logs are private. A foreign, symlinked or ambiguous job file is blocked. Replaced Library definitions are retained recoverably. The loaded definition must match the saved definition before status reports it loaded; an unverified host is never reported running.

If a desktop server currently holds the configuration lock, installing the host reports Restart needed. Quit and reopen Library, then check setup again. The native Restart Library menu performs the desktop restart. No second server is started while the home is locked.

The older `scripts/manage-host.mjs` remains a developer compatibility tool. It is not required for packaged setup.

## Desktop attach and client mode

The desktop first reads its private `client.json`. A saved normal remote host URL takes precedence over local hosting. It contains no owner credentials or bootstrap token. The native Use this Mac as host menu retires this desktop's client setting and restarts only the desktop helper. It does not stop the remote host or a local background job.

For local hosting, the helper checks the private `host.json` descriptor in the Library configuration home. The default home is the operating system's Library Studio KH configuration folder; `LIBRARY_STUDIO_HOME` can override it.

A healthy descriptor is attached. A missing descriptor with an installed login job causes a bounded start and proof check. Without an installed job, the desktop starts its own server. An unsafe, unreachable or mismatched descriptor produces an error instead of silently starting a duplicate.

The descriptor is mode 0600 and records the process, loopback address, version and bootstrap credential. Attachment sends a fresh challenge to the loopback-only host-proof endpoint and checks its HMAC locally. It never sends the bootstrap credential to an unverified listener or follows redirects. The verified live version must match the desktop build. An OS-released configuration lock protects the settings/session home.

## Troubleshooting

A missing job needs Install. A configured but unreachable host needs Start and a review of its private error log. An occupied port requires another selected port. A changed or unverified loaded definition needs a successful restart before it can report ready. Invalid private descriptors are retained for diagnosis; do not discard account or settings files as a shortcut.

Stopping the background host keeps its definition. Starting it reloads the saved Library definition. Existing media engines, native accounts, seeding policy, NAS mounts and unrelated jobs are outside the host controls.

Automatic startup does not enable macOS automatic login or change FileVault, firewall, Tailscale, NAS credentials or privacy permissions. Windows/Linux login-job installation is explicitly unsupported. Automated checks use fake commands and temporary homes; a real login/reboot and OS approval remain separate quiet-point checks.
