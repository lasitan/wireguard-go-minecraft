// Package meshcfg holds the shared mesh schema, agent bootstrap, and
// desired-config compiler used by the Master control plane and agents.
package meshcfg

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/curve25519"
)

const (
	RoleServer = "server"
	RoleClient = "client"

	AgentFileName  = "wireguard-go-agent.json"
	MasterFileName = "wireguard-go-master.json"
	RoleLockName   = ".role"
	RoleMaster     = "master"
	RoleAgent      = "agent"
)

// Mesh is the authoritative configuration table stored on the Master.
type Mesh struct {
	Revision int      `json:"revision"`
	Nodes    []Node   `json:"nodes"`
	Links    []Link   `json:"links"`
	Forwards []Forward `json:"forwards"`
}

// Node is a tunnel endpoint managed by Master.
// ID is a Master-assigned UUID that uniquely identifies the agent.
type Node struct {
	ID           string          `json:"id"` // UUID v4
	Name         string          `json:"name,omitempty"`
	Role         string          `json:"role"` // server | client
	PublicKey    string          `json:"publicKey"`
	PrivateKey   string          `json:"privateKey,omitempty"`
	Address      string          `json:"address"`
	ListenPort   uint16          `json:"listenPort,omitempty"`
	Endpoint     string          `json:"endpoint,omitempty"`
	MTU          int             `json:"mtu,omitempty"`
	Token        string          `json:"token"`
	Interface    string          `json:"interface,omitempty"`    // TUN name, default wg0 (Master-managed)
	PollInterval string          `json:"pollInterval,omitempty"` // default 10s (Master-managed)
	Transport    json.RawMessage `json:"transport,omitempty"`    // per-node override; else mesh default
	LastSeen     time.Time       `json:"lastSeen,omitempty"`

	// Disabled nodes are removed from every peer list (tunnel down) but keep
	// their Master connection so they can be re-enabled.
	Disabled bool `json:"disabled,omitempty"`
	// Routes are extra CIDRs reachable through this node (subnet router).
	Routes []string `json:"routes,omitempty"`
	// AddressChangedAt decides IP-conflict precedence: the later change loses.
	AddressChangedAt time.Time `json:"addressChangedAt,omitempty"`

	// Server-observed fields (never accepted from admin mesh edits).
	PublicV4       string    `json:"publicV4,omitempty"`
	PublicV6       string    `json:"publicV6,omitempty"`
	GeoCountry     string    `json:"geoCountry,omitempty"`
	GeoCountryCode string    `json:"geoCountryCode,omitempty"`
	GeoUpdatedAt   time.Time `json:"geoUpdatedAt,omitempty"`
}

// PreserveServerFields copies Master-owned fields from cur into n, so admin
// mesh edits (which never see private keys) cannot wipe them.
func (n *Node) PreserveServerFields(cur Node) {
	if n.PrivateKey == "" {
		n.PrivateKey = cur.PrivateKey
	}
	if n.Token == "" {
		n.Token = cur.Token
	}
	n.LastSeen = cur.LastSeen
	n.PublicV4 = cur.PublicV4
	n.PublicV6 = cur.PublicV6
	n.GeoCountry = cur.GeoCountry
	n.GeoCountryCode = cur.GeoCountryCode
	n.GeoUpdatedAt = cur.GeoUpdatedAt
	if n.Address != cur.Address {
		n.AddressChangedAt = time.Now().UTC()
	} else {
		n.AddressChangedAt = cur.AddressChangedAt
	}
}

// Link means fromNode dials toNode (client→server typically).
type Link struct {
	FromNodeID string   `json:"fromNodeId"`
	ToNodeID   string   `json:"toNodeId"`
	AllowedIPs []string `json:"allowedIPs,omitempty"`
	Keepalive  int      `json:"keepalive,omitempty"`
}

// Forward is a port-forward task on nodeId toward destNodeId:destPort.
type Forward struct {
	NodeID     string `json:"nodeId"`
	Protocol   string `json:"protocol"`
	Listen     string `json:"listen"`
	DestNodeID string `json:"destNodeId"`
	DestPort   uint16 `json:"destPort"`
}

