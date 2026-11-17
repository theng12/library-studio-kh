#!/usr/bin/env bash
# Native window lifecycle, proven on a real (hidden, unfocused) window:
# closing it hides it instead of quitting, the menu action shows the same window
# again, and the update hold answers while no window is visible. The app is the
# opt-in --features webview-check build; never the release binary, a visible
# window, the owner's app data or the owner's hold credential.
#   scripts/test-window-lifecycle.sh
set -eu

repo="$(cd "$(dirname "$0")/.." && pwd)"
tmp="${TMPDIR:-/tmp}"
work="${tmp%/}/library-window-lifecycle-$$"
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

# The installer's credential, in this test's own 0600 file — never the owner's.
hold_token_file="$work/hold-token"
python3 -I -c 'import secrets; print(secrets.token_urlsafe(24))' >"$hold_token_file"
chmod 600 "$hold_token_file"

# 18643 keeps this fixture off the owner's live hold on 8643. The route and shape are the same.
export KH_TEST_HOME="$work/home" KH_TEST_STOCK="$stock" KH_TEST_DOWNLOADS="$work/downloads" \
  KH_TEST_LIBRARY="$work/library" KH_TEST_METADATA=offline KH_TEST_PORT=0 KH_TEST_HOLD_PORT=18643 \
  KHU_HOLD_TOKEN_FILE="$hold_token_file"
"$work/bin/library-studio" --seed-fixtures --test-mode >"$work/logs/library-studio.log" 2>&1 &
app_pid=$!
url="$(wait_line "$work/logs/library-studio.log" TEST_URL= "$app_pid")"
login="$(grep -m1 '^TEST_LOGIN=' "$work/logs/library-studio.log" | cut -d= -f2-)"
password="${login#*:}"
hold_url="$(sed -n 's/.*TEST_HOLD_URL=//p' "$work/logs/library-studio.log" | head -1)"
[ -n "$hold_url" ] || fail "the test app's hold did not open: $(grep -i 'update hold' "$work/logs/library-studio.log" || true)"
echo "Library up (pid $app_pid): $url  hold: $hold_url"

echo "Building the verification shell (never shipped) ..." >&2
export PATH="/opt/homebrew/opt/rustup/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin"
(cd "$repo/src-tauri" && cargo build --features webview-check >"$work/logs/cargo.log" 2>&1) \
  || fail "the verification build failed: see $work/logs/cargo.log"
exe="$repo/src-tauri/target/debug/library-studio-kh"
[ -x "$exe" ] || fail "the verification binary is missing: $exe"

# The owner's binary is never run with --webview-check; this feature build is the check.
echo "Running the hidden window lifecycle check ..." >&2
KH_TEST_URL="$url" KH_TEST_HOME="$work/home" KH_TEST_PASSWORD="$password" \
  KH_HOLD_URL="$hold_url" KH_HOLD_TOKEN_FILE="$hold_token_file" KH_WEBVIEW_CHECK_TIMEOUT=60 \
  "$exe" --window-lifecycle-check >"$work/logs/check.log" 2>&1 &
check_pid=$!
i=0
while [ $i -lt 900 ] && kill -0 "$check_pid" 2>/dev/null; do sleep 0.1; i=$((i + 1)); done
if kill -0 "$check_pid" 2>/dev/null; then
  kill "$check_pid" 2>/dev/null || true
  fail "the lifecycle check did not finish within 90s (pid $check_pid)"
fi
status=0
wait "$check_pid" || status=$?

receipt="$work/home/native-webview-result.json"
[ -f "$receipt" ] || { cat "$work/logs/check.log" >&2; fail "no receipt was written ($receipt)"; }
cat "$receipt"
echo
[ "$status" -eq 0 ] || fail "the lifecycle check exited $status"
python3 -I -c 'import json,sys; r=json.load(open(sys.argv[1])); assert r.get("ok") is True, r' "$receipt" \
  || fail "the receipt does not pass"
echo "ok   close hides the window, the menu action shows it again, the hold answers while hidden"
