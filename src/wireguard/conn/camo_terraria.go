/* SPDX-License-Identifier: MIT */

package conn

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

const terrariaConnectMsg = 82

func writeTerrariaPacket(conn net.Conn, msgType uint16, payload []byte) error {
	var hdr [4]byte
	binary.LittleEndian.PutUint16(hdr[0:2], uint16(len(payload)+2))
	binary.LittleEndian.PutUint16(hdr[2:4], msgType)
	if err := writeAll(conn, hdr[:]); err != nil {
		return err
	}
	if len(payload) > 0 {
		return writeAll(conn, payload)
	}
	return nil
}

func readTerrariaPacket(conn net.Conn) (uint16, []byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		return 0, nil, err
	}
	length := binary.LittleEndian.Uint16(hdr[0:2])
	msgType := binary.LittleEndian.Uint16(hdr[2:4])
	if length < 2 {
		return 0, nil, fmt.Errorf("terraria short packet")
	}
	payloadLen := int(length) - 2
	payload := make([]byte, payloadLen)
	if payloadLen > 0 {
		if _, err := io.ReadFull(conn, payload); err != nil {
			return 0, nil, err
		}
	}
	return msgType, payload, nil
}

func terrariaConnectPayload() []byte {
	// version 193 + "Terraria" style client string
	p := []byte{193}
	p = append(p, "Terraria255.255.255.255:7777"...)
	p = append(p, 0)
	return p
}

func (b *TCPBind) performTerrariaCamouflageClient(conn net.Conn, cfg camoSharedConfig) error {
	if err := conn.SetDeadline(deadline(cfg.timeout)); err != nil {
		return err
	}
	defer conn.SetDeadline(zeroDeadline())
	if err := writeTerrariaPacket(conn, terrariaConnectMsg, terrariaConnectPayload()); err != nil {
		return err
	}
	msgType, payload, err := readTerrariaPacket(conn)
	if err != nil {
		return fmt.Errorf("terraria connect reply: %w", err)
	}
	if msgType != 2 { // disconnect during probe is ok
		_ = payload
	}
	if !cfg.deep {
		return nil
	}
	challenge := append([]byte{byte(msgType)}, payload...)
	return camoAuthClient(conn, cfg.pluginSecret, challenge)
}

func (b *TCPBind) performTerrariaCamouflageServer(conn net.Conn, cfg camoSharedConfig) error {
	if err := conn.SetDeadline(deadline(cfg.timeout)); err != nil {
		return err
	}
	defer conn.SetDeadline(zeroDeadline())
	msgType, payload, err := readTerrariaPacket(conn)
	if err != nil {
		return fmt.Errorf("terraria connect: %w", err)
	}
	if msgType != terrariaConnectMsg {
		return fmt.Errorf("terraria expected connect, got %d", msgType)
	}
	_ = payload
	// Disconnect text mimics busy auth server
	reject := cfg.rejectMessage
	if reject == "" {
		reject = "Authenticating to server..."
	}
	disPayload := append([]byte(reject), 0)
	if err := writeTerrariaPacket(conn, 2, disPayload); err != nil {
		return err
	}
	if !cfg.deep {
		return nil
	}
	challenge := append([]byte{byte(msgType)}, payload...)
	return camoAuthServer(conn, cfg.pluginSecret, challenge)
}
