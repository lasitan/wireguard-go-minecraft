package api

import (
	"net/http"
	"testing"

	"golang.zx2c4.com/wireguard/src/master/store"

	"golang.zx2c4.com/wireguard/src/core"
)

func u16(v uint16) *uint16 { return &v }
func str(v string) *string { return &v }

func dialedEndpoints(t *testing.T, s *Server, id string) []string {
	t.Helper()
	cfgs, err := s.store.DesiredForNodes([]string{id})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, p := range cfgs[id].Peers {
		if p.Endpoint != "" {
			out = append(out, p.Endpoint)
		}
	}
	return out
}

func TestMagnetAttachSwitchDetach(t *testing.T) {
	s, c := newAPIFixture(t)
	a, _ := s.store.Enroll("a", core.RoleClient, "", 0)
	b, _ := s.store.Enroll("b", core.RoleClient, "", 0)
	m2, _ := s.store.Enroll("m2", core.RoleClient, "", 0)

	patch := func(id string, p store.NodePatch) (core.Mesh, int) {
		var mesh core.Mesh
		code := c.do(http.MethodPatch, "/api/nodes?id="+id, p, &mesh)
		return mesh, code
	}

	if _, code := patch(b.ID, store.NodePatch{ParentID: str(a.ID)}); code != 400 {
		t.Fatalf("attach to non-mother: %d", code)
	}
	mesh, code := patch(a.ID, store.NodePatch{ListenPort: u16(25590), Endpoint: str("1.2.3.4")})
	if code != 200 || mesh.FindNode(a.ID).Role != core.RoleServer {
		t.Fatalf("make mother: %d", code)
	}
	if _, code := patch(b.ID, store.NodePatch{ParentID: str(a.ID)}); code != 200 {
		t.Fatalf("attach: %d", code)
	}
	if got := dialedEndpoints(t, s, b.ID); len(got) != 1 || got[0] != "1.2.3.4:25590" {
		t.Fatalf("child endpoint after attach: %v", got)
	}

	if _, code := patch(m2.ID, store.NodePatch{ListenPort: u16(9000), Endpoint: str("5.6.7.8:9443")}); code != 200 {
		t.Fatalf("second mother: %d", code)
	}
	if _, code := patch(b.ID, store.NodePatch{ParentID: str(m2.ID)}); code != 200 {
		t.Fatalf("switch: %d", code)
	}
	if got := dialedEndpoints(t, s, b.ID); len(got) != 1 || got[0] != "5.6.7.8:9443" {
		t.Fatalf("child endpoint after switch: %v", got)
	}
	if _, code := patch(a.ID, store.NodePatch{ParentID: str(m2.ID)}); code != 400 {
		t.Fatalf("mother attaching to mother: %d", code)
	}
	if _, code := patch(a.ID, store.NodePatch{Endpoint: str("bad host")}); code != 400 {
		t.Fatalf("bad endpoint: %d", code)
	}

	mesh, code = patch(m2.ID, store.NodePatch{ListenPort: u16(0)})
	if code != 200 || mesh.FindNode(m2.ID).Role != core.RoleClient {
		t.Fatalf("clear mother: %d", code)
	}
	for _, l := range mesh.Links {
		if l.FromNodeID == b.ID {
			t.Fatalf("child still attached after mother cleared: %+v", mesh.Links)
		}
	}
}

func TestMagnetAutoEndpointFollowsPublicIP(t *testing.T) {
	s, _ := newAPIFixture(t)
	mother, _ := s.store.Enroll("m", core.RoleServer, "", 0)
	child, _ := s.store.Enroll("c", core.RoleClient, "", 0)

	if got := dialedEndpoints(t, s, child.ID); len(got) != 0 {
		t.Fatalf("no public IP yet, want no endpoint: %v", got)
	}
	rev := s.store.Snapshot().Revision
	bumped, err := s.store.SetPublicIPs(mother.ID, "9.9.9.9", "")
	if err != nil || !bumped || s.store.Snapshot().Revision != rev+1 {
		t.Fatalf("public IP change must bump revision: %v %v", bumped, err)
	}
	if got := dialedEndpoints(t, s, child.ID); len(got) != 1 || got[0] != "9.9.9.9:25590" {
		t.Fatalf("auto endpoint: %v", got)
	}
	if bumped, _ := s.store.SetPublicIPs(mother.ID, "9.9.9.9", ""); bumped {
		t.Fatal("unchanged IP must not bump")
	}
	if bumped, _ := s.store.SetPublicIPs(child.ID, "8.8.8.8", ""); bumped {
		t.Fatal("non-mother IP change must not bump")
	}
}
