//go:build linux

package linux_test

import (
	"encoding/json"
	. "golang.zx2c4.com/wireguard/src/systems/linux"
	"os"
	"path/filepath"
	"testing"
)

func TestRewriteMasterDataDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "master.json")
	in := map[string]any{
		"listen":        ":8443",
		"adminPassword": "x",
		"dataDir":       LegacyDataDir,
	}
	b, _ := json.MarshalIndent(in, "", "  ")
	if err := os.WriteFile(path, append(b, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RewriteMasterDataDir(path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out["dataDir"] != NewDataDir {
		t.Fatalf("dataDir=%v want %s", out["dataDir"], NewDataDir)
	}
}

func TestUniqueStrings(t *testing.T) {
	got := UniqueStrings([]string{"wg0", "", "wg0", " lc0 ", "lc0"})
	if len(got) != 2 || got[0] != "wg0" || got[1] != "lc0" {
		t.Fatalf("got %#v", got)
	}
}
