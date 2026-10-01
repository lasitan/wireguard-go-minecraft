package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/src/core/config"
	. "golang.zx2c4.com/wireguard/src/master/api"
	"golang.zx2c4.com/wireguard/src/master/store"
)

func TestClientIPTrustedHeaders(t *testing.T) {
	mk := func(remote string, set func(http.Header)) string {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = remote
		if set != nil {
			set(r.Header)
		}
		return ClientIP(r)
	}

	// Direct public access: injected CF / XFF headers must be ignored.
	if got := mk("8.8.8.8:1234", func(h http.Header) { h.Set("CF-Connecting-IP", "1.2.3.4") }); got != "8.8.8.8" {
		t.Fatalf("public peer must ignore CDN headers: %q", got)
	}
	if got := mk("203.0.113.10:443", func(h http.Header) {
		h.Set("CF-Connecting-IP", "1.2.3.4")
		h.Set("X-Forwarded-For", "9.9.9.9")
	}); got != "203.0.113.10" {
		t.Fatalf("spoofed headers on direct access: %q", got)
	}

	// Cloudflare edge: only CF-Connecting-IP (incl. Pseudo IPv4 Class E).
	cfEdge := "104.16.1.1:443"
	if got := mk(cfEdge, func(h http.Header) {
		h.Set("CF-Connecting-IP", "240.1.2.3") // Pseudo IPv4 overwrite
		h.Set("X-Real-IP", "198.51.100.1")
		h.Set("X-Forwarded-For", "192.0.2.1, 10.0.0.1")
	}); got != "240.1.2.3" {
		t.Fatalf("CF edge + Pseudo IPv4: %q", got)
	}
	if got := mk(cfEdge, nil); got != "104.16.1.1" {
		t.Fatalf("CF edge without header falls back to edge: %q", got)
	}

	// Local reverse proxy: full header chain.
	if got := mk("10.0.0.1:443", func(h http.Header) {
		h.Set("CF-Connecting-IP", "203.0.113.9")
		h.Set("X-Real-IP", "198.51.100.1")
		h.Set("X-Forwarded-For", "192.0.2.1, 10.0.0.1")
	}); got != "203.0.113.9" {
		t.Fatalf("CF priority behind private proxy: %q", got)
	}
	if got := mk("127.0.0.1:8443", func(h http.Header) {
		h.Set("X-Real-IP", "198.51.100.7")
		h.Set("X-Forwarded-For", "192.0.2.8")
	}); got != "198.51.100.7" {
		t.Fatalf("X-Real-IP: %q", got)
	}
	if got := mk("192.168.1.1:80", func(h http.Header) {
		h.Set("X-Forwarded-For", "192.0.2.55, 10.1.1.1")
	}); got != "192.0.2.55" {
		t.Fatalf("XFF first: %q", got)
	}
}

func TestCloudflareEdgeNotBanable(t *testing.T) {
	if IsBanableAuthIP("104.16.1.1") {
		t.Fatal("cloudflare edge must not be banable")
	}
	if IsBanableAuthIP("162.158.0.10") {
		t.Fatal("cloudflare edge must not be banable")
	}
	if !IsCloudflareIPString("104.16.1.1") {
		t.Fatal("expected cloudflare")
	}
	if IsCloudflareIPString("8.8.8.8") {
		t.Fatal("8.8.8.8 is not cloudflare")
	}
	// Pseudo IPv4 Class E client addresses remain banable.
	if !IsBanableAuthIP("240.1.2.3") {
		t.Fatal("pseudo IPv4 client should be banable")
	}
	if IsBanableAuthIP("127.0.0.1") || IsBanableAuthIP("10.0.0.1") {
		t.Fatal("loopback/private must not be banable")
	}
}

