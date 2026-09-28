package config

import (
	"crypto/rand"
	"encoding/base64"
	"net"
	"sort"
	"strconv"
	"testing"

	"golang.zx2c4.com/wireguard/src/core"
	"golang.zx2c4.com/wireguard/src/wireguard/conn"
	"golang.zx2c4.com/wireguard/src/wireguard/device"
	"golang.zx2c4.com/wireguard/src/wireguard/tun/tuntest"
)

func randKey(t *testing.T) (string, device.NoisePublicKey) {
	t.Helper()
	var k device.NoisePublicKey
	if _, err := rand.Read(k[:]); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(k[:]), k
}

func freePort(t *testing.T) uint16 {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return uint16(l.Addr().(*net.TCPAddr).Port)
}

func allowedOf(t *testing.T, dev *device.Device, pubB64 string) []string {
	t.Helper()
	live, err := readLivePeers(dev)
	if err != nil {
		t.Fatal(err)
	}
	pubHex, _ := base64ToHex(pubB64)
	p := live[pubHex]
	if p == nil {
		return nil
	}
	var out []string
	for a := range p.allowed {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

// A new revision must update peers in place: unchanged peers are the same
// objects (sessions kept), only the delta is added/removed.
func TestApplyDesiredConfigIsHot(t *testing.T) {
	t.Setenv("WG_CONF_DIR", t.TempDir())
	conn.SetTransportConfigJSON(nil)
	logger := device.NewLogger(device.LogLevelSilent, "")
	dev := device.NewDevice(tuntest.NewChannelTUN().TUN(), conn.NewTCPBind(), logger)
	defer dev.Close()
	if err := dev.Up(); err != nil {
		t.Fatal(err)
	}

	priv, _ := randKey(t)
	aB64, aKey := randKey(t)
	bB64, bKey := randKey(t)
	cB64, cKey := randKey(t)

	rev1 := &core.DesiredConfig{Revision: 1, Peers: []core.DesiredPeer{
		{PublicKey: aB64, Endpoint: "127.0.0.1:1", AllowedIPs: []string{"10.0.0.2/32"}},
		{PublicKey: bB64, AllowedIPs: []string{"10.0.0.3/32"}},
	}}
	rev1.Interface.PrivateKey = priv
	if _, err := ApplyDesiredConfig(dev, logger, "wgtest", rev1, &core.DesiredConfig{}); err != nil {
		t.Fatal(err)
	}
	peerA := dev.LookupPeer(aKey)
	if peerA == nil || dev.LookupPeer(bKey) == nil {
		t.Fatal("rev1 peers missing")
	}

	port := freePort(t)
	rev2 := &core.DesiredConfig{Revision: 2, Peers: []core.DesiredPeer{
		{PublicKey: aB64, Endpoint: "127.0.0.1:1", AllowedIPs: []string{"10.0.0.2/32", "10.0.1.7/24"}},
		{PublicKey: cB64, AllowedIPs: []string{"10.0.0.4/32"}, Keepalive: 5},
	}}
	rev2.Interface.PrivateKey = priv
	rev2.Interface.ListenPort = port
	if _, err := ApplyDesiredConfig(dev, logger, "wgtest", rev2, rev1); err != nil {
		t.Fatal(err)
	}
	if dev.LookupPeer(aKey) != peerA {
		t.Fatal("unchanged peer was recreated (cold restart)")
	}
	if dev.LookupPeer(bKey) != nil {
		t.Fatal("removed peer still present")
	}
	if dev.LookupPeer(cKey) == nil {
		t.Fatal("added peer missing")
	}
	if got := allowedOf(t, dev, aB64); len(got) != 2 || got[0] != "10.0.0.2/32" || got[1] != "10.0.1.0/24" {
		t.Fatalf("peer A allowed IPs: %v", got)
	}
	if c, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port)))); err != nil {
		t.Fatalf("listen port not moved to %d: %v", port, err)
	} else {
		c.Close()
	}

	rev3 := &core.DesiredConfig{Revision: 3, Peers: []core.DesiredPeer{
		{PublicKey: aB64, Endpoint: "127.0.0.1:1", AllowedIPs: []string{"10.0.1.0/24"}},
		rev2.Peers[1],
	}}
	rev3.Interface = rev2.Interface
	if _, err := ApplyDesiredConfig(dev, logger, "wgtest", rev3, rev2); err != nil {
		t.Fatal(err)
	}
	if dev.LookupPeer(aKey) != peerA {
		t.Fatal("peer recreated on allowed-ip change")
	}
	if got := allowedOf(t, dev, aB64); len(got) != 1 || got[0] != "10.0.1.0/24" {
		t.Fatalf("peer A allowed IPs after removal: %v", got)
	}
}
