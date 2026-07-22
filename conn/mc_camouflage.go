/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

package conn

import (
	"bytes"
	"crypto/md5"
	cryptorand "crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

	mcIDLoginStart           = 0x00
	mcIDEncryptionResponse   = 0x01
	mcIDLoginPluginResponse  = 0x02
	mcIDDisconnect           = 0x00
	mcIDEncryptionRequest    = 0x01
	mcIDLoginSuccess         = 0x02
	mcIDLoginPluginRequest   = 0x04

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
		"Arrow", "Stone", "Sand",  "Gold",  "Iron",  "Frost",
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

func (b *TCPBind) performMCCamouflageClient(conn net.Conn, dst netip.AddrPort) error {
	cfg := b.mcDeepConfig()
	if !cfg.enabled {
		return nil
	}
	if !cfg.deep {
		return b.performMCCamouflageClientShallow(conn, dst, cfg)
	}
	return b.performMCCamouflageClientDeep(conn, dst, cfg)
}

func (b *TCPBind) performMCCamouflageServer(conn net.Conn) (toNAT bool, err error) {
	cfg := b.mcDeepConfig()
	if !cfg.enabled {
		return false, nil
	}
	if !cfg.deep {
		return false, b.performMCCamouflageServerShallow(conn, cfg)
	}
	return b.performMCCamouflageServerDeep(conn, cfg)
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
	if r.Len() != 0 {
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

const pluginFlagToNAT = "\x00TONAT"

func buildPluginResponse(secret string, challenge []byte, toNAT bool) []byte {
	out := make([]byte, 0, len(secret)+len(challenge)+len(pluginFlagToNAT))
	out = append(out, secret...)
	out = append(out, challenge...)
	if toNAT {
		out = append(out, pluginFlagToNAT...)
	}
	return out
}

func verifyPluginResponse(secret string, challenge, data []byte) (ok bool, toNAT bool) {
	base := buildPluginResponse(secret, challenge, false)
	if len(data) < len(base) {
		return false, false
	}
	for i := range base {
		if data[i] != base[i] {
			return false, false
		}
	}
	rest := data[len(base):]
	if len(rest) == 0 {
		return true, false
	}
	if string(rest) == pluginFlagToNAT {
		return true, true
	}
	return false, false
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
