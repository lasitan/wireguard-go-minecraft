// Package core holds the shared mesh schema and the desired-config compiler
// used by the Master control plane and agents.
package core

import (
	"encoding/json"
	"fmt"
	"time"
)

const (
	RoleServer = "server"
	RoleClient = "client"
)

// Mesh is the authoritative configuration table stored on the Master.
type Mesh struct {
	Revision int       `json:"revision"`
	Nodes    []Node    `json:"nodes"`
	Links    []Link    `json:"links"`
	Forwards []Forward `json:"forwards"`
	// Paths is Master-owned (lowest-RTT gateway per node and subnet); admin
	// mesh edits never write it.
	Paths PathChoices `json:"paths,omitempty"`
	// Relay is Master's built-in fallback relay (nil = disabled); Master-owned.
	Relay *Relay `json:"relay,omitempty"`
	// Standby maps an offline cluster member to the online member that takes
	// over its forwards; Master-owned.
	Standby map[string]string `json:"standby,omitempty"`
}

// RelayNodeID stands for Master's relay wherever a gateway node id is expected.
const RelayNodeID = "master"

// RelayEndpointHost is replaced by each agent with its own Master URL host.
const RelayEndpointHost = "@master"

// Relay describes Master's fallback relay: a WireGuard peer that only forwards
// between agents, used for cross-subnet traffic when no mother is reachable.
type Relay struct {
	PublicKey string `json:"publicKey"`
	Port      uint16 `json:"port"`
}

// Endpoint is the placeholder dial address agents resolve against their Master URL.
func (r *Relay) Endpoint() string {
	return fmt.Sprintf("%s:%d", RelayEndpointHost, r.Port)
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
	Interface    string          `json:"interface,omitempty"`    // TUN name, default lc0 (Master-managed)
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
	// Cluster groups side-by-side magnet cards: members share config, and a
	// card attached to one member mother is served by every member.
	Cluster string `json:"cluster,omitempty"`

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

// WithoutPrivateKeys strips private keys for admin GET responses (tokens are
// kept because the UI shows agent bootstrap).
func (m Mesh) WithoutPrivateKeys() Mesh {
	cp := m
	cp.Nodes = make([]Node, len(m.Nodes))
	copy(cp.Nodes, m.Nodes)
	for i := range cp.Nodes {
		cp.Nodes[i].PrivateKey = ""
	}
	return cp
}
