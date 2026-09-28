package config

import (
	"net/netip"
	"sort"
	"strings"

	"golang.zx2c4.com/wireguard/src/core"
)

// tunnelRoutes lists peer AllowedIPs that fall outside the interface's own
// subnet (cross-subnet gateways, foreign hosts, LAN routes). The OS only sends
// the interface subnet into the tunnel by itself, so these need explicit routes.
func tunnelRoutes(d *core.DesiredConfig) []netip.Prefix {
	if d == nil || d.Interface.Address == "" {
		return nil
	}
	ifPfx, err := netip.ParsePrefix(strings.TrimSpace(d.Interface.Address))
	if err != nil {
		return nil
	}
	ifPfx = ifPfx.Masked()
	var all []netip.Prefix
	seen := map[netip.Prefix]bool{}
	for _, p := range d.Peers {
		for _, a := range p.AllowedIPs {
			pfx, err := netip.ParsePrefix(canonicalPrefix(strings.TrimSpace(a)))
			if err != nil {
				continue
			}
			pfx = pfx.Masked()
			if seen[pfx] || covers(ifPfx, pfx) {
				continue
			}
			seen[pfx] = true
			all = append(all, pfx)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Bits() < all[j].Bits() })
	var out []netip.Prefix
	for _, p := range all {
		redundant := false
		for _, q := range out {
			if covers(q, p) {
				redundant = true
				break
			}
		}
		if !redundant {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

func covers(outer, inner netip.Prefix) bool {
	return outer.Addr().Is4() == inner.Addr().Is4() && outer.Bits() <= inner.Bits() && outer.Contains(inner.Addr())
}

// diffRoutes returns what to add and what to remove to go from have to want.
func diffRoutes(want, have []netip.Prefix) (add, del []string) {
	w := map[netip.Prefix]bool{}
	for _, p := range want {
		w[p] = true
	}
	h := map[netip.Prefix]bool{}
	for _, p := range have {
		h[p] = true
		if !w[p] {
			del = append(del, p.String())
		}
	}
	for _, p := range want {
		if !h[p] {
			add = append(add, p.String())
		}
	}
	return add, del
}