func TestMissingCFConnectingIPDoesNotBanEdge(t *testing.T) {
	dir := t.TempDir()
	s, err := NewServer(config.MasterConfig{AdminPassword: "pw", DataDir: dir, DisableGeoIPOnline: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store().Close()

	// Simulate Cloudflare edge without CF-Connecting-IP (header strip / misconfig).
	for i := 0; i < store.AuthFailLimit()+2; i++ {
		b, _ := json.Marshal(map[string]string{"password": "wrong"})
		req, _ := http.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "104.16.1.1:443"
		rr := httptest.NewRecorder()
		s.Handler().ServeHTTP(rr, req)
		if rr.Code == 403 {
			t.Fatalf("attempt %d banned edge: body %s", i+1, rr.Body.String())
		}
		if rr.Code != 401 {
			t.Fatalf("attempt %d: status %d", i+1, rr.Code)
		}
	}
	banned, err := s.Store().IsAuthBanned("104.16.1.1")
	if err != nil {
		t.Fatal(err)
	}
	if banned {
		t.Fatal("cloudflare edge was banned")
	}
}

func TestLoginRateLimitBan(t *testing.T) {
	dir := t.TempDir()
	s, err := NewServer(config.MasterConfig{AdminPassword: "pw", DataDir: dir, DisableGeoIPOnline: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store().Close()
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	doLogin := func(pw string) (int, string) {
		b, _ := json.Marshal(map[string]string{"password": pw})
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/login", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", "203.0.113.50")
		// httptest uses 127.0.0.1 so headers are trusted
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out struct{ Error string }
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out.Error
	}

	for i := 0; i < store.AuthFailLimit()-1; i++ {
		code, msg := doLogin("wrong")
		if code != 401 {
			t.Fatalf("attempt %d: %d %s", i+1, code, msg)
		}
	}
	code, msg := doLogin("wrong")
	if code != 403 || msg == "" {
		t.Fatalf("ban expected: %d %q", code, msg)
	}
	code, _ = doLogin("pw")
	if code != 403 {
		t.Fatalf("banned IP still accepted: %d", code)
	}
	code, _ = doLogin("wrong")
	if code != 403 {
		t.Fatalf("banned IP webauthn/login must stay blocked: %d", code)
	}
}

func TestAdminSessionPersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	s1, err := NewServer(config.MasterConfig{AdminPassword: "pw", DataDir: dir, DisableGeoIPOnline: true})
	if err != nil {
		t.Fatal(err)
	}
	srv1 := httptest.NewServer(s1.Handler())
	var login struct{ Token string }
	b, _ := json.Marshal(map[string]string{"password": "pw"})
	resp, err := http.Post(srv1.URL+"/api/login", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	_ = json.NewDecoder(resp.Body).Decode(&login)
	resp.Body.Close()
	if login.Token == "" {
		t.Fatal("no token")
	}
	srv1.Close()
	s1.Store().Close()

	s2, err := NewServer(config.MasterConfig{AdminPassword: "pw", DataDir: dir, DisableGeoIPOnline: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Store().Close()
	srv2 := httptest.NewServer(s2.Handler())
	defer srv2.Close()

	req, _ := http.NewRequest(http.MethodGet, srv2.URL+"/api/meta", nil)
	req.Header.Set("Authorization", "Bearer "+login.Token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("persisted token rejected: %d", resp.StatusCode)
	}
}

func TestStepUpRequiredForCredentialMutate(t *testing.T) {
	s, c := newAPIFixture(t)
	// Inject a fake credential row directly via store.
	fakeID := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	err := s.Store().PutWebAuthnCredential(store.WebAuthnCredential{
		ID:        fakeID,
		Name:      "test-key",
		PublicKey: []byte{9, 9, 9},
		Authenticator: store.WebAuthnAuthenticator{
			AAGUID: []byte{},
		},
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	id := "AQIDBAUGBwg" // base64url of fakeID

	var out struct{ Error string }
	code := c.do(http.MethodDelete, "/api/webauthn/credentials", map[string]string{"id": id}, &out)
	if code != 403 {
		t.Fatalf("delete without step-up: %d %v", code, out)
	}
	out = struct{ Error string }{}
	code = c.do(http.MethodPatch, "/api/webauthn/credentials", map[string]string{"id": id, "name": "x"}, &out)
	if code != 403 {
		t.Fatalf("rename without step-up: %d %v", code, out)
	}
	out = struct{ Error string }{}
	code = c.do(http.MethodDelete, "/api/webauthn/credentials", map[string]any{
		"id": id, "password": "pw",
	}, &out)
	if code != 200 {
		t.Fatalf("delete with password: %d %v", code, out)
	}
	n, err := s.Store().CountWebAuthnCredentials()
	if err != nil || n != 0 {
		t.Fatalf("count after delete: %d %v", n, err)
	}
}
