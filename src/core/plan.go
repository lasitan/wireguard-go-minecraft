package core

import (
	"net/netip"
	"sort"
)

// PathChoices records, per node and per VPN subnet, which mother card the node
// uses as its gateway into that subnet (Master picks the lowest-RTT candidate).
type PathChoices map[string]map[string]string

const (
	childKeepalive  = 5
	motherKeepalive = 25
)

type planNode struct {
	n       *Node
	subnet  netip.Prefix
	host    netip.Prefix
	mother  bool
	exposed bool
}

type planPeer struct {
	peerID    string
	dial      bool
	keepalive int
	allowed   []netip.Prefix
	seen      map[netip.Prefix]struct{}
}

func (p *planPeer) allow(pfx netip.Prefix) {
	if !pfx.IsValid() {
		return
	}
	if _, ok := p.seen[pfx]; ok {
		return
	}
	p.seen[pfx] = struct{}{}
	p.allowed = append(p.allowed, pfx)
}

// Plan is the mesh-wide routing decision every node's desired config is cut from.
//
// Model:
//   - A mother (ListenPort > 0) serves its VPN subnet; same-subnet mothers mesh
//     with each other so their children can reach one another.
//   - To reach a VPN subnet listed in its Routes, a node picks one gateway mother
//     in that subnet: among the mothers it is explicitly attached to, else among
//     every exposed mother there. Every candidate stays connected (so its RTT is
//     measured and failover is instant); only the chosen one carries the prefix.
//   - Same-subnet access also needs a connection to the target's mother: a child
//     connects to every dialable mother in its subnet, reaches a mother target
//     directly and a child target through one of that child's own mothers.
//     The subnet route itself rides the fastest mother as a fallback.
//   - A child may be attached to several mothers; each of them delivers to it.
//   - On every mother each foreign /32 is owned by exactly one peer, so WireGuard's
//     source check and return routing agree.
type Plan struct {
	nodes     map[string]*planNode
	order     []string
	peers     map[string]map[string]*planPeer
	peerOrder map[string][]string
	gw        map[string]map[netip.Prefix]string
	cands     map[string]map[netip.Prefix][]string
}

// Candidates returns, per node and subnet, the mothers the node may use as gateway.
func (p *Plan) Candidates() map[string]map[string][]string {
	out := make(map[string]map[string][]string, len(p.cands))
	for id, bySub := range p.cands {
		m := make(map[string][]string, len(bySub))
		for s, c := range bySub {
			m[s.String()] = append([]string(nil), c...)
		}
		out[id] = m
	}
	return out
}

// Gateways returns the gateway actually used per node and subnet.
func (p *Plan) Gateways() PathChoices {
	out := PathChoices{}
	for id, bySub := range p.gw {
		m := make(map[string]string, len(bySub))
		for s, g := range bySub {
			m[s.String()] = g
		}
		out[id] = m
	}
	return out
}

func hostPrefix(addr string) (netip.Prefix, netip.Prefix, bool) {
	p, err := netip.ParsePrefix(addr)
	if err != nil || !p.Addr().Is4() {
		return netip.Prefix{}, netip.Prefix{}, false
	}
	return p.Masked(), netip.PrefixFrom(p.Addr(), 32), true
}

