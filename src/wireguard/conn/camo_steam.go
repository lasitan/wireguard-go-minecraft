/* SPDX-License-Identifier: MIT */

package conn

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

// Simplified Steam/Valheim-style challenge (A2S_GETCHALLENGE-like on TCP).
func (b *TCPBind) performSteamCamouflageClient(conn net.Conn, cfg camoSharedConfig) error {
	if err := conn.SetDeadline(deadline(cfg.timeout)); err != nil {
		return err
	}
	defer conn.SetDeadline(zeroDeadline())
	req := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0x55, 0xFF, 0xFF, 0xFF, 0xFF}
	if err := writeAll(conn, req); err != nil {
		return err
	}
	resp := make([]byte, 64)
	n, err := conn.Read(resp)
	if err != nil {
		return fmt.Errorf("steam challenge: %w", err)
	}
	if n < 9 || resp[4] != 0x41 {
		return fmt.Errorf("steam challenge invalid")
	}
	if !cfg.deep {
		return nil
	}
	return camoAuthClient(conn, cfg.pluginSecret, resp[:min(n, 32)])
}

func (b *TCPBind) performSteamCamouflageServer(conn net.Conn, cfg camoSharedConfig) error {
	if err := conn.SetDeadline(deadline(cfg.timeout)); err != nil {
		return err
	}
	defer conn.SetDeadline(zeroDeadline())
	hdr := make([]byte, 9)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		return fmt.Errorf("steam request: %w", err)
	}
	if hdr[4] != 0x55 {
		return fmt.Errorf("steam request type")
	}
	var out [9 + 4]byte
	copy(out[:4], []byte{0xFF, 0xFF, 0xFF, 0xFF})
	out[4] = 0x41
	binary.BigEndian.PutUint32(out[5:9], 0x00112233)
	if err := writeAll(conn, out[:]); err != nil {
		return err
	}
	if !cfg.deep {
		return nil
	}
	challenge := out[:]
	return camoAuthServer(conn, cfg.pluginSecret, challenge)
}
