package store_test

import (
	"encoding/json"
	"testing"

	"golang.zx2c4.com/wireguard/src/core"
	. "golang.zx2c4.com/wireguard/src/master/store"
)

func TestPatchMotherUdpModeAndTransport(t *testing.T) {
	st := openTestStore(t)
	id := st.Snapshot().Nodes[0].ID
	port := uint16(25590)
	if _, err := st.PatchNode(id, NodePatch{ListenPort: &port}); err != nil {
		t.Fatal(err)
	}
	on := true
	if _, err := st.PatchNode(id, NodePatch{UdpMode: &on}); err != nil {
		t.Fatal(err)
	}
	snap := st.Snapshot()
	n := snap.FindNode(id)
	if n == nil || !n.UdpMode {
		t.Fatal("udpMode not stored")
	}
	d, err := st.DesiredForNode(id)
	if err != nil {
		t.Fatal(err)
	}
	if !d.UdpMode || len(d.Transport) != 0 {
		t.Fatalf("desired udp=%v transport=%s", d.UdpMode, d.Transport)
	}
	off := false
	body := json.RawMessage(`{"camouflage":{"profile":"source","deep":false,"secure":false,"handshakeTimeout":"10s","loginUsername":"Steve","loginPluginChannel":"minecraft:register","loginPluginSecret":"abcdefgh12","rejectMessage":"no","serverName":"x"},"tcp":{"dialTimeout":"3s","reconnectInitialBackoff":"1s","reconnectMaxBackoff":"60s","rxIdleTimeout":"5s"}}`)
	if _, err := st.PatchNode(id, NodePatch{UdpMode: &off, Transport: &body}); err != nil {
		t.Fatal(err)
	}
	d, err = st.DesiredForNode(id)
	if err != nil {
		t.Fatal(err)
	}
	if d.UdpMode || len(d.Transport) == 0 {
		t.Fatalf("desired after tcp: udp=%v transport=%s", d.UdpMode, d.Transport)
	}
	child := st.Snapshot().Nodes[1].ID
	if _, err := st.PatchNode(child, NodePatch{ParentID: &id}); err != nil {
		t.Fatal(err)
	}
	cd, err := st.DesiredForNode(child)
	if err != nil {
		t.Fatal(err)
	}
	if cd.UdpMode || string(cd.Transport) != string(d.Transport) {
		t.Fatalf("child inherit: udp=%v transport=%s want %s", cd.UdpMode, cd.Transport, d.Transport)
	}
}

func TestUdpModeRejectedOnNonMother(t *testing.T) {
	st := openTestStore(t)
	id := st.Snapshot().Nodes[0].ID
	on := true
	if _, err := st.PatchNode(id, NodePatch{UdpMode: &on}); err == nil {
		t.Fatal("expected error")
	}
	_ = core.RoleClient
}
