package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"librarystudio/internal/companioninstall"
)

type optionalPackages struct {
	fakePackages
	result companioninstall.DetectResult
	err    error
}

func (f *optionalPackages) Detect(context.Context, companioninstall.AppID, string) (companioninstall.DetectResult, error) {
	return f.result, f.err
}

func TestOptionalActionChecksIdentityBeforeExecuting(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state companioninstall.DetectState
		path  string
		err   error
	}{
		{"refused identity", companioninstall.DetectKeep, "recyclarr", nil},
		{"missing identity", companioninstall.DetectMissing, "recyclarr", nil},
		{"changed path", companioninstall.DetectAdopt, "replacement", nil},
		{"detection failure", companioninstall.DetectAdopt, "recyclarr", errors.New("fixture failure")},
		{"accepted identity", companioninstall.DetectAdopt, "recyclarr", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := managerFixture(t)
			cfg := filepath.Join(m.home, "recyclarr.yml")
			if err := os.WriteFile(cfg, []byte("# fixture\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			m.recyclarrCfgPath = func() (string, error) { return cfg, nil }
			m.job.View.State = "ready"
			m.job.View.Apps = []AppStep{{ID: companioninstall.Recyclarr, Name: "Recyclarr", State: "configured", Path: filepath.Join(m.home, "recyclarr")}}
			m.installer = &optionalPackages{result: companioninstall.DetectResult{State: tc.state, Path: filepath.Join(m.home, tc.path)}, err: tc.err}
			calls := 0
			m.runTool = func(_ context.Context, _ string, args ...string) ([]byte, error) {
				calls++
				if strings.Join(args, "|") != "sync|-c|"+cfg {
					t.Fatalf("apply arguments: %v", args)
				}
				return []byte("fixture sync complete"), nil
			}
			_, err := m.OptionalAction(context.Background(), m.job.View.ID, companioninstall.Recyclarr, "apply", true)
			accepted := tc.name == "accepted identity"
			if accepted && (err != nil || calls != 1) {
				t.Fatalf("accepted: calls=%d err=%v", calls, err)
			}
			if !accepted && (err == nil || calls != 0) {
				t.Fatalf("refused: calls=%d err=%v", calls, err)
			}
			if m.busy {
				t.Fatal("busy reservation was not released")
			}
		})
	}
}