// AgentBootstrap is the only local file an agent keeps: API URL + key.
// key is enrollToken before join, then replaced by the node API token from Master.
type AgentBootstrap struct {
	MasterURL string `json:"masterUrl"`
	Key       string `json:"key"`

	// Deprecated local fields (ignored if present; migrated away on enroll).
	EnrollToken  string `json:"enrollToken,omitempty"`
	NodeToken    string `json:"nodeToken,omitempty"`
	PollInterval string `json:"pollInterval,omitempty"`
	Interface    string `json:"interface,omitempty"`
}

// MasterConfig is the minimal process bootstrap for Master (path to SQLite + listen/auth).
// Mesh/enroll/transport and all agent settings live in SQLite under DataDir.
type MasterConfig struct {
	Listen        string `json:"listen"`
	AdminPassword string `json:"adminPassword"`
	DataDir       string `json:"dataDir"` // SQLite mesh.db lives here
	TLSCert       string `json:"tlsCert,omitempty"`
	TLSKey        string `json:"tlsKey,omitempty"`
	// Seeded into SQLite on first run when meta is empty:
	EnrollToken string `json:"enrollToken,omitempty"`
	VPNSubnet   string `json:"vpnSubnet,omitempty"`
	// GeoIPDB is an offline country mmdb (GeoLite2-Country / DB-IP Lite).
	// Defaults to <dataDir>/GeoLite2-Country.mmdb or dbip-country-lite.mmdb.
	GeoIPDB string `json:"geoipDb,omitempty"`
	// DisableGeoIPOnline turns off the ip-api.com fallback.
	DisableGeoIPOnline bool `json:"disableGeoipOnline,omitempty"`
	// DisableUpdateCheck stops polling GitHub Releases for new versions.
	DisableUpdateCheck bool `json:"disableUpdateCheck,omitempty"`
}

// DesiredConfig is what an agent applies (fully Master-authored).
type DesiredConfig struct {
	Revision      int              `json:"revision"`
	NodeID        string           `json:"nodeId"`
	Role          string           `json:"role"`
	InterfaceName string           `json:"interfaceName"`
	PollInterval  string           `json:"pollInterval"`
	Interface     DesiredIface     `json:"interface"`
	Peers         []DesiredPeer    `json:"peers"`
	Forwards      []DesiredForward `json:"forwards"`
	IPForward     bool             `json:"ipForward"`
	Transport     json.RawMessage  `json:"transport,omitempty"`
}

type DesiredIface struct {
	PrivateKey string `json:"privateKey"`
	Address    string `json:"address"`
	ListenPort uint16 `json:"listenPort,omitempty"`
	MTU        int    `json:"mtu,omitempty"`
}

type DesiredPeer struct {
	PublicKey  string   `json:"publicKey"`
	Endpoint   string   `json:"endpoint,omitempty"`
	AllowedIPs []string `json:"allowedIPs"`
	Keepalive  int      `json:"keepalive,omitempty"`
}

type DesiredForward struct {
	Protocol string `json:"protocol"`
	Listen   string `json:"listen"`
	DestHost string `json:"destHost"`
	DestPort uint16 `json:"destPort"`
}

func EmptyMesh() Mesh {
	return Mesh{Revision: 0, Nodes: []Node{}, Links: []Link{}, Forwards: []Forward{}}
}

func LoadJSON[T any](path string, dst *T) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

func SaveJSON(path string, v any, mode os.FileMode) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (a AgentBootstrap) APIKey() string {
	if a.Key != "" {
		return a.Key
	}
	if a.NodeToken != "" {
		return a.NodeToken
	}
	return a.EnrollToken
}

func (a AgentBootstrap) PollDuration() time.Duration {
	// Local poll interval is obsolete; Master desired config drives polling.
	return 10 * time.Second
}

func (a AgentBootstrap) IfaceName() string {
	return "wg0"
}

// Normalized returns a bootstrap with only masterUrl + key for persistence.
func (a AgentBootstrap) Normalized() AgentBootstrap {
	return AgentBootstrap{MasterURL: a.MasterURL, Key: a.APIKey()}
}

