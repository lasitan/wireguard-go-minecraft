package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAgentInstallCommandsAPI(t *testing.T) {
	_, c := newAPIFixture(t)
	var out agentInstallCommands
	code := c.do(http.MethodGet, "/api/install/agent?role=server&endpoint=1.2.3.4%3A25590", nil, &out)
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	if !strings.Contains(out.Linux, "WG_MC_BOOTSTRAP=agent") || !strings.Contains(out.Linux, "WG_MC_ENROLL_KEY=") {
		t.Fatalf("linux: %q", out.Linux)
	}
	if !strings.Contains(out.Linux, "WG_MC_ROLE='server'") && !strings.Contains(out.Linux, `WG_MC_ROLE='server'`) {
		t.Fatalf("server role missing: %q", out.Linux)
	}
	if !strings.Contains(out.Linux, "25590") {
		t.Fatalf("listen port: %q", out.Linux)
	}
}

func TestMasterPublicURL(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://localhost:8443/x", nil)
	if u := masterPublicURL(r); u != "http://localhost:8443" {
		t.Fatal(u)
	}
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-Host", "mesh.example.com")
	if u := masterPublicURL(r); u != "https://mesh.example.com" {
		t.Fatal(u)
	}
}
