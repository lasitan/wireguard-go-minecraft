package wire

import "net/netip"

// Hello is the first frame an agent sends after the WebSocket upgrade.
type Hello struct {
	Token    string
	Version  string
	PublicV4 netip.Addr
	PublicV6 netip.Addr
}

func (m Hello) Marshal() []byte {
	var w writer
	w.str(m.Token)
	w.str(m.Version)
	w.ip(m.PublicV4)
	w.ip(m.PublicV6)
	return w.b
}

func (m *Hello) Unmarshal(b []byte) error {
	r := reader{b: b}
	m.Token = r.str()
	m.Version = r.str()
	m.PublicV4 = r.ip()
	m.PublicV6 = r.ip()
	return r.err
}

// HelloAck confirms authentication and tells the agent its node id.
type HelloAck struct {
	NodeID          string
	StatsIntervalMs uint32
}

func (m HelloAck) Marshal() []byte {
	var w writer
	w.str(m.NodeID)
	w.u32(m.StatsIntervalMs)
	return w.b
}

func (m *HelloAck) Unmarshal(b []byte) error {
	r := reader{b: b}
	m.NodeID = r.str()
	m.StatsIntervalMs = r.u32()
	return r.err
}

type PeerStat struct {
	PublicKey         [32]byte
	RxBytes           uint64
	TxBytes           uint64
	LastHandshakeNano int64
	// HandshakeRTTMicros travels in a trailing section so older Masters,
	// which stop reading after Forwards, stay compatible.
	HandshakeRTTMicros uint32
}

// IPStat counts tunnel traffic exchanged with one VPN address.
// Rx = bytes received from that IP, Tx = bytes sent to it.
type IPStat struct {
	IP      netip.Addr
	RxBytes uint64
	TxBytes uint64
}

type ForwardStat struct {
	Protocol string
	Listen   string
	RxBytes  uint64
	TxBytes  uint64
}

// Stats carries monotonically increasing counters since agent process start.
// Master derives rates and deltas; a counter going backwards means a restart.
type Stats struct {
	UnixMilli int64
	RxBytes   uint64
	TxBytes   uint64
	Peers     []PeerStat
	IPs       []IPStat
	Forwards  []ForwardStat
}

func (m Stats) Marshal() []byte {
	var w writer
	w.b = make([]byte, 0, 32+len(m.Peers)*56+len(m.IPs)*33+len(m.Forwards)*40)
	w.i64(m.UnixMilli)
	w.u64(m.RxBytes)
	w.u64(m.TxBytes)
	peers := m.Peers
	if len(peers) > MaxPeers {
		peers = peers[:MaxPeers]
	}
	w.u16(uint16(len(peers)))
	for _, p := range peers {
		w.b = append(w.b, p.PublicKey[:]...)
		w.u64(p.RxBytes)
		w.u64(p.TxBytes)
		w.i64(p.LastHandshakeNano)
	}
	ips := m.IPs
	if len(ips) > MaxIPs {
		ips = ips[:MaxIPs]
	}
	w.u16(uint16(len(ips)))
	for _, s := range ips {
		w.ip(s.IP)
		w.u64(s.RxBytes)
		w.u64(s.TxBytes)
	}
	fwds := m.Forwards
	if len(fwds) > MaxForwards {
		fwds = fwds[:MaxForwards]
	}
	w.u16(uint16(len(fwds)))
	for _, f := range fwds {
		w.str(f.Protocol)
		w.str(f.Listen)
		w.u64(f.RxBytes)
		w.u64(f.TxBytes)
	}
	w.u16(uint16(len(peers)))
	for _, p := range peers {
		w.u32(p.HandshakeRTTMicros)
	}
	return w.b
}

func (m *Stats) Unmarshal(b []byte) error {
	r := reader{b: b}
	m.UnixMilli = r.i64()
	m.RxBytes = r.u64()
	m.TxBytes = r.u64()
	if n := r.count(MaxPeers, "peers"); r.err == nil {
		m.Peers = make([]PeerStat, n)
		for i := range m.Peers {
			copy(m.Peers[i].PublicKey[:], r.bytes(32))
			m.Peers[i].RxBytes = r.u64()
			m.Peers[i].TxBytes = r.u64()
			m.Peers[i].LastHandshakeNano = r.i64()
		}
	}
	if n := r.count(MaxIPs, "ips"); r.err == nil {
		m.IPs = make([]IPStat, n)
		for i := range m.IPs {
			m.IPs[i].IP = r.ip()
			m.IPs[i].RxBytes = r.u64()
			m.IPs[i].TxBytes = r.u64()
		}
	}
	if n := r.count(MaxForwards, "forwards"); r.err == nil {
		m.Forwards = make([]ForwardStat, n)
		for i := range m.Forwards {
			m.Forwards[i].Protocol = r.str()
			m.Forwards[i].Listen = r.str()
			m.Forwards[i].RxBytes = r.u64()
			m.Forwards[i].TxBytes = r.u64()
		}
	}
	if r.err == nil && len(r.b) > 0 {
		if n := r.count(MaxPeers, "peer rtts"); r.err == nil && n == len(m.Peers) {
			for i := range m.Peers {
				m.Peers[i].HandshakeRTTMicros = r.u32()
			}
		}
	}
	return r.err
}

// Ping/Pong carry the sender's clock for RTT measurement.
type Ping struct{ UnixNano int64 }

func (m Ping) Marshal() []byte {
	var w writer
	w.i64(m.UnixNano)
	return w.b
}

func (m *Ping) Unmarshal(b []byte) error {
	r := reader{b: b}
	m.UnixNano = r.i64()
	return r.err
}

// ConfigAck reports whether the agent applied a pushed revision.
type ConfigAck struct {
	Revision uint32
	OK       bool
	Error    string
}

func (m ConfigAck) Marshal() []byte {
	var w writer
	w.u32(m.Revision)
	if m.OK {
		w.u8(1)
	} else {
		w.u8(0)
	}
	w.str(m.Error)
	return w.b
}

func (m *ConfigAck) Unmarshal(b []byte) error {
	r := reader{b: b}
	m.Revision = r.u32()
	m.OK = r.u8() == 1
	m.Error = r.str()
	return r.err
}

// ErrorMsg is sent by Master before closing (e.g. bad token).
type ErrorMsg struct {
	Code    uint16
	Message string
}

const (
	ErrCodeAuth     uint16 = 401
	ErrCodeProtocol uint16 = 400
)

func (m ErrorMsg) Marshal() []byte {
	var w writer
	w.u16(m.Code)
	w.str(m.Message)
	return w.b
}

func (m *ErrorMsg) Unmarshal(b []byte) error {
	r := reader{b: b}
	m.Code = r.u16()
	m.Message = r.str()
	return r.err
}
