package conn

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

const testLoginName = "Qx7Wanderer"

func camoBind(t *testing.T, profile, secret string, secure bool) *TCPBind {
	t.Helper()
	js, _ := json.Marshal(map[string]any{"camouflage": map[string]any{
		"profile": profile, "deep": true, "secure": secure,
		"loginPluginSecret": secret, "handshakeTimeout": "3s", "loginUsername": testLoginName,
	}})
	var f transportConfigFile
	if err := json.Unmarshal(js, &f); err != nil {
		t.Fatal(err)
	}
	shared, mc, err := loadCamouflageConfig(f)
	if err != nil {
		t.Fatal(err)
	}
	b := &TCPBind{}
	b.camoShared, b.mcConfig = shared, mc
	return b
}

// tap forwards between a and b and records what each side sent.
type tap struct {
	mu       sync.Mutex
	c2s, s2c bytes.Buffer
}

func (w *tap) pipe(t *testing.T) (client, server net.Conn) {
	c1, c2 := net.Pipe()
	s1, s2 := net.Pipe()
	cp := func(dst, src net.Conn, rec *bytes.Buffer) {
		buf := make([]byte, 4096)
		for {
			n, err := src.Read(buf)
			if n > 0 {
				w.mu.Lock()
				rec.Write(buf[:n])
				w.mu.Unlock()
				if _, werr := dst.Write(buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				dst.Close()
				return
			}
		}
	}
	go cp(s1, c2, &w.c2s)
	go cp(c2, s1, &w.s2c)
	t.Cleanup(func() { c1.Close(); c2.Close(); s1.Close(); s2.Close() })
	return c1, s2
}

type hsResult struct {
	conn  net.Conn
	toNAT bool
	err   error
}

func handshake(t *testing.T, cli, srv *TCPBind, cc, sc net.Conn) (c, s hsResult) {
	t.Helper()
	dst := netip.MustParseAddrPort("203.0.113.7:25565")
	done := make(chan hsResult, 1)
	go func() {
		conn, toNAT, err := srv.performCamouflageServer(sc)
		if err != nil {
			sc.Close()
		}
		done <- hsResult{conn, toNAT, err}
	}()
	conn, err := cli.performCamouflageClient(cc, dst)
	if err != nil {
		cc.Close()
	}
	c = hsResult{conn: conn, err: err}
	select {
	case s = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("server handshake hung")
	}
	return c, s
}

func TestSecureCamouflageRoundTrip(t *testing.T) {
	for _, profile := range []string{"minecraft", "source", "steam"} {
		t.Run(profile, func(t *testing.T) {
			cli, srv := camoBind(t, profile, "s3cret", true), camoBind(t, profile, "s3cret", true)
			cli.dialToNAT.Store(true)
			var w tap
			cc, sc := w.pipe(t)
			c, s := handshake(t, cli, srv, cc, sc)
			if c.err != nil || s.err != nil {
				t.Fatalf("handshake: client=%v server=%v", c.err, s.err)
			}
			if !s.toNAT {
				t.Fatal("toNAT flag must survive the secure handshake")
			}
			secret := []byte("WG-FRAME-PLAINTEXT-MARKER")
			go func() { _ = writeAll(c.conn, secret) }()
			got := make([]byte, len(secret))
			if _, err := io.ReadFull(s.conn, got); err != nil || !bytes.Equal(got, secret) {
				t.Fatalf("server read %q %v", got, err)
			}
			go func() { _ = writeAll(s.conn, secret) }()
			if _, err := io.ReadFull(c.conn, got); err != nil || !bytes.Equal(got, secret) {
				t.Fatalf("client read %q %v", got, err)
			}
			w.mu.Lock()
			defer w.mu.Unlock()
			if bytes.Contains(w.c2s.Bytes(), secret) || bytes.Contains(w.s2c.Bytes(), secret) {
				t.Fatal("payload visible on the wire")
			}
			if profile == "minecraft" {
				if !bytes.Contains(w.c2s.Bytes(), []byte(testLoginName)) {
					t.Fatal("Login Start is cleartext, like a real client")
				}
				if bytes.Contains(w.s2c.Bytes(), []byte(testLoginName)) {
					t.Fatal("Login Success must already be encrypted")
				}
			}
		})
	}
}

func TestSecureCamouflageRejectsWrongSecret(t *testing.T) {
	for _, profile := range []string{"minecraft", "source"} {
		t.Run(profile, func(t *testing.T) {
			// A middlebox posing as the server without the secret.
			cli, fake := camoBind(t, profile, "right", true), camoBind(t, profile, "wrong", true)
			cc, sc := net.Pipe()
			defer cc.Close()
			defer sc.Close()
			c, s := handshake(t, cli, fake, cc, sc)
			if c.err == nil || s.err == nil {
				t.Fatalf("mismatched secrets must fail on both ends: client=%v server=%v", c.err, s.err)
			}
		})
	}
}

// A recorded client session replayed to the server must not authenticate.
func TestSecureCamouflageRejectsReplay(t *testing.T) {
	for _, profile := range []string{"minecraft", "source"} {
		t.Run(profile, func(t *testing.T) {
			cli, srv := camoBind(t, profile, "s3cret", true), camoBind(t, profile, "s3cret", true)
			var w tap
			cc, sc := w.pipe(t)
			if c, s := handshake(t, cli, srv, cc, sc); c.err != nil || s.err != nil {
				t.Fatalf("baseline: %v %v", c.err, s.err)
			}
			w.mu.Lock()
			recorded := append([]byte(nil), w.c2s.Bytes()...)
			w.mu.Unlock()

			attacker, victim := net.Pipe()
			defer attacker.Close()
			go func() { _, _ = io.Copy(io.Discard, attacker) }()
			go func() { _ = writeAll(attacker, recorded) }()
			_, _, err := srv.performCamouflageServer(victim)
			if err == nil {
				t.Fatal("replayed handshake accepted")
			}
		})
	}
}

func TestLegacyPeerGetsClearError(t *testing.T) {
	cli, old := camoBind(t, "minecraft", "s3cret", true), camoBind(t, "minecraft", "s3cret", false)
	cc, sc := net.Pipe()
	defer cc.Close()
	defer sc.Close()
	c, _ := handshake(t, cli, old, cc, sc)
	if c.err == nil || !strings.Contains(c.err.Error(), "legacy") {
		t.Fatalf("want a legacy hint, got %v", c.err)
	}
}

func TestSecureBlobLooksLikeRSAModulus(t *testing.T) {
	hello, _, err := secureServerHello(securePSK("x"))
	if err != nil {
		t.Fatal(err)
	}
	req, err := encodeMCEncryptionRequest(hello, []byte{1, 2, 3, 4})
	if err != nil {
		t.Fatal(err)
	}
	_, rest, _ := mcPacketHead(req)
	got, err := decodeMCEncryptionRequest(rest)
	if err != nil || !bytes.Equal(got, hello) {
		t.Fatalf("modulus round trip: %v", err)
	}
	if _, _, err := openSecureBlob(securePSK("y"), "server", nil, hello); !errors.Is(err, errSecureAuth) {
		t.Fatal("blob must not open under another secret")
	}
}
