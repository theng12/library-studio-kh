package library

import (
	"encoding/json"
	"testing"
)

func TestActivityZeroProgressIsReported(t *testing.T) {
	data, err := json.Marshal(Item{Kind: "downloading", Progress: 0})
	if err != nil {
		t.Fatal(err)
	}
	var item map[string]json.RawMessage
	if err := json.Unmarshal(data, &item); err != nil {
		t.Fatal(err)
	}
	if string(item["progress"]) != "0" {
		t.Fatalf("zero download progress must remain present, got %s", data)
	}
}
