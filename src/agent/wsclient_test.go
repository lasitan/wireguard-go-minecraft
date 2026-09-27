package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/src/core/config"
)

func TestWSURL(t *testing.T) {
	cases := map[string]string{
		"http://m.example:8443/":  "ws://m.example:8443/api/agent/ws",
		"https://m.example":       "wss://m.example/api/agent/ws",
		"https://m.example/base/": "wss://m.example/base/api/agent/ws",
	}
	for in, want := range cases {
		if got := wsURL(in); got != want {
			t.Errorf("wsURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// An old Master has no /api/agent/ws: the handshake must fail as "not
// connected" so ConfigLoop falls back to HTTP polling instead of spinning.
func TestWSClientOldMasterFallsBack(t *testing.T) {
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer old.Close()

	c := &wsClient{boot: &config.AgentBootstrap{MasterURL: old.URL, Key: "tok"}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connected, err := c.run(ctx)
	if connected || err == nil {
		t.Fatalf("connected=%v err=%v; want handshake failure", connected, err)
	}
}
