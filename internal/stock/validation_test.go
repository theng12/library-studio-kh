package stock

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNullMediaIsRejectedBeforeUpstream(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; http.Error(w, "unexpected", 500) }))
	defer upstream.Close()
	a := NewArr("Radarr", upstream.URL, "fixture-key")
	if _, err := a.Add(json.RawMessage("null")); err == nil {
		t.Fatal("legacy add accepted null")
	}
	if _, err := a.AddSelected(json.RawMessage("null"), AddSelection{}); err == nil {
		t.Fatal("selected add accepted null")
	}
	if calls != 0 {
		t.Fatal("invalid input contacted upstream")
	}
}
