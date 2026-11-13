package library

import "testing"

func TestItemStatusNeverInventsProof(t *testing.T) {
	for _, tc := range []struct {
		kind, state, want string
	}{
		{"downloading", "downloading", "downloading"},
		{"downloading", "queuedDL", "queued"},
		{"downloading", "stalledDL", "stalled"},
		{"downloading", "missingFiles", "missing"},
		{"waiting", "uploading", "importing"},
		{"imported", "stalledUP", "copied"},
		{"removed", "stalledUP", "moved"},
		{"unmanaged", "uploading", "on-disk"},
		{"upcoming", "", "upcoming"},
	} {
		if got := itemStatus(tc.kind, tc.state); got != tc.want {
			t.Fatalf("itemStatus(%q, %q) = %q, want %q", tc.kind, tc.state, got, tc.want)
		}
	}
}
