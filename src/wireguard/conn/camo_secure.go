/* SPDX-License-Identifier: MIT */

package conn

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hmac"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"golang.org/x/crypto/hkdf"
)

// Secure camouflage: after the cosmetic game handshake both sides run a
// pre-shared-secret authenticated ephemeral X25519 exchange and then encrypt
// the whole TCP stream (like an online-mode Minecraft server does after
// Encryption Response). A middlebox that terminates or replays the TCP
// session learns nothing without the secret, and recorded traffic stays
// sealed even if the secret leaks later.
//
// Hello/reply blobs are 128 bytes of uniform-looking data, the size of an
// RSA-1024 modulus / ciphertext:
//
//	[0:16]   salt
//	[16:48]  ephemeral X25519 public key, masked
//	[48]     flags, masked (bit0: toNAT, client only)
//	[49:65]  tag = HMAC(psk, label || prior transcript || blob[0:49])[:16]
//	[65:128] random filler
const (
	secureBlobLen   = 128
	secureTagOff    = 49
	secureTagLen    = 16
	secureConfirmSz = 16
	secureInfo      = "lasitan-secure-v1"
)

var errSecureAuth = errors.New("camouflage peer failed secret authentication")

func securePSK(secret string) []byte {
	sum := sha256.Sum256([]byte(secureInfo + "\x00psk\x00" + secret))
	return sum[:]
}

func secureMask(psk, salt []byte) ([]byte, error) {
	m := make([]byte, 33)
	_, err := io.ReadFull(hkdf.New(sha256.New, psk, salt, []byte(secureInfo+" mask")), m)
	return m, err
}

func secureTag(psk []byte, label string, prior, head []byte) []byte {
	mac := hmac.New(sha256.New, psk)
	mac.Write([]byte(label))
	mac.Write(prior)
	mac.Write(head)
	return mac.Sum(nil)[:secureTagLen]
}

func sealSecureBlob(psk []byte, label string, prior []byte, pub []byte, flags byte) ([]byte, error) {
	blob := make([]byte, secureBlobLen)
	if _, err := cryptorand.Read(blob); err != nil {
		return nil, err
	}
	// Shape it like an RSA-1024 modulus: full bit length, odd.
	blob[0] |= 0x80
	blob[secureBlobLen-1] |= 1
	mask, err := secureMask(psk, blob[:16])
	if err != nil {
		return nil, err
	}
	for i := 0; i < 32; i++ {
		blob[16+i] = pub[i] ^ mask[i]
	}
	blob[48] = flags ^ mask[32]
	copy(blob[secureTagOff:], secureTag(psk, label, prior, blob[:secureTagOff]))
	return blob, nil
}

func openSecureBlob(psk []byte, label string, prior, blob []byte) (pub []byte, flags byte, err error) {
	if len(blob) != secureBlobLen {
		return nil, 0, errSecureAuth
	}
	if !hmac.Equal(blob[secureTagOff:secureTagOff+secureTagLen], secureTag(psk, label, prior, blob[:secureTagOff])) {
		return nil, 0, errSecureAuth
	}
	mask, err := secureMask(psk, blob[:16])
	if err != nil {
		return nil, 0, err
	}
	pub = make([]byte, 32)
	for i := range pub {
		pub[i] = blob[16+i] ^ mask[i]
	}
	return pub, blob[48] ^ mask[32], nil
}

type secureKeys struct {
	c2s, s2c []byte // 32-byte AES key || 16-byte IV
}

func deriveSecureKeys(psk, shared, hello, reply []byte) (secureKeys, error) {
	salt := sha256.New()
	salt.Write(hello)
	salt.Write(reply)
	ikm := append(append([]byte(nil), shared...), psk...)
	r := hkdf.New(sha256.New, ikm, salt.Sum(nil), []byte(secureInfo+" stream"))
	k := secureKeys{c2s: make([]byte, 48), s2c: make([]byte, 48)}
	if _, err := io.ReadFull(r, k.c2s); err != nil {
		return k, err
	}
	_, err := io.ReadFull(r, k.s2c)
	return k, err
}

// secureServerHello returns the server's hello blob and its ephemeral key.
func secureServerHello(psk []byte) ([]byte, *ecdh.PrivateKey, error) {
	priv, err := ecdh.X25519().GenerateKey(cryptorand.Reader)
	if err != nil {
		return nil, nil, err
	}
	blob, err := sealSecureBlob(psk, "server", nil, priv.PublicKey().Bytes(), 0)
	return blob, priv, err
}

