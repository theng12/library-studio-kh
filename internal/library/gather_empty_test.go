package library

import (
	"encoding/json"
	"testing"
	"time"

	"librarystudio/internal/config"
)

func TestGatherUnconfiguredAppsEncodeAsEmptyArray(t *testing.T) {
	// A fresh host has no configured companions. The dashboard maps apps directly.
	s := Gather(config.Config{}, t.TempDir(), time.Now())
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"apps", "items"} {
		if string(wire[field]) != "[]" {
			t.Fatalf("fresh snapshot %s = %s, want []", field, wire[field])
		}
	}
}
