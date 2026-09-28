package core

import "testing"

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
	a2 := peerMap(t, &m, pA2)
	if !sameSet(a2["C"].AllowedIPs, "10.10.0.5/32") {
		t.Fatalf("sibling mother must deliver to the child: %+v", a2)
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
