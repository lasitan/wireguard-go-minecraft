package store

import (
	"fmt"
	"net/netip"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/src/core"
)

func isMagnetChild(m *core.Mesh, id string) bool {
	for _, l := range m.Links {
		if l.FromNodeID != id {
			continue
		}
		to := m.FindNode(l.ToNodeID)
		if to != nil && to.IsMagnetParent() {
			return true
		}
	}
	return false
}

func subnetSwapGuard(m *core.Mesh, id string) error {
	n := m.FindNode(id)
	if n == nil {
		return fmt.Errorf("node %q not found", id)
	}
	if n.Disabled {
		return fmt.Errorf("node %q is disabled", id)
	}
	if isMagnetChild(m, id) {
		return fmt.Errorf("node %q is attached under a mother; detach first", id)
	}
	return nil
}

// SwapNodeAddresses exchanges VPN addresses and VPN-prefix route entries between two nodes.
func (s *Store) SwapNodeAddresses(aID, bID string) error {
	if aID == bID {
		return fmt.Errorf("cannot swap a node with itself")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.loadMeshLocked()
	if err != nil {
		return err
	}
	if err := subnetSwapGuard(&m, aID); err != nil {
		return err
	}
	if err := subnetSwapGuard(&m, bID); err != nil {
		return err
	}
	a := m.FindNode(aID)
	b := m.FindNode(bID)
	oldA, err := core.VPNPrefixFromAddress(a.Address)
	if err != nil {
		return err
	}
	oldB, err := core.VPNPrefixFromAddress(b.Address)
	if err != nil {
		return err
	}
	addrA, addrB := a.Address, b.Address
	a.Address, b.Address = addrB, addrA
	now := time.Now().UTC()
	a.AddressChangedAt = now
	b.AddressChangedAt = now
	newA, err := core.VPNPrefixFromAddress(a.Address)
	if err != nil {
		return err
	}
	newB, err := core.VPNPrefixFromAddress(b.Address)
	if err != nil {
		return err
	}
	a.Routes, err = core.ReplaceVPNRoute(a.Routes, oldA, newA)
	if err != nil {
		return err
	}
	b.Routes, err = core.ReplaceVPNRoute(b.Routes, oldB, newB)
	if err != nil {
		return err
	}
	m.Revision++
	if err := validateMesh(m); err != nil {
		return err
	}
	return s.replaceMeshLocked(m)
}

func nodesInVPNPrefix(m *core.Mesh, prefix string) []core.Node {
	want, err := netip.ParsePrefix(prefix)
	if err != nil {
		return nil
	}
	wantS := want.Masked().String()
	var out []core.Node
	for _, n := range m.Nodes {
		pfx, err := core.VPNPrefixFromAddress(n.Address)
		if err != nil || pfx != wantS {
			continue
		}
		out = append(out, n)
	}
	return out
}

// ReassignNodeSubnet moves nodeID to targetPrefix, allocating the next free host in that pool.
func (s *Store) ReassignNodeSubnet(nodeID, targetPrefix string) error {
	targetPrefix = strings.TrimSpace(targetPrefix)
	p, err := netip.ParsePrefix(targetPrefix)
	if err != nil {
		return fmt.Errorf("prefix: %w", err)
	}
	targetPrefix = p.Masked().String()

	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.loadMeshLocked()
	if err != nil {
		return err
	}
	if err := subnetSwapGuard(&m, nodeID); err != nil {
		return err
	}
	n := m.FindNode(nodeID)
	if n == nil {
		return fmt.Errorf("node %q not found", nodeID)
	}
	oldPrefix, err := core.VPNPrefixFromAddress(n.Address)
	if err != nil {
		return err
	}
	if oldPrefix == targetPrefix {
		return fmt.Errorf("node is already in %s", targetPrefix)
	}
	pool := nodesInVPNPrefix(&m, targetPrefix)
	var others []core.Node
	for _, x := range pool {
		if x.ID != nodeID {
			others = append(others, x)
		}
	}
	addr, err := nextAddress(targetPrefix, others)
	if err != nil {
		return err
	}
	n.Address = addr
	n.AddressChangedAt = time.Now().UTC()
	n.Routes, err = core.ReplaceVPNRoute(n.Routes, oldPrefix, targetPrefix)
	if err != nil {
		return err
	}
	m.Revision++
	if err := validateMesh(m); err != nil {
		return err
	}
	return s.replaceMeshLocked(m)
}
