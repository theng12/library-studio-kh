package stock

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// qBittorrent reports queue position as "priority": queued torrents carry a positive
// number, completed/non-queued ones an explicit 0, and older builds omit the field
// entirely (or send null). Zero must not collapse into "unknown".
func TestTorrentPriorityPreserved(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/torrents/info" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`[
			{"hash":"q1","name":"Queued","priority":5},
			{"hash":"z1","name":"Done","priority":0},
			{"hash":"m1","name":"Missing"},
			{"hash":"n1","name":"Null","priority":null}
		]`))
	}))
	defer srv.Close()

	items, err := NewQBit(srv.URL, "", "").Torrents()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("want 4 torrents, got %d", len(items))
	}
	if p := items[0].Priority; p == nil || *p != 5 {
		t.Fatalf("positive queue priority lost: %+v", p)
	}
	if p := items[1].Priority; p == nil || *p != 0 {
		t.Fatalf("explicit zero must stay 0, not unknown: %+v", p)
	}
	if p := items[2].Priority; p != nil {
		t.Fatalf("missing field must stay unknown (nil), got %d", *p)
	}
	if p := items[3].Priority; p != nil {
		t.Fatalf("null field must stay unknown (nil), got %d", *p)
	}
}
