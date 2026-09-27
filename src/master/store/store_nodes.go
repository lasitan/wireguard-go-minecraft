package store

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/src/core"
)

const (
	maxNodeNameLen = 64
	maxNodeRoutes  = 64
	maxNodeFwds    = 256
)

// NodePatch holds admin-editable node fields; nil means unchanged.
type NodePatch struct {
	Name    *string   `json:"name,omitempty"`
	Address *string   `json:"address,omitempty"`
	Enabled *bool     `json:"enabled,omitempty"`
	Routes  *[]string `json:"routes,omitempty"`
	// ListenPort > 0 makes the node a magnet "mother"; 0 clears it.
	ListenPort *uint16 `json:"listenPort,omitempty"`
	// Endpoint overrides the auto (public IPv4 + ListenPort) dial address.
	Endpoint *string `json:"endpoint,omitempty"`
	// ParentID attaches the node under a mother; "" detaches.
	ParentID *string `json:"parentId,omitempty"`
}

func normalizeAddress(s string) (string, error) {
	p, err := netip.ParsePrefix(strings.TrimSpace(s))
	if err != nil {
		return "", fmt.Errorf("address must be CIDR like 10.10.0.5/24: %w", err)
	}
	if !p.Addr().Is4() {
		return "", fmt.Errorf("address must be IPv4")
	}
	if p.Bits() >= 31 {
		return "", fmt.Errorf("address prefix too narrow")
	}
	return p.String(), nil
}

func normalizeRoutes(in []string) ([]string, error) {
	if len(in) > maxNodeRoutes {
		return nil, fmt.Errorf("at most %d routes", maxNodeRoutes)
	}
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, r := range in {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		p, err := netip.ParsePrefix(r)
		if err != nil {
			return nil, fmt.Errorf("route %q: %w", r, err)
		}
		p = p.Masked()
		if p.Bits() == 0 {
			return nil, fmt.Errorf("route %q: default route is not allowed", r)
		}
		s := p.String()
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out, nil
}

// PatchNode applies admin edits to one node and bumps the revision.
func (s *Store) PatchNode(id string, p NodePatch) (core.Node, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.loadMeshLocked()
	if err != nil {
		return core.Node{}, err
	}
	n := m.FindNode(id)
	if n == nil {
		return core.Node{}, fmt.Errorf("node %q not found", id)
	}
	if p.Name != nil {
		name := strings.TrimSpace(*p.Name)
		if len(name) > maxNodeNameLen {
			return core.Node{}, fmt.Errorf("name longer than %d", maxNodeNameLen)
		}
		n.Name = name
	}
	if p.Address != nil {
		addr, err := normalizeAddress(*p.Address)
		if err != nil {
			return core.Node{}, err
		}
		if addr != n.Address {
			n.Address = addr
			n.AddressChangedAt = time.Now().UTC()
		}
	}
	if p.Enabled != nil {
		n.Disabled = !*p.Enabled
	}
	if p.Routes != nil {
		routes, err := normalizeRoutes(*p.Routes)
		if err != nil {
			return core.Node{}, err
		}
		n.Routes = routes
	}
	if p.Endpoint != nil {
		ep, err := normalizeEndpoint(*p.Endpoint)
		if err != nil {
			return core.Node{}, err
		}
		n.Endpoint = ep
	}
	if p.ListenPort != nil {
		setListenPort(&m, n, *p.ListenPort)
	}
	if p.ParentID != nil {
		if err := attachTo(&m, id, strings.TrimSpace(*p.ParentID)); err != nil {
			return core.Node{}, err
		}
	}
	out := *n
	m.Revision++
	if err := validateMesh(m); err != nil {
		return core.Node{}, err
	}
	if err := s.replaceMeshLocked(m); err != nil {
		return core.Node{}, err
	}
	return out, nil
}

func (s *Store) NodeForwards(id string) ([]core.Forward, error) {
	m := s.Snapshot()
	if m.FindNode(id) == nil {
		return nil, fmt.Errorf("node %q not found", id)
	}
	out := []core.Forward{}
	for _, f := range m.Forwards {
		if f.NodeID == id {
			out = append(out, f)
		}
	}
	return out, nil
}

func validListen(listen string) bool {
	port := listen
	if strings.Contains(listen, ":") {
		host, p, ok := strings.Cut(listen, ":")
		if !ok || host == "" {
			return false
		}
		if _, err := netip.ParseAddr(host); err != nil {
			return false
		}
		port = p
	}
	n, err := strconv.Atoi(port)
	return err == nil && n > 0 && n <= 65535
}

// PutNodeForwards replaces the forwards listening on node id.
func (s *Store) PutNodeForwards(id string, fwds []core.Forward) ([]core.Forward, error) {
	if len(fwds) > maxNodeFwds {
		return nil, fmt.Errorf("at most %d forwards", maxNodeFwds)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.loadMeshLocked()
	if err != nil {
		return nil, err
	}
	if m.FindNode(id) == nil {
		return nil, fmt.Errorf("node %q not found", id)
	}
	seen := map[string]struct{}{}
	clean := make([]core.Forward, 0, len(fwds))
	for i, f := range fwds {
		f.NodeID = id
		f.Protocol = strings.ToLower(strings.TrimSpace(f.Protocol))
		f.Listen = strings.TrimSpace(f.Listen)
		if f.Protocol != "tcp" && f.Protocol != "udp" {
			return nil, fmt.Errorf("forward %d: protocol must be tcp or udp", i+1)
		}
		if !validListen(f.Listen) {
			return nil, fmt.Errorf("forward %d: listen must be port or ip:port", i+1)
		}
		if f.DestPort == 0 {
			return nil, fmt.Errorf("forward %d: destPort required", i+1)
		}
		if m.FindNode(f.DestNodeID) == nil {
			return nil, fmt.Errorf("forward %d: unknown destination node", i+1)
		}
		key := f.Protocol + " " + f.Listen
		if _, dup := seen[key]; dup {
			return nil, fmt.Errorf("forward %d: duplicate %s listen %s", i+1, f.Protocol, f.Listen)
		}
		seen[key] = struct{}{}
		clean = append(clean, f)
	}
	var rest []core.Forward
	for _, f := range m.Forwards {
		if f.NodeID != id {
			rest = append(rest, f)
		}
	}
	m.Forwards = append(rest, clean...)
	m.Revision++
	if err := validateMesh(m); err != nil {
		return nil, err
	}
	if err := s.replaceMeshLocked(m); err != nil {
		return nil, err
	}
	return clean, nil
}
