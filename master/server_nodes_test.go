package master

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/meshcfg"
	"golang.zx2c4.com/wireguard/meshcfg/wire"
)

type apiClient struct {
	t     *testing.T
	base  string
	token string
}

func (c *apiClient) do(method, path string, body any, out any) int {
	c.t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, c.base+path, rd)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func newAPIFixture(t *testing.T) (*Server, *apiClient) {
	t.Helper()
	s, err := NewServer(meshcfg.MasterConfig{AdminPassword: "pw", DataDir: t.TempDir(), EnrollToken: "enroll", VPNSubnet: "10.10.0.0/24", DisableGeoIPOnline: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.store.Close() })
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	c := &apiClient{t: t, base: srv.URL}
	var login struct{ Token string }
	if c.do(http.MethodPost, "/api/login", map[string]string{"password": "pw"}, &login) != 200 {
		t.Fatal("login failed")
	}
	c.token = login.Token
	return s, c
}

func TestAdminNodeAPIs(t *testing.T) {
	s, c := newAPIFixture(t)
	srvNode, _ := s.store.Enroll("srv", meshcfg.RoleServer, "1.2.3.4:25590", 0)
	cli, _ := s.store.Enroll("cli", meshcfg.RoleClient, "", 0)

	var mesh meshcfg.Mesh
	c.do(http.MethodGet, "/api/mesh", nil, &mesh)
	for _, n := range mesh.Nodes {
		if n.PrivateKey != "" {
			t.Fatal("/api/mesh must not expose private keys")
		}
	}

	enabled := false
	routes := []string{"192.168.9.7/24"}
	addr := "10.10.0.50/24"
	if code := c.do(http.MethodPatch, "/api/nodes?id="+cli.ID, NodePatch{Enabled: &enabled, Routes: &routes, Address: &addr}, &mesh); code != 200 {
		t.Fatalf("patch status %d", code)
	}
	n := mesh.FindNode(cli.ID)
	if n == nil || !n.Disabled || n.Address != "10.10.0.50/24" || len(n.Routes) != 1 || n.Routes[0] != "192.168.9.0/24" {
		t.Fatalf("patched node: %+v", n)
	}
	bad := "not-an-ip"
	if code := c.do(http.MethodPatch, "/api/nodes?id="+cli.ID, NodePatch{Address: &bad}, nil); code != 400 {
		t.Fatalf("bad address status %d", code)
	}

	fwds := []meshcfg.Forward{{Protocol: "tcp", Listen: "8080", DestNodeID: cli.ID, DestPort: 80}}
	var gotF []meshcfg.Forward
	if code := c.do(http.MethodPut, "/api/nodes/forwards?id="+srvNode.ID, fwds, &gotF); code != 200 || len(gotF) != 1 || gotF[0].NodeID != srvNode.ID {
		t.Fatalf("put forwards %d %+v", code, gotF)
	}
	dup := append(fwds, fwds[0])
	if code := c.do(http.MethodPut, "/api/nodes/forwards?id="+srvNode.ID, dup, nil); code != 400 {
		t.Fatalf("duplicate forwards status %d", code)
	}
	c.do(http.MethodGet, "/api/nodes/forwards?id="+srvNode.ID, nil, &gotF)
	if len(gotF) != 1 {
		t.Fatalf("get forwards %+v", gotF)
	}

	now := time.Now()
	s.stats.MarkConnected(srvNode.ID, now.Add(-4*time.Second))
	s.stats.Ingest(srvNode.ID, &wire.Stats{RxBytes: 1000, TxBytes: 1000}, now.Add(-4*time.Second))
	s.stats.Ingest(srvNode.ID, &wire.Stats{RxBytes: 5000, TxBytes: 3000}, now.Add(-2*time.Second))
	s.stats.flushNow()

	var st nodeStatsView
	if code := c.do(http.MethodGet, "/api/nodes/stats?id="+srvNode.ID, nil, &st); code != 200 {
		t.Fatalf("stats status %d", code)
	}
	if st.RxRate != 2000 || st.Totals.Rx != 4000 || st.Totals.Tx != 2000 {
		t.Fatalf("stats: %+v", st)
	}

	var series struct {
		Step   int            `json:"step"`
		Points []TrafficPoint `json:"points"`
	}
	if code := c.do(http.MethodGet, "/api/nodes/traffic?id="+srvNode.ID+"&range=1h", nil, &series); code != 200 || series.Step != 60 || len(series.Points) == 0 {
		t.Fatalf("traffic %d %+v", code, series)
	}
	if code := c.do(http.MethodGet, "/api/nodes/traffic?id="+srvNode.ID+"&range=2y", nil, nil); code != 400 {
		t.Fatalf("bad range status %d", code)
	}

	var who struct{ IP string }
	c.token = ""
	if code := c.do(http.MethodGet, "/api/agent/whoami", nil, &who); code != 200 || who.IP != "127.0.0.1" {
		t.Fatalf("whoami %d %+v", code, who)
	}
	if code := c.do(http.MethodGet, "/api/nodes/stats?id="+srvNode.ID, nil, nil); code != 401 {
		t.Fatalf("stats without auth: %d", code)
	}
}

// A legacy agent that only polls /api/agent/config keeps working.
func TestLegacyHTTPAgentStillServed(t *testing.T) {
	s, c := newAPIFixture(t)
	n, _ := s.store.Enroll("old", meshcfg.RoleClient, "", 0)
	c.token = n.Token
	var d meshcfg.DesiredConfig
	if code := c.do(http.MethodGet, "/api/agent/config", nil, &d); code != 200 || d.NodeID != n.ID {
		t.Fatalf("legacy config %d %+v", code, d)
	}
}