// PlanMesh computes the routing plan for every active node.
func PlanMesh(mesh *Mesh) *Plan {
	p := &Plan{
		nodes:     map[string]*planNode{},
		peers:     map[string]map[string]*planPeer{},
		peerOrder: map[string][]string{},
		gw:        map[string]map[netip.Prefix]string{},
		cands:     map[string]map[netip.Prefix][]string{},
	}
	losers := ConflictLosers(mesh)
	mothersIn := map[netip.Prefix][]string{}
	for i := range mesh.Nodes {
		n := &mesh.Nodes[i]
		if n.Disabled || losers[n.ID] {
			continue
		}
		sub, host, ok := hostPrefix(n.Address)
		if !ok {
			continue
		}
		pn := &planNode{n: n, subnet: sub, host: host, mother: n.IsMagnetParent(), exposed: n.DialEndpoint() != ""}
		p.nodes[n.ID] = pn
		p.order = append(p.order, n.ID)
		if pn.mother {
			mothersIn[sub] = append(mothersIn[sub], n.ID)
		}
	}
	// Master's relay is a pseudo peer outside p.order: it never dials and is
	// only used when a node picked it as a cross-subnet gateway.
	relayOn := mesh.Relay != nil && mesh.Relay.PublicKey != "" && mesh.Relay.Port > 0
	if relayOn {
		p.nodes[RelayNodeID] = &planNode{n: &Node{ID: RelayNodeID, PublicKey: mesh.Relay.PublicKey}, exposed: true}
	}

	// Explicit links: magnet attachments (child -> mother) and generic dial links.
	attached := map[string][]string{}
	type genericLink struct {
		from, to string
		link     Link
	}
	var generic []genericLink
	for _, l := range mesh.Links {
		from, to := p.nodes[l.FromNodeID], p.nodes[l.ToNodeID]
		if from == nil || to == nil || from == to {
			continue
		}
		if !from.mother && to.mother {
			attached[from.n.ID] = appendUnique(attached[from.n.ID], to.n.ID)
			continue
		}
		generic = append(generic, genericLink{from: from.n.ID, to: to.n.ID, link: l})
	}
	isAttached := func(child, mother string) bool {
		for _, m := range attached[child] {
			if m == mother {
				return true
			}
		}
		return false
	}

	// Gateway candidates and choices per (node, VPN subnet in its Routes).
	lanRoutes := map[string][]netip.Prefix{}
	for _, id := range p.order {
		pn := p.nodes[id]
		routes, _ := NormalizeRouteCIDRs(pn.n.Routes)
		for _, r := range routes {
			pfx := netip.MustParsePrefix(r)
			if !p.isVPNSubnet(pfx) {
				lanRoutes[id] = append(lanRoutes[id], pfx)
				continue
			}
			mothers := mothersIn[pfx]
			if pn.mother && pn.subnet == pfx {
				continue
			}
			var cands []string
			for _, m := range mothers {
				if m != id && p.nodes[m].exposed && isAttached(id, m) {
					cands = append(cands, m)
				}
			}
			if len(cands) == 0 {
				for _, m := range mothers {
					if m != id && p.nodes[m].exposed {
						cands = append(cands, m)
					}
				}
			}
			// Last resort for cross-subnet only: Master's relay.
			if relayOn && pfx != pn.subnet {
				cands = append(cands, RelayNodeID)
			}
			if len(cands) == 0 {
				continue
			}
			chosen := cands[0]
			if want := mesh.Paths[id][pfx.String()]; want != "" && contains(cands, want) {
				chosen = want
			}
			setNested(p.cands, id, pfx, cands)
			setNested(p.gw, id, pfx, chosen)
		}
	}

	// Same-subnet mothers mesh: the lower id dials when the other is exposed.
	meshEdge := func(a, b string) bool {
		pa, pb := p.nodes[a], p.nodes[b]
		return pa != nil && pb != nil && a != b && pa.mother && pb.mother && pa.subnet == pb.subnet && (pa.exposed || pb.exposed)
	}

	// Dial intents: from -> to.
	type intent struct{ from, to string }
	intents := map[intent]int{}
	addIntent := func(from, to string, ka int) {
		k := intent{from, to}
		if cur, ok := intents[k]; !ok || (ka > 0 && (cur == 0 || ka < cur)) {
			intents[k] = ka
		}
	}
	for child, ms := range attached {
		for _, m := range ms {
			addIntent(child, m, childKeepalive)
		}
	}
	for _, id := range p.order {
		ka := childKeepalive
		if p.nodes[id].mother {
			ka = motherKeepalive
		}
		for _, sub := range sortedCands(p.cands[id]) {
			for _, c := range p.cands[id][sub] {
				if c == RelayNodeID && p.gw[id][sub] != RelayNodeID {
					continue // the relay stays idle until it is actually needed
				}
				addIntent(id, c, ka)
			}
		}
		// Same-subnet access goes through the target's own mother (or the target
		// itself when it is a mother), so connect to every dialable mother here.
		if pn := p.nodes[id]; !pn.mother && p.gw[id][pn.subnet] != "" {
			for _, m := range mothersIn[pn.subnet] {
				if m != id && p.nodes[m].exposed {
					addIntent(id, m, childKeepalive)
				}
			}
		}
	}
	for _, ms := range mothersIn {
		for i := 0; i < len(ms); i++ {
			for j := i + 1; j < len(ms); j++ {
				if !meshEdge(ms[i], ms[j]) {
					continue
				}
				a, b := ms[i], ms[j]
				if a > b {
					a, b = b, a
				}
				if p.nodes[b].exposed {
					addIntent(a, b, motherKeepalive)
				} else {
					addIntent(b, a, motherKeepalive)
				}
			}
		}
	}
	for _, g := range generic {
		ka := g.link.Keepalive
		if ka == 0 && g.from != "" && !p.nodes[g.from].mother {
			ka = childKeepalive
		}
		addIntent(g.from, g.to, ka)
	}
	// Anyone routing into a subnet via the relay needs that subnet's nodes on
	// the relay too, or it has nowhere to deliver.
	relayServed := map[netip.Prefix]bool{}
	for _, id := range p.order {
		for sub, g := range p.gw[id] {
			if g == RelayNodeID {
				relayServed[sub] = true
			}
		}
	}
	for _, id := range p.order {
		if relayServed[p.nodes[id].subnet] {
			addIntent(id, RelayNodeID, childKeepalive)
		}
	}

	// Resolve each unordered pair to one dialer.
	keys := make([]intent, 0, len(intents))
	for k := range intents {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].from != keys[j].from {
			return keys[i].from < keys[j].from
		}
		return keys[i].to < keys[j].to
	})
	for _, k := range keys {
		if _, done := p.peerOf(k.from, k.to); done {
			continue
		}
		dialer, target := k.from, k.to
		if _, both := intents[intent{k.to, k.from}]; both {
			fwdOK, revOK := p.nodes[k.to].exposed, p.nodes[k.from].exposed
			if (revOK && !fwdOK) || (revOK == fwdOK && k.to < k.from) {
				dialer, target = k.to, k.from
			}
		}
		ka := intents[intent{dialer, target}]
		d := p.ensurePeer(dialer, target)
		d.dial = true
		d.keepalive = ka
		p.ensurePeer(target, dialer)
	}

	// owner[M][z] = the peer on mother M that carries z's /32.
	owner := map[string]map[string]string{}
	for _, mid := range p.order {
		m := p.nodes[mid]
		if !m.mother {
			continue
		}
		own := map[string]string{}
		for _, zid := range p.order {
			if zid == mid {
				continue
			}
			z := p.nodes[zid]
			if z.mother && z.subnet == m.subnet {
				if meshEdge(mid, zid) {
					own[zid] = zid
				}
				continue
			}
			g := p.gw[zid][m.subnet]
			_, dialsHere := intents[intent{zid, mid}]
			switch {
			case g == mid || (isAttached(zid, mid) && m.exposed) || (z.subnet == m.subnet && dialsHere):
				own[zid] = zid
			case g != "" && meshEdge(mid, g):
				own[zid] = g
			}
		}
		owner[mid] = own
	}
	// homes(t) = the mothers a child lives under: its dialable attachments in
	// its own subnet, else the gateway it picked there.
	homes := func(tid string) []string {
		t := p.nodes[tid]
		var out []string
		for _, m := range attached[tid] {
			if pm := p.nodes[m]; pm.exposed && pm.subnet == t.subnet {
				out = append(out, m)
			}
		}
		if len(out) == 0 {
			if g := p.gw[tid][t.subnet]; g != "" {
				out = append(out, g)
			}
		}
		return out
	}

	// Allowed IPs.
	lanClaimed := map[string]map[netip.Prefix]bool{}
	for _, id := range p.order {
		pn := p.nodes[id]
		if pn.mother {
			for _, zid := range p.order {
				peerID, ok := owner[id][zid]
				if !ok {
					continue
				}
				pp, _ := p.peerOf(id, peerID)
				if pp == nil {
					continue
				}
				pp.allow(p.nodes[zid].host)
				if peerID == zid && p.gw[zid][pn.subnet] == id {
					for _, lan := range lanRoutes[zid] {
						if lanClaimed[id] == nil {
							lanClaimed[id] = map[netip.Prefix]bool{}
						}
						if !lanClaimed[id][lan] {
							lanClaimed[id][lan] = true
							pp.allow(lan)
						}
					}
				}
			}
		}
		for _, sub := range sortedCands(p.gw[id]) {
			g := p.gw[id][sub]
			pp, _ := p.peerOf(id, g)
			if pp == nil {
				continue
			}
			pp.allow(sub)
			if pn.mother || sub != pn.subnet {
				continue
			}
			// Same subnet: reach a mother directly and a child through one of its
			// own mothers (the shared fastest one when possible). Targets whose
			// mother is not dialable fall back to the subnet route via g.
			for _, tid := range p.order {
				t := p.nodes[tid]
				if tid == id || tid == g || t.subnet != sub {
					continue
				}
				if t.mother {
					if tp, _ := p.peerOf(id, tid); tp != nil {
						tp.allow(t.host)
					}
					continue
				}
				hs := homes(tid)
				if contains(hs, g) {
					continue
				}
				via := ""
				if th := p.gw[tid][t.subnet]; contains(hs, th) {
					if tp, _ := p.peerOf(id, th); tp != nil {
						via = th
					}
				}
				for _, h := range hs {
					if via != "" {
						break
					}
					if tp, _ := p.peerOf(id, h); tp != nil {
						via = h
					}
				}
				if via != "" {
					tp, _ := p.peerOf(id, via)
					tp.allow(t.host)
				}
			}
		}
		if !pn.mother {
			if home := p.gw[id][pn.subnet]; home != "" {
				if pp, _ := p.peerOf(id, home); pp != nil {
					for _, lan := range lanRoutes[id] {
						pp.allow(lan)
					}
				}
			}
		}
	}
	// Cross-subnet child pairs must use one gateway in both directions or
	// WireGuard's source check drops the replies. The pair rides a real mother
	// over the relay, else the lower id's choice; the other end pins a /32.
	pairVia := func(x, y string) string {
		gx, gy := p.gw[x][p.nodes[y].subnet], p.gw[y][p.nodes[x].subnet]
		switch {
		case gx == "" || gy == "":
			return gx + gy
		case gx == RelayNodeID && gy != RelayNodeID:
			return gy
		case gy == RelayNodeID && gx != RelayNodeID:
			return gx
		case x < y:
			return gx
		}
		return gy
	}
	for _, x := range p.order {
		px := p.nodes[x]
		if px.mother {
			continue
		}
		for _, y := range p.order {
			py := p.nodes[y]
			if py.mother || py.subnet == px.subnet {
				continue
			}
			via := pairVia(x, y)
			if via == "" || via == p.gw[x][py.subnet] {
				continue
			}
			if pp, _ := p.peerOf(x, via); pp != nil {
				pp.allow(py.host)
			}
		}
	}
	for _, pid := range p.peerOrder[RelayNodeID] {
		p.peers[RelayNodeID][pid].allow(p.nodes[pid].host)
	}
	for _, g := range generic {
		fp, _ := p.peerOf(g.from, g.to)
		tp, _ := p.peerOf(g.to, g.from)
		if fp != nil {
			if len(g.link.AllowedIPs) > 0 {
				ips, _ := NormalizeRouteCIDRs(g.link.AllowedIPs)
				for _, s := range ips {
					fp.allow(netip.MustParsePrefix(s))
				}
			} else {
				fp.allow(p.nodes[g.to].host)
			}
		}
		if tp != nil && !p.nodes[g.to].mother {
			tp.allow(p.nodes[g.from].host)
		}
	}
	return p
}

