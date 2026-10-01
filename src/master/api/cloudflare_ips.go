package api

import (
	"net/netip"
	"sync"
)

// Cloudflare published edge ranges (https://www.cloudflare.com/ips/).
// Used to decide when CF-Connecting-IP is trustworthy, and to refuse
// banning edge addresses if client headers are missing.
// Update periodically from Cloudflare's ips-v4 / ips-v6 lists.
var cloudflareCIDRs = []string{
	// IPv4
	"173.245.48.0/20",
	"103.21.244.0/22",
	"103.22.200.0/22",
	"103.31.4.0/22",
	"141.101.64.0/18",
	"108.162.192.0/18",
	"190.93.240.0/20",
	"188.114.96.0/20",
	"197.234.240.0/22",
	"198.41.128.0/17",
	"162.158.0.0/15",
	"104.16.0.0/13",
	"104.24.0.0/14",
	"172.64.0.0/13",
	"131.0.72.0/22",
	// IPv6
	"2400:cb00::/32",
	"2606:4700::/32",
	"2803:f800::/32",
	"2405:b500::/32",
	"2405:8100::/32",
	"2a06:98c0::/29",
	"2c0f:f248::/32",
}

var (
	cfPrefixesOnce sync.Once
	cfPrefixes     []netip.Prefix
)

func cloudflarePrefixes() []netip.Prefix {
	cfPrefixesOnce.Do(func() {
		out := make([]netip.Prefix, 0, len(cloudflareCIDRs))
		for _, s := range cloudflareCIDRs {
			p, err := netip.ParsePrefix(s)
			if err != nil {
				continue
			}
			out = append(out, p)
		}
		cfPrefixes = out
	})
	return cfPrefixes
}

func isCloudflareIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	for _, p := range cloudflarePrefixes() {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

func parseIPString(s string) (netip.Addr, bool) {
	ip, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, false
	}
	return ip.Unmap(), true
}

// isBanableAuthIP reports whether ip may be permanently banned.
// Cloudflare edge addresses must never be banned: with Pseudo IPv4
// Overwrite Headers, a missing/invalid client header would otherwise
// lock out every visitor sharing that edge.
func isBanableAuthIP(ip string) bool {
	addr, ok := parseIPString(ip)
	if !ok || !addr.IsValid() {
		return false
	}
	if addr.IsLoopback() || addr.IsPrivate() || addr.IsUnspecified() || addr.IsLinkLocalUnicast() {
		return false
	}
	if isCloudflareIP(addr) {
		return false
	}
	return true
}
