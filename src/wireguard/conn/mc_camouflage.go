/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

package conn

import (
	"bytes"
	"crypto/hmac"
	"crypto/md5"
	cryptorand "crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"math/rand/v2"
	"net"
	"net/netip"
	"time"
)

// Minecraft Java protocol packet IDs (protocol ~760, 1.20.x).
const (
	mcStateHandshake = 0
	mcStateStatus    = 1
	mcStateLogin     = 2

	mcIDHandshake = 0x00

	mcIDStatusRequest  = 0x00
	mcIDStatusResponse = 0x00
	mcIDPingRequest    = 0x01
	mcIDPongResponse   = 0x01

	mcIDLoginStart          = 0x00
	mcIDEncryptionResponse  = 0x01
	mcIDLoginPluginResponse = 0x02
	mcIDDisconnect          = 0x00
	mcIDEncryptionRequest   = 0x01
	mcIDLoginSuccess        = 0x02
	mcIDLoginPluginRequest  = 0x04

	defaultLoginUsername       = "Steve"
	defaultLoginPluginChannel  = "minecraft:register"
	defaultLoginPluginSecret   = "wireguard-go-internal"
	defaultRejectDisconnectMsg = "You are not whitelisted on this server!"
)

type mcDeepConfig struct {
	enabled       bool
	deep          bool
	timeout       time.Duration
	loginUsername string
	pluginChannel string
	pluginSecret  string
	rejectMessage string
}

func (b *TCPBind) mcDeepConfig() mcDeepConfig {
	cfg := b.getMCConfig()
	return mcDeepConfig{
		enabled:       cfg.enabled,
		deep:          cfg.deep,
		timeout:       cfg.timeout,
		loginUsername: cfg.loginUsername,
		pluginChannel: cfg.pluginChannel,
		pluginSecret:  cfg.pluginSecret,
		rejectMessage: cfg.rejectMessage,
	}
}

// randomMCStatusJSON generates a fresh Minecraft server list ping response
// each call, with a randomised online-player count and sample names so that
// repeated probes see slightly different responses — consistent with a live server.
func randomMCStatusJSON() string {
	// Adjectives and nouns used to build random player names.
	adjectives := []string{
		"Dark", "Swift", "Brave", "Cool", "Wise", "Wild", "Bold",
		"Calm", "Keen", "Soft", "Fast", "Deep", "True", "Just",
	}
	nouns := []string{
		"Steve", "Alex", "Notch", "Creep", "Ender", "Blaze",
		"Arrow", "Stone", "Sand", "Gold", "Iron", "Frost",
	}

	maxPlayers := 20
	// Online player count: 0–4.
	count := rand.IntN(5)

	sample := make([]map[string]string, 0, count)
	for i := 0; i < count; i++ {
		name := adjectives[rand.IntN(len(adjectives))] + nouns[rand.IntN(len(nouns))]
		sample = append(sample, map[string]string{
			"name": name,
			"id":   "00000000-0000-0000-0000-000000000000",
		})
	}

	payload := map[string]any{
		"version": map[string]any{
			"name":     "1.20.4",
			"protocol": defaultMCProtocol,
		},
		"players": map[string]any{
			"max":    maxPlayers,
			"online": count,
			"sample": sample,
		},
		"description": map[string]string{
			"text": "A Minecraft Server",
		},
	}
	b, _ := json.Marshal(payload)
	return string(b)
}

func (b *TCPBind) performMCCamouflageClient(conn net.Conn, dst netip.AddrPort) (net.Conn, error) {
	cfg := b.mcDeepConfig()
	if !cfg.enabled {
		return conn, nil
	}
	if b.getCamo().secure {
		return b.performMCCamouflageClientSecure(conn, dst, cfg)
	}
	if !cfg.deep {
		return conn, b.performMCCamouflageClientShallow(conn, dst, cfg)
	}
	return conn, b.performMCCamouflageClientDeep(conn, dst, cfg)
}

func (b *TCPBind) performMCCamouflageServer(conn net.Conn) (net.Conn, bool, error) {
	cfg := b.mcDeepConfig()
	if !cfg.enabled {
		return conn, false, nil
	}
	if b.getCamo().secure {
		return b.performMCCamouflageServerSecure(conn, cfg)
	}
	if !cfg.deep {
		return conn, false, b.performMCCamouflageServerShallow(conn, cfg)
	}
	toNAT, err := b.performMCCamouflageServerDeep(conn, cfg)
	return conn, toNAT, err
}

