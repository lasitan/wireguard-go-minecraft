package agent_test

import (
	"context"
	. "golang.zx2c4.com/wireguard/src/agent"
	"golang.zx2c4.com/wireguard/src/core/config"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWSURL(t *testing.T) {
	cases := map[string]string{
		"http://m.example:8443/":  "ws://m.example:8443/api/agent/ws",
		"https://m.example":       "wss://m.example/api/agent/ws",
		"https://m.example/base/": "wss://m.example/base/api/agent/ws",
	}
	for in, want := range cases {
		if got := WsURL(in); got != want {
			t.Errorf("WsURL(%q) = %q, want %q", in, got, want)
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

	c := NewWsClientForTest(&config.AgentBootstrap{MasterURL: old.URL, Key: "tok"})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connected, err := c.Run(ctx)
	if connected || err == nil {
		t.Fatalf("connected=%v err=%v; want handshake failure", connected, err)
	}
}
