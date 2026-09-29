package agent_test

import (
	"testing"

	. "golang.zx2c4.com/wireguard/src/agent"
	"golang.zx2c4.com/wireguard/src/core"
)

func TestApplyUsesLocalIface(t *testing.T) {
	a := NewApplier(nil, nil, "lc0", nil, nil, "")
	d := &core.DesiredConfig{Revision: 3, InterfaceName: "wg0"}
	got := a.WithLocalIface(d)
	if got.InterfaceName != "lc0" || got.Revision != 3 {
		t.Fatalf("got %+v", got)
	}
	if d.InterfaceName != "wg0" {
		t.Fatal("input must not be mutated")
	}
	if same := &(core.DesiredConfig{InterfaceName: "lc0"}); a.WithLocalIface(same) != same {
		t.Fatal("matching name should not copy")
	}
}

func TestApplyFallsBackToMasterIface(t *testing.T) {
	a := NewApplier(nil, nil, "", nil, nil, "")
	if got := a.WithLocalIface(&core.DesiredConfig{InterfaceName: "wg0"}); got.InterfaceName != "wg0" {
		t.Fatalf("got %q", got.InterfaceName)
	}
}