// mcStatusPing is the server-list probe a real client sends before logging in.
func mcStatusPingClient(conn net.Conn, host string, port uint16) error {
	if err := writeMCPacket(conn, encodeMCHandshake(defaultMCProtocol, host, port, mcStateStatus)); err != nil {
		return fmt.Errorf("mc status handshake: %w", err)
	}
	if err := writeMCPacket(conn, encodeMCStatusRequest()); err != nil {
		return fmt.Errorf("mc status request: %w", err)
	}
	statusResp, err := readMCPacket(conn)
	if err != nil {
		return fmt.Errorf("mc status response: %w", err)
	}
	if err := decodeMCStatusResponse(statusResp); err != nil {
		return fmt.Errorf("mc status invalid: %w", err)
	}
	pingTime := time.Now().UnixMilli()
	if err := writeMCPacket(conn, encodeMCPing(pingTime)); err != nil {
		return fmt.Errorf("mc ping: %w", err)
	}
	pong, err := readMCPacket(conn)
	if err != nil {
		return fmt.Errorf("mc pong: %w", err)
	}
	return decodeMCPong(pong, pingTime)
}

func mcStatusPingServer(conn net.Conn) error {
	hs, err := readMCPacket(conn)
	if err != nil {
		return fmt.Errorf("mc status handshake: %w", err)
	}
	if err := decodeMCHandshake(hs, mcStateStatus); err != nil {
		return fmt.Errorf("mc status handshake invalid: %w", err)
	}
	req, err := readMCPacket(conn)
	if err != nil {
		return fmt.Errorf("mc status request: %w", err)
	}
	if err := decodeMCStatusRequest(req); err != nil {
		return fmt.Errorf("mc status request invalid: %w", err)
	}
	if err := writeMCPacket(conn, encodeMCStatusResponse(randomMCStatusJSON())); err != nil {
		return fmt.Errorf("mc status response: %w", err)
	}
	ping, err := readMCPacket(conn)
	if err != nil {
		return fmt.Errorf("mc ping: %w", err)
	}
	t, err := decodeMCPing(ping)
	if err != nil {
		return fmt.Errorf("mc ping invalid: %w", err)
	}
	return writeMCPacket(conn, encodeMCPong(t))
}

// Secure login looks like an online-mode server: Login Start → Encryption
// Request (our hello inside the RSA modulus) → Encryption Response (our reply
// as the "encrypted shared secret") → everything encrypted from here on,
// starting with Login Success, which doubles as the server's key confirmation.
func (b *TCPBind) performMCCamouflageClientSecure(conn net.Conn, dst netip.AddrPort, cfg mcDeepConfig) (net.Conn, error) {
	if err := conn.SetDeadline(time.Now().Add(cfg.timeout)); err != nil {
		return nil, err
	}
	defer conn.SetDeadline(time.Time{})
	host, port := dst.Addr().String(), dst.Port()
	if err := mcStatusPingClient(conn, host, port); err != nil {
		return nil, err
	}
	if err := writeMCPacket(conn, encodeMCHandshake(defaultMCProtocol, host, port, mcStateLogin)); err != nil {
		return nil, fmt.Errorf("mc login handshake: %w", err)
	}
	if err := writeMCPacket(conn, encodeMCLoginStartUUID(cfg.loginUsername)); err != nil {
		return nil, fmt.Errorf("mc login start: %w", err)
	}
	pkt, err := readMCPacket(conn)
	if err != nil {
		return nil, fmt.Errorf("mc encryption request: %w", err)
	}
	id, rest, err := mcPacketHead(pkt)
	if err != nil {
		return nil, err
	}
	switch id {
	case mcIDEncryptionRequest:
	case mcIDDisconnect:
		return nil, fmt.Errorf("server rejected MC login (%s)", decodeMCDisconnectMessage(rest))
	case mcIDLoginPluginRequest:
		return nil, errors.New("peer runs legacy (unencrypted) camouflage; upgrade it or set camouflage.secure=false on every node")
	default:
		return nil, fmt.Errorf("unexpected packet id %d", id)
	}
	hello, err := decodeMCEncryptionRequest(rest)
	if err != nil {
		return nil, fmt.Errorf("mc encryption request invalid: %w", err)
	}
	psk := securePSK(cfg.pluginSecret)
	reply, keys, err := secureClientReply(psk, hello, b.dialToNAT.Load())
	if err != nil {
		return nil, fmt.Errorf("mc encryption request: %w (possible MITM, or loginPluginSecret differs)", err)
	}
	token := make([]byte, secureBlobLen)
	if _, err := cryptorand.Read(token); err != nil {
		return nil, err
	}
	if err := writeMCPacket(conn, encodeMCEncryptionResponse(reply, token)); err != nil {
		return nil, fmt.Errorf("mc encryption response: %w", err)
	}
	sc, err := newSecureConn(conn, keys, false)
	if err != nil {
		return nil, err
	}
	success, err := readMCPacket(sc)
	if err != nil {
		return nil, fmt.Errorf("mc login success: %w", err)
	}
	if err := decodeMCLoginSuccess(success, cfg.loginUsername); err != nil {
		return nil, fmt.Errorf("mc login success invalid (possible MITM): %w", err)
	}
	return sc, nil
}

