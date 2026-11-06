package stock

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRemovalAlreadyOffIsIdempotentButUnknownIsNotSuccess(t *testing.T) {
	for _, known := range []bool{true, false} {
		t.Run(map[bool]string{true: "already off", false: "not reported"}[known], func(t *testing.T) {
			writes := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					writes++
					t.Error("no setting should be written")
					w.WriteHeader(500)
					return
				}
				client := map[string]any{"id": 9, "implementation": "QBittorrent"}
				if known {
					client["removeCompletedDownloads"] = false
				}
				json.NewEncoder(w).Encode([]any{client})
			}))
			defer s.Close()
			err := NewArr("Sonarr", s.URL, "fixture").DisableRemoveCompletedDownloads(9)
			if (err == nil) != known || writes != 0 {
				t.Fatal("already-off and unknown removal settings were confused")
			}
		})
	}
}
