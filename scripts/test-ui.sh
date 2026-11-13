#!/usr/bin/env bash
# Headless UI test server: real dashboard on 127.0.0.1, mock stock apps, isolated
# data under $TMPDIR. Never opens a window or touches the owner's live data.
#   scripts/test-ui.sh [--offline] [--port N]
# Prints TEST_URL, TEST_LOGIN and TEST_HOME, then blocks until Ctrl-C/SIGTERM.
set -eu

offline=0
port=0
while [ $# -gt 0 ]; do
  case "$1" in
    --offline) offline=1 ;;
    --port) port="${2:?--port needs a number}"; shift ;;
    *) echo "usage: $0 [--offline] [--port N]" >&2; exit 2 ;;
  esac
  shift
done

repo="$(cd "$(dirname "$0")/.." && pwd)"
tmp="${TMPDIR:-/tmp}"
work="${tmp%/}/library-ui-$$"
mkdir -p "$work/bin" "$work/home" "$work/downloads" "$work/library" "$work/logs"
work="$(cd "$work" && pwd -P)" # resolve /var -> /private/var; the cleanup ledger refuses symlinked paths

pids=""
cleanup() {
  for p in $pids; do kill "$p" 2>/dev/null || true; done
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

echo "Building into $work/bin ..." >&2
(cd "$repo" && go build -o "$work/bin/mock-stock" ./tests/mock-stock && go build -o "$work/bin/library-studio" ./cmd/library-studio)

"$work/bin/mock-stock" -addr 127.0.0.1:0 -downloads "$work/downloads" -library "$work/library" >"$work/logs/mock-stock.log" 2>&1 &
pids="$pids $!"
stock="$(wait_line "$work/logs/mock-stock.log" MOCK_STOCK= $!)"

metadata=offline
if [ "$offline" = 0 ]; then
  # Keyless provider stub with the iTunes Search and TVmaze shapes metadata.go reads.
  python3 -I -c '
import base64, http.server, json, sys
JPG = base64.b64decode("/9j/2wCEABALDA4MChAODQ4SERATGCgaGBYWGDEjJR0oOjM9PDkzODdASFxOQERXRTc4UG1RV19iZ2hnPk1xeXBkeFxlZ2MBERISGBUYLxoaL2NCOEJjY2NjY2NjY2NjY2NjY2NjY2NjY2NjY2NjY2NjY2NjY2NjY2NjY2NjY2NjY2NjY2NjY//AABEIAAEAAQMBIgACEQEDEQH/xAGiAAABBQEBAQEBAQAAAAAAAAAAAQIDBAUGBwgJCgsQAAIBAwMCBAMFBQQEAAABfQECAwAEEQUSITFBBhNRYQcicRQygZGhCCNCscEVUtHwJDNicoIJChYXGBkaJSYnKCkqNDU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6g4SFhoeIiYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2drh4uPk5ebn6Onq8fLz9PX29/j5+gEAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoLEQACAQIEBAMEBwUEBAABAncAAQIDEQQFITEGEkFRB2FxEyIygQgUQpGhscEJIzNS8BVictEKFiQ04SXxFxgZGiYnKCkqNTY3ODk6Q0RFRkdISUpTVFVWV1hZWmNkZWZnaGlqc3R1dnd4eXqCg4SFhoeIiYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2dri4+Tl5ufo6ery8/T19vf4+fr/2gAMAwEAAhEDEQA/AKlFFFdBif/Z")
class H(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def send(self, body, kind):
        self.send_response(200); self.send_header("Content-Type", kind); self.send_header("Content-Length", str(len(body))); self.end_headers(); self.wfile.write(body)
    def do_GET(self):
        base = "http://%s:%d" % self.server.server_address
        if self.path.startswith("/art/"):
            return self.send(JPG, "image/jpeg")
        if self.path.startswith("/search"):
            return self.send(json.dumps({"resultCount": 1, "results": [{"artworkUrl100": base + "/art/100x100bb.jpg", "longDescription": "Stub provider overview.", "releaseDate": "2020-05-01T07:00:00Z", "primaryGenreName": "Drama", "trackTimeMillis": 5400000}]}).encode(), "application/json")
        if self.path.startswith("/singlesearch/shows"):
            return self.send(json.dumps({"image": {"original": base + "/art/show.jpg"}, "summary": "<p>Stub provider summary.</p>", "premiered": "2019-01-01", "genres": ["Drama"], "averageRuntime": 42, "rating": {"average": 7.1}}).encode(), "application/json")
        self.send_error(404)
s = http.server.ThreadingHTTPServer(("127.0.0.1", 0), H)
print("METADATA_STUB=http://%s:%d" % s.server_address, flush=True)
s.serve_forever()
' >"$work/logs/metadata-stub.log" 2>&1 &
  pids="$pids $!"
  metadata="$(wait_line "$work/logs/metadata-stub.log" METADATA_STUB= $!)"
fi

export KH_TEST_HOME="$work/home" KH_TEST_STOCK="$stock" KH_TEST_DOWNLOADS="$work/downloads" \
  KH_TEST_LIBRARY="$work/library" KH_TEST_METADATA="$metadata" KH_TEST_PORT="$port"
"$work/bin/library-studio" --seed-fixtures --test-mode >"$work/logs/library-studio.log" 2>&1 &
app=$!
pids="$pids $app"
url="$(wait_line "$work/logs/library-studio.log" TEST_URL= $app)"
login="$(grep -m1 '^TEST_LOGIN=' "$work/logs/library-studio.log" | cut -d= -f2-)"

echo "TEST_URL=$url"
echo "TEST_LOGIN=$login"
echo "TEST_HOME=$work/home"
echo "MOCK_STOCK=$stock"
echo "METADATA=$metadata"
echo "Logs: $work/logs  (Ctrl-C to stop)" >&2
wait "$app" || true
