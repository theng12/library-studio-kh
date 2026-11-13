package main

import "testing"

func TestUIFixturePauseResume(t *testing.T) {
	paused := map[string]bool{}
	u := uiFixture{paused: paused}
	for _, step := range []struct {
		name           string
		stop           bool
		wantC4, wantC3 string
	}{
		{"initial", false, "stalledUP", "missingFiles"},
		{"after stop", true, "stoppedUP", "missingFiles"},
		{"after start", false, "stalledUP", "missingFiles"},
	} {
		// Same map writes the mock's stop/start handler makes from the hashes form value.
		paused[uiHash("c4")] = step.stop
		paused[uiHash("c3")] = step.stop
		rows := u.torrents()
		for hash, want := range map[string]string{"c4": step.wantC4, "c3": step.wantC3} {
			got := ""
			for _, row := range rows {
				if row["hash"] == uiHash(hash) {
					got = row["state"].(string)
				}
			}
			if got != want {
				t.Errorf("%s: %s state = %q, want %q", step.name, hash, got, want)
			}
		}
	}
}
