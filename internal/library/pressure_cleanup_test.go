package library

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPressureBypassClearsOldestBeforeRetentionAndStopsAtFloor(t *testing.T) {
	h := newHarness(t)
	older := h.add("Older", h.clock.Add(-48*time.Hour))
	newer := h.add("Newer", *h.clock)
	h.src.torrents[0].State, h.src.torrents[0].Ratio, h.src.torrents[0].SeedingTime = "uploading", 0, 1
	h.cfg.PressureFreeGB = 200
	h.cfg.PressureBypassRetention = true
	h.freeFn = func() int64 {
		if !exists(older.root) {
			return 205_000_000_000
		}
		return 190_000_000_000
	}
	result := h.run()
	if result.Purged != 1 || phaseOf(t, result, older.hash) != phaseRemoved || !exists(newer.src) {
		t.Fatalf("oldest only must be removed before normal retention: %+v", result)
	}
	if h.src.stopCalls != 1 || len(h.src.metadataCalls) != 1 || h.src.metadataCalls[0] != older.hash {
		t.Fatalf("release completed torrent safely first: %+v", h.src)
	}
	if !exists(filepath.Join(older.lib, "renamed-by-app.mkv")) {
		t.Fatal("NAS copy must remain")
	}
	if !result.Status.PressureBypassRetention {
		t.Fatal("the active override must be visible")
	}
}

func TestPressureBypassIsExplicit(t *testing.T) {
	h := newHarness(t)
	item := h.add("Recent", *h.clock)
	h.cfg.PressureFreeGB = 200
	h.freeFn = func() int64 { return 10_000_000_000 }
	result := h.run()
	if result.Purged != 0 || !exists(item.src) || phaseOf(t, result, item.hash) != phaseStaging {
		t.Fatal("existing installations must not silently bypass waits")
	}
}

func TestFreshNASProofRequiredForPressureAndExpiry(t *testing.T) {
	for _, stage := range []string{phaseStaging, phaseManaged, phaseNative} {
		for _, damage := range []string{"missing", "changed"} {
			t.Run(stage+"/"+damage, func(t *testing.T) {
				h := newHarness(t)
				item := h.add("Protected", *h.clock)
				start := *h.clock
				h.run()
				if stage == phaseManaged || stage == phaseNative {
					h.clockNow(start.Add(3 * 24 * time.Hour))
					h.run()
				}
				if stage == phaseNative {
					h.clockNow(start.Add(7 * 24 * time.Hour))
					h.run()
				}
				file := filepath.Join(item.lib, "renamed-by-app.mkv")
				if damage == "missing" {
					if err := os.Rename(file, file+".held"); err != nil {
						t.Fatal(err)
					}
					if err := os.Rename(item.lib, item.lib+".offline"); err != nil {
						t.Fatal(err)
					}
				} else {
					data, err := os.ReadFile(file)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(file, bytes.Repeat([]byte("x"), len(data)), 0600); err != nil {
						t.Fatal(err)
					}
				}
				h.cfg.PressureFreeGB = 200
				h.cfg.PressureBypassRetention = true
				h.freeFn = func() int64 { return 1 }
				result := h.run()
				if result.Purged != 0 || phaseOf(t, result, item.hash) == phaseRemoved {
					t.Fatalf("unverified NAS must prevent permanent removal: %+v", result)
				}
				if !strings.Contains(strings.Join(result.Problems, " "), "library copy") {
					t.Fatalf("show the NAS blocker: %v", result.Problems)
				}
			})
		}
	}
	t.Run("expiry", func(t *testing.T) {
		h := newHarness(t)
		item := h.add("Expiry", *h.clock)
		start := *h.clock
		h.run()
		h.clockNow(start.Add(3 * 24 * time.Hour))
		h.run()
		h.clockNow(start.Add(7 * 24 * time.Hour))
		h.run()
		if err := os.Rename(item.lib, item.lib+".offline"); err != nil {
			t.Fatal(err)
		}
		h.clockNow(start.Add(37 * 24 * time.Hour))
		result := h.run()
		if result.Purged != 0 || phaseOf(t, result, item.hash) != phaseNative {
			t.Fatal("30-day expiry also requires fresh NAS proof")
		}
	})
}

func TestPressureBypassProtectsQueuedAndIncomplete(t *testing.T) {
	for _, mode := range []string{"queued", "incomplete", "checking"} {
		t.Run(mode, func(t *testing.T) {
			h := newHarness(t)
			item := h.add("Busy", *h.clock)
			h.run()
			switch mode {
			case "queued":
				h.src.queued[item.hash] = "Radarr"
			case "incomplete":
				h.src.torrents[0].Progress = .5
			case "checking":
				h.src.torrents[0].State = "checkingUP"
			}
			h.cfg.PressureFreeGB = 200
			h.cfg.PressureBypassRetention = true
			h.freeFn = func() int64 { return 1 }
			result := h.run()
			if result.Purged != 0 || !exists(item.src) || len(h.src.metadataCalls) != 0 {
				t.Fatal("active or queued data must remain protected")
			}
		})
	}
}

func TestPressureBypassPreviewDoesNotChangeFilesOrTorrent(t *testing.T) {
	h := newHarness(t)
	item := h.add("Preview", *h.clock)
	h.run()
	h.cfg.PressureFreeGB = 200
	h.cfg.PressureBypassRetention = true
	h.freeFn = func() int64 { return 1 }
	result, err := h.engine().Preview(h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !exists(item.src) || h.src.stopCalls != 0 || len(h.src.metadataCalls) != 0 || result.Purged != 0 {
		t.Fatal("preview must remain read-only")
	}
	if !strings.Contains(strings.Join(result.Actions, " "), "skip retention waits") {
		t.Fatalf("preview must explain pressure override: %v", result.Actions)
	}
}

func TestFileSnapshotRejectsChangedOrReplacedContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !sameFileSnapshot(before, before) {
		t.Fatal("unchanged regular file should match")
	}
	if err := os.WriteFile(path, []byte("changed-size"), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if sameFileSnapshot(before, after) {
		t.Fatal("mutation must invalidate checksum snapshot")
	}
}

type pendingMetadataSource struct{ *fakeSource }

func (s pendingMetadataSource) RemoveMetadata(hash string) error {
	s.metadataCalls = append(s.metadataCalls, hash)
	return nil
}

func TestPressureWaitsForNativeMetadataRemoval(t *testing.T) {
	h := newHarness(t)
	item := h.add("Pending", *h.clock)
	h.run()
	h.cfg.PressureFreeGB = 200
	h.cfg.PressureBypassRetention = true
	h.freeFn = func() int64 { return 1 }
	engine := h.engine()
	engine.Source = pendingMetadataSource{h.src}
	for attempt := 0; attempt < 2; attempt++ {
		result, err := engine.Run(h.cfg)
		if err != nil {
			t.Fatal(err)
		}
		if result.Purged != 0 || !exists(item.src) {
			t.Fatal("an accepted delete request is not confirmed native removal")
		}
	}
}