func (b *TCPBind) performMCCamouflageServerSecure(conn net.Conn, cfg mcDeepConfig) (net.Conn, bool, error) {
	if err := conn.SetDeadline(time.Now().Add(cfg.timeout)); err != nil {
		return nil, false, err
	}
	defer conn.SetDeadline(time.Time{})
	if err := mcStatusPingServer(conn); err != nil {
		return nil, false, err
	}
	hs, err := readMCPacket(conn)
	if err != nil {
		return nil, false, fmt.Errorf("mc login handshake: %w", err)
	}
	if err := decodeMCHandshake(hs, mcStateLogin); err != nil {
		return nil, false, fmt.Errorf("mc login handshake invalid: %w", err)
	}
	start, err := readMCPacket(conn)
	if err != nil {
		return nil, false, fmt.Errorf("mc login start: %w", err)
	}
	if _, err := decodeMCLoginStart(start); err != nil {
		return nil, false, fmt.Errorf("mc login start invalid: %w", err)
	}
	psk := securePSK(cfg.pluginSecret)
	hello, priv, err := secureServerHello(psk)
	if err != nil {
		return nil, false, err
	}
	verify := make([]byte, 4)
	if _, err := cryptorand.Read(verify); err != nil {
		return nil, false, err
	}
	req, err := encodeMCEncryptionRequest(hello, verify)
	if err != nil {
		return nil, false, err
	}
	if err := writeMCPacket(conn, req); err != nil {
		return nil, false, fmt.Errorf("mc encryption request: %w", err)
	}
	resp, err := readMCPacket(conn)
	if err != nil {
		return nil, false, fmt.Errorf("mc encryption response: %w", err)
	}
	id, rest, err := mcPacketHead(resp)
	if err != nil || id != mcIDEncryptionResponse {
		return nil, false, sendMCDisconnect(conn, cfg.rejectMessage)
	}
	reply, err := decodeMCEncryptionResponse(rest)
	if err != nil {
		return nil, false, sendMCDisconnect(conn, cfg.rejectMessage)
	}
	keys, toNAT, err := secureServerFinish(psk, hello, priv, reply)
	if err != nil {
		// Same as a real server failing to decrypt: drop without a word.
		return nil, false, err
	}
	sc, err := newSecureConn(conn, keys, true)
	if err != nil {
		return nil, false, err
	}
	if err := writeMCPacket(sc, encodeMCLoginSuccess(cfg.loginUsername)); err != nil {
		return nil, false, fmt.Errorf("mc login success: %w", err)
	}
	return sc, toNAT, nil
}

// encodeMCEncryptionRequest carries hello as the modulus of an RSA-1024 key.
func encodeMCEncryptionRequest(hello, verifyToken []byte) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(&rsa.PublicKey{N: new(big.Int).SetBytes(hello), E: 65537})
	if err != nil {
		return nil, err
	}
	var p bytes.Buffer
	writeMCVarInt(&p, mcIDEncryptionRequest)
	writeMCString(&p, "") // server id: empty since 1.7
	writeMCByteArray(&p, der)
	writeMCByteArray(&p, verifyToken)
	return p.Bytes(), nil
}

func decodeMCEncryptionRequest(rest []byte) ([]byte, error) {
	r := bytes.NewReader(rest)
	if _, err := readMCString(r); err != nil {
		return nil, err
	}
	der, err := readMCByteArray(r)
	if err != nil {
		return nil, err
	}
	if _, err := readMCByteArray(r); err != nil {
		return nil, err
	}
	pub, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, err
	}
	rk, ok := pub.(*rsa.PublicKey)
	if !ok || rk.N.BitLen() != secureBlobLen*8 {
		return nil, errors.New("unexpected server key")
	}
	return rk.N.FillBytes(make([]byte, secureBlobLen)), nil
}

