package core_test

import (
	"encoding/json"
	"testing"

	. "golang.zx2c4.com/wireguard/src/core"
)

func TestResolveBindConfigMotherOwns(t *testing.T) {
	tr := json.RawMessage(`{"camouflage":{"profile":"source"}}`)
	def := json.RawMessage(`{"camouflage":{"profile":"minecraft"}}`)
	m := Mesh{Nodes: []Node{{ID: "m1", Address: "10.0.0.1/24", ListenPort: 25590, Transport: tr}}}
	got, udp := ResolveBindConfig(&m, &m.Nodes[0], def)
	if udp || string(got) != string(tr) {
		t.Fatalf("mother: udp=%v transport=%s", udp, got)
	}
	m.Nodes[0].UdpMode = true
	got, udp = ResolveBindConfig(&m, &m.Nodes[0], def)
	if !udp || got != nil {
		t.Fatalf("udp mother: udp=%v transport=%s", udp, got)
	}
}

func TestResolveBindConfigChildInherits(t *testing.T) {
	tr := json.RawMessage(`{"camouflage":{"profile":"steam"}}`)
	m := Mesh{
		Nodes: []Node{
			{ID: "c1", Address: "10.0.0.2/24"},
			{ID: "m1", Address: "10.0.0.1/24", ListenPort: 25590, Transport: tr, UdpMode: true},
		},
		Links: []Link{{FromNodeID: "c1", ToNodeID: "m1"}},
	}
	got, udp := ResolveBindConfig(&m, &m.Nodes[0], nil)
	if !udp || got != nil {
		t.Fatalf("child: udp=%v transport=%s", udp, got)
	}
}
