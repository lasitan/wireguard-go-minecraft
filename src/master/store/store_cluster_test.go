package store

import (
	"testing"

	"golang.zx2c4.com/wireguard/src/core"
)

func ptr[T any](v T) *T { return &v }

func TestClusterJoinSyncAndLeave(t *testing.T) {
	st := openTestStore(t)
	m1, err := st.Enroll("m1", core.RoleServer, "1.1.1.1", 25590)
	if err != nil {
		t.Fatal(err)
	}
	m2, err := st.Enroll("m2", core.RoleServer, "2.2.2.2", 25591)
	if err != nil {
		t.Fatal(err)
	}
	kid, err := st.Enroll("kid", core.RoleClient, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutNodeForwards(m1.ID, []core.Forward{{Protocol: "tcp", Listen: "25565", DestNodeID: m1.ID, DestPort: 25566}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PatchNode(kid.ID, NodePatch{ClusterWith: ptr(m1.ID)}); err == nil {
		t.Fatal("plain card joined a mother cluster")
	}

	if _, err := st.PatchNode(m2.ID, NodePatch{ClusterWith: ptr(m1.ID)}); err != nil {
		t.Fatal(err)
	}
	m := st.Snapshot()
	n1, n2 := m.FindNode(m1.ID), m.FindNode(m2.ID)
	if n1.Cluster == "" || n1.Cluster != n2.Cluster {
		t.Fatalf("cluster ids %q %q", n1.Cluster, n2.Cluster)
	}
	if n2.ListenPort != 25590 {
		t.Fatalf("joiner should adopt the listen port, got %d", n2.ListenPort)
	}
	fw, _ := st.NodeForwards(m2.ID)
	if len(fw) != 1 || fw[0].DestNodeID != m2.ID {
		t.Fatalf("joiner forwards = %+v", fw)
	}

	// Editing one member rewrites the other.
	port := uint16(25600)
	if _, err := st.PatchNode(m2.ID, NodePatch{ListenPort: &port}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutNodeForwards(m2.ID, nil); err != nil {
		t.Fatal(err)
	}
	m = st.Snapshot()
	if m.FindNode(m1.ID).ListenPort != 25600 {
		t.Fatal("listen port not synced")
	}
	if fw, _ := st.NodeForwards(m1.ID); len(fw) != 0 {
		t.Fatalf("forwards not synced: %+v", fw)
	}

	// Leaving dissolves a two-card cluster.
	if _, err := st.PatchNode(m1.ID, NodePatch{ClusterWith: ptr("")}); err != nil {
		t.Fatal(err)
	}
	m = st.Snapshot()
	if m.FindNode(m1.ID).Cluster != "" || m.FindNode(m2.ID).Cluster != "" {
		t.Fatal("single-member cluster should dissolve")
	}
}

func TestSetStandbyBumpsRevision(t *testing.T) {
	st := openTestStore(t)
	rev := st.Snapshot().Revision
	changed, err := st.SetStandby(map[string]string{"a": "b"})
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	m := st.Snapshot()
	if m.Revision != rev+1 || m.Standby["a"] != "b" {
		t.Fatalf("rev %d standby %v", m.Revision, m.Standby)
	}
	if changed, _ := st.SetStandby(map[string]string{"a": "b"}); changed {
		t.Fatal("unchanged standby reported as changed")
	}
}
