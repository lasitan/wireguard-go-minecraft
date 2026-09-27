package master

import "testing"

const testEnrollToken = "enroll-test-token"

func openTestStore(t *testing.T, pool string) *Store {
	t.Helper()
	st, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	setTestSettings(t, st, pool)
	return st
}

func setTestSettings(t *testing.T, st *Store, pool string) {
	t.Helper()
	token := testEnrollToken
	if err := st.UpdateSettings(SettingsPatch{EnrollToken: &token, VPNSubnet: &pool}); err != nil {
		t.Fatal(err)
	}
}
