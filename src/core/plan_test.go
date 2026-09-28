package core

import (
	"reflect"
	"sort"
	"testing"
)

const (
	pA1 = "00000000-0000-4000-8000-0000000000a1" // mother 10.10
	pA2 = "00000000-0000-4000-8000-0000000000a2" // mother 10.10
	pB1 = "00000000-0000-4000-8000-0000000000b1" // mother 10.20
	pB2 = "00000000-0000-4000-8000-0000000000b2" // mother 10.20
	pC  = "00000000-0000-4000-8000-0000000000c1" // child 10.10
	pX  = "00000000-0000-4000-8000-0000000000c2" // child 10.10
	pY  = "00000000-0000-4000-8000-0000000000c3" // child 10.20
)

func mother(id, pub, addr, ep string) Node {
	pfx, _ := VPNPrefixFromAddress(addr)
	return Node{ID: id, Role: RoleServer, PublicKey: pub, PrivateKey: "k", Address: addr, ListenPort: 25590, Endpoint: ep, Routes: []string{pfx}, Token: id}
}

func child(id, pub, addr string, routes ...string) Node {
	return Node{ID: id, Role: RoleClient, PublicKey: pub, PrivateKey: "k", Address: addr, Routes: routes, Token: id}
}

func peerMap(t *testing.T, m *Mesh, id string) map[string]DesiredPeer {
	t.Helper()
	d, err := CompileDesired(m, id, DesiredDefaults{})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]DesiredPeer{}
	for _, p := range d.Peers {
		if _, dup := out[p.PublicKey]; dup {
			t.Fatalf("duplicate peer %s on %s", p.PublicKey, id)
		}
		out[p.PublicKey] = p
	}
	return out
}

func sameSet(a []string, b ...string) bool {
	x := append([]string(nil), a...)
	y := append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	if len(x) == 0 && len(y) == 0 {
		return true
	}
	return reflect.DeepEqual(x, y)
}

func TestMotherChildrenDoNotOverlap(t *testing.T) {
	m := policyMesh()
	srv := peerMap(t, &m, tSrv)
	if !sameSet(srv["C1"].AllowedIPs, "10.10.0.2/32") || !sameSet(srv["C2"].AllowedIPs, "10.10.0.3/32") {
		t.Fatalf("mother must route each child by /32: %+v", srv)
	}
}

func crossMesh() Mesh {
	return Mesh{
		Revision: 1,
		Nodes: []Node{
			mother(pA1, "A1", "10.10.0.1/24", "1.1.1.1"),
			mother(pB1, "B1", "10.20.0.1/24", "2.2.2.1"),
			mother(pB2, "B2", "10.20.0.2/24", "2.2.2.2"),
			child(pC, "C", "10.10.0.5/24", "10.10.0.0/24", "10.20.0.0/24"),
			child(pY, "Y", "10.20.0.9/24", "10.20.0.0/24", "10.10.0.0/24"),
		},
		Links: []Link{{FromNodeID: pC, ToNodeID: pA1}, {FromNodeID: pY, ToNodeID: pB1}},
	}
}

func TestCrossSubnetConnectsEveryCandidateButRoutesViaChosen(t *testing.T) {
	m := crossMesh()
	m.Paths = PathChoices{pC: {"10.20.0.0/24": pB2}}

	c := peerMap(t, &m, pC)
	if len(c) != 3 {
		t.Fatalf("child should dial A1 plus both 10.20 mothers: %+v", c)
	}
	if !sameSet(c["A1"].AllowedIPs, "10.10.0.0/24") || c["A1"].Endpoint != "1.1.1.1:25590" {
		t.Fatalf("home mother: %+v", c["A1"])
	}
	if !sameSet(c["B2"].AllowedIPs, "10.20.0.0/24") || c["B2"].Endpoint != "2.2.2.2:25590" {
		t.Fatalf("chosen gateway must carry the subnet: %+v", c["B2"])
	}
	if !sameSet(c["B1"].AllowedIPs) || c["B1"].Endpoint == "" {
		t.Fatalf("other candidate stays connected without routes: %+v", c["B1"])
	}

	b2 := peerMap(t, &m, pB2)
	if !sameSet(b2["C"].AllowedIPs, "10.10.0.5/32") {
		t.Fatalf("gateway owns the foreign child: %+v", b2["C"])
	}
	b1 := peerMap(t, &m, pB1)
	if !sameSet(b1["C"].AllowedIPs) {
		t.Fatalf("non-chosen candidate must not own the child: %+v", b1["C"])
	}
	if !contains(b1["B2"].AllowedIPs, "10.10.0.5/32") {
		t.Fatalf("B1 reaches the foreign child through the mother mesh: %+v", b1["B2"])
	}
	if !sameSet(b1["Y"].AllowedIPs, "10.20.0.9/32") || !sameSet(b2["Y"].AllowedIPs, "10.20.0.9/32") {
		t.Fatalf("Y connects to both 10.20 mothers directly: b1=%+v b2=%+v", b1["Y"], b2["Y"])
	}
	y := peerMap(t, &m, pY)
	if !sameSet(y["B2"].AllowedIPs, "10.20.0.2/32", "10.10.0.5/32") || y["B2"].Endpoint == "" {
		t.Fatalf("a mother target is reached directly; C's gateway B2 also carries Y<->C both ways: %+v", y["B2"])
	}
	if !sameSet(y["A1"].AllowedIPs, "10.10.0.0/24") {
		t.Fatalf("Y's own gateway still carries the rest of 10.10: %+v", y["A1"])
	}
}

