// Package netaddr holds small IP address helpers shared across modules.
package netaddr

import "net/netip"

// IsPublicAddr reports whether a is a globally routable unicast address.
func IsPublicAddr(a netip.Addr) bool {
	a = a.Unmap()
	return a.IsValid() && a.IsGlobalUnicast() && !a.IsPrivate() && !a.IsLoopback() && !a.IsLinkLocalUnicast()
}
