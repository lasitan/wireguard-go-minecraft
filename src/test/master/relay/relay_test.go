package relay_test

import (
	"fmt"
	"golang.zx2c4.com/wireguard/src/core"
	. "golang.zx2c4.com/wireguard/src/master/relay"
	"golang.zx2c4.com/wireguard/src/master/store"
	"golang.zx2c4.com/wireguard/src/wireguard/conn"
	"golang.zx2c4.com/wireguard/src/wireguard/device"
	"golang.zx2c4.com/wireguard/src/wireguard/tun/tuntest"
	"net"
	"net/netip"
	"testing"
	"time"
)

func freePort(t *testing.T) uint16 {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return uint16(l.Addr().(*net.TCPAddr).Port)
}

func keys(t *testing.T) (priv, pub, privHex, pubHex string) {
	priv, pub, err := core.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	privHex, _ = B64hex(priv)
	pubHex, _ = B64hex(pub)
	return
}

// Two nodes that only know Master's relay reach each other through it.
func TestRelayForwardsBetweenNodes(t *testing.T) {
	rPriv, rPub, _, rPubHex := keys(t)
	_, cPub, cPrivHex, _ := keys(t)
	_, yPub, yPrivHex, _ := keys(t)
	port := freePort(t)

	s := &Service{}
	if err := s.ApplyLocked(store.RelayConfig{Port: port, PrivateKey: rPriv, PublicKey: rPub}, []core.DesiredPeer{
		{PublicKey: cPub, AllowedIPs: []string{"10.10.0.5/32"}},
		{PublicKey: yPub, AllowedIPs: []string{"10.20.0.9/32"}},
	}); err != nil {
		t.Fatal(err)
	}
	defer s.CloseLocked()

	node := func(privHex, allowed string) (*tuntest.ChannelTUN, *device.Device) {
		tn := tuntest.NewChannelTUN()
		d := device.NewDevice(tn.TUN(), conn.NewTCPBind(), device.NewLogger(device.LogLevelError, "node: "))
		cfg := fmt.Sprintf("private_key=%s\nlisten_port=0\npublic_key=%s\nendpoint=127.0.0.1:%d\nallowed_ip=%s\npersistent_keepalive_interval=1\n", privHex, rPubHex, port, allowed)
		if err := d.IpcSet(cfg); err != nil {
			t.Fatal(err)
		}
		if err := d.Up(); err != nil {
			t.Fatal(err)
		}
		return tn, d
	}
	cTun, cDev := node(cPrivHex, "10.20.0.0/24")
	defer cDev.Close()
	yTun, yDev := node(yPrivHex, "10.10.0.5/32")
	defer yDev.Close()

	c, y := netip.MustParseAddr("10.10.0.5"), netip.MustParseAddr("10.20.0.9")
	send := func(from *tuntest.ChannelTUN, to *tuntest.ChannelTUN, pkt []byte) {
		deadline := time.After(10 * time.Second)
		for {
			select {
			case from.Outbound <- pkt:
			case <-deadline:
				t.Fatal("timed out sending")
			}
			select {
			case got := <-to.Inbound:
				if string(got) != string(pkt) {
					t.Fatalf("mangled packet")
				}
				return
			case <-time.After(500 * time.Millisecond):
			case <-deadline:
				t.Fatal("packet never arrived through the relay")
			}
		}
	}
	send(cTun, yTun, tuntest.Ping(y, c))
	send(yTun, cTun, tuntest.Ping(c, y))
}
