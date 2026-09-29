package wire

import (
	"math"
)

// Admin UI <-> Master messages share the frame format with the agent channel
// but use their own type range so the two streams can never be confused.
// Snapshot-style messages (mesh, meta, version, node stats) carry JSON; the
// periodic presence/traffic broadcast is fixed binary.
const (
	TypeUIHello     MsgType = 0x41 // UI -> Master: admin session token
	TypeUIHelloAck  MsgType = 0x42 // Master -> UI: accepted
	TypeUIError     MsgType = 0x43 // Master -> UI: ErrorMsg, then close
	TypeUIMesh      MsgType = 0x44 // Master -> UI: JSON core.Mesh (no private keys)
	TypeUIMeta      MsgType = 0x45 // Master -> UI: JSON meta
	TypeUIVersion   MsgType = 0x46 // Master -> UI: JSON version view
	TypeUIPresence  MsgType = 0x47 // Master -> UI: Presence
	TypeUISubscribe MsgType = 0x48 // UI -> Master: UISubscribe
	TypeUINodeStats MsgType = 0x49 // Master -> UI: JSON node stats view
)

// MaxPresenceNodes bounds a Presence frame.
const MaxPresenceNodes = 8192

type UIHello struct {
	Token string
}

func (h UIHello) Marshal() []byte {
	w := writer{}
	w.str(h.Token)
	return w.b
}

func (h *UIHello) Unmarshal(b []byte) error {
	r := reader{b: b}
	h.Token = r.str()
	return r.err
}

type UIHelloAck struct {
	PresenceIntervalMs uint32
}

func (a UIHelloAck) Marshal() []byte {
	w := writer{}
	w.u32(a.PresenceIntervalMs)
	return w.b
}

func (a *UIHelloAck) Unmarshal(b []byte) error {
	r := reader{b: b}
	a.PresenceIntervalMs = r.u32()
	return r.err
}

// UISubscribe selects the one node whose detailed stats the UI wants pushed;
// an empty NodeID unsubscribes.
type UISubscribe struct {
	NodeID string
}

func (s UISubscribe) Marshal() []byte {
	w := writer{}
	w.str(s.NodeID)
	return w.b
}

func (s *UISubscribe) Unmarshal(b []byte) error {
	r := reader{b: b}
	s.NodeID = r.str()
	return r.err
}

// Link states in a Presence entry.
const (
	LinkOffline uint8 = 0
	LinkHTTP    uint8 = 1
	LinkWS      uint8 = 2
)

type PresenceNode struct {
	NodeID     string
	Link       uint8
	LastSeenMs int64 // unix ms; 0 = never
	RxRate     float64
	TxRate     float64
	RTTMicros  uint32
}

type Presence struct {
	NowMs int64
	Nodes []PresenceNode
}

func (p Presence) Marshal() []byte {
	w := writer{b: make([]byte, 0, 10+len(p.Nodes)*48)}
	w.i64(p.NowMs)
	n := min(len(p.Nodes), MaxPresenceNodes)
	w.u16(uint16(n))
	for _, e := range p.Nodes[:n] {
		w.str(e.NodeID)
		w.u8(e.Link)
		w.i64(e.LastSeenMs)
		w.u64(math.Float64bits(e.RxRate))
		w.u64(math.Float64bits(e.TxRate))
		w.u32(e.RTTMicros)
	}
	return w.b
}

func (p *Presence) Unmarshal(b []byte) error {
	r := reader{b: b}
	p.NowMs = r.i64()
	n := r.count(MaxPresenceNodes, "presence nodes")
	if r.err != nil {
		return r.err
	}
	p.Nodes = make([]PresenceNode, 0, n)
	for i := 0; i < n && r.err == nil; i++ {
		p.Nodes = append(p.Nodes, PresenceNode{
			NodeID:     r.str(),
			Link:       r.u8(),
			LastSeenMs: r.i64(),
			RxRate:     math.Float64frombits(r.u64()),
			TxRate:     math.Float64frombits(r.u64()),
			RTTMicros:  r.u32(),
		})
	}
	return r.err
}
