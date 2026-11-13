//go:build darwin

package platform

import (
	"encoding/xml"
	"os/exec"
	"strings"
	"sync"
)

var (
	bootVolumeOnce  sync.Once
	bootVolumeLabel string
)

// LoadVolumeLabel reads this Mac's startup volume name once, at startup, so
// pages can name the real disk without running a command on a request path.
func LoadVolumeLabel() {
	bootVolumeOnce.Do(func() {
		out, err := exec.Command("diskutil", "info", "-plist", "/").Output()
		if err != nil {
			return
		}
		bootVolumeLabel = plistVolumeName(out)
	})
}

// VolumeLabel names the volume a path lives on when the OS reports a label;
// empty when the path is on a mounted disk the caller already knows by name.
func VolumeLabel(path string) string {
	if strings.TrimSpace(path) != "/" {
		return ""
	}
	bootVolumeOnce.Do(func() {}) // use whatever startup loaded; never block here
	return bootVolumeLabel
}

// plistVolumeName pulls VolumeName out of diskutil's plist output.
func plistVolumeName(b []byte) string {
	var doc struct {
		Dict []struct {
			Key   string `xml:"key"`
			Value string `xml:"string"`
		} `xml:"dict"`
	}
	if xml.Unmarshal(b, &doc) != nil {
		return ""
	}
	for _, kv := range doc.Dict {
		if kv.Key == "VolumeName" {
			return strings.TrimSpace(kv.Value)
		}
	}
	return ""
}
