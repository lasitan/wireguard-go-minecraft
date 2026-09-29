package api_test

import (
	"golang.zx2c4.com/wireguard/src/core"
	"golang.zx2c4.com/wireguard/src/master/store"
	"golang.zx2c4.com/wireguard/src/master/storetest"
	"net/http"
	"testing"
)

type metaResp struct {
	EnrollToken string `json:"enrollToken"`
	VPNSubnet   string `json:"vpnSubnet"`
	Error       string `json:"error"`
}

func TestFirstRunGeneratesEnrollToken(t *testing.T) {
	st, err := store.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	settings, err := st.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if len(settings.EnrollToken) < 16 {
		t.Fatalf("enroll token not generated: %q", settings.EnrollToken)
	}
	if settings.VPNSubnet != store.DefaultVPNSubnet {
		t.Fatalf("pool = %q", settings.VPNSubnet)
	}
}

func TestPatchMeta(t *testing.T) {
	s, c := newAPIFixture(t)

	var m metaResp
	if code := c.do(http.MethodGet, "/api/meta", nil, &m); code != 200 || m.EnrollToken != storetest.EnrollToken {
		t.Fatalf("get meta: %d %+v", code, m)
	}

	for _, bad := range []map[string]string{
		{"enrollToken": "short"},
		{"enrollToken": "has space in it"},
		{"vpnSubnet": "nope"},
		{"vpnSubnet": "fd00::/64"},
		{"vpnSubnet": "10.0.0.0/31"},
	} {
		m = metaResp{}
		if code := c.do(http.MethodPatch, "/api/meta", bad, &m); code != 400 || m.Error == "" {
			t.Fatalf("patch %v: %d %+v", bad, code, m)
		}
	}

	m = metaResp{}
	code := c.do(http.MethodPatch, "/api/meta", map[string]string{"enrollToken": "new-enroll-key", "vpnSubnet": "10.20.3.4/16"}, &m)
	if code != 200 || m.EnrollToken != "new-enroll-key" || m.VPNSubnet != "10.20.0.0/16" {
		t.Fatalf("patch ok: %d %+v", code, m)
	}

	anon := &apiClient{t: t, base: c.base}
	if code := anon.do(http.MethodPatch, "/api/meta", map[string]string{"enrollToken": "hijacked-key"}, nil); code != 401 {
		t.Fatalf("anon patch: %d", code)
	}
	if code := anon.do(http.MethodPost, "/api/agent/enroll", map[string]string{"enrollToken": storetest.EnrollToken}, nil); code != 401 {
		t.Fatalf("old token enroll: %d", code)
	}
	var created struct{ Address string }
	if code := anon.do(http.MethodPost, "/api/agent/enroll", map[string]string{"enrollToken": "new-enroll-key", "role": core.RoleClient}, &created); code != 201 {
		t.Fatalf("new token enroll: %d", code)
	}
	if created.Address != "10.20.0.1/16" {
		t.Fatalf("address from new pool: %q", created.Address)
	}
	_ = s
}
