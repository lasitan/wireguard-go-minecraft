package master

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"golang.zx2c4.com/wireguard/meshcfg"
	"golang.zx2c4.com/wireguard/meshcfg/wire"
)

func newHubFixture(t *testing.T) (*Store, *Hub, *httptest.Server) {
	t.Helper()
	st, err := OpenStore(t.TempDir(), "enroll", "10.10.0.0/16")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hub := NewHub(st, NewStatsService(st))
	srv := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	t.Cleanup(srv.Close)
	return st, hub, srv
}

func dialAgent(ctx context.Context, srv *httptest.Server, token string) (*websocket.Conn, error) {
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	ws, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, err
	}
	ws.SetReadLimit(wire.MaxFrameBytes)
	hello := wire.EncodeFrame(wire.TypeHello, 1, wire.Hello{Token: token, Version: "test"}.Marshal())
	if err := ws.Write(ctx, websocket.MessageBinary, hello); err != nil {
		ws.CloseNow()
		return nil, err
	}
	return ws, nil
}

func readFrame(ctx context.Context, ws *websocket.Conn) (wire.Frame, error) {
	_, b, err := ws.Read(ctx)
	if err != nil {
		return wire.Frame{}, err
	}
	return wire.DecodeFrame(b)
}

func TestHubHandshakeStatsAndPush(t *testing.T) {
	st, hub, srv := newHubFixture(t)
	node, err := st.Enroll("a", meshcfg.RoleClient, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ws, err := dialAgent(ctx, srv, node.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()

	f, err := readFrame(ctx, ws)
	if err != nil || f.Type != wire.TypeHelloAck {
		t.Fatalf("want HelloAck, got %v %v", f.Type, err)
	}
	var ack wire.HelloAck
	if err := ack.Unmarshal(f.Payload); err != nil || ack.NodeID != node.ID {
		t.Fatalf("ack: %+v %v", ack, err)
	}
	f, err = readFrame(ctx, ws)
	if err != nil || f.Type != wire.TypeConfigPush {
		t.Fatalf("want ConfigPush, got %v %v", f.Type, err)
	}
	var d meshcfg.DesiredConfig
	if err := json.Unmarshal(f.Payload, &d); err != nil || d.NodeID != node.ID {
		t.Fatalf("config: %+v %v", d, err)
	}

	for i, rx := range []uint64{100, 300} {
		b := wire.EncodeFrame(wire.TypeStats, uint32(i+2), wire.Stats{RxBytes: rx, TxBytes: rx / 2}.Marshal())
		if err := ws.Write(ctx, websocket.MessageBinary, b); err != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if live, _ := hub.stats.Live(node.ID); live.RxRate > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("stats not ingested")
		}
		time.Sleep(20 * time.Millisecond)
	}

	hub.PushAll()
	f, err = readFrame(ctx, ws)
	if err != nil || f.Type != wire.TypeConfigPush {
		t.Fatalf("want pushed config, got %v %v", f.Type, err)
	}
}

func TestHubRejectsBadToken(t *testing.T) {
	_, hub, srv := newHubFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, err := dialAgent(ctx, srv, "nope")
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	f, err := readFrame(ctx, ws)
	if err != nil || f.Type != wire.TypeError {
		t.Fatalf("want Error, got %v %v", f.Type, err)
	}
	var em wire.ErrorMsg
	if em.Unmarshal(f.Payload) != nil || em.Code != wire.ErrCodeAuth {
		t.Fatalf("error msg: %+v", em)
	}
	if hub.Count() != 0 {
		t.Fatal("rejected agent must not be registered")
	}
}

func TestHubReplacesDuplicateConnection(t *testing.T) {
	st, hub, srv := newHubFixture(t)
	node, err := st.Enroll("a", meshcfg.RoleClient, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, err := dialAgent(ctx, srv, node.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer first.CloseNow()
	if _, err := readFrame(ctx, first); err != nil {
		t.Fatal(err)
	}
	second, err := dialAgent(ctx, srv, node.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer second.CloseNow()
	if _, err := readFrame(ctx, second); err != nil {
		t.Fatal(err)
	}
	// The old connection must be closed by the server.
	for {
		if _, err := readFrame(ctx, first); err != nil {
			break
		}
	}
	if hub.Count() != 1 {
		t.Fatalf("count = %d", hub.Count())
	}
}

// TestHubManyConnections holds 1000 concurrent agents, each reporting stats,
// then broadcasts a config push to all of them.
func TestHubManyConnections(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test")
	}
	const n = 1000
	st, hub, srv := newHubFixture(t)
	tokens := make([]string, n)
	for i := range tokens {
		node, err := st.Enroll(fmt.Sprintf("n%d", i), meshcfg.RoleClient, "", 0)
		if err != nil {
			t.Fatal(err)
		}
		tokens[i] = node.Token
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	conns := make([]*websocket.Conn, n)
	var wg sync.WaitGroup
	errs := make(chan error, n)
	sem := make(chan struct{}, 64)
	for i := range tokens {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ws, err := dialAgent(ctx, srv, tokens[i])
			if err != nil {
				errs <- err
				return
			}
			conns[i] = ws
			for want := 0; want < 2; want++ { // HelloAck + ConfigPush
				if _, err := readFrame(ctx, ws); err != nil {
					errs <- err
					return
				}
			}
			b := wire.EncodeFrame(wire.TypeStats, 2, wire.Stats{RxBytes: 1, TxBytes: 1}.Marshal())
			if err := ws.Write(ctx, websocket.MessageBinary, b); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	defer func() {
		for _, c := range conns {
			if c != nil {
				c.CloseNow()
			}
		}
	}()
	if got := hub.Count(); got != n {
		t.Fatalf("connected = %d, want %d", got, n)
	}

	start := time.Now()
	hub.PushAll()
	var got sync.WaitGroup
	for _, ws := range conns {
		got.Add(1)
		go func(ws *websocket.Conn) {
			defer got.Done()
			f, err := readFrame(ctx, ws)
			if err != nil || f.Type != wire.TypeConfigPush {
				t.Errorf("push: %v %v", f.Type, err)
			}
		}(ws)
	}
	got.Wait()
	t.Logf("pushed config to %d agents in %v", n, time.Since(start))
}
