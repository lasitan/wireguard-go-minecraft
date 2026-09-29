package config

import (
	"net/netip"

	"golang.zx2c4.com/wireguard/src/core"
	"golang.zx2c4.com/wireguard/src/tunnel/spec"
	"golang.zx2c4.com/wireguard/src/wireguard/device"
)

func BuildNatGatewayResult(toNATClient bool, natUpstreamB64 string, listenPort uint16, netCfg IfaceNetConfig, peers []spec.PeerHookConfig) (spec.NatGatewayResult, []spec.PeerHookConfig, error) {
	return buildNatGatewayResult(toNATClient, natUpstreamB64, listenPort, netCfg, peers)
}

func ReadLivePeers(dev *device.Device) (map[string]*livePeer, error) {
	return readLivePeers(dev)
}

func Base64ToHex(b64 string) (string, error) {
	return base64ToHex(b64)
}

func TunnelRoutes(d *core.DesiredConfig) []netip.Prefix {
	return tunnelRoutes(d)
}

func DiffRoutes(want, have []netip.Prefix) (add, del []string) {
	return diffRoutes(want, have)
}

func (p *livePeer) AllowedList() []string {
	if p == nil {
		return nil
	}
	out := make([]string, 0, len(p.allowed))
	for a := range p.allowed {
		out = append(out, a)
	}
	return out
}
