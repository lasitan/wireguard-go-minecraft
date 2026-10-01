package api

import (
	"net/http"
)

const (
	errBanned         = "ip permanently banned due to too many failed logins"
	errInvalidPass    = "invalid password"
	errUnauthorized   = "unauthorized"
	errWebAuthnFailed = "webauthn authentication failed"
)

// authGate rejects permanently banned clients. Returns false if the response was written.
func (s *Server) authGate(w http.ResponseWriter, r *http.Request) (ip string, ok bool) {
	ip = clientIP(r)
	// Never enforce bans against Cloudflare edges / private peers — those
	// addresses are shared and must not lock out unrelated clients.
	if !isBanableAuthIP(ip) {
		return ip, true
	}
	banned, err := s.store.IsAuthBanned(ip)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "auth store error"})
		return ip, false
	}
	if banned {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": errBanned})
		return ip, false
	}
	return ip, true
}

// recordLoginFailure increments the failure counter and may permanently ban the IP.
// Cloudflare / private / loopback addresses are never banned (and not counted),
// so a missing CF-Connecting-IP under Pseudo IPv4 overwrite cannot lock the edge.
func (s *Server) recordLoginFailure(w http.ResponseWriter, ip string, failMsg string) {
	if !isBanableAuthIP(ip) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": failMsg})
		return
	}
	banned, _, err := s.store.RecordAuthFailure(ip)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "auth store error"})
		return
	}
	if banned {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": errBanned})
		return
	}
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": failMsg})
}

func (s *Server) clearLoginFailures(ip string) {
	_ = s.store.ClearAuthFailures(ip)
}
