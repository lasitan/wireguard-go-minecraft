package meshcfg

import "testing"

func TestCompileDesiredClientAndServer(t *testing.T) {
	mesh := Mesh{
		Revision: 3,
		Nodes: []Node{
			{ID: "sa", Role: RoleServer, PublicKey: "SApub", PrivateKey: "SApriv", Address: "10.10.0.1/24", ListenPort: 25590, Endpoint: "1.2.3.4:25590", Token: "t1"},
			{ID: "ca", Role: RoleClient, PublicKey: "CApub", PrivateKey: "CApriv", Address: "10.10.0.7/24", Token: "t2"},
		},
		Links: []Link{
			{FromNodeID: "ca", ToNodeID: "sa"},
		},
		Forwards: []Forward{
			{NodeID: "sa", Protocol: "tcp", Listen: "3389", DestNodeID: "ca", DestPort: 3389},
			{NodeID: "sa", Protocol: "tcp", Listen: "45", DestNodeID: "ca", DestPort: 3389},
		},
	}

	client, err := CompileDesired(&mesh, "ca")
	if err != nil {
		t.Fatal(err)
	}
	if client.NodeID != "ca" {
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

	server, err := CompileDesired(&mesh, "sa")
	if err != nil {
		t.Fatal(err)
	}
	if server.NodeID != "sa" {
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
