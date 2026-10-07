package store_test

import (
	"golang.zx2c4.com/wireguard/src/core"
	. "golang.zx2c4.com/wireguard/src/master/store"
	"testing"
)

func TestSwapNodeAddresses(t *testing.T) {
	st := openTestStore(t)
	m := st.Snapshot()
	if len(m.Nodes) < 2 {
		t.Skip("need enrolled nodes")
	}
	a, b := m.Nodes[0], m.Nodes[1]
	addrA, addrB := a.Address, b.Address
	routesA := append([]string(nil), a.Routes...)
	routesB := append([]string(nil), b.Routes...)
	if err := st.SwapNodeAddresses(a.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	m2 := st.Snapshot()
	na := m2.FindNode(a.ID)
	nb := m2.FindNode(b.ID)
	if na.Address != addrB || nb.Address != addrA {
		t.Fatalf("addresses not swapped: %s %s", na.Address, nb.Address)
	}
	pfxA, _ := core.VPNPrefixFromAddress(na.Address)
	pfxB, _ := core.VPNPrefixFromAddress(nb.Address)
	if len(na.Routes) == 0 || na.Routes[0] != pfxA {
		t.Fatalf("a routes after swap: %v want prefix %s", na.Routes, pfxA)
	}
	if len(nb.Routes) == 0 || nb.Routes[0] != pfxB {
		t.Fatalf("b routes after swap: %v", nb.Routes)
	}
	_ = routesA
	_ = routesB
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	token := "test-enroll-token-12345"
	pool := "10.10.0.0/24"
	if err := st.UpdateSettings(SettingsPatch{EnrollToken: &token, VPNSubnet: &pool}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Enroll("a", core.RoleClient, "", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Enroll("b", core.RoleClient, "", 0); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestReassignNodeSubnet(t *testing.T) {
	st := openTestStore(t)
	m := st.Snapshot()
	id := m.Nodes[0].ID
	if _, err := st.PatchNode(id, NodePatch{Address: strPtr("10.20.0.5/24")}); err != nil {
		t.Fatal(err)
	}
	m = st.Snapshot()
	node := m.FindNode(id)
	if err := st.ReassignNodeSubnet(m.Nodes[1].ID, "10.20.0.0/24"); err != nil {
		t.Fatal(err)
	}
	m2 := st.Snapshot()
	moved := m2.FindNode(m.Nodes[1].ID)
	pfx, _ := core.VPNPrefixFromAddress(moved.Address)
	if pfx != "10.20.0.0/24" {
		t.Fatalf("address %s not in 10.20/24", moved.Address)
	}
	for _, r := range moved.Routes {
		if r == "10.20.0.0/24" {
			return
		}
	}
	t.Fatalf("routes missing 10.20.0.0/24: %v", moved.Routes)
	_ = node
}

func TestPatchAddressMovesSubnetRoute(t *testing.T) {
	st := openTestStore(t)
	id := st.Snapshot().Nodes[0].ID
	lan := []string{"10.10.0.0/24", "192.168.1.0/24"}
	if _, err := st.PatchNode(id, NodePatch{Routes: &lan}); err != nil {
		t.Fatal(err)
	}
	before := st.Snapshot().Revision
	if _, err := st.PatchNode(id, NodePatch{Address: strPtr("10.30.0.9/24")}); err != nil {
		t.Fatal(err)
	}
	m := st.Snapshot()
	n := m.FindNode(id)
	if n.Address != "10.30.0.9/24" || m.Revision <= before {
		t.Fatalf("address %s rev %d (was %d)", n.Address, m.Revision, before)
	}
	want := map[string]bool{"10.30.0.0/24": true, "192.168.1.0/24": true}
	if len(n.Routes) != len(want) {
		t.Fatalf("routes %v", n.Routes)
	}
	for _, r := range n.Routes {
		if !want[r] {
			t.Fatalf("routes %v still hold the old subnet", n.Routes)
		}
	}
	d, err := st.DesiredForNode(id)
	if err != nil {
		t.Fatal(err)
	}
	if d.Interface.Address != "10.30.0.9/24" {
		t.Fatalf("desired address %s", d.Interface.Address)
	}
}

func strPtr(s string) *string { return &s }
