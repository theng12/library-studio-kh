# Background host

Library Studio KH can run two ways. Both keep the local dashboard on loopback; the separate optional Tailscale listener is controlled in Settings.

| Mode | What starts it | Lifecycle |
| --- | --- | --- |
| Desktop only (default) | The desktop app starts the server itself | Ends with the window, as before |
| Background host | A macOS LaunchAgent runs `-service` | Keeps running when the window closes; the desktop app attaches to it |

Nothing changes for existing desktop-only users until a background host is installed. Optional features stay off until selected.

## Desktop attach

`-with-shell` looks for the private descriptor `host.json` in the same configuration home
(`~/Library/Application Support/Library Studio KH` unless `LIBRARY_STUDIO_HOME` says otherwise).

- Descriptor present and healthy: the desktop helper prints the existing loopback address and
  token for the window, then waits on its stdin. Closing the window stops only the helper; the
  background host keeps running.
- Descriptor missing: the old desktop lifecycle is used, with no change.
- Descriptor present but unreachable or unsafe: the desktop stops with a clear error instead of
  silently starting a second server. Start the host again, or remove the descriptor to return to
  a desktop-only session.

The descriptor is created by `-service` with mode `0600`. It contains the process id, loopback
address, version and bootstrap token. It is never printed or logged. A descriptor that is a
symlink, readable by other users, damaged, or points anywhere but loopback is refused.

Attachment sends a fresh challenge to the loopback-only host-proof endpoint and checks the returned HMAC locally. It does not send the bootstrap key to an unverified listener or follow redirects. Both the descriptor version and verified live version must match the desktop build; an old host is refused with a clear restart instruction. An OS-released configuration lock prevents multiple real servers from writing the same settings/session home.

## Background host

```sh
library-studio-server -service -port 7881
```

- Loopback only, fixed port (default `7881`, must be 1024–65535).
- Writes the private descriptor, then logs one token-free ready line. It never prints the token.
- If the port is taken it exits with an error naming the port instead of starting a duplicate.
- On `SIGTERM`/interrupt it removes its own descriptor only.

## macOS LaunchAgent manager

`scripts/manage-host.mjs` installs one Library-only job named `kh.librarystudio.host` at
`~/Library/LaunchAgents/kh.librarystudio.host.plist`. It never touches other agents, network
settings, the firewall, passwords or the stock apps.

```sh
# Install and start (absolute server path required)
node scripts/manage-host.mjs install --exe "/Applications/Library Studio KH.app/Contents/MacOS/library-studio-server" \
  --home "$HOME/Library/Application Support/Library Studio KH" --port 7881

node scripts/manage-host.mjs start
node scripts/manage-host.mjs stop
node scripts/manage-host.mjs status
```

- `install` is idempotent: unchanged settings with a loaded job are a no-op; changed settings
  rewrite the plist and reload only this job. `--dry-run` prints the plist and the `launchctl`
  commands without running them.
- Paths are escaped for the plist. Control characters are rejected.
- Logs go to `host.log` and `host.err.log` in the selected configuration home. They contain no
  credentials.
- `stop` keeps the plist, so `install` or `start` can bring the host back.
- `--self-test` checks escaping, validation and dry-run safety without calling `launchctl`.

Other operating systems are unsupported for now. The manager exits with a clear message instead
of pretending to install; run the server directly with `-service` there.

## Troubleshooting

| Symptom | Meaning and fix |
| --- | --- |
| `not installed` | Run `install` with the absolute server path first. |
| Port already in use | Another program or host uses the port. Choose another with `--port` / `-port`, then reinstall. |
| Desktop says the host is configured but not reachable | The job is stopped or crashed. Run `start`; check `host.err.log`. |
| `readable by other users` | Run `chmod 600` on `host.json`, or reinstall the host so it is recreated privately. |
| `not this computer` | The descriptor names a non-loopback address and is refused. Remove it and restart the host. |
| `launchctl bootstrap failed` | Read the message: usually a bad plist path or a job that needs `stop` first. |

## Limits

- The initial transport remains private Tailnet HTTP, matching the rest of the app. This does not
  claim HTTPS or Secure-cookie protection.
- The host manager changes only Library's own login agent. It does not change macOS automatic login, FileVault, firewall, Tailscale, NAS credentials or stock-app settings.
- The manager requires Node as a developer tool; the packaged Go host itself has no Node dependency. A built-in user-facing service installer is part of the future guided-setup work, not a completed UI control.
- For 1301 adoption, create configuration with `cleanupOn: false` before the first connected launch. Preserve the existing stock-stack jobs and seeding policy.
- Automated tests use mocks and dry-run service operations. Actual reboot/fresh-login behavior must be checked at a quiet point before claiming unattended recovery.
