#!/usr/bin/env bash
# Update hold, proven end to end: the installer's two calls against a real headless
# Library with its own home under $TMPDIR. No window, no focus, never the owner's app
# data. Stops only the processes it started, by PID.
#   scripts/test-update-hold.sh
# Contract: _orchestra/youtube-stack/update-hold/CONTRACT.md
set -eu

repo="$(cd "$(dirname "$0")/.." && pwd)"
tmp="${TMPDIR:-/tmp}"
work="${tmp%/}/library-update-hold-$$"
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

fail() { echo "FAIL $*" >&2; exit 1; }

wait_line() { # wait_line FILE PREFIX PID
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

echo "Building into $work/bin ..." >&2
(cd "$repo" && go build -o "$work/bin/mock-stock" ./tests/mock-stock \
  && go build -o "$work/bin/library-studio" ./cmd/library-studio)

"$work/bin/mock-stock" -addr 127.0.0.1:0 -downloads "$work/downloads" \
  -library "$work/library" >"$work/logs/mock-stock.log" 2>&1 &
stock_pid=$!
stock="$(wait_line "$work/logs/mock-stock.log" MOCK_STOCK= "$stock_pid")"

export KH_TEST_HOME="$work/home" KH_TEST_STOCK="$stock" KH_TEST_DOWNLOADS="$work/downloads" \
  KH_TEST_LIBRARY="$work/library" KH_TEST_METADATA=offline KH_TEST_PORT=0

start_app() { # start_app LOGNAME
  "$work/bin/library-studio" --test-mode >"$work/logs/$1" 2>&1 &
  app_pid=$!
  url="$(wait_line "$work/logs/$1" TEST_URL= "$app_pid")"
  url="${url%/}"
  token="$(python3 -I -c 'import json,sys; print(json.load(open(sys.argv[1]))["token"])' "$work/home/access.json")"
  # The shipped shape: the hold on its fixed loopback port, the address the feed advertises.
  # log.Printf prefixes a timestamp, so the marker is not at the start of the line.
  hold_url="$(sed -n 's/.*TEST_HOLD_URL=//p' "$work/logs/$1" | head -1)"
  [ -n "$hold_url" ] || fail "the hold did not open on its fixed port (is 8643 already taken?): $(grep -i 'update hold' "$work/logs/$1" || true)"
}

hold() { # hold TIMEOUT PAUSE — against the fixed port, with no token, like the installer
  curl -sS -o "$work/logs/hold.json" -w '%{http_code}' -X POST \
    "$hold_url?timeout_s=$1&pause_s=$2"
}
resume() {
  curl -sS -o "$work/logs/resume.json" -w '%{http_code}' -X DELETE "$hold_url"
}
run_code() { # a real work claim: a cleanup pass
  curl -sS -o "$work/logs/run.json" -w '%{http_code}' -X POST "$url/api/cleanup/run" \
    -H 'X-Library-Request: 1' -H "X-Library-Token: $token"
}
field() { python3 -I -c 'import json,sys; print(json.load(open(sys.argv[1])).get(sys.argv[2]))' "$1" "$2"; }

start_app first.log
echo "Library up (pid $app_pid): $url"
[ "$hold_url" = "http://127.0.0.1:8643/api/maintenance/hold" ] || fail "hold must answer on its published address, got $hold_url"
echo "ok   hold published on the fixed address the feed advertises: $hold_url"

# ---- 1. hold pauses the claim path and reports the real in-flight count ----
code="$(hold 20 120)"
[ "$code" = 200 ] || fail "hold answered $code: $(cat "$work/logs/hold.json")"
echo "ok   POST hold -> 200"
[ "$(field "$work/logs/hold.json" safe)" = True ] || fail "idle hold must be safe: $(cat "$work/logs/hold.json")"
echo "ok   idle hold reports safe=true"
[ "$(field "$work/logs/hold.json" in_flight)" = 0 ] || fail "idle in_flight must be 0"
echo "ok   in_flight is real (0 with nothing running)"
[ "$(field "$work/logs/hold.json" dispatch_paused_s)" = 120 ] || fail "pause not reported"
echo "ok   dispatch_paused_s = 120"

code="$(run_code)"
[ "$code" = 503 ] || fail "a held app must refuse new work, got $code: $(cat "$work/logs/run.json")"
grep -qi "pausing for an update" "$work/logs/run.json" || fail "refusal must say why: $(cat "$work/logs/run.json")"
echo "ok   held app refuses new work and says why in plain words"

# ---- 2. DELETE resumes at once ----
code="$(resume)"
[ "$code" = 200 ] || fail "resume answered $code"
code="$(run_code)"
[ "$code" != 503 ] || fail "work still refused after DELETE"
echo "ok   DELETE resumes at once (work accepted again, $code)"

# ---- 3. a hold with no DELETE expires after pause_s ----
code="$(hold 5 2)"
[ "$code" = 200 ] || fail "short hold answered $code"
code="$(run_code)"
[ "$code" = 503 ] || fail "work must still be refused during the pause, got $code"
echo "ok   work refused while the pause lasts"
sleep 3
code="$(run_code)"
[ "$code" != 503 ] || fail "an aborted install must resume on its own after pause_s"
echo "ok   pause expired by itself after 2s with no DELETE (work accepted, $code)"

# ---- 4. a restart resumes by itself ----
code="$(hold 5 600)"
[ "$code" = 200 ] || fail "hold before restart answered $code"
code="$(run_code)"
[ "$code" = 503 ] || fail "work must be refused before the restart"
kill "$app_pid" 2>/dev/null || true
wait "$app_pid" 2>/dev/null || true
app_pid=""
start_app second.log
echo "ok   restarted over the same home (pid $app_pid): $url"
code="$(run_code)"
[ "$code" != 503 ] || fail "a restart must resume with no operator step"
echo "ok   restart resumed by itself, no DELETE and no manual step (work accepted, $code)"

echo "all update-hold checks passed"
