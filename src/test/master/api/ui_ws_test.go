package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"golang.zx2c4.com/wireguard/src/core"
	"golang.zx2c4.com/wireguard/src/core/wire"
)

func dialUI(t *testing.T, ctx context.Context, base, token string) *websocket.Conn {
	t.Helper()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(base, "http")+"/api/ui/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.CloseNow() })
	if err := ws.Write(ctx, websocket.MessageBinary, wire.EncodeFrame(wire.TypeUIHello, 0, wire.UIHello{Token: token}.Marshal())); err != nil {
		t.Fatal(err)
	}
	return ws
}

func readUI(t *testing.T, ctx context.Context, ws *websocket.Conn) wire.Frame {
	t.Helper()
	_, b, err := ws.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	f, err := wire.DecodeFrame(b)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// readUntil skips periodic frames until one of type want arrives.
func readUntil(t *testing.T, ctx context.Context, ws *websocket.Conn, want wire.MsgType, ok func(wire.Frame) bool) wire.Frame {
	t.Helper()
	for {
		f := readUI(t, ctx, ws)
		if f.Type == want && (ok == nil || ok(f)) {
			return f
		}
	}
}

func TestUIWebSocketRejectsBadToken(t *testing.T) {
	_, c := newAPIFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws := dialUI(t, ctx, c.base, "nope")
	f := readUI(t, ctx, ws)
	var e wire.ErrorMsg
	if f.Type != wire.TypeUIError || e.Unmarshal(f.Payload) != nil || e.Code != wire.ErrCodeAuth {
		t.Fatalf("got type %d %+v", f.Type, e)
	}
}

func TestUIWebSocketPushes(t *testing.T) {
	s, c := newAPIFixture(t)
	stop := make(chan struct{})
	defer close(stop)
	go s.RunUI(stop)
	node, err := s.Store().Enroll("srv", core.RoleServer, "1.2.3.4:25590", 0)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ws := dialUI(t, ctx, c.base, c.token)

	for _, want := range []wire.MsgType{wire.TypeUIHelloAck, wire.TypeUIMesh, wire.TypeUIMeta, wire.TypeUIVersion, wire.TypeUIPresence} {
		f := readUI(t, ctx, ws)
		if f.Type != want {
			t.Fatalf("initial frame type %d, want %d", f.Type, want)
		}
		switch f.Type {
		case wire.TypeUIMesh:
			var m core.Mesh
			if err := json.Unmarshal(f.Payload, &m); err != nil || m.FindNode(node.ID) == nil {
				t.Fatalf("mesh: %v", err)
			}
			for _, n := range m.Nodes {
				if n.PrivateKey != "" {
					t.Fatal("ui mesh must not expose private keys")
				}
			}
		case wire.TypeUIPresence:
			var p wire.Presence
			if err := p.Unmarshal(f.Payload); err != nil || len(p.Nodes) != 1 || p.Nodes[0].NodeID != node.ID || p.Nodes[0].Link == wire.LinkWS {
				t.Fatalf("presence: %+v %v", p, err)
			}
		}
	}

	if err := ws.Write(ctx, websocket.MessageBinary, wire.EncodeFrame(wire.TypeUISubscribe, 0, wire.UISubscribe{NodeID: node.ID}.Marshal())); err != nil {
		t.Fatal(err)
	}
	f := readUntil(t, ctx, ws, wire.TypeUINodeStats, nil)
	var st struct {
		NodeID string `json:"nodeId"`
		Link   string `json:"link"`
	}
	if err := json.Unmarshal(f.Payload, &st); err != nil || st.NodeID != node.ID || st.Link == "ws" {
		t.Fatalf("node stats: %+v %v", st, err)
	}

	if code := c.do(http.MethodPatch, "/api/nodes?id="+node.ID, map[string]string{"name": "renamed"}, nil); code != 200 {
		t.Fatalf("patch: %d", code)
	}
	readUntil(t, ctx, ws, wire.TypeUIMesh, func(f wire.Frame) bool {
		var m core.Mesh
		if json.Unmarshal(f.Payload, &m) != nil {
			return false
		}
		n := m.FindNode(node.ID)
		return n != nil && n.Name == "renamed"
	})
}
