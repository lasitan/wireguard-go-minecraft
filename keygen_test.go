package main

import (
	"encoding/base64"
	"testing"

	"golang.org/x/crypto/curve25519"
)

func TestGenkeyClampedAndPubkeyRoundTrip(t *testing.T) {
	var priv [32]byte
	for i := range priv {
		priv[i] = byte(i * 7)
	}
	clampKey(&priv)
	if priv[0]&7 != 0 {
		t.Fatalf("low bits not cleared: %02x", priv[0])
	}
	if priv[31]&128 != 0 || priv[31]&64 == 0 {
		t.Fatalf("high bits not clamped: %02x", priv[31])
	}

	b64 := base64.StdEncoding.EncodeToString(priv[:])
	decoded, err := decodeKey32(b64)
	if err != nil {
		t.Fatal(err)
	}
	clampKey(&decoded)
	var pub [32]byte
	curve25519.ScalarBaseMult(&pub, &decoded)
	pubB64 := base64.StdEncoding.EncodeToString(pub[:])
	raw, err := base64.StdEncoding.DecodeString(pubB64)
	if err != nil || len(raw) != 32 {
		t.Fatalf("bad public key: %v len=%d", err, len(raw))
	}
}

func TestDecodeKey32RejectsBadLength(t *testing.T) {
	_, err := decodeKey32(base64.StdEncoding.EncodeToString([]byte("short")))
	if err == nil {
		t.Fatal("expected error for short key")
	}
}
