package conn_test

import (
	"fmt"
	"testing"
	"time"

	. "golang.zx2c4.com/wireguard/src/wireguard/conn"
)

func recvWithEndpoint(t *testing.T, fn ReceiveFunc) ([]byte, Endpoint) {
	t.Helper()
	type res struct {
		b   []byte
		ep  Endpoint
		err error
	}
	ch := make(chan res, 1)
	go func() {
		bufs := [][]byte{make([]byte, 2048)}
		sizes := make([]int, 1)
		eps := make([]Endpoint, 1)
		n, err := fn(bufs, sizes, eps)
		if err != nil || n == 0 {
			ch <- res{err: fmt.Errorf("receive n=%d err=%v", n, err)}
			return
		}
		ch <- res{b: bufs[0][:sizes[0]], ep: eps[0]}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatal(r.err)
		}
		return r.b, r.ep
	case <-time.After(15 * time.Second):
		t.Fatal("receive timed out")
		return nil, nil
	}
}

// WireGuard keepalives flow one way; heartbeats must keep an idle tunnel up in
// both directions past rxIdleTimeout, and never surface as packets.
func TestIdleSessionSurvivesRxIdleTimeout(t *testing.T) {
	t.Setenv("LASITAN_CONF_DIR", t.TempDir())
	SetTransportConfigJSON([]byte(`{"tcp":{"rxIdleTimeout":"2s"}}`))
	defer SetTransportConfigJSON(nil)

	srv := NewTCPBind().(*TCPBind)
	srvFns, port, err := srv.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	cli := NewTCPBind().(*TCPBind)
	cliFns, _, err := cli.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	ep, err := cli.ParseEndpoint(fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	if err := cli.Send([][]byte{[]byte("hello")}, ep); err != nil {
		t.Fatal(err)
	}
	got, back := recvWithEndpoint(t, srvFns[0])
	if string(got) != "hello" {
		t.Fatalf("got %q", got)
	}

	time.Sleep(5 * time.Second)

	// The server never dials back: this only works if the inbound session lived.
	if err := srv.Send([][]byte{[]byte("reply")}, back); err != nil {
		t.Fatalf("idle session was torn down: %v", err)
	}
	if got, _ := recvWithEndpoint(t, cliFns[0]); string(got) != "reply" {
		t.Fatalf("client got %q (heartbeat leaked?)", got)
	}
	// Same client source port = the original TCP connection, not a redial.
	if err := cli.Send([][]byte{[]byte("again")}, ep); err != nil {
		t.Fatal(err)
	}
	if _, again := recvWithEndpoint(t, srvFns[0]); again.DstToString() != back.DstToString() {
		t.Fatalf("session was redialed: %s -> %s", back.DstToString(), again.DstToString())
	}
}

// A dropped outbound session is rebuilt right away, so the listening side can
// reply without waiting for the client's next packet or keepalive.
func TestDroppedSessionRedialsInBackground(t *testing.T) {
	t.Setenv("LASITAN_CONF_DIR", t.TempDir())
	SetTransportConfigJSON(nil)

	srv := NewTCPBind().(*TCPBind)
	srvFns, port, err := srv.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	cli := NewTCPBind().(*TCPBind)
	cliFns, _, err := cli.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	ep, _ := cli.ParseEndpoint(fmt.Sprintf("127.0.0.1:%d", port))
	if err := cli.Send([][]byte{[]byte("hello")}, ep); err != nil {
		t.Fatal(err)
	}
	_, back := recvWithEndpoint(t, srvFns[0])

	srv.KillSessions()
	deadline := time.Now().Add(5 * time.Second)
	for srv.Send([][]byte{[]byte("reply")}, back) != nil {
		if time.Now().After(deadline) {
			t.Fatal("client did not reconnect on its own")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if got, _ := recvWithEndpoint(t, cliFns[0]); string(got) != "reply" {
		t.Fatalf("client got %q", got)
	}
}
