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

// DesiredDefaults are mesh-wide defaults applied when a node omits overrides.
type DesiredDefaults struct {
	InterfaceName string
	PollInterval  string
	Transport     json.RawMessage
}

// CompileDesired builds the per-node desired config from the mesh table.
func CompileDesired(mesh *Mesh, nodeID string, defaults DesiredDefaults) (*DesiredConfig, error) {
	return CompileDesiredWithPlan(mesh, PlanMesh(mesh), nodeID, defaults)
}

// CompileDesiredWithPlan reuses a precomputed plan (one per mesh load).
func CompileDesiredWithPlan(mesh *Mesh, plan *Plan, nodeID string, defaults DesiredDefaults) (*DesiredConfig, error) {
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
		IPForward: n.Role == RoleServer || n.IsMagnetParent() || len(n.Routes) > 0,
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

	for _, pp := range plan.peersFor(nodeID) {
		var pub, ep string
		if pp.peerID == RelayNodeID && mesh.Relay != nil {
			pub, ep = mesh.Relay.PublicKey, mesh.Relay.Endpoint()
		} else if peer := mesh.FindNode(pp.peerID); peer != nil {
			pub, ep = peer.PublicKey, peer.DialEndpoint()
		} else {
			continue
		}
		dp := DesiredPeer{PublicKey: pub, AllowedIPs: make([]string, 0, len(pp.allowed))}
		for _, a := range pp.allowed {
			dp.AllowedIPs = append(dp.AllowedIPs, a.String())
		}
		if pp.dial {
			dp.Endpoint = ep
			dp.Keepalive = pp.keepalive
		}
		out.Peers = append(out.Peers, dp)
	}

	for _, fw := range mesh.Forwards {
		if fw.NodeID != nodeID {
			continue
		}
		dest := mesh.FindNode(fw.DestNodeID)
		if dest == nil {
			return nil, fmt.Errorf("forward dest unknown node %q", fw.DestNodeID)
		}
		if sb := mesh.FindNode(mesh.Standby[dest.ID]); sb != nil && !excluded(sb) {
			dest = sb
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