func encodeMCEncryptionResponse(sharedSecret, verifyToken []byte) []byte {
	var p bytes.Buffer
	writeMCVarInt(&p, mcIDEncryptionResponse)
	writeMCByteArray(&p, sharedSecret)
	writeMCByteArray(&p, verifyToken)
	return p.Bytes()
}

func decodeMCEncryptionResponse(rest []byte) ([]byte, error) {
	r := bytes.NewReader(rest)
	secret, err := readMCByteArray(r)
	if err != nil {
		return nil, err
	}
	if _, err := readMCByteArray(r); err != nil {
		return nil, err
	}
	if r.Len() != 0 || len(secret) != secureBlobLen {
		return nil, errors.New("bad encryption response")
	}
	return secret, nil
}

// encodeMCLoginStartUUID is the 1.20.2+ Login Start: name + player UUID.
func encodeMCLoginStartUUID(username string) []byte {
	var payload bytes.Buffer
	writeMCVarInt(&payload, mcIDLoginStart)
	writeMCString(&payload, username)
	uuid := offlinePlayerUUID(username)
	payload.Write(uuid[:])
	return payload.Bytes()
}

func (b *TCPBind) performMCCamouflageClientShallow(conn net.Conn, dst netip.AddrPort, cfg mcDeepConfig) error {
	if err := conn.SetDeadline(time.Now().Add(cfg.timeout)); err != nil {
		return err
	}
	defer conn.SetDeadline(time.Time{})

	host, port := dst.Addr().String(), dst.Port()
	if err := writeMCPacket(conn, encodeMCHandshake(defaultMCProtocol, host, port, mcStateStatus)); err != nil {
		return fmt.Errorf("mc shallow client handshake: %w", err)
	}
	if err := writeMCPacket(conn, encodeMCStatusRequest()); err != nil {
		return fmt.Errorf("mc shallow client status request: %w", err)
	}
	resp, err := readMCPacket(conn)
	if err != nil {
		return fmt.Errorf("mc shallow client status response: %w", err)
	}
	if err := decodeMCStatusResponse(resp); err != nil {
		return fmt.Errorf("mc shallow client status invalid: %w", err)
	}
	return nil
}

func (b *TCPBind) performMCCamouflageServerShallow(conn net.Conn, cfg mcDeepConfig) error {
	if err := conn.SetDeadline(time.Now().Add(cfg.timeout)); err != nil {
		return err
	}
	defer conn.SetDeadline(time.Time{})

	handshakePayload, err := readMCPacket(conn)
	if err != nil {
		return fmt.Errorf("mc shallow server handshake: %w", err)
	}
	if err := decodeMCHandshake(handshakePayload, mcStateStatus); err != nil {
		return fmt.Errorf("mc shallow server handshake invalid: %w", err)
	}
	statusReq, err := readMCPacket(conn)
	if err != nil {
		return fmt.Errorf("mc shallow server status request: %w", err)
	}
	if err := decodeMCStatusRequest(statusReq); err != nil {
		return fmt.Errorf("mc shallow server status request invalid: %w", err)
	}
	if err := writeMCPacket(conn, encodeMCStatusResponse(randomMCStatusJSON())); err != nil {
		return fmt.Errorf("mc shallow server status response: %w", err)
	}
	return nil
}

