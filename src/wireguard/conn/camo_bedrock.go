/* SPDX-License-Identifier: MIT */

package conn

import (
	"fmt"
	"io"
	"net"
)

// Bedrock/RakNet-style offline ping (0x01) over TCP for shallow probes.
var bedrockPing = []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 'M', 'C', 'P', 'E', ';'}

func bedrockPong(serverName string) []byte {
	// Unconnected pong magic + MOTD-ish string
	p := append([]byte{0x1c}, bedrockPing[9:]...)
	p = append(p, serverName...)
	p = append(p, ';', '1', '9', '3', ';', '2', '0', ';')
	return p
}

func (b *TCPBind) performBedrockCamouflageClient(conn net.Conn, cfg camoSharedConfig) error {
	if err := conn.SetDeadline(deadline(cfg.timeout)); err != nil {
		return err
	}
	defer conn.SetDeadline(zeroDeadline())
	if err := writeAll(conn, bedrockPing); err != nil {
		return err
	}
	resp := make([]byte, 256)
	n, err := conn.Read(resp)
	if err != nil {
		return fmt.Errorf("bedrock pong: %w", err)
	}
	if n < 1 || resp[0] != 0x1c {
		return fmt.Errorf("bedrock pong invalid")
	}
	if !cfg.deep {
		return nil
	}
	return camoAuthClient(conn, cfg.pluginSecret, resp[:min(n, 32)])
}

func (b *TCPBind) performBedrockCamouflageServer(conn net.Conn, cfg camoSharedConfig) error {
	if err := conn.SetDeadline(deadline(cfg.timeout)); err != nil {
		return err
	}
	defer conn.SetDeadline(zeroDeadline())
	buf := make([]byte, len(bedrockPing))
	if _, err := io.ReadFull(conn, buf); err != nil {
		return fmt.Errorf("bedrock ping: %w", err)
	}
	if err := writeAll(conn, bedrockPong(cfg.serverName)); err != nil {
		return err
	}
	if !cfg.deep {
		return nil
	}
	pong := bedrockPong(cfg.serverName)
	return camoAuthServer(conn, cfg.pluginSecret, pong[:min(len(pong), 32)])
}
