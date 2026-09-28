package store

import (
	"testing"

	"golang.zx2c4.com/wireguard/src/core"
)

func TestPlanCacheFollowsMeshEdits(t *testing.T) {
	st, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	mom, err := st.Enroll("mom", core.RoleServer, "1.1.1.1", 25590)
	if err != nil {
		t.Fatal(err)
	}
	kid, err := st.Enroll("kid", core.RoleClient, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	endpointOf := func() string {
		d, err := st.DesiredForNodes([]string{kid.ID})
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range d[kid.ID].Peers {
			if p.PublicKey == mom.PublicKey {
				return p.Endpoint
			}
		}
		return ""
	}
	if got := endpointOf(); got != "1.1.1.1:25590" {
		t.Fatalf("before: %q", got)
	}
	ep := "9.9.9.9"
	if _, err := st.PatchNode(mom.ID, NodePatch{Endpoint: &ep}); err != nil {
		t.Fatal(err)
	}
	if got := endpointOf(); got != "9.9.9.9:25590" {
		t.Fatalf("a cached plan must not outlive the edit: %q", got)
	}
}
