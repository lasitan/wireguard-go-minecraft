package store_test

import (
	. "golang.zx2c4.com/wireguard/src/master/store"
	"testing"
)

func TestRelaySettings(t *testing.T) {
	dir := t.TempDir()
	st, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	rc := st.Relay()
	if rc.Port != DefaultRelayPort || rc.PrivateKey == "" || rc.PublicKey == "" {
		t.Fatalf("seeded relay: %+v", rc)
	}
	m := st.Snapshot()
	if m.Relay == nil || m.Relay.PublicKey != rc.PublicKey || m.Relay.Port != DefaultRelayPort {
		t.Fatalf("mesh carries the relay's public half: %+v", m.Relay)
	}
	rev := m.Revision

	zero := 0
	if err := st.UpdateSettings(SettingsPatch{RelayPort: &zero}); err != nil {
		t.Fatal(err)
	}
	m = st.Snapshot()
	if m.Relay != nil || m.Revision <= rev {
		t.Fatalf("disabled relay leaves the mesh and bumps the revision: %+v rev=%d", m.Relay, m.Revision)
	}
	bad := 70000
	if err := st.UpdateSettings(SettingsPatch{RelayPort: &bad}); err == nil {
		t.Fatal("out-of-range port accepted")
	}
	st.Close()

	st2, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	if again := st2.Relay(); again.PrivateKey != rc.PrivateKey || again.Port != 0 {
		t.Fatalf("keys and the disabled port survive restarts: %+v", again)
	}
}

func TestRelayHostPinAndAutoIP(t *testing.T) {
	dir := t.TempDir()
	st, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if m := st.Snapshot(); m.Relay == nil || m.Relay.Host != "" {
		t.Fatalf("unpinned relay has empty host: %+v", m.Relay)
	}
	changed, err := st.SetRelayPublicV4("203.0.113.9")
	if err != nil || !changed {
		t.Fatalf("auto ip: changed=%v err=%v", changed, err)
	}
	if m := st.Snapshot(); m.Relay.Host != "203.0.113.9" {
		t.Fatalf("auto ip must be compiled into the mesh: %+v", m.Relay)
	}
	again, err := st.SetRelayPublicV4("203.0.113.9")
	if err != nil || again {
		t.Fatalf("same ip must not bump: changed=%v err=%v", again, err)
	}
	pin := "198.51.100.7"
	if err := st.UpdateSettings(SettingsPatch{RelayHost: &pin}); err != nil {
		t.Fatal(err)
	}
	if m := st.Snapshot(); m.Relay.Host != pin {
		t.Fatalf("admin pin wins: %+v", m.Relay)
	}
	revPinned := st.Snapshot().Revision
	changed, err = st.SetRelayPublicV4("203.0.113.10")
	if err != nil || changed {
		t.Fatalf("pin must ignore auto bump: changed=%v err=%v", changed, err)
	}
	if m := st.Snapshot(); m.Relay.Host != pin || m.Revision != revPinned {
		t.Fatalf("pin stays, auto still recorded: %+v rev=%d", m.Relay, m.Revision)
	}
	empty := ""
	if err := st.UpdateSettings(SettingsPatch{RelayHost: &empty}); err != nil {
		t.Fatal(err)
	}
	if m := st.Snapshot(); m.Relay.Host != "203.0.113.10" {
		t.Fatalf("clearing pin falls back to auto: %+v", m.Relay)
	}
}
