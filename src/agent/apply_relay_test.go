package agent

import (
	"testing"

	"golang.zx2c4.com/wireguard/src/core"
)

func TestResolveRelayEndpoint(t *testing.T) {
	a := newApplier(nil, nil, "", nil, nil, "https://mc.example.com:8443")
	d := &core.DesiredConfig{Peers: []core.DesiredPeer{
		{PublicKey: "A", Endpoint: "1.1.1.1:25590"},
		{PublicKey: "R", Endpoint: (&core.Relay{Port: 25599}).Endpoint()},
	}}
	got := a.resolveRelay(d)
	if got.Peers[0].Endpoint != "1.1.1.1:25590" || got.Peers[1].Endpoint != "mc.example.com:25599" {
		t.Fatalf("resolved: %+v", got.Peers)
	}
	if d.Peers[1].Endpoint != "@master:25599" {
		t.Fatal("input must not be mutated")
	}
	v6 := newApplier(nil, nil, "", nil, nil, "http://[2001:db8::1]:8443").resolveRelay(d)
	if v6.Peers[1].Endpoint != "[2001:db8::1]:25599" {
		t.Fatalf("v6: %s", v6.Peers[1].Endpoint)
	}
}