// secureClientReply authenticates the server hello and answers it.
func secureClientReply(psk, hello []byte, toNAT bool) ([]byte, secureKeys, error) {
	spub, _, err := openSecureBlob(psk, "server", nil, hello)
	if err != nil {
		return nil, secureKeys{}, err
	}
	peer, err := ecdh.X25519().NewPublicKey(spub)
	if err != nil {
		return nil, secureKeys{}, errSecureAuth
	}
	priv, err := ecdh.X25519().GenerateKey(cryptorand.Reader)
	if err != nil {
		return nil, secureKeys{}, err
	}
	shared, err := priv.ECDH(peer)
	if err != nil {
		return nil, secureKeys{}, errSecureAuth
	}
	var flags byte
	if toNAT {
		flags = 1
	}
	reply, err := sealSecureBlob(psk, "client", hello, priv.PublicKey().Bytes(), flags)
	if err != nil {
		return nil, secureKeys{}, err
	}
	keys, err := deriveSecureKeys(psk, shared, hello, reply)
	return reply, keys, err
}

// secureServerFinish authenticates the client reply (bound to this hello, so
// replays fail) and derives the stream keys.
func secureServerFinish(psk, hello []byte, priv *ecdh.PrivateKey, reply []byte) (secureKeys, bool, error) {
	cpub, flags, err := openSecureBlob(psk, "client", hello, reply)
	if err != nil || flags > 1 {
		return secureKeys{}, false, errSecureAuth
	}
	peer, err := ecdh.X25519().NewPublicKey(cpub)
	if err != nil {
		return secureKeys{}, false, errSecureAuth
	}
	shared, err := priv.ECDH(peer)
	if err != nil {
		return secureKeys{}, false, errSecureAuth
	}
	keys, err := deriveSecureKeys(psk, shared, hello, reply)
	return keys, flags == 1, err
}

// secureConn encrypts everything after the key exchange with AES-256-CTR,
// one keystream per direction.
type secureConn struct {
	net.Conn
	rd  cipher.Stream
	wmu sync.Mutex
	wr  cipher.Stream
}

func newSecureConn(c net.Conn, k secureKeys, server bool) (*secureConn, error) {
	in, out := k.s2c, k.c2s
	if server {
		in, out = k.c2s, k.s2c
	}
	rb, err := aes.NewCipher(in[:32])
	if err != nil {
		return nil, err
	}
	wb, err := aes.NewCipher(out[:32])
	if err != nil {
		return nil, err
	}
	return &secureConn{Conn: c, rd: cipher.NewCTR(rb, in[32:48]), wr: cipher.NewCTR(wb, out[32:48])}, nil
}

func (c *secureConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.rd.XORKeyStream(p[:n], p[:n])
	}
	return n, err
}

func (c *secureConn) Write(p []byte) (int, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	buf := make([]byte, len(p))
	c.wr.XORKeyStream(buf, p)
	// A short write desyncs the keystream; the stream is dead either way.
	return c.Conn.Write(buf)
}

// Generic (non-Minecraft profiles): raw blobs after the cosmetic handshake,
// then an encrypted confirm from the server proves it derived the same keys.

func (b *TCPBind) secureGenericClient(conn net.Conn, cfg camoSharedConfig) (net.Conn, error) {
	if err := conn.SetDeadline(deadline(cfg.timeout)); err != nil {
		return nil, err
	}
	defer conn.SetDeadline(zeroDeadline())
	psk := securePSK(cfg.pluginSecret)
	hello := make([]byte, secureBlobLen)
	if _, err := io.ReadFull(conn, hello); err != nil {
		return nil, fmt.Errorf("secure hello: %w", err)
	}
	reply, keys, err := secureClientReply(psk, hello, b.dialToNAT.Load())
	if err != nil {
		return nil, fmt.Errorf("secure hello: %w (check loginPluginSecret on both peers)", err)
	}
	if err := writeAll(conn, reply); err != nil {
		return nil, err
	}
	sc, err := newSecureConn(conn, keys, false)
	if err != nil {
		return nil, err
	}
	confirm := make([]byte, secureConfirmSz)
	if _, err := io.ReadFull(sc, confirm); err != nil {
		return nil, fmt.Errorf("secure confirm: %w", err)
	}
	for _, x := range confirm {
		if x != 0 {
			return nil, errSecureAuth
		}
	}
	return sc, nil
}

func (b *TCPBind) secureGenericServer(conn net.Conn, cfg camoSharedConfig) (net.Conn, bool, error) {
	if err := conn.SetDeadline(deadline(cfg.timeout)); err != nil {
		return nil, false, err
	}
	defer conn.SetDeadline(zeroDeadline())
	psk := securePSK(cfg.pluginSecret)
	hello, priv, err := secureServerHello(psk)
	if err != nil {
		return nil, false, err
	}
	if err := writeAll(conn, hello); err != nil {
		return nil, false, err
	}
	reply := make([]byte, secureBlobLen)
	if _, err := io.ReadFull(conn, reply); err != nil {
		return nil, false, err
	}
	keys, toNAT, err := secureServerFinish(psk, hello, priv, reply)
	if err != nil {
		return nil, false, err
	}
	sc, err := newSecureConn(conn, keys, true)
	if err != nil {
		return nil, false, err
	}
	if err := writeAll(sc, make([]byte, secureConfirmSz)); err != nil {
		return nil, false, err
	}
	return sc, toNAT, nil
}