func TestSameSubnetGoesThroughTargetsMother(t *testing.T) {
	m := Mesh{
		Revision: 1,
		Nodes: []Node{
			mother(pA1, "A1", "10.10.0.1/24", "1.1.1.1"),
			mother(pA2, "A2", "10.10.0.2/24", "1.1.1.2"),
			child(pC, "C", "10.10.0.5/24", "10.10.0.0/24"),
			child(pX, "X", "10.10.0.6/24", "10.10.0.0/24"),
		},
		Links: []Link{{FromNodeID: pC, ToNodeID: pA1}, {FromNodeID: pX, ToNodeID: pA2}},
	}
	c := peerMap(t, &m, pC)
	if !sameSet(c["A1"].AllowedIPs, "10.10.0.0/24") {
		t.Fatalf("own mother carries the subnet: %+v", c["A1"])
	}
	if !sameSet(c["A2"].AllowedIPs, "10.10.0.2/32", "10.10.0.6/32") || c["A2"].Endpoint != "1.1.1.2:25590" {
		t.Fatalf("C dials X's mother and reaches X and A2 there: %+v", c["A2"])
	}
	x := peerMap(t, &m, pX)
	if !sameSet(x["A1"].AllowedIPs, "10.10.0.1/32", "10.10.0.5/32") {
		t.Fatalf("X reaches C via C's mother: %+v", x["A1"])
	}
	a2 := peerMap(t, &m, pA2)
	if !sameSet(a2["C"].AllowedIPs, "10.10.0.5/32") || !sameSet(a2["A1"].AllowedIPs, "10.10.0.1/32") {
		t.Fatalf("A2 owns C directly, not via A1: c=%+v a1=%+v", a2["C"], a2["A1"])
	}

	// A mother that is not dialable: fall back to relaying via the own mother.
	m.Nodes[1].Endpoint = ""
	m.Links = m.Links[:1]
	c = peerMap(t, &m, pC)
	if _, ok := c["A2"]; ok {
		t.Fatalf("undialable mother must not be a peer: %+v", c)
	}
	a1 := peerMap(t, &m, pA1)
	if !sameSet(a1["A2"].AllowedIPs, "10.10.0.2/32") {
		t.Fatalf("mother mesh still links the two mothers: %+v", a1["A2"])
	}
}

func TestCrossSubnetDefaultsToFirstCandidate(t *testing.T) {
	m := crossMesh()
	c := peerMap(t, &m, pC)
	if !sameSet(c["B1"].AllowedIPs, "10.20.0.0/24") || !sameSet(c["B2"].AllowedIPs) {
		t.Fatalf("default gateway: %+v", c)
	}
	m.Paths = PathChoices{pC: {"10.20.0.0/24": pA1}}
	c = peerMap(t, &m, pC)
	if !sameSet(c["B1"].AllowedIPs, "10.20.0.0/24") {
		t.Fatalf("invalid choice must fall back: %+v", c)
	}
}

