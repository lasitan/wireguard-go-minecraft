package update_test

import (
	. "golang.zx2c4.com/wireguard/src/update"
	"os"
	"path/filepath"
	"testing"
)

func TestManifestRoundTrip(t *testing.T) {
	rel := &Release{Tag: "v2.1.0", Version: "2.1.0", Assets: []Asset{
		{Name: "lasitan-cluster-linux-amd64", URL: "https://github.com/x/y/releases/download/v2.1.0/a", Size: 42, SHA256: "ab"},
	}}
	b, err := rel.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseManifest(b)
	if err != nil {
		t.Fatal(err)
	}
	a, ok := got.Asset("lasitan-cluster-linux-amd64")
	if !ok || got.Tag != rel.Tag || a != rel.Assets[0] {
		t.Fatalf("round trip: %+v", got)
	}
	if _, err := ParseManifest([]byte(`{"tag":"v1"}`)); err == nil {
		t.Fatal("manifest without assets accepted")
	}
}

func TestLoadManifestRemovesFile(t *testing.T) {
	rel := &Release{Tag: "v2.1.0", Assets: []Asset{{Name: "a", URL: "u"}}}
	b, _ := rel.Manifest()
	path := filepath.Join(t.TempDir(), "m.json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	got := LoadManifest(path)
	if got == nil || got.Version != "2.1.0" {
		t.Fatalf("load: %+v", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("manifest left behind: %v", err)
	}
}