func (b *TCPBind) performMCCamouflageClientDeep(conn net.Conn, dst netip.AddrPort, cfg mcDeepConfig) error {
	if err := conn.SetDeadline(time.Now().Add(cfg.timeout)); err != nil {
		return err
	}
	defer conn.SetDeadline(time.Time{})

	host, port := dst.Addr().String(), dst.Port()

	// --- Status state (server list probe compatible) ---
	if err := writeMCPacket(conn, encodeMCHandshake(defaultMCProtocol, host, port, mcStateStatus)); err != nil {
		return fmt.Errorf("mc deep client status handshake: %w", err)
	}
	if err := writeMCPacket(conn, encodeMCStatusRequest()); err != nil {
		return fmt.Errorf("mc deep client status request: %w", err)
	}
	statusResp, err := readMCPacket(conn)
	if err != nil {
		return fmt.Errorf("mc deep client status response: %w", err)
	}
	if err := decodeMCStatusResponse(statusResp); err != nil {
		return fmt.Errorf("mc deep client status invalid: %w", err)
	}
	pingTime := time.Now().UnixMilli()
	if err := writeMCPacket(conn, encodeMCPing(pingTime)); err != nil {
		return fmt.Errorf("mc deep client ping: %w", err)
	}
	pongPayload, err := readMCPacket(conn)
	if err != nil {
		return fmt.Errorf("mc deep client pong: %w", err)
	}
	if err := decodeMCPong(pongPayload, pingTime); err != nil {
		return fmt.Errorf("mc deep client pong invalid: %w", err)
	}

	// --- Login state (WG peer authentication) ---
	if err := writeMCPacket(conn, encodeMCHandshake(defaultMCProtocol, host, port, mcStateLogin)); err != nil {
		return fmt.Errorf("mc deep client login handshake: %w", err)
	}
	if err := writeMCPacket(conn, encodeMCLoginStart(cfg.loginUsername)); err != nil {
		return fmt.Errorf("mc deep client login start: %w", err)
	}
	pluginReq, err := readMCPacket(conn)
	if err != nil {
		return fmt.Errorf("mc deep client login plugin request: %w", err)
	}
	challenge, err := decodeMCLoginPluginRequest(pluginReq)
	if err != nil {
		return fmt.Errorf("mc deep client login plugin request invalid: %w", err)
	}
	if err := validatePluginChannel(challenge.channel, cfg.pluginChannel); err != nil {
		return err
	}
	responseData := buildPluginResponse(cfg.pluginSecret, challenge.data, b.dialToNAT.Load())
	if err := writeMCPacket(conn, encodeMCLoginPluginResponse(cfg.pluginChannel, responseData)); err != nil {
		return fmt.Errorf("mc deep client login plugin response: %w", err)
	}
	loginSuccess, err := readMCPacket(conn)
	if err != nil {
		return fmt.Errorf("mc deep client login success: %w", err)
	}
	if err := decodeMCLoginSuccess(loginSuccess, cfg.loginUsername); err != nil {
		return fmt.Errorf("mc deep client login success invalid: %w", err)
	}
	return nil
}

func (b *TCPBind) performMCCamouflageServerDeep(conn net.Conn, cfg mcDeepConfig) (bool, error) {
	if err := conn.SetDeadline(time.Now().Add(cfg.timeout)); err != nil {
		return false, err
	}

	// --- Status state ---
	handshakePayload, err := readMCPacket(conn)
	if err != nil {
		return false, fmt.Errorf("mc deep server status handshake: %w", err)
	}
	if err := decodeMCHandshake(handshakePayload, mcStateStatus); err != nil {
		return false, fmt.Errorf("mc deep server status handshake invalid: %w", err)
	}
	statusReq, err := readMCPacket(conn)
	if err != nil {
		return false, fmt.Errorf("mc deep server status request: %w", err)
	}
	if err := decodeMCStatusRequest(statusReq); err != nil {
		return false, fmt.Errorf("mc deep server status request invalid: %w", err)
	}
	if err := writeMCPacket(conn, encodeMCStatusResponse(randomMCStatusJSON())); err != nil {
		return false, fmt.Errorf("mc deep server status response: %w", err)
	}
	pingPayload, err := readMCPacket(conn)
	if err != nil {
		return false, fmt.Errorf("mc deep server ping: %w", err)
	}
	pingTime, err := decodeMCPing(pingPayload)
	if err != nil {
		return false, fmt.Errorf("mc deep server ping invalid: %w", err)
	}
	if err := writeMCPacket(conn, encodeMCPong(pingTime)); err != nil {
		return false, fmt.Errorf("mc deep server pong: %w", err)
	}

	// --- Login state ---
	loginHandshake, err := readMCPacket(conn)
	if err != nil {
		return false, fmt.Errorf("mc deep server login handshake: %w", err)
	}
	if err := decodeMCHandshake(loginHandshake, mcStateLogin); err != nil {
		return false, fmt.Errorf("mc deep server login handshake invalid: %w", err)
	}
	loginStart, err := readMCPacket(conn)
	if err != nil {
		return false, fmt.Errorf("mc deep server login start: %w", err)
	}
	username, err := decodeMCLoginStart(loginStart)
	if err != nil {
		return false, fmt.Errorf("mc deep server login start invalid: %w", err)
	}
	_ = username

	challenge := make([]byte, 8)
	if _, err := cryptorand.Read(challenge); err != nil {
		return false, err
	}
	if err := writeMCPacket(conn, encodeMCLoginPluginRequest(cfg.pluginChannel, challenge)); err != nil {
		return false, fmt.Errorf("mc deep server login plugin request: %w", err)
	}

	if err := conn.SetDeadline(time.Now().Add(cfg.timeout)); err != nil {
		return false, err
	}
	nextPayload, err := readMCPacket(conn)
	if err != nil {
		return false, fmt.Errorf("mc deep server login follow-up: %w", err)
	}
	packetID, rest, err := mcPacketHead(nextPayload)
	if err != nil {
		return false, fmt.Errorf("mc deep server login follow-up invalid: %w", err)
	}

	switch packetID {
	case mcIDLoginPluginResponse:
		channel, data, decErr := decodeMCLoginPluginResponseBody(rest)
		if decErr != nil {
			return false, fmt.Errorf("mc deep server login plugin response invalid: %w", decErr)
		}
		if err := validatePluginChannel(channel, cfg.pluginChannel); err != nil {
			return false, err
		}
		ok, toNAT := verifyPluginResponse(cfg.pluginSecret, challenge, data)
		if !ok {
			return false, sendMCDisconnect(conn, cfg.rejectMessage)
		}
		if err := writeMCPacket(conn, encodeMCLoginSuccess(cfg.loginUsername)); err != nil {
			return false, fmt.Errorf("mc deep server login success: %w", err)
		}
		_ = conn.SetDeadline(time.Time{})
		return toNAT, nil

	case mcIDEncryptionResponse:
		return false, sendMCDisconnect(conn, cfg.rejectMessage)

	default:
		return false, sendMCDisconnect(conn, cfg.rejectMessage)
	}
}

