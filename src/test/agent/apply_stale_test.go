package agent_test

import (
	"testing"

	. "golang.zx2c4.com/wireguard/src/agent"
	"golang.zx2c4.com/wireguard/src/core"
)

func TestApplyIgnoresStaleRevision(t *testing.T) {
	a := NewApplier(nil, nil, "lc0", nil, nil, "")
	a.SetNewestRevForTest(10)
	// A nil device would panic if the stale config were actually applied.
	if err := a.Apply(&core.DesiredConfig{Revision: 9, Interface: core.DesiredIface{Address: "10.10.0.2/24"}}); err != nil {
		t.Fatal(err)
	}
	if got := a.Revision(); got != -1 {
		t.Fatalf("stale revision applied: %d", got)
	}
}

func TestApplyAcceptsAnyRevisionAfterSessionReset(t *testing.T) {
	a := NewApplier(nil, nil, "lc0", nil, nil, "")
	a.SetNewestRevForTest(10)
	a.ResetSession()
	reached := func() (ok bool) {
		defer func() {
			if recover() != nil {
				ok = true
			}
		}()
		// No private key and a nil device: reaching the real apply path fails.
		return a.Apply(&core.DesiredConfig{Revision: 3}) != nil
	}()
	if !reached {
		t.Fatal("revision 3 was treated as stale after a session reset")
	}
}
