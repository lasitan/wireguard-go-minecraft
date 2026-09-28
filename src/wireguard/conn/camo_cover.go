/* SPDX-License-Identifier: MIT */

package conn

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

// Play-state custom-payload-shaped wrapper. Inner bytes are AES-CTR sealed so
// WireGuard message types never appear on the wire.
const coverMCPacketID = 0x19

func coverKey(secret string) []byte {
	sum := sha256.Sum256([]byte("wgmc-cover-v1\x00" + secret))
	return sum[:]
}

func sealCover(secret string, plain []byte) ([]byte, error) {
	var padLen [1]byte
	if _, err := rand.Read(padLen[:]); err != nil {
		return nil, err
	}
	n := int(padLen[0] & 0x0f)
	pad := make([]byte, n)
	if n > 0 {
		if _, err := rand.Read(pad); err != nil {
			return nil, err
		}
	}
	body := make([]byte, 1+len(plain)+n)
	body[0] = byte(n)
	copy(body[1:], plain)
	copy(body[1+len(plain):], pad)

	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(coverKey(secret))
	if err != nil {
		return nil, err
	}
	stream := cipher.NewCTR(block, nonce[:])
	out := make([]byte, 16+len(body))
	copy(out, nonce[:])
	stream.XORKeyStream(out[16:], body)
	return out, nil
}

func openCover(secret string, sealed []byte) ([]byte, error) {
	if len(sealed) < 17 {
		return nil, fmt.Errorf("cover frame short")
	}
	block, err := aes.NewCipher(coverKey(secret))
	if err != nil {
		return nil, err
	}
	stream := cipher.NewCTR(block, sealed[:16])
	body := make([]byte, len(sealed)-16)
	stream.XORKeyStream(body, sealed[16:])
	n := int(body[0])
	if n > 15 || 1+n > len(body) {
		return nil, fmt.Errorf("cover pad invalid")
	}
	return body[1 : len(body)-n], nil
}

func encodeCoverMC(channel string, data []byte) []byte {
	var p bytes.Buffer
	writeMCVarInt(&p, coverMCPacketID)
	writeMCString(&p, channel)
	writeMCByteArray(&p, data)
	return p.Bytes()
}

func decodeCoverMC(payload []byte, wantChannel string) ([]byte, error) {
	id, rest, err := mcPacketHead(payload)
	if err != nil {
		return nil, err
	}
	if id != coverMCPacketID {
		return nil, fmt.Errorf("cover packet id %d", id)
	}
	r := bytes.NewReader(rest)
	ch, err := readMCString(r)
	if err != nil {
		return nil, err
	}
	if ch != wantChannel {
		return nil, fmt.Errorf("cover channel")
	}
	return readMCByteArray(r)
}

func (b *TCPBind) writeCoverFrame(conn net.Conn, payload []byte) error {
	cfg := b.getCamo()
	sealed, err := sealCover(cfg.pluginSecret, payload)
	if err != nil {
		return err
	}
	switch cfg.profile {
	case camoProfileMinecraft:
		return writeMCPacket(conn, encodeCoverMC(cfg.pluginChannel, sealed))
	default:
		// Generic: 4-byte profile tag is avoided; use length-prefixed opaque blob
		// preceded by a protocol-looking header byte that is not a WG type (1-4).
		var hdr [3]byte
		hdr[0] = 0xFE
		binary.BigEndian.PutUint16(hdr[1:], uint16(len(sealed)))
		if err := writeAll(conn, hdr[:]); err != nil {
			return err
		}
		return writeAll(conn, sealed)
	}
}

func (b *TCPBind) readCoverFrame(conn net.Conn) ([]byte, error) {
	cfg := b.getCamo()
	var sealed []byte
	switch cfg.profile {
	case camoProfileMinecraft:
		pkt, err := readMCPacket(conn)
		if err != nil {
			return nil, err
		}
		sealed, err = decodeCoverMC(pkt, cfg.pluginChannel)
		if err != nil {
			return nil, err
		}
	default:
		var hdr [3]byte
		if _, err := io.ReadFull(conn, hdr[:]); err != nil {
			return nil, err
		}
		if hdr[0] != 0xFE {
			return nil, fmt.Errorf("cover header")
		}
		n := int(binary.BigEndian.Uint16(hdr[1:]))
		sealed = make([]byte, n)
		if _, err := io.ReadFull(conn, sealed); err != nil {
			return nil, err
		}
	}
	return openCover(cfg.pluginSecret, sealed)
}

func (b *TCPBind) camouflageHidesVPN() bool {
	return b.getCamo().profile != camoProfileNone
}