func (p *Plan) isVPNSubnet(pfx netip.Prefix) bool {
	for _, id := range p.order {
		if p.nodes[id].subnet == pfx {
			return true
		}
	}
	return false
}

func (p *Plan) peerOf(a, b string) (*planPeer, bool) {
	pp := p.peers[a][b]
	return pp, pp != nil
}

func (p *Plan) ensurePeer(a, b string) *planPeer {
	if p.peers[a] == nil {
		p.peers[a] = map[string]*planPeer{}
	}
	pp := p.peers[a][b]
	if pp == nil {
		pp = &planPeer{peerID: b, seen: map[netip.Prefix]struct{}{}}
		p.peers[a][b] = pp
		p.peerOrder[a] = append(p.peerOrder[a], b)
	}
	return pp
}

// RelayPeers is the peer list for Master's relay (empty when nobody needs it).
func (p *Plan) RelayPeers() []DesiredPeer {
	var out []DesiredPeer
	for _, pp := range p.peersFor(RelayNodeID) {
		pn := p.nodes[pp.peerID]
		if pn == nil {
			continue
		}
		dp := DesiredPeer{PublicKey: pn.n.PublicKey, AllowedIPs: make([]string, 0, len(pp.allowed))}
		for _, a := range pp.allowed {
			dp.AllowedIPs = append(dp.AllowedIPs, a.String())
		}
		out = append(out, dp)
	}
	return out
}

// peersFor returns n's peers in mesh order.
func (p *Plan) peersFor(id string) []*planPeer {
	ids := append([]string(nil), p.peerOrder[id]...)
	idx := map[string]int{}
	for i, nid := range p.order {
		idx[nid] = i
	}
	sort.SliceStable(ids, func(i, j int) bool { return idx[ids[i]] < idx[ids[j]] })
	out := make([]*planPeer, 0, len(ids))
	for _, pid := range ids {
		out = append(out, p.peers[id][pid])
	}
	return out
}

func setNested[T any](m map[string]map[netip.Prefix]T, id string, pfx netip.Prefix, v T) {
	if m[id] == nil {
		m[id] = map[netip.Prefix]T{}
	}
	m[id][pfx] = v
}

func sortedCands[T any](m map[netip.Prefix]T) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

func appendUnique(list []string, v string) []string {
	if contains(list, v) {
		return list
	}
	return append(list, v)
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
