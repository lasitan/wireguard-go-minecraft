// Package wire defines the binary frame protocol spoken over the Agent<->Master
// WebSocket. Frame layout: [u8 type][u32 seq][payload], all integers big-endian.
// Hot-path messages (Stats, Ping) use fixed-width binary encoding; rare
// messages (ConfigPush) carry a JSON payload inside the binary envelope.
package wire

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
)

type MsgType uint8

const (
	TypeHello      MsgType = 1
	TypeHelloAck   MsgType = 2
	TypeStats      MsgType = 3
	TypePing       MsgType = 4
	TypePong       MsgType = 5
	TypeConfigPush MsgType = 6
	TypeConfigAck  MsgType = 7
	TypeError      MsgType = 8
	TypeUpdate     MsgType = 9  // Master -> Agent: self-update now
	TypeUpdateAck  MsgType = 10 // Agent -> Master: updater started or refused
)

const headerLen = 5

// Limits keep a malicious or buggy peer from forcing huge allocations.
const (
	MaxFrameBytes = 1 << 20
	MaxPeers      = 4096
	MaxIPs        = 8192
	MaxForwards   = 1024
	maxString     = 4096
)

var ErrShort = errors.New("wire: frame truncated")

type Frame struct {
	Type    MsgType
	Seq     uint32
	Payload []byte
}

func EncodeFrame(t MsgType, seq uint32, payload []byte) []byte {
	b := make([]byte, headerLen+len(payload))
	b[0] = byte(t)
	binary.BigEndian.PutUint32(b[1:5], seq)
	copy(b[headerLen:], payload)
	return b
}

func DecodeFrame(b []byte) (Frame, error) {
	if len(b) < headerLen {
		return Frame{}, ErrShort
	}
	if len(b) > MaxFrameBytes {
		return Frame{}, fmt.Errorf("wire: frame too large (%d)", len(b))
	}
	return Frame{Type: MsgType(b[0]), Seq: binary.BigEndian.Uint32(b[1:5]), Payload: b[headerLen:]}, nil
}

// ---- encoding helpers ----

type writer struct{ b []byte }

func (w *writer) u8(v uint8)   { w.b = append(w.b, v) }
func (w *writer) u16(v uint16) { w.b = binary.BigEndian.AppendUint16(w.b, v) }
func (w *writer) u32(v uint32) { w.b = binary.BigEndian.AppendUint32(w.b, v) }
func (w *writer) u64(v uint64) { w.b = binary.BigEndian.AppendUint64(w.b, v) }
func (w *writer) i64(v int64)  { w.u64(uint64(v)) }
func (w *writer) str(s string) {
	if len(s) > maxString {
		s = s[:maxString]
	}
	w.u16(uint16(len(s)))
	w.b = append(w.b, s...)
}

// ip writes [u8 len(0|4|16)][bytes].
func (w *writer) ip(a netip.Addr) {
	if !a.IsValid() {
		w.u8(0)
		return
	}
	if a.Is4() || a.Is4In6() {
		v := a.Unmap().As4()
		w.u8(4)
		w.b = append(w.b, v[:]...)
		return
	}
	v := a.As16()
	w.u8(16)
	w.b = append(w.b, v[:]...)
}

type reader struct {
	b   []byte
	err error
}

func (r *reader) need(n int) bool {
	if r.err != nil {
		return false
	}
	if len(r.b) < n {
		r.err = ErrShort
		return false
	}
	return true
}

func (r *reader) u8() uint8 {
	if !r.need(1) {
		return 0
	}
	v := r.b[0]
	r.b = r.b[1:]
	return v
}

func (r *reader) u16() uint16 {
	if !r.need(2) {
		return 0
	}
	v := binary.BigEndian.Uint16(r.b)
	r.b = r.b[2:]
	return v
}

func (r *reader) u32() uint32 {
	if !r.need(4) {
		return 0
	}
	v := binary.BigEndian.Uint32(r.b)
	r.b = r.b[4:]
	return v
}

func (r *reader) u64() uint64 {
	if !r.need(8) {
		return 0
	}
	v := binary.BigEndian.Uint64(r.b)
	r.b = r.b[8:]
	return v
}

func (r *reader) i64() int64 { return int64(r.u64()) }

func (r *reader) bytes(n int) []byte {
	if !r.need(n) {
		return nil
	}
	v := r.b[:n]
	r.b = r.b[n:]
	return v
}

func (r *reader) str() string {
	n := int(r.u16())
	return string(r.bytes(n))
}

func (r *reader) ip() netip.Addr {
	switch n := r.u8(); n {
	case 0:
		return netip.Addr{}
	case 4:
		b := r.bytes(4)
		if b == nil {
			return netip.Addr{}
		}
		return netip.AddrFrom4([4]byte(b))
	case 16:
		b := r.bytes(16)
		if b == nil {
			return netip.Addr{}
		}
		return netip.AddrFrom16([16]byte(b))
	default:
		if r.err == nil {
			r.err = fmt.Errorf("wire: bad ip length %d", n)
		}
		return netip.Addr{}
	}
}

func (r *reader) count(max int, what string) int {
	n := int(r.u16())
	if n > max && r.err == nil {
		r.err = fmt.Errorf("wire: too many %s (%d)", what, n)
	}
	return n
}
