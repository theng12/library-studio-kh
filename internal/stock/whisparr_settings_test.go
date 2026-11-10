package stock

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWhisparrSettingsSkipsUnusedNaming(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodGet || r.Header.Get("X-Api-Key") != "fixture-key" {
			t.Error("unexpected settings request")
		}
		switch r.URL.Path {
		case "/api/v3/config/downloadclient":
			_, _ = w.Write([]byte(`{"id":1,"enableCompletedDownloadHandling":true}`))
		case "/api/v3/downloadclient":
			_, _ = w.Write([]byte(`[]`))
		default:
			t.Errorf("unused native endpoint called: %s", r.URL.Path)
			http.Error(w, "unexpected request", 500)
		}
	}))
	defer srv.Close()
	got, err := NewArr("Whisparr", srv.URL, "fixture-key").CompanionSettings()
	if err != nil || calls != 2 || !got.Handling || got.RenameMovies != nil || got.NamingError != "" {
		t.Fatalf("unexpected Whisparr settings: %+v, %v, calls=%d", got, err, calls)
	}
}
