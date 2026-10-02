package core_test

import (
	. "golang.zx2c4.com/wireguard/src/core"
	"testing"
)

func clusterMesh() Mesh {
	a1 := mother(pA1, "A1", "10.10.0.1/24", "1.1.1.1:25590")
	a2 := mother(pA2, "A2", "10.10.0.2/24", "2.2.2.2:25590")
	a1.Cluster, a2.Cluster = "c1", "c1"
	return Mesh{
		Nodes:    []Node{a1, a2, child(pC, "C", "10.10.0.5/24", "10.10.0.0/24")},
		Links:    []Link{{FromNodeID: pC, ToNodeID: pA1}},
		Forwards: []Forward{{NodeID: pC, Protocol: "tcp", Listen: "25565", DestNodeID: pA1, DestPort: 25565}},
	}
}

func TestClusterMotherServesSiblingChildren(t *testing.T) {
	m := clusterMesh()
	cands := PlanMesh(&m).Candidates()[pC]["10.10.0.0/24"]
	if !sameSet(cands, pA1, pA2) {
		t.Fatalf("child attached to one member must see every member as gateway, got %v", cands)
	}
	c := peerMap(t, &m, pC)
	if c["A1"].Endpoint == "" || c["A1"].Keepalive == 0 {
		t.Fatalf("child must keep a live dial to A1: %+v", c["A1"])
	}
	if c["A2"].Endpoint == "" || c["A2"].Keepalive == 0 {
		t.Fatalf("child must keep a live dial to every cluster mother: %+v", c["A2"])
	}
	if !Contains(c["A1"].AllowedIPs, "10.10.0.1/32") || !Contains(c["A2"].AllowedIPs, "10.10.0.2/32") {
		t.Fatalf("each cluster mother stays directly reachable: %+v", c)
	}
	a2 := peerMap(t, &m, pA2)
	if !sameSet(a2["C"].AllowedIPs, "10.10.0.5/32") {
		t.Fatalf("sibling mother must deliver to the child: %+v", a2)
	}
}

func TestClusterMothersMeshAcrossSubnets(t *testing.T) {
	a1 := mother(pA1, "A1", "10.10.0.1/24", "1.1.1.1:25590")
	a2 := mother(pB1, "B1", "10.20.0.1/24", "2.2.2.1:25590")
	a1.Cluster, a2.Cluster = "c1", "c1"
	a1.Routes = []string{"10.10.0.0/24", "10.20.0.0/24"}
	a2.Routes = []string{"10.20.0.0/24", "10.10.0.0/24"}
	m := Mesh{Nodes: []Node{a1, a2}}
	x := peerMap(t, &m, pA1)
	y := peerMap(t, &m, pB1)
	if (x["B1"].Endpoint == "") == (y["A1"].Endpoint == "") {
		t.Fatalf("cluster mothers must keep a long-lived mesh: a1=%+v b1=%+v", x["B1"], y["A1"])
	}
	if !Contains(x["B1"].AllowedIPs, "10.20.0.1/32") {
		t.Fatalf("A1 must reach sibling host directly: %+v", x["B1"])
	}
}

func TestClusterStandbyRetargetsForwards(t *testing.T) {
	m := clusterMesh()
	sb := ClusterStandby(&m, func(id string) bool { return id != pA1 })
	if sb[pA1] != pA2 || len(sb) != 1 {
		t.Fatalf("standby = %v", sb)
	}
	m.Standby = sb
	d, err := CompileDesired(&m, pC, DesiredDefaults{})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Forwards) != 1 || d.Forwards[0].DestHost != "10.10.0.2" {
		t.Fatalf("forward should fail over to A2: %+v", d.Forwards)
	}
	if got := ClusterStandby(&m, func(string) bool { return false }); len(got) != 0 {
		t.Fatalf("no live member means no standby, got %v", got)
	}
}
