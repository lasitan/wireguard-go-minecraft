package config_test

import (
	"fmt"
	"golang.zx2c4.com/wireguard/src/core"
	. "golang.zx2c4.com/wireguard/src/tunnel/config"
	"testing"
)

func TestTunnelRoutes(t *testing.T) {
	d := &core.DesiredConfig{
		Interface: core.DesiredIface{Address: "10.10.0.5/24"},
		Peers: []core.DesiredPeer{
			{AllowedIPs: []string{"10.10.0.0/24", "10.10.0.9/32"}},
			{AllowedIPs: []string{"10.20.0.0/24", "10.20.0.7/32", "192.168.1.0/24"}},
			{AllowedIPs: []string{"10.30.0.4/32"}},
		},
	}
	got := fmt.Sprint(TunnelRoutes(d))
	if got != "[10.20.0.0/24 10.30.0.4/32 192.168.1.0/24]" {
		t.Fatalf("routes: %s", got)
	}

	prev := &core.DesiredConfig{
		Interface: d.Interface,
		Peers:     []core.DesiredPeer{{AllowedIPs: []string{"10.20.0.0/24", "10.40.0.0/24"}}},
	}
	add, del := DiffRoutes(TunnelRoutes(d), TunnelRoutes(prev))
	if fmt.Sprint(add) != "[10.30.0.4/32 192.168.1.0/24]" || fmt.Sprint(del) != "[10.40.0.0/24]" {
		t.Fatalf("add=%v del=%v", add, del)
	}
}
