/* SPDX-License-Identifier: MIT */

package conn

import (
	"fmt"
	"net"
)

// FiveM / CitizenFX init HTTP-like GET probe (many scanners send plain HTTP).
func (b *TCPBind) performFiveMCamouflageClient(conn net.Conn, cfg camoSharedConfig) error {
	if err := conn.SetDeadline(deadline(cfg.timeout)); err != nil {
		return err
	}
	defer conn.SetDeadline(zeroDeadline())
	req := "GET /info.json HTTP/1.1\r\nHost: cfx.re\r\nConnection: close\r\n\r\n"
	if err := writeAll(conn, []byte(req)); err != nil {
		return err
	}
	resp := make([]byte, 512)
	n, err := conn.Read(resp)
	if err != nil {
		return fmt.Errorf("fivem info: %w", err)
	}
	if n < 12 || string(resp[:4]) != "HTTP" {
		return fmt.Errorf("fivem info invalid")
	}
	if !cfg.deep {
		return nil
	}
	return camoAuthClient(conn, cfg.pluginSecret, resp[:min(n, 32)])
}

func (b *TCPBind) performFiveMCamouflageServer(conn net.Conn, cfg camoSharedConfig) error {
	if err := conn.SetDeadline(deadline(cfg.timeout)); err != nil {
		return err
	}
	defer conn.SetDeadline(zeroDeadline())
	buf := make([]byte, 128)
	n, err := conn.Read(buf)
	if err != nil {
		return fmt.Errorf("fivem request: %w", err)
	}
	if n < 3 || string(buf[:3]) != "GET" {
		return fmt.Errorf("fivem expected GET")
	}
	body := fmt.Sprintf(`{"server":"%s","vars":{"gamename":"gta5"},"maxClients":48}`, cfg.serverName)
	resp := fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
	if err := writeAll(conn, []byte(resp)); err != nil {
		return err
	}
	if !cfg.deep {
		return nil
	}
	return camoAuthServer(conn, cfg.pluginSecret, []byte(body))
}
