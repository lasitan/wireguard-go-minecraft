package core

import "encoding/json"

// ResolveBindConfig picks bind mode for a node. TCP camouflage and native
// WireGuard UDP are exclusive: when udp is true, transport is always nil
// (no camouflage). Mothers own the choice; children inherit from the first
// attached mother; everyone else falls back to defaults (TCP).
func ResolveBindConfig(mesh *Mesh, n *Node, defaults json.RawMessage) (transport json.RawMessage, udp bool) {
	if n == nil {
		return defaults, false
	}
	if n.IsMagnetParent() {
		if n.UdpMode {
			return nil, true // native WG UDP — max performance, no camouflage
		}
		tr := n.Transport
		if len(tr) == 0 {
			tr = defaults
		}
		return tr, false
	}
	for _, l := range mesh.Links {
		if l.FromNodeID != n.ID {
			continue
		}
		to := mesh.FindNode(l.ToNodeID)
		if to == nil || !to.IsMagnetParent() {
			continue
		}
		if to.UdpMode {
			return nil, true // inherit native UDP — no camouflage
		}
		tr := to.Transport
		if len(tr) == 0 {
			tr = defaults
		}
		return tr, false
	}
	return defaults, false
}
