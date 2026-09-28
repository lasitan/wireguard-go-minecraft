package store

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"golang.zx2c4.com/wireguard/src/core"
)

// Magnet cards: a node with a ListenPort is a "mother"; every other node
// attaches to one or more mothers via outbound links, and Master pushes those
// mothers' DialEndpoints to it. With several mothers in its subnet, the
// lowest-RTT one carries the subnet route and the others deliver to it directly.

const (
	magnetKeepalive  = 5
	maxMagnetParents = 8
)

// setListenPort turns n into a mother (port > 0) or back into a plain card.
// A new mother drops its own attachment; a former mother releases its children.
func setListenPort(m *core.Mesh, n *core.Node, port uint16) {
	n.ListenPort = port
	if port > 0 {
		n.Role = core.RoleServer
		m.Links = dropLinks(m.Links, func(l core.Link) bool { return l.FromNodeID == n.ID })
		return
	}
	n.Role = core.RoleClient
	m.Links = dropLinks(m.Links, func(l core.Link) bool { return l.ToNodeID == n.ID })
}

// attachTo replaces child's outbound links with one link to parentID ("" = detach).
func attachTo(m *core.Mesh, childID, parentID string) error {
	if parentID == "" {
		return setParents(m, childID, nil)
	}
	return setParents(m, childID, []string{parentID})
}

// setParents replaces child's outbound links with one link per mother.
func setParents(m *core.Mesh, childID string, parentIDs []string) error {
	child := m.FindNode(childID)
	if child == nil {
		return fmt.Errorf("node %q not found", childID)
	}
	var ids []string
	seen := map[string]bool{}
	for _, pid := range parentIDs {
		pid = strings.TrimSpace(pid)
		if pid == "" || seen[pid] {
			continue
		}
		seen[pid] = true
		ids = append(ids, pid)
	}
	if len(ids) > maxMagnetParents {
		return fmt.Errorf("at most %d mothers per card", maxMagnetParents)
	}
	if len(ids) > 0 && child.IsMagnetParent() {
		return fmt.Errorf("a node with a listen port cannot attach to another node")
	}
	for _, pid := range ids {
		parent := m.FindNode(pid)
		if parent == nil || parent.ID == childID {
			return fmt.Errorf("parent %q not found", pid)
		}
		if !parent.IsMagnetParent() {
			return fmt.Errorf("parent %q has no listen port", pid)
		}
	}
	m.Links = dropLinks(m.Links, func(l core.Link) bool { return l.FromNodeID == childID })
	for _, pid := range ids {
		m.Links = append(m.Links, core.Link{FromNodeID: childID, ToNodeID: pid, Keepalive: magnetKeepalive})
	}
	return nil
}

func dropLinks(links []core.Link, drop func(core.Link) bool) []core.Link {
	out := links[:0:0]
	for _, l := range links {
		if !drop(l) {
			out = append(out, l)
		}
	}
	return out
}

// normalizeEndpoint accepts "", "host", "host:port" or "[v6]:port".
func normalizeEndpoint(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if strings.ContainsAny(s, " /\t") {
		return "", fmt.Errorf("endpoint must be host or host:port")
	}
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		if strings.Contains(s, ":") {
			if _, perr := netip.ParseAddr(s); perr != nil {
				return "", fmt.Errorf("endpoint must be host or host:port")
			}
		}
		return s, nil
	}
	if host == "" {
		return "", fmt.Errorf("endpoint host required")
	}
	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		return "", fmt.Errorf("endpoint port must be 1-65535")
	}
	return s, nil
}
