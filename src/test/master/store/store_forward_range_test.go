package store_test

import (
	"golang.zx2c4.com/wireguard/src/core"
	. "golang.zx2c4.com/wireguard/src/master/store"
	"testing"
)

func TestForwardRangeAndStar(t *testing.T) {
	st, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	token := "test-enroll-token-12345"
	pool := "10.10.0.0/24"
	if err := st.UpdateSettings(SettingsPatch{EnrollToken: &token, VPNSubnet: &pool}); err != nil {
		t.Fatal(err)
	}
	a, err := st.Enroll("a", core.RoleClient, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.Enroll("b", core.RoleClient, "", 0)
	if err != nil {
		t.Fatal(err)
	}

	saved, err := st.PutNodeForwards(a.ID, []core.Forward{
		{Protocol: "tcp", Listen: "1000-1002", DestNodeID: b.ID, DestPort: 2000},
		{Protocol: "udp", Listen: "*", DestNodeID: b.ID, Dest: "*"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved[0].Dest != "2000-2002" || saved[0].DestPort != 2000 {
		t.Fatalf("range dest: %+v", saved[0])
	}
	if saved[1].Dest != "*" || saved[1].DestPort != 0 {
		t.Fatalf("star dest: %+v", saved[1])
	}

	if _, err := st.PutNodeForwards(a.ID, []core.Forward{
		{Protocol: "tcp", Listen: "80", DestNodeID: b.ID, DestPort: 80},
		{Protocol: "tcp", Listen: "79-81", DestNodeID: b.ID, Dest: "*"},
	}); err == nil {
		t.Fatal("overlapping range accepted")
	}
	if _, err := st.PutNodeForwards(a.ID, []core.Forward{
		{Protocol: "tcp", Listen: "1-3", DestNodeID: b.ID, Dest: "9-10"},
	}); err == nil {
		t.Fatal("mismatched dest length accepted")
	}
}