// GenerateKeyPair returns (privateKeyB64, publicKeyB64).
func GenerateKeyPair() (string, string, error) {
	var priv [32]byte
	if _, err := rand.Read(priv[:]); err != nil {
		return "", "", err
	}
	priv[0] &= 248
	priv[31] = (priv[31] & 127) | 64
	var pub [32]byte
	curve25519.ScalarBaseMult(&pub, &priv)
	return base64.StdEncoding.EncodeToString(priv[:]),
		base64.StdEncoding.EncodeToString(pub[:]), nil
}

func GenerateToken() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// GenerateNodeID returns a random UUID v4 used as the Master-assigned agent id.
func GenerateNodeID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

// ValidNodeID reports whether id is a canonical UUID (8-4-4-4-12 hex).
func ValidNodeID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i, c := range id {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}

func (m *Mesh) FindNode(id string) *Node {
	for i := range m.Nodes {
		if m.Nodes[i].ID == id {
			return &m.Nodes[i]
		}
	}
	return nil
}

func (m *Mesh) FindNodeByToken(token string) *Node {
	for i := range m.Nodes {
		if m.Nodes[i].Token != "" && m.Nodes[i].Token == token {
			return &m.Nodes[i]
		}
	}
	return nil
}

func hostFromAddress(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", fmt.Errorf("empty address")
	}
	if i := strings.IndexByte(addr, '/'); i >= 0 {
		return addr[:i], nil
	}
	return addr, nil
}

func hostAsSlash32(addr string) (string, error) {
	host, err := hostFromAddress(addr)
	if err != nil {
		return "", err
	}
	if strings.Contains(host, ":") {
		return host + "/128", nil
	}
	return host + "/32", nil
}

// ConflictLosers returns node ids whose VPN host IP duplicates another node's.
// The node that changed its address earliest keeps the IP; later changers lose.
// Ties (or missing timestamps) fall back to mesh order (earlier enrolled wins).
func ConflictLosers(mesh *Mesh) map[string]bool {
	type holder struct {
		idx int
		at  time.Time
	}
	winners := map[string]holder{}
	losers := map[string]bool{}
	better := func(a, b holder) bool {
		switch {
		case a.at.IsZero() && b.at.IsZero():
			return a.idx < b.idx
		case a.at.IsZero():
			return true
		case b.at.IsZero():
			return false
		case !a.at.Equal(b.at):
			return a.at.Before(b.at)
		default:
			return a.idx < b.idx
		}
	}
	for i, n := range mesh.Nodes {
		host, err := hostFromAddress(n.Address)
		if err != nil {
			continue
		}
		h := holder{idx: i, at: n.AddressChangedAt}
		w, ok := winners[host]
		if !ok {
			winners[host] = h
			continue
		}
		if better(h, w) {
			losers[mesh.Nodes[w.idx].ID] = true
			winners[host] = h
		} else {
			losers[n.ID] = true
		}
	}
	return losers
}

// peerAllowedIPs is the node's own /32 plus its advertised subnet routes.
func peerAllowedIPs(n *Node, base []string) ([]string, error) {
	allowed := append([]string(nil), base...)
	if len(allowed) == 0 {
		slash32, err := hostAsSlash32(n.Address)
		if err != nil {
			return nil, fmt.Errorf("node %s address: %w", n.ID, err)
		}
		allowed = []string{slash32}
	}
	seen := make(map[string]struct{}, len(allowed))
	for _, a := range allowed {
		seen[a] = struct{}{}
	}
	for _, r := range n.Routes {
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		allowed = append(allowed, r)
	}
	return allowed, nil
}

// DesiredDefaults are mesh-wide defaults applied when a node omits overrides.
type DesiredDefaults struct {
	InterfaceName string
	PollInterval  string
	Transport     json.RawMessage
}

