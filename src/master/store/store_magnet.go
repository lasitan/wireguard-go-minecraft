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
// attaches to at most one mother via a single outbound link, and Master
// pushes that mother's DialEndpoint to it.

const magnetKeepalive = 5

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
	child := m.FindNode(childID)
	if child == nil {
		return fmt.Errorf("node %q not found", childID)
	}
	if parentID != "" {
		if child.IsMagnetParent() {
			return fmt.Errorf("a node with a listen port cannot attach to another node")
		}
		parent := m.FindNode(parentID)
		if parent == nil || parent.ID == childID {
			return fmt.Errorf("parent %q not found", parentID)
		}
		if !parent.IsMagnetParent() {
			return fmt.Errorf("parent %q has no listen port", parentID)
		}
	}
	m.Links = dropLinks(m.Links, func(l core.Link) bool { return l.FromNodeID == childID })
	if parentID != "" {
		m.Links = append(m.Links, core.Link{FromNodeID: childID, ToNodeID: parentID, Keepalive: magnetKeepalive})
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
