package core

import (
	"net"
	"strconv"
	"strings"
)

// IsMagnetParent reports whether n accepts inbound tunnels (a "mother" card):
// any node with a ListenPort can have child nodes attached to it.
func (n *Node) IsMagnetParent() bool {
	return n.ListenPort > 0
}

// DialEndpoint is the host:port other nodes dial to reach n. An explicit
// Endpoint wins (a bare host gets ListenPort appended); otherwise the
// Master-observed public IPv4 is combined with ListenPort. "" = not dialable.
func (n *Node) DialEndpoint() string {
	ep := strings.TrimSpace(n.Endpoint)
	if ep != "" {
		if _, _, err := net.SplitHostPort(ep); err == nil || n.ListenPort == 0 {
			return ep
		}
		return net.JoinHostPort(strings.Trim(ep, "[]"), strconv.Itoa(int(n.ListenPort)))
	}
	if n.ListenPort == 0 || n.PublicV4 == "" {
		return ""
	}
	return net.JoinHostPort(n.PublicV4, strconv.Itoa(int(n.ListenPort)))
}
