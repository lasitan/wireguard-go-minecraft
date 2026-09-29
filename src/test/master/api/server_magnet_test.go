package api_test

import (
	"golang.zx2c4.com/wireguard/src/core"
	. "golang.zx2c4.com/wireguard/src/master/api"
	"golang.zx2c4.com/wireguard/src/master/store"
	"net/http"
	"testing"
)

func u16(v uint16) *uint16 { return &v }
func str(v string) *string { return &v }

func dialedEndpoints(t *testing.T, s *Server, id string) []string {
	t.Helper()
	cfgs, err := s.Store().DesiredForNodes([]string{id})
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

// subnetEndpoint is the endpoint of the peer carrying prefix on node id.
func subnetEndpoint(t *testing.T, s *Server, id, prefix string) string {
	t.Helper()
	cfgs, err := s.Store().DesiredForNodes([]string{id})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range cfgs[id].Peers {
		for _, a := range p.AllowedIPs {
			if a == prefix {
				return p.Endpoint
			}
		}
	}
	return ""
}

func TestMagnetAttachSwitchDetach(t *testing.T) {
	s, c := newAPIFixture(t)
	a, _ := s.Store().Enroll("a", core.RoleClient, "", 0)
	b, _ := s.Store().Enroll("b", core.RoleClient, "", 0)
	m2, _ := s.Store().Enroll("m2", core.RoleClient, "", 0)

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
	if got := subnetEndpoint(t, s, b.ID, "10.10.0.0/24"); got != "5.6.7.8:9443" {
		t.Fatalf("subnet route after switch goes via %q", got)
	}
	if got := dialedEndpoints(t, s, b.ID); len(got) != 2 {
		t.Fatalf("child still connects to the other mother for same-subnet access: %v", got)
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

func TestMagnetMultipleMothers(t *testing.T) {
	s, c := newAPIFixture(t)
	m1, _ := s.Store().Enroll("m1", core.RoleClient, "", 0)
	m2, _ := s.Store().Enroll("m2", core.RoleClient, "", 0)
	kid, _ := s.Store().Enroll("kid", core.RoleClient, "", 0)
	patch := func(id string, p store.NodePatch) int {
		var mesh core.Mesh
		return c.do(http.MethodPatch, "/api/nodes?id="+id, p, &mesh)
	}
	if patch(m1.ID, store.NodePatch{ListenPort: u16(25590), Endpoint: str("1.1.1.1")}) != 200 ||
		patch(m2.ID, store.NodePatch{ListenPort: u16(25591), Endpoint: str("2.2.2.2")}) != 200 {
		t.Fatal("make mothers")
	}
	ids := []string{m1.ID, m2.ID}
	if code := patch(kid.ID, store.NodePatch{ParentIDs: &ids}); code != 200 {
		t.Fatalf("attach to two mothers: %d", code)
	}
	got := dialedEndpoints(t, s, kid.ID)
	if len(got) != 2 {
		t.Fatalf("child should dial both mothers: %v", got)
	}
	bad := []string{m1.ID, kid.ID}
	if code := patch(kid.ID, store.NodePatch{ParentIDs: &bad}); code != 400 {
		t.Fatalf("non-mother parent: %d", code)
	}
	none := []string{}
	if code := patch(kid.ID, store.NodePatch{ParentIDs: &none}); code != 200 {
		t.Fatalf("detach all: %d", code)
	}
	for _, l := range s.Store().Snapshot().Links {
		if l.FromNodeID == kid.ID {
			t.Fatalf("links remain: %+v", l)
		}
	}
}

func TestMagnetAutoEndpointFollowsPublicIP(t *testing.T) {
	s, _ := newAPIFixture(t)
	mother, _ := s.Store().Enroll("m", core.RoleServer, "", 0)
	child, _ := s.Store().Enroll("c", core.RoleClient, "", 0)

	if got := dialedEndpoints(t, s, child.ID); len(got) != 0 {
		t.Fatalf("no public IP yet, want no endpoint: %v", got)
	}
	rev := s.Store().Snapshot().Revision
	bumped, err := s.Store().SetPublicIPs(mother.ID, "9.9.9.9", "")
	if err != nil || !bumped || s.Store().Snapshot().Revision != rev+1 {
		t.Fatalf("public IP change must bump revision: %v %v", bumped, err)
	}
	if got := dialedEndpoints(t, s, child.ID); len(got) != 1 || got[0] != "9.9.9.9:25590" {
		t.Fatalf("auto endpoint: %v", got)
	}
	if bumped, _ := s.Store().SetPublicIPs(mother.ID, "9.9.9.9", ""); bumped {
		t.Fatal("unchanged IP must not bump")
	}
	if bumped, _ := s.Store().SetPublicIPs(child.ID, "8.8.8.8", ""); bumped {
		t.Fatal("non-mother IP change must not bump")
	}
}
