package store

import (
	"testing"

	"golang.zx2c4.com/wireguard/src/core"
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

func strPtr(s string) *string { return &s }
