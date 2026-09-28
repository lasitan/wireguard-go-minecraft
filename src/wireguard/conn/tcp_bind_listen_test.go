package conn

import (
	"fmt"
	"net"
	"testing"
	"time"
)

func recvOne(t *testing.T, fn ReceiveFunc) []byte {
	t.Helper()
	type res struct {
		b   []byte
		err error
	}
	ch := make(chan res, 1)
	go func() {
		bufs := [][]byte{make([]byte, 2048)}
		sizes := make([]int, 1)
		eps := make([]Endpoint, 1)
		n, err := fn(bufs, sizes, eps)
		if err != nil || n == 0 {
			ch <- res{nil, fmt.Errorf("receive n=%d err=%v", n, err)}
			return
		}
		ch <- res{bufs[0][:sizes[0]], nil}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatal(r.err)
		}
		return r.b
	case <-time.After(15 * time.Second):
		t.Fatal("receive timed out")
		return nil
	}
}

// A node turning into (or out of) a server must keep the tunnels it already has.
func TestUpdateListenPortKeepsSessions(t *testing.T) {
	t.Setenv("LASITAN_CONF_DIR", t.TempDir())
	SetTransportConfigJSON(nil)

	srv := NewTCPBind().(*TCPBind)
	fns, port, err := srv.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	cli := NewTCPBind().(*TCPBind)
	if _, _, err := cli.Open(0); err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	ep, err := cli.ParseEndpoint(fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	if err := cli.Send([][]byte{[]byte("before")}, ep); err != nil {
		t.Fatal(err)
	}
	if got := string(recvOne(t, fns[0])); got != "before" {
		t.Fatalf("got %q", got)
	}

	newPort, err := srv.UpdateListenPort(0)
	if err != nil {
		t.Fatal(err)
	}
	if newPort == port || newPort == 0 {
		t.Fatalf("port not moved: %d -> %d", port, newPort)
	}

	if err := cli.Send([][]byte{[]byte("after")}, ep); err != nil {
		t.Fatal(err)
	}
	if got := string(recvOne(t, fns[0])); got != "after" {
		t.Fatalf("got %q", got)
	}

	if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", newPort), time.Second); err != nil {
		t.Fatalf("new port not accepting: %v", err)
	} else {
		c.Close()
	}
	if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second); err == nil {
		c.Close()
		t.Fatal("old port still accepting")
	}
}
