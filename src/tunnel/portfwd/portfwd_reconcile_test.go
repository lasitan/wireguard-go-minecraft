package portfwd

import (
	"io"
	"net"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/src/tunnel/spec"
	"golang.zx2c4.com/wireguard/src/wireguard/device"
)

func echoServer(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); _, _ = io.Copy(c, c) }()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func roundTrip(c net.Conn, msg string) error {
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := c.Write([]byte(msg)); err != nil {
		return err
	}
	buf := make([]byte, len(msg))
	_, err := io.ReadFull(c, buf)
	return err
}

// Unchanged forwards must keep their live connections across revisions.
func TestReconcileKeepsUnchangedForwards(t *testing.T) {
	dest := echoServer(t)
	fwA := spec.PortForwardSpec{Proto: spec.ProtoTCP, ListenHost: "127.0.0.1", ListenPort: freeTCPPort(t), DestHost: "127.0.0.1", DestPort: dest}
	fwB := spec.PortForwardSpec{Proto: spec.ProtoTCP, ListenHost: "127.0.0.1", ListenPort: freeTCPPort(t), DestHost: "127.0.0.1", DestPort: dest}

	m := NewPortForwardManager(device.NewLogger(device.LogLevelSilent, ""))
	defer m.Close()
	if err := m.Reconcile([]spec.PeerHookConfig{{Label: "p", Forwards: []spec.PortForwardSpec{fwA}}}); err != nil {
		t.Fatal(err)
	}
	c, err := net.Dial("tcp", fwA.ListenAddr())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := roundTrip(c, "one"); err != nil {
		t.Fatal(err)
	}

	if err := m.Reconcile([]spec.PeerHookConfig{{Label: "p", Forwards: []spec.PortForwardSpec{fwA, fwB}}}); err != nil {
		t.Fatal(err)
	}
	if err := roundTrip(c, "two"); err != nil {
		t.Fatalf("unchanged forward dropped its connection: %v", err)
	}
	if m.Count() != 2 {
		t.Fatalf("count %d", m.Count())
	}

	if err := m.Reconcile([]spec.PeerHookConfig{{Label: "p", Forwards: []spec.PortForwardSpec{fwB}}}); err != nil {
		t.Fatal(err)
	}
	if err := roundTrip(c, "three"); err == nil {
		t.Fatal("removed forward still proxying")
	}
	if _, err := net.DialTimeout("tcp", fwA.ListenAddr(), time.Second); err == nil {
		t.Fatal("removed forward still listening")
	}
}
