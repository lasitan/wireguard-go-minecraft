package api

import (
	"net/http"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/src/master/stats"
	"golang.zx2c4.com/wireguard/src/master/store"

	"golang.zx2c4.com/wireguard/src/core"
	"golang.zx2c4.com/wireguard/src/core/wire"
)

func nodeIDParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id query required"})
		return "", false
	}
	return id, true
}

func (s *Server) handleNodesSwap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	var body struct {
		A string `json:"a"`
		B string `json:"b"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if err := s.store.SwapNodeAddresses(strings.TrimSpace(body.A), strings.TrimSpace(body.B)); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.pushAll()
	writeJSON(w, http.StatusOK, s.store.Snapshot().WithoutPrivateKeys())
}

func (s *Server) handleNodesReassignSubnet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	var body struct {
		ID     string `json:"id"`
		Prefix string `json:"prefix"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if err := s.store.ReassignNodeSubnet(strings.TrimSpace(body.ID), strings.TrimSpace(body.Prefix)); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.pushAll()
	writeJSON(w, http.StatusOK, s.store.Snapshot().WithoutPrivateKeys())
}

func (s *Server) handlePatchNode(w http.ResponseWriter, r *http.Request) {
	id, ok := nodeIDParam(w, r)
	if !ok {
		return
	}
	var p store.NodePatch
	if err := readJSON(r, &p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if _, err := s.store.PatchNode(id, p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.pushAll()
	writeJSON(w, http.StatusOK, s.store.Snapshot().WithoutPrivateKeys())
}

func (s *Server) handleNodeForwards(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, ok := nodeIDParam(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		fwds, err := s.store.NodeForwards(id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, fwds)
	case http.MethodPut:
		var fwds []core.Forward
		if err := readJSON(r, &fwds); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		out, err := s.store.PutNodeForwards(id, fwds)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		s.pushAll()
		writeJSON(w, http.StatusOK, out)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type ipTrafficView struct {
	IP        string  `json:"ip"`
	NodeID    string  `json:"nodeId,omitempty"`
	Name      string  `json:"name,omitempty"`
	Rx        uint64  `json:"rx"`
	Tx        uint64  `json:"tx"`
	RxRate    float64 `json:"rxRate"`
	TxRate    float64 `json:"txRate"`
	UpdatedAt int64   `json:"updatedAt,omitempty"`
}

type peerView struct {
	stats.LivePeer
	NodeID string `json:"nodeId,omitempty"`
	Name   string `json:"name,omitempty"`
}

type nodeStatsView struct {
	NodeID         string              `json:"nodeId"`
	Link           string              `json:"link"` // ws | http | offline
	LastSeen       time.Time           `json:"lastSeen,omitempty"`
	ConnectedAt    time.Time           `json:"connectedAt,omitempty"`
	RTTMillis      float64             `json:"rttMs,omitempty"`
	AgentVersion   string              `json:"agentVersion,omitempty"`
	PublicV4       string              `json:"publicV4,omitempty"`
	PublicV6       string              `json:"publicV6,omitempty"`
	GeoCountry     string              `json:"geoCountry,omitempty"`
	GeoCountryCode string              `json:"geoCountryCode,omitempty"`
	RxRate         float64             `json:"rxRate"`
	TxRate         float64             `json:"txRate"`
	SampleAt       time.Time           `json:"sampleAt,omitempty"`
	Totals         store.TrafficTotals `json:"totals"`
	Peers          []peerView          `json:"peers"`
	Forwards       []stats.LiveForward `json:"forwards"`
	IPs            []ipTrafficView     `json:"ips"`
}

const httpOnlineWindow = 45 * time.Second

func (s *Server) handleNodeStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	id, ok := nodeIDParam(w, r)
	if !ok {
		return
	}
	v, ok := s.nodeStats(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "node not found"})
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// nodeLink reports how a node currently talks to Master.
func (s *Server) nodeLink(n *core.Node, now time.Time) (uint8, string) {
	if ver, ok := s.hub.AgentVersion(n.ID); ok {
		return wire.LinkWS, ver
	}
	if !n.LastSeen.IsZero() && now.Sub(n.LastSeen) < httpOnlineWindow {
		return wire.LinkHTTP, ""
	}
	return wire.LinkOffline, ""
}

var linkNames = map[uint8]string{wire.LinkOffline: "offline", wire.LinkHTTP: "http", wire.LinkWS: "ws"}

func (s *Server) nodeStats(id string) (nodeStatsView, bool) {
	m := s.store.Snapshot()
	n := m.FindNode(id)
	if n == nil {
		return nodeStatsView{}, false
	}
	live, ipRates := s.stats.Live(id)
	totals, _ := s.store.TrafficTotal(id)
	persisted, _ := s.store.PeerIPTraffic(id)

	byHost := map[string]*core.Node{}
	byKey := map[string]*core.Node{}
	for i := range m.Nodes {
		nn := &m.Nodes[i]
		if host, _, ok := strings.Cut(nn.Address, "/"); ok {
			byHost[host] = nn
		}
		byKey[nn.PublicKey] = nn
	}

	link, ver := s.nodeLink(n, time.Now())
	v := nodeStatsView{
		NodeID:         id,
		Link:           linkNames[link],
		AgentVersion:   ver,
		LastSeen:       n.LastSeen,
		ConnectedAt:    live.ConnectedAt,
		RTTMillis:      live.RTTMillis,
		PublicV4:       n.PublicV4,
		PublicV6:       n.PublicV6,
		GeoCountry:     n.GeoCountry,
		GeoCountryCode: n.GeoCountryCode,
		RxRate:         live.RxRate,
		TxRate:         live.TxRate,
		SampleAt:       live.SampleAt,
		Totals:         totals,
		Peers:          []peerView{},
		Forwards:       live.Forwards,
		IPs:            []ipTrafficView{},
	}
	if v.Forwards == nil {
		v.Forwards = []stats.LiveForward{}
	}
	for _, p := range live.Peers {
		pv := peerView{LivePeer: p}
		if nn := byKey[p.PublicKey]; nn != nil {
			pv.NodeID, pv.Name = nn.ID, nn.Name
		}
		v.Peers = append(v.Peers, pv)
	}
	seen := map[string]bool{}
	addIP := func(ip string, rx, tx uint64, updated int64) {
		iv := ipTrafficView{IP: ip, Rx: rx, Tx: tx, UpdatedAt: updated}
		if rt, ok := ipRates[ip]; ok {
			iv.RxRate, iv.TxRate = rt.Rx, rt.Tx
		}
		if nn := byHost[ip]; nn != nil {
			iv.NodeID, iv.Name = nn.ID, nn.Name
		}
		seen[ip] = true
		v.IPs = append(v.IPs, iv)
	}
	for _, p := range persisted {
		addIP(p.IP, p.Rx, p.Tx, p.UpdatedAt)
	}
	for ip := range ipRates {
		if !seen[ip] {
			addIP(ip, 0, 0, 0)
		}
	}
	return v, true
}

var trafficRanges = map[string]struct {
	span   time.Duration
	hourly bool
}{
	"1h":  {time.Hour, false},
	"24h": {24 * time.Hour, false},
	"7d":  {7 * 24 * time.Hour, true},
	"30d": {30 * 24 * time.Hour, true},
}

func (s *Server) handleNodeTraffic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	id, ok := nodeIDParam(w, r)
	if !ok {
		return
	}
	rng := r.URL.Query().Get("range")
	if rng == "" {
		rng = "1h"
	}
	spec, ok := trafficRanges[rng]
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "range must be 1h, 24h, 7d or 30d"})
		return
	}
	pts, err := s.store.TrafficSeries(id, time.Now().Add(-spec.span), spec.hourly)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	step := 60
	if spec.hourly {
		step = 3600
	}
	writeJSON(w, http.StatusOK, map[string]any{"range": rng, "step": step, "points": pts})
}

const httpAgentGeoEvery = 30 * time.Minute

// observeHTTPAgent records the source IP of HTTP-polling (legacy) agents,
// which never send a Hello with public addresses.
func (s *Server) observeHTTPAgent(token, remote string) {
	node, err := s.store.NodeByToken(token)
	if err != nil {
		return
	}
	now := time.Now()
	if v, ok := s.httpGeoAt.Load(node.ID); ok && now.Sub(v.(time.Time)) < httpAgentGeoEvery {
		return
	}
	s.httpGeoAt.Store(node.ID, now)
	s.hub.RecordPublicIPs(node, wire.Hello{}, remote)
}

// handleAgentWhoami echoes the caller's source address for public IP discovery.
func (s *Server) handleAgentWhoami(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ip": clientIP(r)})
}
