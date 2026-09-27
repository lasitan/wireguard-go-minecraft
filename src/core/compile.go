package core

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

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