func encodeMCHandshake(protocolVersion int, serverHost string, serverPort uint16, nextState int) []byte {
	var payload bytes.Buffer
	writeMCVarInt(&payload, mcIDHandshake)
	writeMCVarInt(&payload, protocolVersion)
	writeMCString(&payload, serverHost)
	_ = binary.Write(&payload, binary.BigEndian, serverPort)
	writeMCVarInt(&payload, nextState)
	return payload.Bytes()
}

func decodeMCHandshake(payload []byte, wantNextState int) error {
	r := bytes.NewReader(payload)
	packetID, err := readMCVarInt(r)
	if err != nil {
		return err
	}
	if packetID != mcIDHandshake {
		return fmt.Errorf("unexpected packet id %d", packetID)
	}
	if _, err := readMCVarInt(r); err != nil {
		return err
	}
	if _, err := readMCString(r); err != nil {
		return err
	}
	var port uint16
	if err := binary.Read(r, binary.BigEndian, &port); err != nil {
		return err
	}
	nextState, err := readMCVarInt(r)
	if err != nil {
		return err
	}
	if nextState != wantNextState {
		return fmt.Errorf("unexpected next state %d want %d", nextState, wantNextState)
	}
	if r.Len() != 0 {
		return errors.New("unexpected trailing data in handshake")
	}
	return nil
}

func encodeMCStatusRequest() []byte {
	var payload bytes.Buffer
	writeMCVarInt(&payload, mcIDStatusRequest)
	return payload.Bytes()
}

func decodeMCStatusRequest(payload []byte) error {
	packetID, rest, err := mcPacketHead(payload)
	if err != nil {
		return err
	}
	if packetID != mcIDStatusRequest {
		return fmt.Errorf("unexpected packet id %d", packetID)
	}
	if len(rest) != 0 {
		return errors.New("unexpected trailing data in status request")
	}
	return nil
}

func encodeMCStatusResponse(statusJSON string) []byte {
	var payload bytes.Buffer
	writeMCVarInt(&payload, mcIDStatusResponse)
	writeMCString(&payload, statusJSON)
	return payload.Bytes()
}

func decodeMCStatusResponse(payload []byte) error {
	packetID, rest, err := mcPacketHead(payload)
	if err != nil {
		return err
	}
	if packetID != mcIDStatusResponse {
		return fmt.Errorf("unexpected packet id %d", packetID)
	}
	r := bytes.NewReader(rest)
	body, err := readMCString(r)
	if err != nil {
		return err
	}
	if !json.Valid([]byte(body)) {
		return errors.New("status response is not valid json")
	}
	if r.Len() != 0 {
		return errors.New("unexpected trailing data in status response")
	}
	return nil
}

