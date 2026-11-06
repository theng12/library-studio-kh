package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"

	librarystudio "librarystudio"
)

const hostProofDomain = "library-studio-kh host proof v1\x00"

// health is registered by the lead at GET /api/health. The existing guard keeps it
// authenticated, and the body carries no settings or credentials.
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
	}{true, librarystudio.Version()})
}

// HostProof proves a host owns the bootstrap token without the token crossing
// the socket: the caller supplies a fresh challenge and checks the HMAC itself.
func HostProof(token, challenge string) string {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte(hostProofDomain + challenge))
	return hex.EncodeToString(mac.Sum(nil))
}

// hostProof is public only on the loopback listener; the guard permits the route
// and this handler refuses any peer that is not on this computer.
func (s *Server) hostProof(w http.ResponseWriter, r *http.Request) {
	if !loopbackPeer(r) {
		writeJSON(w, 403, map[string]string{"error": "Host proof is only available on this computer."})
		return
	}
	challenge := r.URL.Query().Get("challenge")
	if len(challenge) != 64 {
		writeJSON(w, 400, map[string]string{"error": "A fresh 64-character challenge is required."})
		return
	}
	if _, err := hex.DecodeString(challenge); err != nil {
		writeJSON(w, 400, map[string]string{"error": "A fresh 64-character challenge is required."})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "version": librarystudio.Version(), "proof": HostProof(s.Token, challenge)})
}
