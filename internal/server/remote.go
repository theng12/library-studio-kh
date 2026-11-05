package server

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"
)

var tailIPv4 = netip.MustParsePrefix("100.64.0.0/10")
var tailIPv6 = netip.MustParsePrefix("fd7a:115c:a1e0::/48")

func tailAddress(ip netip.Addr) bool {
	return tailIPv4.Contains(ip.Unmap()) || tailIPv6.Contains(ip)
}

type remoteAccess struct {
	mu      sync.Mutex
	server  *http.Server
	address string
	error   string
}

type RemoteStatus struct {
	URL   string `json:"url,omitempty"`
	Error string `json:"error,omitempty"`
	On    bool   `json:"on"`
}

// ConfigureRemote binds only an address assigned to a local Tailscale interface.
// No wildcard/LAN listener, proxy headers, or changes to Tailscale's own settings.
func (s *Server) ConfigureRemote() error {
	s.remote.mu.Lock()
	defer s.remote.mu.Unlock()
	c := s.Store.Get()
	if !c.TailscaleOn {
		s.closeRemote()
		s.remote.error = ""
		return nil
	}
	addrs, err := net.InterfaceAddrs()
	var ip netip.Addr
	if err == nil {
		for _, a := range addrs {
			p, e := netip.ParsePrefix(a.String())
			if e == nil && tailAddress(p.Addr()) {
				ip = p.Addr()
				if ip.Is4() {
					break
				}
			}
		}
	}
	if !ip.IsValid() {
		s.closeRemote()
		s.remote.error = "No Tailscale address found. Connect this computer to Tailscale, then save Settings again."
		return fmt.Errorf("%s", s.remote.error)
	}
	address := net.JoinHostPort(ip.String(), strconv.Itoa(c.TailscalePort))
	if s.remote.server != nil && s.remote.address == address {
		return nil
	}
	s.closeRemote()
	ln, err := net.Listen("tcp", address)
	if err != nil {
		s.remote.error = fmt.Sprintf("Couldn't open Tailscale port %d: %v. Choose another port and save again.", c.TailscalePort, err)
		return fmt.Errorf("%s", s.remote.error)
	}
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: s.remoteHandler(address)}
	s.remote.server, s.remote.address, s.remote.error = srv, address, ""
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.remote.mu.Lock()
			defer s.remote.mu.Unlock()
			if s.remote.server == srv {
				s.remote.server = nil
				s.remote.error = "Tailscale access stopped: " + err.Error()
			}
		}
	}()
	return nil
}

func (s *Server) remoteHandler(address string) http.Handler {
	h := s.Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer, _, err := net.SplitHostPort(r.RemoteAddr)
		ip, parseErr := netip.ParseAddr(peer)
		// Never trust proxy headers: the socket peer must be on Tailscale.
		if err != nil || parseErr != nil || !tailAddress(ip) || r.Host != address {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		// Only this listener may reach the localhost guard this way; API token checks still apply.
		r2 := r.Clone(r.Context())
		r2.Host = "127.0.0.1"
		h.ServeHTTP(w, r2)
	})
}

// Caller holds remote.mu. Close also disconnects existing phone sessions immediately.
func (s *Server) closeRemote() {
	if s.remote.server != nil {
		_ = s.remote.server.Close()
	}
	s.remote.server, s.remote.address = nil, ""
}

func (s *Server) RemoteStatus() RemoteStatus {
	s.remote.mu.Lock()
	defer s.remote.mu.Unlock()
	r := RemoteStatus{On: s.remote.server != nil, Error: s.remote.error}
	if r.On {
		r.URL = "http://" + s.remote.address + "/#" + s.Token
	}
	return r
}
