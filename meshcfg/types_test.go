package meshcfg

import "testing"

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
			{ID: serverID, Role: RoleServer, PublicKey: "SApub", PrivateKey: "SApriv", Address: "10.10.0.1/24", ListenPort: 25590, Endpoint: "1.2.3.4:25590", Token: "t1"},
			{ID: clientID, Role: RoleClient, PublicKey: "CApub", PrivateKey: "CApriv", Address: "10.10.0.7/24", Token: "t2"},
		},
		Links: []Link{
			{FromNodeID: clientID, ToNodeID: serverID},
		},
		Forwards: []Forward{
			{NodeID: serverID, Protocol: "tcp", Listen: "3389", DestNodeID: clientID, DestPort: 3389},
			{NodeID: serverID, Protocol: "tcp", Listen: "45", DestNodeID: clientID, DestPort: 3389},
		},
	}

	client, err := CompileDesired(&mesh, clientID)
	if err != nil {
		t.Fatal(err)
	}
	if client.NodeID != clientID {
		t.Fatalf("client nodeId: %q", client.NodeID)
	}
	if client.Role != RoleClient || client.IPForward {
		t.Fatalf("client role/ipforward: %+v", client)
	}
	if len(client.Peers) != 1 || client.Peers[0].Endpoint != "1.2.3.4:25590" {
		t.Fatalf("client peers: %+v", client.Peers)
	}
	if client.Peers[0].AllowedIPs[0] != "10.10.0.1/32" {
		t.Fatalf("client allowed: %v", client.Peers[0].AllowedIPs)
	}
	if len(client.Forwards) != 0 {
		t.Fatalf("client should have no forwards")
	}

	server, err := CompileDesired(&mesh, serverID)
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
