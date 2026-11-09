package stock

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type historyRoundTrip func(*http.Request) (*http.Response, error)

func (f historyRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHistoryReadFailureHTTPAndRedaction(t *testing.T) {
	const secret = "private-key-private-host-and-path"
	cases := []struct {
		name       string
		status     int
		body, want string
	}{
		{"unauthorized", 401, secret, "unauthorized; HTTP 401"},
		{"forbidden", 403, `[{"errorMessage":"` + secret + `"}]`, "forbidden; HTTP 403"},
		{"server", 503, `[{"errorMessage":"` + secret + `"}]`, "http_error; HTTP 503"},
		{"decode", 200, `{"` + secret + `":`, "invalid_response; HTTP 200"},
		{"shape", 200, `{"records":[]}`, "invalid_history; HTTP 200"},
		{"path", 200, `{"totalRecords":1,"records":[{"eventType":"downloadImported","downloadId":"` + secret + `","date":"2026-01-01T00:00:00Z","data":{}}]}`, "invalid_history; HTTP 200"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Api-Key") != secret || !strings.HasPrefix(r.URL.Path, "/api/v1/history") {
					t.Error("existing native auth or route changed")
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			arr := NewArr("Lidarr", server.URL, secret)
			proofs, err := arr.WholeImports()
			if err == nil || proofs != nil {
				t.Fatalf("failed history became usable: %v", err)
			}
			got := HistoryReadFailure(err)
			if got != tc.want || strings.Contains(got, secret) || strings.Contains(got, server.URL) {
				t.Fatalf("unsafe/wrong diagnostic: %q", got)
			}
		})
	}
	if got := HistoryReadFailure(errors.New(secret)); got != "unavailable" {
		t.Fatalf("raw fallback: %q", got)
	}
}

func TestHistoryReadFailureTimeoutAndConnection(t *testing.T) {
	for _, tc := range []struct {
		name    string
		failure error
		want    string
	}{
		{"timeout", context.DeadlineExceeded, "timeout"},
		{"connection", errors.New("https://private-host/?apikey=private-key"), "connection"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			arr := NewArr("Lidarr", "http://unused.invalid", "private-key")
			arr.c.Transport = historyRoundTrip(func(*http.Request) (*http.Response, error) { return nil, tc.failure })
			_, err := arr.WholeImports()
			if got := HistoryReadFailure(err); got != tc.want {
				t.Fatalf("got %q", got)
			}
			if !errors.Is(err, tc.failure) {
				t.Fatal("lost original error chain")
			}
		})
	}
}
