package core

import (
	"fmt"
	"net/netip"
	"strings"
)

// VPNPrefixFromAddress returns the masked network prefix from a node VPN address CIDR.
func VPNPrefixFromAddress(addr string) (string, error) {
	p, err := netip.ParsePrefix(strings.TrimSpace(addr))
	if err != nil {
		return "", fmt.Errorf("address: %w", err)
	}
	if !p.Addr().Is4() {
		return "", fmt.Errorf("address must be IPv4")
	}
	return p.Masked().String(), nil
}

// NormalizeRouteCIDRs deduplicates and canonicalizes route prefixes.
func NormalizeRouteCIDRs(in []string) ([]string, error) {
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

// ReplaceVPNRoute swaps the VPN pool prefix in routes, keeping other CIDRs (e.g. LAN).
func ReplaceVPNRoute(routes []string, oldPrefix, newPrefix string) ([]string, error) {
	oldP, err := netip.ParsePrefix(oldPrefix)
	if err != nil {
		return nil, err
	}
	newP, err := netip.ParsePrefix(newPrefix)
	if err != nil {
		return nil, err
	}
	oldS := oldP.Masked().String()
	newS := newP.Masked().String()
	out := make([]string, 0, len(routes)+1)
	replaced := false
	for _, r := range routes {
		p, err := netip.ParsePrefix(r)
		if err != nil {
			return nil, err
		}
		if p.Masked().String() == oldS {
			if !replaced {
				out = append(out, newS)
				replaced = true
			}
			continue
		}
		out = append(out, p.Masked().String())
	}
	if !replaced {
		out = append(out, newS)
	}
	return NormalizeRouteCIDRs(out)
}

// InboundAllowedIPs is what remote peers accept for traffic sourced from this node.
func InboundAllowedIPs(n *Node) ([]string, error) {
	return NormalizeRouteCIDRs(n.Routes)
}

// OutboundAllowedIPs is what this node sends through a dial peer.
func OutboundAllowedIPs(local *Node, link Link) ([]string, error) {
	if len(link.AllowedIPs) > 0 {
		return NormalizeRouteCIDRs(link.AllowedIPs)
	}
	return NormalizeRouteCIDRs(local.Routes)
}

// EnsureDefaultRoutes fills empty Routes with the VPN prefix from Address. Returns true if changed.
func EnsureDefaultRoutes(m *Mesh) bool {
	changed := false
	for i := range m.Nodes {
		if len(m.Nodes[i].Routes) > 0 {
			continue
		}
		pfx, err := VPNPrefixFromAddress(m.Nodes[i].Address)
		if err != nil {
			continue
		}
		m.Nodes[i].Routes = []string{pfx}
		changed = true
	}
	return changed
}
