package core_test

import (
	. "golang.zx2c4.com/wireguard/src/core"
	"testing"
	"time"
)

const (
	tSrv = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0001"
	tC1  = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0002"
	tC2  = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0003"
)

func policyMesh() Mesh {
	return Mesh{
		Revision: 1,
		Nodes: []Node{
			{ID: tSrv, Role: RoleServer, PublicKey: "S", PrivateKey: "s", Address: "10.10.0.1/24", Routes: []string{"10.10.0.0/24"}, ListenPort: 25590, Endpoint: "1.1.1.1", Token: "a"},
			{ID: tC1, Role: RoleClient, PublicKey: "C1", PrivateKey: "c1", Address: "10.10.0.2/24", Routes: []string{"10.10.0.0/24"}, Token: "b"},
			{ID: tC2, Role: RoleClient, PublicKey: "C2", PrivateKey: "c2", Address: "10.10.0.3/24", Routes: []string{"10.10.0.0/24"}, Token: "c"},
		},
		Links: []Link{{FromNodeID: tC1, ToNodeID: tSrv}, {FromNodeID: tC2, ToNodeID: tSrv}},
		Forwards: []Forward{
			{NodeID: tSrv, Protocol: "tcp", Listen: "80", DestNodeID: tC1, DestPort: 80},
		},
	}
}

func TestCompileDisabled(t *testing.T) {
	m := policyMesh()
	m.Nodes[1].Disabled = true

	own, err := CompileDesired(&m, tC1, DesiredDefaults{})
	if err != nil {
		t.Fatal(err)
	}
	if len(own.Peers) != 0 || len(own.Forwards) != 0 {
		t.Fatalf("disabled node should have empty config: %+v", own)
	}
	srv, err := CompileDesired(&m, tSrv, DesiredDefaults{})
	if err != nil {
		t.Fatal(err)
	}
	if len(srv.Peers) != 1 || srv.Peers[0].PublicKey != "C2" {
		t.Fatalf("server should drop disabled peer: %+v", srv.Peers)
	}
	if len(srv.Forwards) != 0 {
		t.Fatalf("forward to disabled node should be dropped: %+v", srv.Forwards)
	}
}

func TestCompileRoutes(t *testing.T) {
	m := policyMesh()
	m.Nodes[1].Routes = []string{"10.10.0.0/24", "192.168.50.0/24"}

	c1, err := CompileDesired(&m, tC1, DesiredDefaults{})
	if err != nil {
		t.Fatal(err)
	}
	if !c1.IPForward {
		t.Fatal("node with routes must enable ip forwarding")
	}
	srv, err := CompileDesired(&m, tSrv, DesiredDefaults{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range srv.Peers {
		if p.PublicKey == "C1" {
			got = p.AllowedIPs
		}
	}
	if len(got) != 2 || got[0] != "10.10.0.2/32" || got[1] != "192.168.50.0/24" {
		t.Fatalf("server allowed for C1: %v", got)
	}
	c1Peer := c1.Peers[0].AllowedIPs
	if len(c1Peer) != 2 || c1Peer[0] != "10.10.0.0/24" || c1Peer[1] != "192.168.50.0/24" {
		t.Fatalf("client outbound allowed: %v", c1Peer)
	}
}

func TestCompileIPConflict(t *testing.T) {
	m := policyMesh()
	now := time.Now()
	m.Nodes[1].AddressChangedAt = now.Add(-time.Hour)
	// C2 later changes to C1's IP: C2 loses, C1 keeps running.
	m.Nodes[2].Address = "10.10.0.2/24"
	m.Nodes[2].AddressChangedAt = now

	losers := ConflictLosers(&m)
	if !losers[tC2] || losers[tC1] {
		t.Fatalf("losers: %v", losers)
	}
	srv, err := CompileDesired(&m, tSrv, DesiredDefaults{})
	if err != nil {
		t.Fatal(err)
	}
	if len(srv.Peers) != 1 || srv.Peers[0].PublicKey != "C1" {
		t.Fatalf("server peers under conflict: %+v", srv.Peers)
	}
}

func TestGenerateAndValidNodeID(t *testing.T) {
	id, err := GenerateNodeID()
	if err != nil {
		t.Fatal(err)
	}
	if !ValidNodeID(id) {
		t.Fatalf("generated id not valid UUID: %q", id)
	}
	if ValidNodeID("client-a") {
		t.Fatal("human-readable id should be rejected")
	}
	if ValidNodeID("") {
		t.Fatal("empty id should be rejected")
	}
}

func TestCompileDesiredClientAndServer(t *testing.T) {
	const serverID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0001"
	const clientID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0002"
	mesh := Mesh{
		Revision: 3,
		Nodes: []Node{
			{ID: serverID, Role: RoleServer, PublicKey: "SApub", PrivateKey: "SApriv", Address: "10.10.0.1/24", Routes: []string{"10.10.0.0/24"}, ListenPort: 25590, Endpoint: "1.2.3.4:25590", Token: "t1"},
			{ID: clientID, Role: RoleClient, PublicKey: "CApub", PrivateKey: "CApriv", Address: "10.10.0.7/24", Routes: []string{"10.10.0.0/24"}, Token: "t2"},
		},
		Links: []Link{
			{FromNodeID: clientID, ToNodeID: serverID},
		},
		Forwards: []Forward{
			{NodeID: serverID, Protocol: "tcp", Listen: "3389", DestNodeID: clientID, DestPort: 3389},
			{NodeID: serverID, Protocol: "tcp", Listen: "45", DestNodeID: clientID, DestPort: 3389},
		},
	}

	client, err := CompileDesired(&mesh, clientID, DesiredDefaults{})
	if err != nil {
		t.Fatal(err)
	}
	if client.NodeID != clientID {
		t.Fatalf("client nodeId: %q", client.NodeID)
	}
	if client.Role != RoleClient || !client.IPForward {
		t.Fatalf("client role/ipforward: %+v", client)
	}
	if len(client.Peers) != 1 || client.Peers[0].Endpoint != "1.2.3.4:25590" {
		t.Fatalf("client peers: %+v", client.Peers)
	}
	if len(client.Peers[0].AllowedIPs) != 1 || client.Peers[0].AllowedIPs[0] != "10.10.0.0/24" {
		t.Fatalf("client allowed: %v", client.Peers[0].AllowedIPs)
	}
	if len(client.Forwards) != 0 {
		t.Fatalf("client should have no forwards")
	}

	server, err := CompileDesired(&mesh, serverID, DesiredDefaults{})
	if err != nil {
		t.Fatal(err)
	}
	if server.NodeID != serverID {
		t.Fatalf("server nodeId: %q", server.NodeID)
	}
	if !server.IPForward || server.Interface.ListenPort != 25590 {
		t.Fatalf("server: %+v", server)
	}
	if len(server.Peers) != 1 || server.Peers[0].PublicKey != "CApub" {
		t.Fatalf("server peers: %+v", server.Peers)
	}
	if len(server.Forwards) != 2 {
		t.Fatalf("server forwards: %+v", server.Forwards)
	}
	if server.Forwards[0].DestHost != "10.10.0.7" || server.Forwards[0].DestPort != 3389 {
		t.Fatalf("forward0: %+v", server.Forwards[0])
	}
}
