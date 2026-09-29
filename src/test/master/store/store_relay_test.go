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
