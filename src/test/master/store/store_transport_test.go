package store_test

import (
	"encoding/json"
	. "golang.zx2c4.com/wireguard/src/master/store"
	"strings"
	"testing"
)

// The web UI sends transportJson as a string; it must be stored as the object
// itself or GET /api/meta (and every agent) sees a string instead of a config.
func TestTransportJSONStringIsUnwrapped(t *testing.T) {
	dir := t.TempDir()
	st, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{
		"transportJson": `{"camouflage":{"profile":"source","deep":false,"loginPluginSecret":"abcdefgh12"}}`,
	})
	var p SettingsPatch
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateSettings(p); err != nil {
		t.Fatal(err)
	}
	s, err := st.Settings()
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Camouflage struct {
			Profile string `json:"profile"`
		} `json:"camouflage"`
	}
	if err := json.Unmarshal(s.TransportJSON, &got); err != nil || got.Camouflage.Profile != "source" {
		t.Fatalf("stored %s: %v", s.TransportJSON, err)
	}

	bad, _ := json.Marshal(map[string]any{"transportJson": 42})
	_ = json.Unmarshal(bad, &p)
	if err := st.UpdateSettings(p); err == nil {
		t.Fatal("non-object transport accepted")
	}

	// A database already holding the string form is repaired on open.
	wrapped, _ := json.Marshal(string(s.TransportJSON))
	if err := st.MetaSet(MetaTransportJSON, string(wrapped)); err != nil {
		t.Fatal(err)
	}
	st.Close()
	st2, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	s2, _ := st2.Settings()
	if !strings.HasPrefix(strings.TrimSpace(string(s2.TransportJSON)), "{") {
		t.Fatalf("not repaired: %s", s2.TransportJSON)
	}
}
