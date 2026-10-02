package core_test

import (
	. "golang.zx2c4.com/wireguard/src/core"
	"testing"
)

func TestReplaceVPNRoute(t *testing.T) {
	got, err := ReplaceVPNRoute([]string{"10.10.0.0/24", "192.168.1.0/24"}, "10.10.0.0/24", "10.20.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "10.20.0.0/24" || got[1] != "192.168.1.0/24" {
		t.Fatalf("got %v", got)
	}
}

func TestOutboundRequiresLocalRoutes(t *testing.T) {
	m := policyMesh()
	m.Nodes[1].Routes = nil
	m.Nodes[2].Routes = nil
	c1, err := CompileDesired(&m, tC1, DesiredDefaults{})
	if err != nil {
		t.Fatal(err)
	}
	if len(c1.Peers) != 1 || !sameSet(c1.Peers[0].AllowedIPs, "10.10.0.1/32") {
		t.Fatalf("client with no routes still keeps a host /32 to its mother: %v", c1.Peers[0].AllowedIPs)
	}
}

func TestUnknownCIDRGoesViaHomeMother(t *testing.T) {
	m := policyMesh()
	m.Nodes[1].Routes = []string{"10.10.0.0/24", "10.20.0.0/24"}
	c1, err := CompileDesired(&m, tC1, DesiredDefaults{})
	if err != nil {
		t.Fatal(err)
	}
	if len(c1.Peers) != 1 || !sameSet(c1.Peers[0].AllowedIPs, "10.10.0.0/24", "10.20.0.0/24", "10.10.0.1/32") {
		t.Fatalf("non-VPN CIDR is a LAN route via the home mother: %+v", c1.Peers)
	}
}

func TestVPNSubnetWithoutExposedMotherIsDropped(t *testing.T) {
	m := crossMesh()
	m.Nodes[1].Endpoint = ""
	m.Nodes[2].Endpoint = ""
	c := peerMap(t, &m, pC)
	if len(c) != 1 || !sameSet(c["A1"].AllowedIPs, "10.10.0.0/24", "10.20.0.9/32", "10.10.0.1/32") {
		t.Fatalf("no subnet route to 10.20, only Y (who reaches us via A1) back through A1: %+v", c)
	}
}
