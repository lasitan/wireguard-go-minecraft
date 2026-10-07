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
	// ParentIDs attaches the node to several mothers at once (replaces ParentID's set).
	ParentIDs *[]string `json:"parentIds,omitempty"`
	// ClusterWith joins the cluster of that node and adopts its config; "" leaves.
	ClusterWith *string `json:"clusterWith,omitempty"`
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
			oldPfx, errOld := core.VPNPrefixFromAddress(n.Address)
			newPfx, _ := core.VPNPrefixFromAddress(addr)
			n.Address = addr
			n.AddressChangedAt = time.Now().UTC()
			// The node's own subnet route follows its address, like ReassignNodeSubnet.
			if p.Routes == nil && errOld == nil && oldPfx != newPfx {
				if n.Routes, err = core.ReplaceVPNRoute(n.Routes, oldPfx, newPfx); err != nil {
					return core.Node{}, err
				}
			}
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
	if p.ParentIDs != nil {
		if err := setParents(&m, id, *p.ParentIDs); err != nil {
			return core.Node{}, err
		}
	}
	what := clusterSync{
		port:    p.ListenPort != nil,
		routes:  p.Routes != nil,
		parents: p.ParentID != nil || p.ParentIDs != nil,
	}
	if what != (clusterSync{}) {
		if err := syncCluster(&m, id, what); err != nil {
			return core.Node{}, err
		}
	}
	if p.ClusterWith != nil {
		if peer := strings.TrimSpace(*p.ClusterWith); peer == "" {
			leaveCluster(n)
		} else if err := joinCluster(&m, id, peer); err != nil {
			return core.Node{}, err
		}
	}
	pruneClusters(&m)
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

func normalizeForwardProtocol(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "tcp":
		return "tcp"
	case "udp":
		return "udp"
	case "tcp/udp", "udp/tcp", "tcp,udp", "udp,tcp", "both":
		return "tcp/udp"
	default:
		return ""
	}
}

func expandForwardProtocols(p string) []string {
	if p == "tcp/udp" {
		return []string{"tcp", "udp"}
	}
	return []string{p}
}

// mergeForwardPairs collapses matching tcp+udp rules on the same listen/dest into tcp/udp.
func mergeForwardPairs(fwds []core.Forward) []core.Forward {
	type key struct {
		listen, dest string
		port         uint16
	}
	type pair struct {
		tcp, udp *core.Forward
		others   []core.Forward
	}
	by := map[key]*pair{}
	order := []key{}
	for i := range fwds {
		f := fwds[i]
		k := key{listen: f.Listen, dest: f.DestNodeID, port: f.DestPort}
		p, ok := by[k]
		if !ok {
			p = &pair{}
			by[k] = p
			order = append(order, k)
		}
		switch f.Protocol {
		case "tcp":
			cp := f
			p.tcp = &cp
		case "udp":
			cp := f
			p.udp = &cp
		default:
			p.others = append(p.others, f)
		}
	}
	out := make([]core.Forward, 0, len(fwds))
	for _, k := range order {
		p := by[k]
		out = append(out, p.others...)
		if p.tcp != nil && p.udp != nil {
			m := *p.tcp
			m.Protocol = "tcp/udp"
			out = append(out, m)
			continue
		}
		if p.tcp != nil {
			out = append(out, *p.tcp)
		}
		if p.udp != nil {
			out = append(out, *p.udp)
		}
	}
	return out
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
		f.Protocol = normalizeForwardProtocol(f.Protocol)
		f.Listen = strings.TrimSpace(f.Listen)
		if f.Protocol == "" {
			return nil, fmt.Errorf("forward %d: protocol must be tcp, udp, or tcp/udp", i+1)
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
		for _, p := range expandForwardProtocols(f.Protocol) {
			key := p + " " + f.Listen
			if _, dup := seen[key]; dup {
				return nil, fmt.Errorf("forward %d: duplicate %s listen %s", i+1, p, f.Listen)
			}
			seen[key] = struct{}{}
		}
		clean = append(clean, f)
	}
	clean = mergeForwardPairs(clean)
	var rest []core.Forward
	for _, f := range m.Forwards {
		if f.NodeID != id {
			rest = append(rest, f)
		}
	}
	m.Forwards = append(rest, clean...)
	if err := syncCluster(&m, id, clusterSync{forwards: true}); err != nil {
		return nil, err
	}
	m.Revision++
	if err := validateMesh(m); err != nil {
		return nil, err
	}
	if err := s.replaceMeshLocked(m); err != nil {
		return nil, err
	}
	return clean, nil
}
