package core

// ClusterMembers returns the ids of every node in cluster c, in mesh order.
func (m *Mesh) ClusterMembers(c string) []string {
	if c == "" {
		return nil
	}
	var out []string
	for i := range m.Nodes {
		if m.Nodes[i].Cluster == c {
			out = append(out, m.Nodes[i].ID)
		}
	}
	return out
}

// ClusterStandby maps every unavailable cluster member (offline or disabled)
// to the first available member of its cluster. Members with no available
// sibling are left out.
func ClusterStandby(m *Mesh, alive func(id string) bool) map[string]string {
	up := map[string]string{}
	for i := range m.Nodes {
		n := &m.Nodes[i]
		if n.Cluster == "" || n.Disabled || (alive != nil && !alive(n.ID)) {
			continue
		}
		if _, ok := up[n.Cluster]; !ok {
			up[n.Cluster] = n.ID
		}
	}
	out := map[string]string{}
	for i := range m.Nodes {
		n := &m.Nodes[i]
		if n.Cluster == "" {
			continue
		}
		if n.Disabled || (alive != nil && !alive(n.ID)) {
			if sb := up[n.Cluster]; sb != "" {
				out[n.ID] = sb
			}
		}
	}
	return out
}
