//go:build darwin

package platform

import (
	"bytes"
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

// plistVolumeName pulls VolumeName out of diskutil's plist output. The dict
// mixes strings, booleans and integers, so walk key/string pairs in order
// instead of decoding the whole dict into one shape.
func plistVolumeName(b []byte) string {
	dec := xml.NewDecoder(bytes.NewReader(b))
	lastKey := ""
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "key":
			var k string
			if dec.DecodeElement(&k, &start) != nil {
				return ""
			}
			lastKey = strings.TrimSpace(k)
		case "string":
			var v string
			if dec.DecodeElement(&v, &start) != nil {
				return ""
			}
			if lastKey == "VolumeName" {
				return strings.TrimSpace(v)
			}
			lastKey = ""
		}
	}
}
