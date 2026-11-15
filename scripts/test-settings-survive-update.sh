#!/usr/bin/env bash
# Settings survive an update: write non-default settings through the app's own
# HTTP API, stop that server, start a SECOND build over the same home (the
# update-installed-over-the-settings case) and read every value back.
# Headless on 127.0.0.1 with a home under $TMPDIR — no window, no focus, and
# never the owner's live app data. Prints one line per checked value and exits
# non-zero if any value dropped. Stops only the processes it started, by PID.
#   scripts/test-settings-survive-update.sh
set -eu

repo="$(cd "$(dirname "$0")/.." && pwd)"
tmp="${TMPDIR:-/tmp}"
work="${tmp%/}/library-settings-update-$$"
mkdir -p "$work/bin" "$work/home" "$work/downloads" "$work/library" "$work/logs"
work="$(cd "$work" && pwd -P)" # resolve /var -> /private/var, same as test-ui.sh

stock_pid=""
app_pid=""
cleanup() {
  for p in "$stock_pid" "$app_pid"; do
    [ -n "$p" ] && kill "$p" 2>/dev/null || true
  done
  wait 2>/dev/null || true
  echo "Stopped. Test data kept in $work" >&2
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# wait_line FILE PREFIX PID: print the value after PREFIX once it appears, or fail if PID dies.
wait_line() {
  i=0
  while [ $i -lt 300 ]; do
    v="$(grep -m1 "^$2" "$1" 2>/dev/null | cut -d= -f2- || true)"
    if [ -n "$v" ]; then echo "$v"; return 0; fi
    kill -0 "$3" 2>/dev/null || { echo "process for $2 exited; log: $1" >&2; cat "$1" >&2; return 1; }
    sleep 0.1
    i=$((i + 1))
  done
  echo "timed out waiting for $2; log: $1" >&2
  return 1
}

# start_app BINARY LOGNAME: start one headless copy and wait until it serves.
start_app() {
  "$1" --test-mode >"$work/logs/$2" 2>&1 &
  app_pid=$!
  url="$(wait_line "$work/logs/$2" TEST_URL= "$app_pid")"
  url="${url%/}"
}

echo "Building into $work/bin ..." >&2
(cd "$repo" && go build -o "$work/bin/mock-stock" ./tests/mock-stock \
  && go build -o "$work/bin/library-studio-v1" ./cmd/library-studio \
  && go build -o "$work/bin/library-studio-v2" ./cmd/library-studio)

"$work/bin/mock-stock" -addr 127.0.0.1:0 -downloads "$work/downloads" \
  -library "$work/library" -empty-library >"$work/logs/mock-stock.log" 2>&1 &
stock_pid=$!
stock="$(wait_line "$work/logs/mock-stock.log" MOCK_STOCK= "$stock_pid")"

# Isolated home under $TMPDIR: test mode refuses the owner's real settings folder.
export KH_TEST_HOME="$work/home" KH_TEST_STOCK="$stock" KH_TEST_DOWNLOADS="$work/downloads" \
  KH_TEST_LIBRARY="$work/library" KH_TEST_METADATA=offline KH_TEST_PORT=0

# ---- first server: write non-default settings through its own API ----
start_app "$work/bin/library-studio-v1" first.log
echo "first server up (pid $app_pid): $url"
# The bootstrap token the fixture writes to its own access.json authorises the API.
token="$(python3 -I -c 'import json,sys; print(json.load(open(sys.argv[1]))["token"])' "$work/home/access.json")"

body="$(cat <<EOF
{"appearance":"dark","tailscaleOn":true,"tailscalePort":7999,
 "stagedCleanupOn":true,"stagedDays":7,"managedTrashDays":9,"nativeTrashDays":45,
 "pressureFreeGB":120,"seedRatio":1.5,"seedMinutes":900,
 "downloadPath":"$work/downloads","libraryPath":"$work/library",
 "deadCleanupOn":true,"deadCleanupHours":48,
 "sonarr":{"url":"http://127.0.0.1:8989","apiKey":"settings-survive-sonarr"}}
EOF
)"
code="$(curl -sS -o "$work/logs/save.json" -w '%{http_code}' -X POST "$url/api/settings" \
  -H 'Content-Type: application/json' -H 'X-Library-Request: 1' -H "X-Library-Token: $token" \
  --data "$body")"
if [ "$code" != 200 ]; then
  echo "POST /api/settings answered $code:" >&2
  cat "$work/logs/save.json" >&2
  exit 1
fi
echo "wrote non-default settings through POST /api/settings"

# ---- the update: stop it, start a second build over the same home ----
kill "$app_pid" 2>/dev/null || true
wait "$app_pid" 2>/dev/null || true
app_pid=""
echo "first server stopped"

start_app "$work/bin/library-studio-v2" second.log
echo "second build up over the same home (pid $app_pid): $url"

curl -sS -o "$work/logs/state.json" -H "X-Library-Token: $token" "$url/api/state"
python3 -I -c '
import json, sys
settings = json.load(open(sys.argv[1]))["settings"]
checks = [
    ("appearance", settings.get("appearance"), "dark"),
    ("tailscaleOn", settings.get("tailscaleOn"), True),
    ("tailscalePort", settings.get("tailscalePort"), 7999),
    ("stagedCleanupOn", settings.get("stagedCleanupOn"), True),
    ("stagedDays", settings.get("stagedDays"), 7),
    ("managedTrashDays", settings.get("managedTrashDays"), 9),
    ("nativeTrashDays", settings.get("nativeTrashDays"), 45),
    ("pressureFreeGB", settings.get("pressureFreeGB"), 120),
    ("seedRatio", settings.get("seedRatio"), 1.5),
    ("seedMinutes", settings.get("seedMinutes"), 900),
    ("downloadPath", settings.get("downloadPath"), sys.argv[2]),
    ("libraryPath", settings.get("libraryPath"), sys.argv[3]),
    ("deadCleanupOn", settings.get("deadCleanupOn"), True),
    ("deadCleanupHours", settings.get("deadCleanupHours"), 48),
    ("sonarr.url", (settings.get("sonarr") or {}).get("url"), "http://127.0.0.1:8989"),
]
dropped = 0
for name, got, want in checks:
    ok = got == want
    print(("ok   " if ok else "FAIL ") + "%s = %r" % (name, got) + ("" if ok else ", want %r" % (want,)))
    dropped += 0 if ok else 1
if dropped:
    print("%d setting(s) dropped across the update" % dropped, file=sys.stderr)
    sys.exit(1)
print("all %d settings survived the update" % len(checks))
' "$work/logs/state.json" "$work/downloads" "$work/library"