func encodeMCPing(timestamp int64) []byte {
	var payload bytes.Buffer
	writeMCVarInt(&payload, mcIDPingRequest)
	_ = binary.Write(&payload, binary.BigEndian, timestamp)
	return payload.Bytes()
}

func decodeMCPing(payload []byte) (int64, error) {
	packetID, rest, err := mcPacketHead(payload)
	if err != nil {
		return 0, err
	}
	if packetID != mcIDPingRequest {
		return 0, fmt.Errorf("unexpected packet id %d", packetID)
	}
	if len(rest) != 8 {
		return 0, errors.New("invalid ping payload size")
	}
	return int64(binary.BigEndian.Uint64(rest)), nil
}

func encodeMCPong(timestamp int64) []byte {
	var payload bytes.Buffer
	writeMCVarInt(&payload, mcIDPongResponse)
	_ = binary.Write(&payload, binary.BigEndian, timestamp)
	return payload.Bytes()
}

func decodeMCPong(payload []byte, want int64) error {
	packetID, rest, err := mcPacketHead(payload)
	if err != nil {
		return err
	}
	if packetID != mcIDPongResponse {
		return fmt.Errorf("unexpected packet id %d", packetID)
	}
	if len(rest) != 8 {
		return errors.New("invalid pong payload size")
	}
	got := int64(binary.BigEndian.Uint64(rest))
	if got != want {
		return fmt.Errorf("pong timestamp mismatch got %d want %d", got, want)
	}
	return nil
}

func encodeMCLoginStart(username string) []byte {
	var payload bytes.Buffer
	writeMCVarInt(&payload, mcIDLoginStart)
	writeMCString(&payload, username)
	return payload.Bytes()
}

func decodeMCLoginStart(payload []byte) (string, error) {
	packetID, rest, err := mcPacketHead(payload)
	if err != nil {
		return "", err
	}
	if packetID != mcIDLoginStart {
		return "", fmt.Errorf("unexpected packet id %d", packetID)
	}
	r := bytes.NewReader(rest)
	username, err := readMCString(r)
	if err != nil {
		return "", err
	}
	// 1.20.2+ clients append the player UUID.
	if r.Len() != 0 && r.Len() != 16 {
		return "", errors.New("unexpected trailing data in login start")
	}
	return username, nil
}

type mcLoginPluginChallenge struct {
	channel string
	data    []byte
}

func encodeMCLoginPluginRequest(channel string, data []byte) []byte {
	var payload bytes.Buffer
	writeMCVarInt(&payload, mcIDLoginPluginRequest)
	writeMCString(&payload, channel)
	writeMCByteArray(&payload, data)
	return payload.Bytes()
}

func decodeMCLoginPluginRequest(payload []byte) (mcLoginPluginChallenge, error) {
	packetID, rest, err := mcPacketHead(payload)
	if err != nil {
		return mcLoginPluginChallenge{}, err
	}
	if packetID != mcIDLoginPluginRequest {
		return mcLoginPluginChallenge{}, fmt.Errorf("unexpected packet id %d", packetID)
	}
	r := bytes.NewReader(rest)
	channel, err := readMCString(r)
	if err != nil {
		return mcLoginPluginChallenge{}, err
	}
	data, err := readMCByteArray(r)
	if err != nil {
		return mcLoginPluginChallenge{}, err
	}
	if r.Len() != 0 {
		return mcLoginPluginChallenge{}, errors.New("unexpected trailing data in login plugin request")
	}
	return mcLoginPluginChallenge{channel: channel, data: data}, nil
}

func encodeMCLoginPluginResponse(channel string, data []byte) []byte {
	var payload bytes.Buffer
	writeMCVarInt(&payload, mcIDLoginPluginResponse)
	writeMCString(&payload, channel)
	writeMCByteArray(&payload, data)
	return payload.Bytes()
}

func decodeMCLoginPluginResponseBody(rest []byte) (string, []byte, error) {
	r := bytes.NewReader(rest)
	channel, err := readMCString(r)
	if err != nil {
		return "", nil, err
	}
	data, err := readMCByteArray(r)
	if err != nil {
		return "", nil, err
	}
	if r.Len() != 0 {
		return "", nil, errors.New("unexpected trailing data in login plugin response")
	}
	return channel, data, nil
}