// CompileDesired builds the per-node desired config from the mesh table.
func CompileDesired(mesh *Mesh, nodeID string, defaults DesiredDefaults) (*DesiredConfig, error) {
	n := mesh.FindNode(nodeID)
	if n == nil {
		return nil, fmt.Errorf("node %q not found", nodeID)
	}
	if n.PrivateKey == "" {
		return nil, fmt.Errorf("node %q missing privateKey", nodeID)
	}
	mtu := n.MTU
	if mtu == 0 {
		mtu = 1420
	}
	ifaceName := n.Interface
	if ifaceName == "" {
		ifaceName = defaults.InterfaceName
	}
	if ifaceName == "" {
		ifaceName = "wg0"
	}
	poll := n.PollInterval
	if poll == "" {
		poll = defaults.PollInterval
	}
	if poll == "" {
		poll = "10s"
	}
	transport := n.Transport
	if len(transport) == 0 {
		transport = defaults.Transport
	}
	out := &DesiredConfig{
		Revision:      mesh.Revision,
		NodeID:        n.ID,
		Role:          n.Role,
		InterfaceName: ifaceName,
		PollInterval:  poll,
		Interface: DesiredIface{
			PrivateKey: n.PrivateKey,
			Address:    n.Address,
			ListenPort: n.ListenPort,
			MTU:        mtu,
		},
		IPForward: n.Role == RoleServer || len(n.Routes) > 0,
		Transport: transport,
		Peers:     []DesiredPeer{},
		Forwards:  []DesiredForward{},
	}
	if n.Disabled {
		// Tunnel down: no peers, no forwards. The control connection stays up.
		return out, nil
	}
	losers := ConflictLosers(mesh)
	excluded := func(peer *Node) bool {
		return peer.Disabled || losers[peer.ID]
	}

	// Peers from links where this node is the "from" side (outbound dial).
	for _, link := range mesh.Links {
		if link.FromNodeID != nodeID {
			continue
		}
		to := mesh.FindNode(link.ToNodeID)
		if to == nil {
			return nil, fmt.Errorf("link to unknown node %q", link.ToNodeID)
		}
		if excluded(to) {
			continue
		}
		allowed, err := peerAllowedIPs(to, link.AllowedIPs)
		if err != nil {
			return nil, err
		}
		ep := to.Endpoint
		ka := link.Keepalive
		if ka == 0 && n.Role == RoleClient {
			ka = 5
		}
		out.Peers = append(out.Peers, DesiredPeer{
			PublicKey:  to.PublicKey,
			Endpoint:   ep,
			AllowedIPs: allowed,
			Keepalive:  ka,
		})
	}

	// Inbound peers: nodes that link TO us (server side) need their public keys
	// with AllowedIPs = their /32 so cryptokey routing and forwards work.
	seenPub := make(map[string]struct{})
	for _, p := range out.Peers {
		seenPub[p.PublicKey] = struct{}{}
	}
	for _, link := range mesh.Links {
		if link.ToNodeID != nodeID {
			continue
		}
		from := mesh.FindNode(link.FromNodeID)
		if from == nil {
			return nil, fmt.Errorf("link from unknown node %q", link.FromNodeID)
		}
		if _, ok := seenPub[from.PublicKey]; ok {
			continue
		}
		if excluded(from) {
			continue
		}
		allowed, err := peerAllowedIPs(from, nil)
		if err != nil {
			return nil, err
		}
		out.Peers = append(out.Peers, DesiredPeer{
			PublicKey:  from.PublicKey,
			AllowedIPs: allowed,
		})
		seenPub[from.PublicKey] = struct{}{}
	}

	for _, fw := range mesh.Forwards {
		if fw.NodeID != nodeID {
			continue
		}
		dest := mesh.FindNode(fw.DestNodeID)
		if dest == nil {
			return nil, fmt.Errorf("forward dest unknown node %q", fw.DestNodeID)
		}
		if excluded(dest) {
			continue
		}
		host, err := hostFromAddress(dest.Address)
		if err != nil {
			return nil, err
		}
		proto := strings.ToLower(fw.Protocol)
		if proto == "" {
			proto = "tcp"
		}
		out.Forwards = append(out.Forwards, DesiredForward{
			Protocol: proto,
			Listen:   fw.Listen,
			DestHost: host,
			DestPort: fw.DestPort,
		})
	}

	return out, nil
}

// PublicMesh strips private keys and tokens for admin GET responses that should
// not leak secrets into browser localStorage dumps (tokens still needed for UI
// to show agent bootstrap — include tokens but strip private keys for list view).
func (m Mesh) WithoutPrivateKeys() Mesh {
	cp := m
	cp.Nodes = make([]Node, len(m.Nodes))
	copy(cp.Nodes, m.Nodes)
	for i := range cp.Nodes {
		cp.Nodes[i].PrivateKey = ""
	}
	return cp
}
