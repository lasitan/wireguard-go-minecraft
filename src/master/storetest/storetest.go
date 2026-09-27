// Package storetest provides Store fixtures shared by master package tests.
package storetest

import (
	"testing"

	"golang.zx2c4.com/wireguard/src/master/store"
)

const EnrollToken = "enroll-test-token"

// Open returns a fresh Store in a temp dir with EnrollToken and pool set.
func Open(t *testing.T, pool string) *store.Store {
	t.Helper()
	st, err := store.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	SetSettings(t, st, pool)
	return st
}

func SetSettings(t *testing.T, st *store.Store, pool string) {
	t.Helper()
	token := EnrollToken
	if err := st.UpdateSettings(store.SettingsPatch{EnrollToken: &token, VPNSubnet: &pool}); err != nil {
		t.Fatal(err)
	}
}
