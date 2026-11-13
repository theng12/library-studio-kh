package server

import (
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"librarystudio/internal/config"
)

func TestRemoteGuard(t *testing.T) {
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := New(store, t.TempDir())
	if s.RemoteStatus().On || store.Get().TailscaleOn {
		t.Fatal("remote access must start off")
	}
	const address = "100.88.1.2:7882"
	const magicName = "terranash-1301-m1-16-1000.tail093f2c.ts.net"
	h := s.remoteHandler(address, []string{magicName})
	for _, tc := range []struct {
		name, peer, host, token string
		want                    int
	}{
		{"phone with key", "100.90.1.4:1234", address, s.Token, 200},
		{"phone by MagicDNS name", "100.90.1.4:1234", magicName + ":7882", s.Token, 200},
		{"MagicDNS name wrong port", "100.90.1.4:1234", magicName + ":7883", s.Token, 403},
		{"missing key", "100.90.1.4:1234", address, "", 403},
		{"wrong key", "100.90.1.4:1234", address, "wrong", 403},
		{"LAN peer", "192.168.1.4:1234", address, s.Token, 403},
		{"public peer", "8.8.8.8:1234", address, s.Token, 403},
		{"foreign host", "100.90.1.4:1234", "evil.example:7882", s.Token, 403},
		{"wrong port", "100.90.1.4:1234", "100.88.1.2:7883", s.Token, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "http://"+tc.host+"/api/state", nil)
			r.RemoteAddr = tc.peer
			r.Header.Set("X-Library-Token", tc.token)
			r.Header.Set("X-Forwarded-For", "100.90.1.4")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("got %d want %d", w.Code, tc.want)
			}
		})
	}
	// A Tailnet-looking Host must still be refused on the local listener.
	r := httptest.NewRequest("GET", "http://"+address+"/api/state", nil)
	r.Header.Set("X-Library-Token", s.Token)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("local listener accepted foreign Host")
	}
	if err := s.ConfigureRemote(); err != nil || s.RemoteStatus().On {
		t.Fatal("off must not start a listener")
	}
}

func TestTailAddressAndPortValidation(t *testing.T) {
	for _, tc := range []struct {
		ip   string
		want bool
	}{
		{"100.64.0.1", true}, {"100.127.255.254", true}, {"fd7a:115c:a1e0::1", true},
		{"100.128.0.1", false}, {"192.168.1.1", false}, {"127.0.0.1", false}, {"::1", false},
	} {
		if tailAddress(netip.MustParseAddr(tc.ip)) != tc.want {
			t.Error(tc.ip)
		}
	}
	c := config.Default()
	c.TailscaleOn = true
	for _, port := range []int{-1, 0, 1023, 65536} {
		c.TailscalePort = port
		if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "Tailscale port") {
			t.Errorf("accepted %d", port)
		}
	}
	c.TailscalePort = 7882
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}