func TestRelayFallbackOnlyWhenNoMother(t *testing.T) {
	m := crossMesh()
	m.Relay = &Relay{PublicKey: "R", Port: 25599}
	if c := peerMap(t, &m, pC); len(c["R"].PublicKey) != 0 {
		t.Fatalf("relay must stay idle while a mother is reachable: %+v", c)
	}

	m.Nodes[1].Endpoint = ""
	m.Nodes[2].Endpoint = ""
	c := peerMap(t, &m, pC)
	if !sameSet(c["R"].AllowedIPs, "10.20.0.0/24") || c["R"].Endpoint != "@master:25599" {
		t.Fatalf("no 10.20 mother reachable: go via Master: %+v", c["R"])
	}
	if !sameSet(c["A1"].AllowedIPs, "10.10.0.0/24", "10.20.0.9/32") {
		t.Fatalf("same subnet never uses the relay; Y answers via A1 so C talks to Y via A1 too: %+v", c["A1"])
	}
	y := peerMap(t, &m, pY)
	if y["R"].Endpoint == "" || !sameSet(y["R"].AllowedIPs) {
		t.Fatalf("nodes of a relay-served subnet join the relay: %+v", y["R"])
	}
	if !sameSet(y["A1"].AllowedIPs, "10.10.0.0/24") {
		t.Fatalf("Y still reaches 10.10 through its mother: %+v", y["A1"])
	}
	got := map[string][]string{}
	for _, p := range PlanMesh(&m).RelayPeers() {
		got[p.PublicKey] = p.AllowedIPs
	}
	if !sameSet(got["C"], "10.10.0.5/32") || !sameSet(got["Y"], "10.20.0.9/32") || !sameSet(got["B1"], "10.20.0.1/32") {
		t.Fatalf("relay peers: %+v", got)
	}
	if _, ok := got["A1"]; ok {
		t.Fatalf("10.10 is not relay-served: %+v", got)
	}
}

func TestRelayChosenWhenMothersDown(t *testing.T) {
	m := crossMesh()
	m.Relay = &Relay{PublicKey: "R", Port: 25599}
	m.Paths = PathChoices{pC: {"10.20.0.0/24": RelayNodeID}}
	c := peerMap(t, &m, pC)
	if !sameSet(c["R"].AllowedIPs, "10.20.0.0/24") || !sameSet(c["B1"].AllowedIPs) {
		t.Fatalf("Master picked the relay: %+v", c)
	}
	m.Relay = nil
	c = peerMap(t, &m, pC)
	if _, ok := c["R"]; ok || !sameSet(c["B1"].AllowedIPs, "10.20.0.0/24") {
		t.Fatalf("relay disabled falls back to mothers: %+v", c)
	}
}

func TestMotherMeshSingleDialer(t *testing.T) {
	m := crossMesh()
	b1 := peerMap(t, &m, pB1)
	b2 := peerMap(t, &m, pB2)
	if (b1["B2"].Endpoint == "") == (b2["B1"].Endpoint == "") {
		t.Fatalf("exactly one side dials: b1=%+v b2=%+v", b1["B2"], b2["B1"])
	}
	if b1["B2"].Endpoint != "2.2.2.2:25590" {
		t.Fatalf("lower id dials: %+v", b1["B2"])
	}
}

func TestMultiMotherChild(t *testing.T) {
	m := Mesh{
		Revision: 1,
		Nodes: []Node{
			mother(pA1, "A1", "10.10.0.1/24", "1.1.1.1"),
			mother(pA2, "A2", "10.10.0.2/24", "1.1.1.2"),
			child(pC, "C", "10.10.0.5/24", "10.10.0.0/24"),
			child(pX, "X", "10.10.0.6/24", "10.10.0.0/24"),
		},
		Links: []Link{
			{FromNodeID: pC, ToNodeID: pA1},
			{FromNodeID: pC, ToNodeID: pA2},
			{FromNodeID: pX, ToNodeID: pA2},
		},
		Paths: PathChoices{pC: {"10.10.0.0/24": pA1}},
	}
	c := peerMap(t, &m, pC)
	if !sameSet(c["A1"].AllowedIPs, "10.10.0.0/24") {
		t.Fatalf("fastest mother carries the subnet: %+v", c["A1"])
	}
	if !sameSet(c["A2"].AllowedIPs, "10.10.0.2/32", "10.10.0.6/32") {
		t.Fatalf("secondary mother serves its own members directly: %+v", c["A2"])
	}
	a1 := peerMap(t, &m, pA1)
	a2 := peerMap(t, &m, pA2)
	if !sameSet(a1["C"].AllowedIPs, "10.10.0.5/32") || !sameSet(a2["C"].AllowedIPs, "10.10.0.5/32") {
		t.Fatalf("both mothers forward to the child: a1=%+v a2=%+v", a1["C"], a2["C"])
	}
	if !sameSet(a1["X"].AllowedIPs, "10.10.0.6/32") || !sameSet(a1["A2"].AllowedIPs, "10.10.0.2/32") {
		t.Fatalf("X connects to A1 directly, no relay via A2: x=%+v a2=%+v", a1["X"], a1["A2"])
	}
	if contains(a2["A1"].AllowedIPs, "10.10.0.5/32") {
		t.Fatalf("A2 must not route C via A1: %+v", a2["A1"])
	}
}
