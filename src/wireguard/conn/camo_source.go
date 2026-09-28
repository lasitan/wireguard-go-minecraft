/* SPDX-License-Identifier: MIT */

package conn

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"time"
)

var sourceEngineQuery = []byte{
	0xFF, 0xFF, 0xFF, 0xFF, 'T', 'S', 'o', 'u', 'r', 'c', 'e', ' ',
	'E', 'n', 'g', 'i', 'n', 'e', ' ', 'Q', 'u', 'e', 'r', 'y', 0x00,
}

func buildSourceInfoResponse(serverName string) []byte {
	// A2S_INFO-style payload after 0xFFFFFFFF header byte 'I'
	var b bytes.Buffer
	b.Write([]byte{0xFF, 0xFF, 0xFF, 0xFF, 'I'})
	b.WriteByte(17) // protocol
	writeSourceCString(&b, serverName)
	writeSourceCString(&b, "survival")
	b.WriteByte(0)   // players
	b.WriteByte(32)  // max
	b.WriteByte('d') // dedicated
	writeSourceCString(&b, "csgo")
	writeSourceCString(&b, "Counter-Strike 2")
	b.WriteByte(0) // steamid placeholder
	b.WriteByte(0)
	b.WriteByte(0)
	b.WriteByte(0)
	b.WriteByte(0)
	b.WriteByte(0)
	b.WriteByte(0)
	b.WriteByte(0)
	b.WriteByte(0)
	b.WriteByte(0x80) // vac on
	writeSourceCString(&b, "de_dust2")
	return b.Bytes()
}

func writeSourceCString(b *bytes.Buffer, s string) {
	b.WriteString(s)
	b.WriteByte(0)
}

func (b *TCPBind) performSourceCamouflageClient(conn net.Conn, cfg camoSharedConfig) error {
	if err := conn.SetDeadline(deadline(cfg.timeout)); err != nil {
		return err
	}
	defer conn.SetDeadline(zeroDeadline())
	if err := writeAll(conn, sourceEngineQuery); err != nil {
		return err
	}
	resp := make([]byte, 512)
	n, err := conn.Read(resp)
	if err != nil {
		return fmt.Errorf("source info: %w", err)
	}
	if n < 5 || resp[4] != 'I' {
		return fmt.Errorf("source info invalid")
	}
	if !cfg.deep {
		return nil
	}
	challenge := resp[:min(n, 32)]
	if err := camoAuthClient(conn, cfg.pluginSecret, challenge); err != nil {
		return err
	}
	return nil
}

func (b *TCPBind) performSourceCamouflageServer(conn net.Conn, cfg camoSharedConfig) error {
	if err := conn.SetDeadline(deadline(cfg.timeout)); err != nil {
		return err
	}
	defer conn.SetDeadline(zeroDeadline())
	buf := make([]byte, len(sourceEngineQuery))
	if _, err := io.ReadFull(conn, buf); err != nil {
		return fmt.Errorf("source query read: %w", err)
	}
	if !bytes.Equal(buf, sourceEngineQuery) {
		return fmt.Errorf("source query mismatch")
	}
	if err := writeAll(conn, buildSourceInfoResponse(cfg.serverName)); err != nil {
		return err
	}
	if !cfg.deep {
		return nil
	}
	challenge := buildSourceInfoResponse(cfg.serverName)
	return camoAuthServer(conn, cfg.pluginSecret, challenge[:min(len(challenge), 32)])
}

func deadline(d time.Duration) time.Time {
	return time.Now().Add(d)
}

func zeroDeadline() time.Time {
	return time.Time{}
}
