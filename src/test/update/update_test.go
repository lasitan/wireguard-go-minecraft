package update_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	. "golang.zx2c4.com/wireguard/src/update"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"2.0.3", "2.0.2", true},
		{"v2.0.3", "2.0.2", true},
		{"2.0.2", "v2.0.2", false},
		{"2.1", "2.0.9", true},
		{"2.0.10", "2.0.9", true},
		{"2.0.2", "2.0.3", false},
		{"2.0.3", "2.0.3-dev", true},
		{"2.0.3-dev", "2.0.3", false},
		{"2.0.2", "dev", false},
		{"", "2.0.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestLatestParsesGitHub(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v2.0.3","html_url":"https://x/r","body":"notes",
			"published_at":"2026-09-27T08:00:00Z",
			"assets":[{"name":"lasitan-cluster_2.0.3-1_amd64.deb","browser_download_url":"https://x/a.deb","size":10,"digest":"sha256:abcd"}]}`))
	}))
	defer srv.Close()
	defer SwapLatestURL(srv.URL)()

	rel, err := Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "2.0.3" || rel.Tag != "v2.0.3" {
		t.Fatalf("version %q tag %q", rel.Version, rel.Tag)
	}
	a, ok := rel.Asset("lasitan-cluster_2.0.3-1_amd64.deb")
	if !ok || a.SHA256 != "abcd" || a.Size != 10 {
		t.Fatalf("asset %+v ok=%v", a, ok)
	}
}

func TestDownloadVerifiesDigest(t *testing.T) {
	body := []byte("new-binary")
	sum := sha256.Sum256(body)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	dir := t.TempDir()

	good := Asset{Name: "bin", URL: srv.URL, Size: int64(len(body)), SHA256: hex.EncodeToString(sum[:])}
	p, err := Download(context.Background(), good, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != string(body) {
		t.Fatalf("content %q", b)
	}

	bad := good
	bad.SHA256 = "00"
	if _, err := Download(context.Background(), bad, dir, nil); err == nil {
		t.Fatal("expected digest mismatch")
	}
	left, _ := filepath.Glob(filepath.Join(dir, ".lasitan-update-*"))
	if len(left) != 1 {
		t.Fatalf("temp files left: %v", left)
	}
}

func TestReplaceExecutable(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "lasitan-cluster")
	next := filepath.Join(dir, "next")
	_ = os.WriteFile(exe, []byte("old"), 0o755)
	_ = os.WriteFile(next, []byte("new"), 0o644)
	if err := ReplaceExecutable(exe, next); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new" {
		t.Fatalf("exe = %q", b)
	}
}

func TestCheckerStatus(t *testing.T) {
	SetCurrent("v2.0.2")
	c := NewChecker(0, false)
	c.Fetch = func(context.Context) (*Release, error) { return &Release{Tag: "v2.0.3", Version: "2.0.3"}, nil }
	c.CheckNow()
	s := c.Status()
	if s.Current != "2.0.2" || s.Latest != "2.0.3" || !s.HasUpdate {
		t.Fatalf("status %+v", s)
	}
}
