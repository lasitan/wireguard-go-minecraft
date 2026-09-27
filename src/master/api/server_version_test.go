package master

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"golang.zx2c4.com/wireguard/internal/update"
	"golang.zx2c4.com/wireguard/meshcfg"
	"golang.zx2c4.com/wireguard/meshcfg/wire"
)

func TestVersionAPI(t *testing.T) {
	update.SetCurrent("2.0.2")
	s, c := newAPIFixture(t)
	s.updates.Fetch = func(context.Context) (*update.Release, error) {
		return &update.Release{Tag: "v2.0.3", Version: "2.0.3", URL: update.ReleasesURL + "/tag/v2.0.3"}, nil
	}
	s.updates.CheckNow()

	oldAgent, _ := s.store.Enroll("old", meshcfg.RoleClient, "", 0)
	newAgent, _ := s.store.Enroll("new", meshcfg.RoleClient, "", 0)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, a := range []struct{ token, ver string }{{oldAgent.Token, "wg-mc-agent/2"}, {newAgent.Token, "2.0.3"}} {
		ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(c.base, "http")+"/api/agent/ws", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer ws.CloseNow()
		hello := wire.EncodeFrame(wire.TypeHello, 1, wire.Hello{Token: a.token, Version: a.ver}.Marshal())
		if err := ws.Write(ctx, websocket.MessageBinary, hello); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ws.Read(ctx); err != nil { // HelloAck
			t.Fatal(err)
		}
	}

	var v versionView
	if code := c.do(http.MethodGet, "/api/version", nil, &v); code != 200 {
		t.Fatalf("status %d", code)
	}
	if v.Current != "2.0.2" || v.Latest != "2.0.3" || !v.HasUpdate {
		t.Fatalf("version view %+v", v.Status)
	}
	if !strings.Contains(v.Commands.Linux, "install.sh") || v.Commands.Installed == "" {
		t.Fatalf("commands %+v", v.Commands)
	}
	if len(v.OutdatedAgents) != 1 || v.OutdatedAgents[0].NodeID != oldAgent.ID || v.OutdatedAgents[0].Version != "" {
		t.Fatalf("outdated %+v", v.OutdatedAgents)
	}

	var st nodeStatsView
	c.do(http.MethodGet, "/api/nodes/stats?id="+newAgent.ID, nil, &st)
	if st.AgentVersion != "2.0.3" || st.Link != "ws" {
		t.Fatalf("stats version %q link %q", st.AgentVersion, st.Link)
	}

	anon := &apiClient{t: t, base: c.base}
	if code := anon.do(http.MethodGet, "/api/version", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("anonymous status %d", code)
	}
}
