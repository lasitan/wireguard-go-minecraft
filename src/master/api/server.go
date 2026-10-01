package api

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/src/master/geoip"
	"golang.zx2c4.com/wireguard/src/master/hub"
	"golang.zx2c4.com/wireguard/src/master/pathsel"
	"golang.zx2c4.com/wireguard/src/master/relay"
	"golang.zx2c4.com/wireguard/src/master/stats"
	"golang.zx2c4.com/wireguard/src/master/store"
	"golang.zx2c4.com/wireguard/src/master/ui"

	"golang.zx2c4.com/wireguard/src/core"
	"golang.zx2c4.com/wireguard/src/core/config"
	"golang.zx2c4.com/wireguard/src/update"
)

// Server is the Master control-plane HTTP API + embedded UI.
type Server struct {
	cfg      config.MasterConfig
	store    *store.Store
	sessions *sessionStore
	waSessions *waCeremonyStore
	stats    *stats.StatsService
	hub      *hub.Hub
	geo      *geoip.GeoIP
	updates  *update.Checker
	paths    *pathsel.Selector
	relay    *relay.Service
	ui       *uiHub

	httpGeoAt sync.Map // nodeID -> time.Time of last legacy-agent IP observation
}

func NewServer(cfg config.MasterConfig) (*Server, error) {
	if cfg.Listen == "" {
		cfg.Listen = ":8443"
	}
	switch {
	case cfg.DataDir == "" && runtime.GOOS == "windows":
		cfg.DataDir = filepath.Join(config.ConfDir(), "master-data")
	case cfg.DataDir == "":
		cfg.DataDir = "/var/lib/lasitan-cluster"
	case !filepath.IsAbs(cfg.DataDir):
		cfg.DataDir = filepath.Join(config.ConfDir(), cfg.DataDir)
	}
	if cfg.AdminPassword == "" {
		return nil, fmt.Errorf("adminPassword is required in %s", config.MasterFileName)
	}
	st, err := store.OpenStore(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	statsSvc := stats.NewStatsService(st)
	agentHub := hub.NewHub(st, statsSvc)
	geo := geoip.NewGeoIP(st, cfg.DataDir, cfg.GeoIPDB, !cfg.DisableGeoIPOnline)
	agentHub.OnPublicIPs = geo.ResolveNode
	relaySvc := &relay.Service{Store: st}
	s := &Server{
		cfg:        cfg,
		store:      st,
		sessions:   newSessionStore(cfg.AdminPassword, st),
		waSessions: newWACeremonyStore(),
		stats:      statsSvc,
		hub:        agentHub,
		geo:        geo,
		updates:    update.NewChecker(6*time.Hour, cfg.DisableUpdateCheck),
		relay:      relaySvc,
	}
	s.ui = newUIHub(s)
	s.paths = &pathsel.Selector{Store: st, Stats: statsSvc, Alive: agentHub.IsConnected, Push: func() {
		agentHub.PushAll()
		relaySvc.Sync()
		s.ui.meshChanged()
	}}
	agentHub.OnConnChange = s.ui.presenceChanged
	s.updates.OnChange = s.ui.versionChanged
	return s, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/webauthn/login/begin", s.handleWebAuthnLoginBegin)
	mux.HandleFunc("/api/webauthn/login/finish", s.handleWebAuthnLoginFinish)
	mux.HandleFunc("/api/webauthn/register/begin", s.handleWebAuthnRegisterBegin)
	mux.HandleFunc("/api/webauthn/register/finish", s.handleWebAuthnRegisterFinish)
	mux.HandleFunc("/api/webauthn/credentials", s.handleWebAuthnCredentials)
	mux.HandleFunc("/api/webauthn/assert/begin", s.handleWebAuthnAssertBegin)
	mux.HandleFunc("/api/mesh", s.handleMesh)
	mux.HandleFunc("/api/nodes", s.handleNodes)
	mux.HandleFunc("/api/nodes/swap", s.handleNodesSwap)
	mux.HandleFunc("/api/nodes/reassign-subnet", s.handleNodesReassignSubnet)
	mux.HandleFunc("/api/nodes/forwards", s.handleNodeForwards)
	mux.HandleFunc("/api/nodes/stats", s.handleNodeStats)
	mux.HandleFunc("/api/nodes/traffic", s.handleNodeTraffic)
	mux.HandleFunc("/api/agent/enroll", s.handleAgentEnroll)
	mux.HandleFunc("/api/agent/config", s.handleAgentConfig)
	mux.HandleFunc("/api/agent/whoami", s.handleAgentWhoami)
	mux.HandleFunc("/api/agent/ws", s.hub.ServeWS)
	mux.HandleFunc("/api/ui/ws", s.ui.serveWS)
	mux.HandleFunc("/api/meta", s.handleMeta)
	mux.HandleFunc("/api/version", s.handleVersion)
	mux.HandleFunc("/api/version/upgrade", s.handleUpgrade)
	mux.HandleFunc("/api/install/agent", s.handleAgentInstallCommands)

	sub := ui.FS()
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

// pushAll re-pushes every agent after a mesh edit and re-evaluates gateways
// right away (a changed choice pushes once more).
func (s *Server) pushAll() {
	s.hub.PushAll()
	s.relay.Sync()
	s.paths.Tick(time.Now())
	s.ui.meshChanged()
}

func (s *Server) ListenAndServe() error {
	h := s.Handler()
	stop := make(chan struct{})
	defer close(stop)
	go s.stats.Run(stop)
	go s.updates.Run(stop)
	go s.paths.Run(stop)
	go s.relay.Run(stop)
	go s.ui.run(stop)
	fmt.Fprintf(os.Stderr, "lasitan-cluster master: listening on %s (data %s)\n", s.cfg.Listen, s.cfg.DataDir)
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
	if r.Method != http.MethodGet && r.Method != http.MethodPatch {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	if r.Method == http.MethodPatch {
		var p store.SettingsPatch
		if err := readJSON(r, &p); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		if err := s.store.UpdateSettings(p); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		s.pushAll()
		s.ui.metaChanged()
	}
	meta, err := s.metaView()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

func (s *Server) metaView() (map[string]any, error) {
	settings, err := s.store.Settings()
	if err != nil {
		return nil, err
	}
	var transport any
	if len(settings.TransportJSON) > 0 {
		_ = json.Unmarshal(settings.TransportJSON, &transport)
	}
	return map[string]any{
		"enrollToken":  settings.EnrollToken,
		"vpnSubnet":    settings.VPNSubnet,
		"listen":       s.cfg.Listen,
		"defaultIface": settings.DefaultIface,
		"defaultPoll":  settings.DefaultPoll,
		"transport":    transport,
		"relayPort":    settings.RelayPort,
	}, nil
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ip, ok := s.authGate(w, r)
	if !ok {
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
		s.recordLoginFailure(w, ip, errInvalidPass)
		return
	}
	s.clearLoginFailures(ip)
	writeJSON(w, http.StatusOK, map[string]string{"token": tok})
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if !s.sessions.valid(bearerToken(r)) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": errUnauthorized})
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
		writeJSON(w, http.StatusOK, s.store.Snapshot().WithoutPrivateKeys())
	case http.MethodPut:
		if !s.requireAdmin(w, r) {
			return
		}
		var m core.Mesh
		if err := readJSON(r, &m); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := s.store.PutMesh(m); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		s.pushAll()
		writeJSON(w, http.StatusOK, s.store.Snapshot().WithoutPrivateKeys())
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
		s.hub.Disconnect(id)
		s.stats.Forget(id)
		s.pushAll()
		writeJSON(w, http.StatusOK, s.store.Snapshot().WithoutPrivateKeys())
	case http.MethodPatch:
		if !s.requireAdmin(w, r) {
			return
		}
		s.handlePatchNode(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
			"error": "nodes are created by agent enroll only; use PATCH or DELETE ?id=",
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
	if body.Role == core.RoleServer && body.Endpoint == "" {
		if host := enrollSourceHost(r); host != "" {
			port := body.ListenPort
			if port == 0 {
				port = store.DefaultServerListenPort
			}
			body.Endpoint = net.JoinHostPort(host, strconv.Itoa(int(port)))
		}
	}
	created, err := s.store.Enroll(body.Name, body.Role, body.Endpoint, body.ListenPort)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.pushAll()
	writeJSON(w, http.StatusCreated, map[string]any{
		"nodeId":    created.ID,
		"nodeToken": created.Token,
		"role":      created.Role,
		"address":   created.Address,
		"name":      created.Name,
	})
}

// mustEnroll returns the current enroll token, or a value no agent can send on error.
func mustEnroll(s *Server) string {
	settings, err := s.store.Settings()
	if err != nil || settings.EnrollToken == "" {
		return "\x00"
	}
	return settings.EnrollToken
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
		if err == store.ErrUnauthorized {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.observeHTTPAgent(tok, r.RemoteAddr)
	etag := fmt.Sprintf(`"%d"`, desired.Revision)
	w.Header().Set("ETag", etag)
	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, fmt.Sprintf("%d", desired.Revision)) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, http.StatusOK, desired)
}