func encodeMCLoginSuccess(username string) []byte {
	var payload bytes.Buffer
	writeMCVarInt(&payload, mcIDLoginSuccess)
	// Offline-mode style UUID derived from username for stable appearance.
	uuid := offlinePlayerUUID(username)
	payload.Write(uuid[:])
	writeMCString(&payload, username)
	writeMCVarInt(&payload, 0) // no properties
	return payload.Bytes()
}

func decodeMCLoginSuccess(payload []byte, wantUsername string) error {
	packetID, rest, err := mcPacketHead(payload)
	if err != nil {
		return err
	}
	if packetID != mcIDLoginSuccess {
		if packetID == mcIDDisconnect {
			msg := decodeMCDisconnectMessage(rest)
			return fmt.Errorf("server rejected MC login (%s); check wireguard-go-transport.json loginPluginSecret/loginPluginChannel match on both peers", msg)
		}
		return fmt.Errorf("unexpected packet id %d", packetID)
	}
	r := bytes.NewReader(rest)
	uuid := make([]byte, 16)
	if _, err := io.ReadFull(r, uuid); err != nil {
		return err
	}
	username, err := readMCString(r)
	if err != nil {
		return err
	}
	if username != wantUsername {
		return fmt.Errorf("unexpected username %q", username)
	}
	propCount, err := readMCVarInt(r)
	if err != nil {
		return err
	}
	if propCount != 0 {
		return fmt.Errorf("unexpected property count %d", propCount)
	}
	if r.Len() != 0 {
		return errors.New("unexpected trailing data in login success")
	}
	return nil
}

func decodeMCDisconnectMessage(rest []byte) string {
	r := bytes.NewReader(rest)
	s, err := readMCString(r)
	if err != nil || s == "" {
		return "disconnect"
	}
	var chat map[string]any
	if json.Unmarshal([]byte(s), &chat) == nil {
		if text, ok := chat["text"].(string); ok && text != "" {
			return text
		}
	}
	return s
}

func sendMCDisconnect(conn net.Conn, message string) error {
	return writeMCPacket(conn, encodeMCDisconnect(message))
}

func encodeMCDisconnect(message string) []byte {
	chatJSON, _ := json.Marshal(map[string]string{"text": message})
	var payload bytes.Buffer
	writeMCVarInt(&payload, mcIDDisconnect)
	writeMCString(&payload, string(chatJSON))
	return payload.Bytes()
}

func mcPacketHead(payload []byte) (packetID int, rest []byte, err error) {
	r := bytes.NewReader(payload)
	packetID, err = readMCVarInt(r)
	if err != nil {
		return 0, nil, err
	}
	rest = payload[int(r.Size())-r.Len():]
	return packetID, rest, nil
}

func writeMCByteArray(w io.Writer, data []byte) {
	writeMCVarInt(w, len(data))
	_, _ = w.Write(data)
}

func readMCByteArray(r *bytes.Reader) ([]byte, error) {
	size, err := readMCVarInt(r)
	if err != nil {
		return nil, err
	}
	if size < 0 || size > r.Len() {
		return nil, errors.New("invalid byte array size")
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}
	return data, nil
}

func validatePluginChannel(got, want string) error {
	if got != want {
		return fmt.Errorf("unexpected plugin channel %q want %q", got, want)
	}
	return nil
}

func buildPluginResponse(secret string, challenge []byte, toNAT bool) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(challenge)
	flag := byte(0)
	if toNAT {
		flag = 1
	}
	mac.Write([]byte{flag})
	return append(mac.Sum(nil), flag)
}

func verifyPluginResponse(secret string, challenge, data []byte) (ok bool, toNAT bool) {
	if len(data) != 33 {
		return false, false
	}
	flag := data[32]
	if flag > 1 {
		return false, false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(challenge)
	mac.Write([]byte{flag})
	if !hmac.Equal(data[:32], mac.Sum(nil)) {
		return false, false
	}
	return true, flag == 1
}

// offlinePlayerUUID returns the standard Minecraft offline-mode UUID bytes (big-endian).
func offlinePlayerUUID(username string) [16]byte {
	const offlinePrefix = "OfflinePlayer:"
	sum := md5.Sum([]byte(offlinePrefix + username))
	h := sum[:]
	h[6] = (h[6] & 0x0f) | 0x30
	h[8] = (h[8] & 0x3f) | 0x80
	var out [16]byte
	copy(out[:], h)
	return out
}
