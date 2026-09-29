package hub_test

import (
	"context"
	"errors"
	"github.com/coder/websocket"
	"golang.zx2c4.com/wireguard/src/core"
	"golang.zx2c4.com/wireguard/src/core/wire"
	. "golang.zx2c4.com/wireguard/src/master/hub"
	"testing"
	"time"
)

func TestRequestUpdateRoundTrip(t *testing.T) {
	st, hub, srv := newHubFixture(t)
	node, err := st.Enroll("a", core.RoleClient, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hub.RequestUpdate(node.ID, wire.UpdateCmd{}); !errors.Is(err, ErrAgentOffline) {
		t.Fatalf("offline: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ws, err := dialAgent(ctx, srv, node.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	if f, err := readFrame(ctx, ws); err != nil || f.Type != wire.TypeHelloAck {
		t.Fatalf("want HelloAck, got %v %v", f.Type, err)
	}

	// Fake agent: answer the update command with an ack.
	go func() {
		for {
			f, err := readNonPing(ctx, ws)
			if err != nil {
				return
			}
			if f.Type != wire.TypeUpdate {
				continue
			}
			var cmd wire.UpdateCmd
			_ = cmd.Unmarshal(f.Payload)
			ack := wire.UpdateAck{OK: true, Message: "started"}
			if cmd.Force {
				ack.Message = "forced"
			}
			if cmd.Proxy != "" {
				ack.Message += " via " + cmd.Proxy + " " + string(cmd.Release)
			}
			_ = ws.Write(ctx, websocket.MessageBinary, wire.EncodeFrame(wire.TypeUpdateAck, 9, ack.Marshal()))
		}
	}()

	for _, force := range []bool{false, true} {
		ack, err := hub.RequestUpdate(node.ID, wire.UpdateCmd{Force: force})
		if err != nil || !ack.OK {
			t.Fatalf("force=%v: %+v %v", force, ack, err)
		}
		if want := map[bool]string{false: "started", true: "forced"}[force]; ack.Message != want {
			t.Fatalf("force=%v: message %q", force, ack.Message)
		}
	}
	ack, err := hub.RequestUpdate(node.ID, wire.UpdateCmd{Proxy: "https://ghfast.top/", Release: []byte(`{"tag":"v1"}`)})
	if err != nil || ack.Message != `started via https://ghfast.top/ {"tag":"v1"}` {
		t.Fatalf("proxy: %+v %v", ack, err)
	}
}
