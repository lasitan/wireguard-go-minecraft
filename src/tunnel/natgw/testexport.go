package natgw

import "net/netip"

func IsNestedUpstreamDial(packet []byte, upIP netip.Addr, upPort uint16) bool {
	return isNestedUpstreamDial(packet, upIP, upPort)
}
