package agent_test

import (
	. "golang.zx2c4.com/wireguard/src/agent"
	"golang.zx2c4.com/wireguard/src/core"
	"testing"
)

func TestResolveRelayEndpoint(t *testing.T) {
	a := NewApplier(nil, nil, "", nil, nil, "https://mc.example.com:8443")
	d := &core.DesiredConfig{Peers: []core.DesiredPeer{
		{PublicKey: "A", Endpoint: "1.1.1.1:25590"},
		{PublicKey: "R", Endpoint: (&core.Relay{Port: 25599}).Endpoint()},
	}}
	got := a.ResolveRelay(d)
	if got.Peers[0].Endpoint != "1.1.1.1:25590" || got.Peers[1].Endpoint != "mc.example.com:25599" {
		t.Fatalf("resolved: %+v", got.Peers)
	}
	if d.Peers[1].Endpoint != "@master:25599" {
		t.Fatal("input must not be mutated")
	}
	v6 := NewApplier(nil, nil, "", nil, nil, "http://[2001:db8::1]:8443").ResolveRelay(d)
	if v6.Peers[1].Endpoint != "[2001:db8::1]:25599" {
		t.Fatalf("v6: %s", v6.Peers[1].Endpoint)
	}
}
