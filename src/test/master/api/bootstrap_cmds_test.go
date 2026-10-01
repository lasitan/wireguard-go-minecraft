package api_test

import (
	. "golang.zx2c4.com/wireguard/src/master/api"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAgentInstallCommandsAPI(t *testing.T) {
	_, c := newAPIFixture(t)
	var out AgentInstallCommands
	code := c.do(http.MethodGet, "/api/install/agent?role=server&endpoint=1.2.3.4%3A25590", nil, &out)
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	if !strings.Contains(out.Linux, "LASITAN_BOOTSTRAP=agent") || !strings.Contains(out.Linux, "LASITAN_ENROLL_KEY=") {
		t.Fatalf("linux: %q", out.Linux)
	}
	if !strings.Contains(out.Linux, "LASITAN_ROLE='server'") && !strings.Contains(out.Linux, `LASITAN_ROLE='server'`) {
		t.Fatalf("server role missing: %q", out.Linux)
	}
	if !strings.Contains(out.Linux, "25590") {
		t.Fatalf("listen port: %q", out.Linux)
	}
	// Pasted into PowerShell, a double-quoted -c body would have $env: expanded by the outer shell.
	if !strings.HasPrefix(out.Windows, "$env:LASITAN_BOOTSTRAP='agent';") {
		t.Fatalf("windows env must be set in the pasting shell: %q", out.Windows)
	}
	if i := strings.Index(out.Windows, `-c "`); i < 0 || strings.Contains(out.Windows[i:], "$") {
		t.Fatalf("windows -c body must not reference variables: %q", out.Windows)
	}
}

func TestMasterPublicURL(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://localhost:8443/x", nil)
	if u := MasterPublicURL(r); u != "http://localhost:8443" {
		t.Fatal(u)
	}
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-Host", "mesh.example.com")
	if u := MasterPublicURL(r); u != "https://mesh.example.com" {
		t.Fatal(u)
	}
}
