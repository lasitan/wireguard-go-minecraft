package api

import (
	"net/http"
	"strings"

	"golang.zx2c4.com/wireguard/src/core"
)

// handleAgentInstallCommands returns one-line install+enroll commands for a
// machine that has never run lasitan-cluster (admin only).
func (s *Server) handleAgentInstallCommands(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	role := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("role")))
	if role == "" {
		role = core.RoleClient
	}
	if role != core.RoleClient && role != core.RoleServer {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role must be client or server"})
		return
	}
	endpoint := strings.TrimSpace(r.URL.Query().Get("endpoint"))

	settings, err := s.store.Settings()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	masterURL := masterPublicURL(r)
	cmds := buildAgentInstallCommands(masterURL, settings.EnrollToken, role, endpoint)
	writeJSON(w, http.StatusOK, cmds)
}
