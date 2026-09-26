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
type Node struct {
	ID         string `json:"id"`
	Name       string `json:"name,omitempty"`
	Role       string `json:"role"` // server | client
	PublicKey  string `json:"publicKey"`
	PrivateKey string `json:"privateKey,omitempty"` // never returned to other nodes
	Address    string `json:"address"`              // e.g. 10.10.0.7/24
	ListenPort uint16 `json:"listenPort,omitempty"`
	Endpoint   string `json:"endpoint,omitempty"` // public host:port for servers
	MTU        int    `json:"mtu,omitempty"`
	Token      string `json:"token"` // agent auth bearer token
}

// Link means fromNode dials toNode (client→server typically).
type Link struct {
	FromNodeID string   `json:"fromNodeId"`
	ToNodeID   string   `json:"toNodeId"`
	AllowedIPs []string `json:"allowedIPs,omitempty"` // default: toNode address /32
	Keepalive  int      `json:"keepalive,omitempty"`
}

// Forward is a port-forward task on nodeId toward destNodeId:destPort.
type Forward struct {
	NodeID     string `json:"nodeId"`
	Protocol   string `json:"protocol"` // tcp | udp
	Listen     string `json:"listen"`   // port or host:port
	DestNodeID string `json:"destNodeId"`
	DestPort   uint16 `json:"destPort"`
}

// AgentBootstrap is the only local config an agent needs.
// Node identity (nodeId) is assigned by Master and delivered in DesiredConfig.
type AgentBootstrap struct {
	MasterURL    string `json:"masterUrl"`
	NodeToken    string `json:"nodeToken"`
	PollInterval string `json:"pollInterval,omitempty"` // default 10s
	Interface    string `json:"interface,omitempty"`    // default wg0
}

// MasterConfig is local config for the Master process.
type MasterConfig struct {
	Listen       string `json:"listen"`                 // e.g. :8443
	AdminPassword string `json:"adminPassword"`
	DataDir      string `json:"dataDir"`                // mesh.json lives here
	TLSCert      string `json:"tlsCert,omitempty"`
	TLSKey       string `json:"tlsKey,omitempty"`
}

// DesiredConfig is what an agent applies (UAPI + forwards).
type DesiredConfig struct {
	Revision  int              `json:"revision"`
	NodeID    string           `json:"nodeId"`
	Role      string           `json:"role"`
	Interface DesiredIface     `json:"interface"`
	Peers     []DesiredPeer    `json:"peers"`
	Forwards  []DesiredForward `json:"forwards"`
	IPForward bool             `json:"ipForward"`
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

func (a AgentBootstrap) PollDuration() time.Duration {
	if a.PollInterval == "" {
		return 10 * time.Second
	}
	d, err := time.ParseDuration(a.PollInterval)
	if err != nil || d < time.Second {
		return 10 * time.Second
	}
	return d
}

func (a AgentBootstrap) IfaceName() string {
	if a.Interface == "" {
		return "wg0"
	}
	return a.Interface
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

// CompileDesired builds the per-node desired config from the mesh table.
func CompileDesired(mesh *Mesh, nodeID string) (*DesiredConfig, error) {
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
	out := &DesiredConfig{
		Revision: mesh.Revision,
		NodeID:   n.ID,
		Role:     n.Role,
		Interface: DesiredIface{
			PrivateKey: n.PrivateKey,
			Address:    n.Address,
			ListenPort: n.ListenPort,
			MTU:        mtu,
		},
		IPForward: n.Role == RoleServer,
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
		allowed := link.AllowedIPs
		if len(allowed) == 0 {
			slash32, err := hostAsSlash32(to.Address)
			if err != nil {
				return nil, fmt.Errorf("node %s address: %w", to.ID, err)
			}
			allowed = []string{slash32}
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
		slash32, err := hostAsSlash32(from.Address)
		if err != nil {
			return nil, err
		}
		out.Peers = append(out.Peers, DesiredPeer{
			PublicKey:  from.PublicKey,
			AllowedIPs: []string{slash32},
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
