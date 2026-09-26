package master

import (
	"crypto/subtle"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strings"

	"golang.zx2c4.com/wireguard/meshcfg"
)

//go:embed ui/*
var uiFS embed.FS

// Server is the Master control-plane HTTP API + embedded UI.
type Server struct {
	cfg      meshcfg.MasterConfig
	store    *Store
	sessions *sessionStore
}

func NewServer(cfg meshcfg.MasterConfig) (*Server, error) {
	if cfg.Listen == "" {
		cfg.Listen = ":8443"
	}
	if cfg.DataDir == "" {
		cfg.DataDir = "/var/lib/wireguard-mc"
	}
	if cfg.AdminPassword == "" {
		return nil, fmt.Errorf("adminPassword is required in %s", meshcfg.MasterFileName)
	}
	st, err := OpenStore(cfg.DataDir, cfg.EnrollToken, cfg.VPNSubnet)
	if err != nil {
		return nil, err
	}
	return &Server{
		cfg:      cfg,
		store:    st,
		sessions: newSessionStore(cfg.AdminPassword),
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/mesh", s.handleMesh)
	mux.HandleFunc("/api/nodes", s.handleNodes)
	mux.HandleFunc("/api/agent/enroll", s.handleAgentEnroll)
	mux.HandleFunc("/api/agent/config", s.handleAgentConfig)
	mux.HandleFunc("/api/meta", s.handleMeta)

	sub, err := fs.Sub(uiFS, "ui")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(sub, path); err != nil {
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
	return mux
}

func (s *Server) ListenAndServe() error {
	h := s.Handler()
	fmt.Fprintf(os.Stderr, "wireguard-go master: listening on %s (data %s)\n", s.cfg.Listen, s.cfg.DataDir)
	if s.cfg.TLSCert != "" && s.cfg.TLSKey != "" {
		return http.ListenAndServeTLS(s.cfg.Listen, s.cfg.TLSCert, s.cfg.TLSKey, h)
	}
	return http.ListenAndServe(s.cfg.Listen, h)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "revision": s.store.Snapshot().Revision})
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	settings, err := s.store.Settings()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enrollToken": settings.EnrollToken,
		"vpnSubnet":   settings.VPNSubnet,
		"listen":      s.cfg.Listen,
		"defaultIface": settings.DefaultIface,
		"defaultPoll":  settings.DefaultPoll,
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	tok, ok := s.sessions.login(body.Password)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid password"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": tok})
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if !s.sessions.valid(bearerToken(r)) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return false
	}
	return true
}

func (s *Server) handleMesh(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.requireAdmin(w, r) {
			return
		}
		writeJSON(w, http.StatusOK, s.store.Snapshot())
	case http.MethodPut:
		if !s.requireAdmin(w, r) {
			return
		}
		var m meshcfg.Mesh
		if err := readJSON(r, &m); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := s.store.PutMesh(m); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, s.store.Snapshot())
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodDelete:
		if !s.requireAdmin(w, r) {
			return
		}
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id query required"})
			return
		}
		if err := s.store.DeleteNode(id); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, s.store.Snapshot())
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
			"error": "nodes are created by agent enroll only; use DELETE ?id=",
		})
	}
}

func (s *Server) handleAgentEnroll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		EnrollToken string `json:"enrollToken"`
		Name        string `json:"name,omitempty"`
		Role        string `json:"role,omitempty"`
		Endpoint    string `json:"endpoint,omitempty"`
		ListenPort  uint16 `json:"listenPort,omitempty"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if subtle.ConstantTimeCompare([]byte(body.EnrollToken), []byte(mustEnroll(s))) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid enrollToken"})
		return
	}
	created, err := s.store.Enroll(body.Name, body.Role, body.Endpoint, body.ListenPort)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"nodeId":    created.ID,
		"nodeToken": created.Token,
		"role":      created.Role,
		"address":   created.Address,
		"name":      created.Name,
	})
}

func mustEnroll(s *Server) string {
	settings, err := s.store.Settings()
	if err != nil {
		return s.cfg.EnrollToken
	}
	if settings.EnrollToken != "" {
		return settings.EnrollToken
	}
	return s.cfg.EnrollToken
}

func (s *Server) handleAgentConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tok := bearerToken(r)
	if tok == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing bearer token"})
		return
	}
	desired, err := s.store.DesiredForToken(tok)
	if err != nil {
		if err == errUnauthorized {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	etag := fmt.Sprintf(`"%d"`, desired.Revision)
	w.Header().Set("ETag", etag)
	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, fmt.Sprintf("%d", desired.Revision)) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, http.StatusOK, desired)
}
