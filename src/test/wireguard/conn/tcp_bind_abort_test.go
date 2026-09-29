package conn_test

import (
	. "golang.zx2c4.com/wireguard/src/wireguard/conn"
	"net"
	"testing"
	"time"
)

// Removing a peer must not wait out dial/MC-handshake timeouts: the device
// holds its locks while the sender is blocked in Send.
func TestAbortDialUnblocksSend(t *testing.T) {
	t.Setenv("LASITAN_CONF_DIR", t.TempDir())
	SetTransportConfigJSON(nil)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close() // accept but never answer the handshake
		}
	}()

	b := NewTCPBind().(*TCPBind)
	if _, _, err := b.Open(0); err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	ep, err := b.ParseEndpoint(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	sent := make(chan error, 1)
	go func() { sent <- b.Send([][]byte{make([]byte, 32)}, ep) }()

	select {
	case err := <-sent:
		t.Fatalf("Send returned before abort: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	b.AbortDial(ep)
	select {
	case err := <-sent:
		if err == nil {
			t.Fatal("aborted Send reported success")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Send still blocked after AbortDial")
	}
}
