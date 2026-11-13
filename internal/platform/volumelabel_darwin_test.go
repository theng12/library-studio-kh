//go:build darwin

package platform

import "testing"

func TestPlistVolumeNameSkipsNonStringValues(t *testing.T) {
	plist := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
	<key>AESHardware</key><true/>
	<key>APFSContainerFree</key><integer>166541152256</integer>
	<key>VolumeName</key><string>Macintosh HD</string>
</dict></plist>`)
	if got := plistVolumeName(plist); got != "Macintosh HD" {
		t.Fatalf("plistVolumeName = %q", got)
	}
	if got := plistVolumeName([]byte("<plist><dict><key>Name</key><string>x</string></dict></plist>")); got != "" {
		t.Fatalf("a missing VolumeName must stay empty, got %q", got)
	}
}
