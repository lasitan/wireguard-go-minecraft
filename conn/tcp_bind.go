/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

package conn

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const tcpFrameHeaderSize = 2

const (
	transportConfigFileName = "wireguard-go-transport.json"
	defaultMCProtocol       = 760
	defaultMCHandshakeTime  = 5 * time.Second
	defaultDialTimeout      = 3 * time.Second
	defaultReconnectInitial = time.Second
	defaultReconnectMax     = 30 * time.Second
	// Keep Send-path dial attempts tiny. Servers must not burn time dialing
	// stale NAT mappings; clients only need a couple of tries before WG retries.
	maxSendDialAttempts = 2
	tcpKeepAlivePeriod  = 30 * time.Second
	// If no framed payload is received for this long, tear down the TCP
	// session so the client redials (pair with PersistentKeepalive ≈ 5s).
	defaultRXIdleTimeout = 5 * time.Second

	// Post-camouflage ToNAT client announcement (all MC modes / MC-off).
	toNATMagic = "WGNT\x01"
)

// ErrWaitingInboundReconnect is returned when a previously inbound peer's TCP
// session is gone. The server must not dial back through NAT; the client is
// expected to reconnect. Callers should clear the peer endpoint.
var ErrWaitingInboundReconnect = errors.New("tcp session gone; waiting for peer reconnect")

type tcpPacket struct {
	payload  []byte
	endpoint Endpoint
}

type tcpSession struct {
	bind     *TCPBind
	key      string
	dst      netip.AddrPort
	conn     net.Conn
	writeMu  sync.Mutex
	alive    bool
	inbound  bool // accepted from peer (server side) vs dialed out (client)
	toNAT    bool // inbound session signaled ToNAT client mode
	lastRX   atomic.Int64 // unix nano of last framed payload read
}

type reconnectConfig struct {
	initial        time.Duration
	max            time.Duration
	dialTimeout    time.Duration
	rxIdleTimeout  time.Duration
	allowOutbound  bool // if false, never dial (listen-only server)
}

// TCPBind implements Bind over TCP transport with simple length-prefixed
// framing: uint16 big-endian payload length + payload bytes.
// MC camouflage is applied on top of plain TCP (no TLS).
type TCPBind struct {
	mu sync.Mutex

	listener4 net.Listener
	listener6 net.Listener
	sessions  map[string]*tcpSession
	dialMu    map[string]*sync.Mutex
	// IPs that have connected inbound at least once. Send must not dial these
	// back (NAT); wait for the peer to reconnect instead.
	inboundIPs map[netip.Addr]struct{}
	mcConfig   mcCamouflageConfig
	reconnect  reconnectConfig

	recvCh chan tcpPacket
	done   chan struct{}

	ctx    context.Context
	cancel context.CancelFunc

	pendingMu sync.Mutex
	pending   map[net.Conn]struct{}

	wg     sync.WaitGroup
	isOpen bool

	// dialToNAT: C-side Interface ToNAT — announce ToNAT on outbound dials.
	dialToNAT atomic.Bool
}

type TCPEndpoint struct {
	dst   netip.AddrPort
	src   netip.AddrPort
	toNAT bool
}

// IsToNAT reports whether this endpoint's TCP session was a ToNAT client.
func (e *TCPEndpoint) IsToNAT() bool {
	return e != nil && e.toNAT
}

// SetDialToNAT marks this bind as a ToNAT client (Interface ToNAT=).
func (b *TCPBind) SetDialToNAT(v bool) {
	b.dialToNAT.Store(v)
}

type mcCamouflageConfig struct {
	enabled       bool
	deep          bool
	timeout       time.Duration
	loginUsername string
	pluginChannel string
	pluginSecret  string
	rejectMessage string
}

type transportConfigFile struct {
	TCP tcpConfigFile `json:"tcp"`
	MC  mcConfigFile  `json:"mc"`
}

type tcpConfigFile struct {
	DialTimeout             string `json:"dialTimeout"`
	ReconnectInitialBackoff string `json:"reconnectInitialBackoff"`
	ReconnectMaxBackoff     string `json:"reconnectMaxBackoff"`
	// No framed RX for this long → close session (client will redial). Default 5s.
	RXIdleTimeout string `json:"rxIdleTimeout"`
	// If false, never dial out (typical for the listen/server role). Default true.
	AllowOutboundDial *bool `json:"allowOutboundDial"`
}

type mcConfigFile struct {
	Enabled            *bool  `json:"enabled"`
	HandshakeTimeout   string `json:"handshakeTimeout"`
	DeepCamouflage     *bool  `json:"deepCamouflage"`
	LoginUsername      string `json:"loginUsername"`
	LoginPluginChannel string `json:"loginPluginChannel"`
	LoginPluginSecret  string `json:"loginPluginSecret"`
	RejectMessage      string `json:"rejectMessage"`
}

var (
	_ Bind     = (*TCPBind)(nil)
	_ Endpoint = (*TCPEndpoint)(nil)
)

func NewTCPBind() Bind {
	return &TCPBind{}
}

func (*TCPBind) ParseEndpoint(s string) (Endpoint, error) {
	addr, err := netip.ParseAddrPort(s)
	if err != nil {
		return nil, err
	}
	return &TCPEndpoint{dst: addr}, nil
}

func (e *TCPEndpoint) ClearSrc() {
	e.src = netip.AddrPort{}
}

func (e *TCPEndpoint) SrcToString() string {
	if !e.src.IsValid() {
		return ""
	}
	return e.src.String()
}

func (e *TCPEndpoint) DstToString() string {
	return e.dst.String()
}

func (e *TCPEndpoint) DstToBytes() []byte {
	b, _ := e.dst.MarshalBinary()
	return b
}

func (e *TCPEndpoint) DstIP() netip.Addr {
	return e.dst.Addr()
}

func (e *TCPEndpoint) SrcIP() netip.Addr {
	return e.src.Addr()
}

func (b *TCPBind) Open(uport uint16) ([]ReceiveFunc, uint16, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.isOpen {
		return nil, 0, ErrBindAlreadyOpen
	}
	fileCfg, err := loadTransportConfigFile()
	if err != nil {
		return nil, 0, err
	}
	mcConfig, err := loadMCCamouflageConfig(fileCfg.MC)
	if err != nil {
		return nil, 0, err
	}
	b.mcConfig = mcConfig
	b.reconnect = loadReconnectConfig(fileCfg.TCP)

	var (
		listener4 net.Listener
		listener6 net.Listener
		port      int
		tries     int
	)

again:
	port = int(uport)
	listener4, port, err = listenTCP("tcp4", port)
	if err != nil && !errors.Is(err, syscall.EAFNOSUPPORT) {
		return nil, 0, err
	}

	listener6, port, err = listenTCP("tcp6", port)
	if uport == 0 && errors.Is(err, syscall.EADDRINUSE) && tries < 100 {
		if listener4 != nil {
			listener4.Close()
		}
		tries++
		goto again
	}
	if err != nil && !errors.Is(err, syscall.EAFNOSUPPORT) {
		if listener4 != nil {
			listener4.Close()
		}
		return nil, 0, err
	}
	if listener4 == nil && listener6 == nil {
		return nil, 0, syscall.EAFNOSUPPORT
	}

	b.listener4 = listener4
	b.listener6 = listener6
	b.sessions = make(map[string]*tcpSession)
	b.dialMu = make(map[string]*sync.Mutex)
	b.inboundIPs = make(map[netip.Addr]struct{})
	b.pending = make(map[net.Conn]struct{})
	b.recvCh = make(chan tcpPacket, 256)
	b.done = make(chan struct{})
	b.ctx, b.cancel = context.WithCancel(context.Background())
	b.isOpen = true

	if b.listener4 != nil {
		b.wg.Add(1)
		go b.acceptLoop(b.listener4)
	}
	if b.listener6 != nil {
		b.wg.Add(1)
		go b.acceptLoop(b.listener6)
	}

	return []ReceiveFunc{b.receive}, uint16(port), nil
}

func listenTCP(network string, port int) (net.Listener, int, error) {
	l, err := net.Listen(network, ":"+strconv.Itoa(port))
	if err != nil {
		return nil, 0, err
	}
	tcpAddr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		l.Close()
		return nil, 0, fmt.Errorf("unexpected listener addr type %T", l.Addr())
	}
	return l, tcpAddr.Port, nil
}

func (b *TCPBind) Close() error {
	b.mu.Lock()
	if !b.isOpen {
		b.mu.Unlock()
		return nil
	}
	done := b.done
	cancel := b.cancel
	listener4 := b.listener4
	listener6 := b.listener6
	sessions := b.sessions

	b.isOpen = false
	b.listener4 = nil
	b.listener6 = nil
	b.sessions = nil
	b.inboundIPs = nil
	b.mu.Unlock()

	// Cancel in-flight DialContext / MC handshake first so peer.Stop is not blocked.
	if cancel != nil {
		cancel()
	}
	b.pendingMu.Lock()
	for c := range b.pending {
		_ = c.SetDeadline(time.Now())
		_ = c.Close()
	}
	b.pending = nil
	b.pendingMu.Unlock()

	close(done)
	if listener4 != nil {
		_ = listener4.Close()
	}
	if listener6 != nil {
		_ = listener6.Close()
	}
	for _, session := range sessions {
		_ = session.conn.SetDeadline(time.Now())
		_ = session.conn.Close()
	}

	waitDone := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(waitDone)
	}()
	select {
	case <-waitDone:
	case <-time.After(2 * time.Second):
		// accept/read loops did not exit in time; proceed anyway so peer.Stop is not wedged
	}

	b.mu.Lock()
	// Keep b.done as the closed channel (do NOT nil it). In-flight dialWithBackoff
	// selects on getDone(); a nil channel would block forever and stall peer.Stop.
	b.recvCh = nil
	b.cancel = nil
	b.ctx = nil
	b.mcConfig = mcCamouflageConfig{}
	b.reconnect = reconnectConfig{}
	b.dialMu = nil
	b.mu.Unlock()
	return nil
}

func (*TCPBind) SetMark(uint32) error {
	return nil
}

func (*TCPBind) BatchSize() int {
	return 1
}

func (b *TCPBind) Send(bufs [][]byte, endpoint Endpoint) error {
	te, ok := endpoint.(*TCPEndpoint)
	if !ok {
		return ErrWrongEndpointType
	}
	session, err := b.getOrDialSession(te.dst)
	if err != nil {
		return err
	}
	for _, buf := range bufs {
		if len(buf) > int(^uint16(0)) {
			return fmt.Errorf("tcp frame too large: %d", len(buf))
		}
		err = b.sessionWrite(session, buf)
		if err == nil {
			continue
		}
		// Connection died — drop and redial (client) or wait for inbound (server).
		b.dropSession(session.key, session)
		session, err = b.getOrDialSession(te.dst)
		if err != nil {
			return err
		}
		if err = b.sessionWrite(session, buf); err != nil {
			b.dropSession(session.key, session)
			return err
		}
	}
	return nil
}

func (b *TCPBind) sessionWrite(session *tcpSession, buf []byte) error {
	var header [tcpFrameHeaderSize]byte
	binary.BigEndian.PutUint16(header[:], uint16(len(buf)))
	session.writeMu.Lock()
	defer session.writeMu.Unlock()
	if session.conn == nil || !session.alive {
		return net.ErrClosed
	}
	_ = session.conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	defer session.conn.SetWriteDeadline(time.Time{})
	if err := writeAll(session.conn, header[:]); err != nil {
		return err
	}
	return writeAll(session.conn, buf)
}

func (b *TCPBind) getOrDialSession(dst netip.AddrPort) (*tcpSession, error) {
	key := dst.String()

	if session := b.lookupSession(dst); session != nil {
		return session, nil
	}

	mu := b.dialMuFor(key)
	mu.Lock()
	defer mu.Unlock()

	if session := b.lookupSession(dst); session != nil {
		return session, nil
	}

	// Do not dial back through NAT to a peer that previously connected inbound.
	// That path produced: dial tcp x.x.x.x:port: i/o timeout and stalled recovery.
	if b.mustWaitInbound(dst) {
		return nil, ErrWaitingInboundReconnect
	}

	conn, err := b.dialWithRetries(dst, maxSendDialAttempts)
	if err != nil {
		return nil, err
	}
	session := b.installSession(dst, conn, false)
	if session == nil {
		_ = conn.Close()
		return nil, net.ErrClosed
	}
	return session, nil
}

func (b *TCPBind) mustWaitInbound(dst netip.AddrPort) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.reconnect.allowOutbound {
		return true
	}
	if b.inboundIPs == nil {
		return false
	}
	_, ok := b.inboundIPs[dst.Addr()]
	return ok
}

func (b *TCPBind) lookupSession(dst netip.AddrPort) *tcpSession {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.isOpen || b.sessions == nil {
		return nil
	}
	if session, ok := b.sessions[dst.String()]; ok && session != nil && session.alive {
		return session
	}
	// NAT reconnect: fall back to same-IP only when unambiguous (single session).
	ip := dst.Addr()
	var match *tcpSession
	n := 0
	for _, session := range b.sessions {
		if session == nil || !session.alive {
			continue
		}
		if session.dst.Addr() == ip {
			n++
			match = session
		}
	}
	if n == 1 {
		return match
	}
	return nil
}

func (b *TCPBind) installSession(dst netip.AddrPort, conn net.Conn, inbound bool) *tcpSession {
	return b.installSessionToNAT(dst, conn, inbound, false)
}

func (b *TCPBind) installSessionToNAT(dst netip.AddrPort, conn net.Conn, inbound, toNAT bool) *tcpSession {
	key := dst.String()
	session := &tcpSession{
		bind:    b,
		key:     key,
		dst:     dst,
		conn:    conn,
		alive:   true,
		inbound: inbound,
		toNAT:   toNAT,
	}
	session.lastRX.Store(time.Now().UnixNano())

	b.mu.Lock()
	if !b.isOpen || b.sessions == nil {
		b.mu.Unlock()
		return nil
	}
	var stale *tcpSession
	if old, ok := b.sessions[key]; ok {
		stale = old
		delete(b.sessions, key)
	}
	b.sessions[key] = session
	if inbound {
		if b.inboundIPs == nil {
			b.inboundIPs = make(map[netip.Addr]struct{})
		}
		b.inboundIPs[dst.Addr()] = struct{}{}
	}
	idle := b.reconnect.rxIdleTimeout
	if idle <= 0 {
		idle = defaultRXIdleTimeout
	}
	b.wg.Add(2) // readLoop + idleWatch
	b.mu.Unlock()

	if stale != nil {
		stale.alive = false
		if stale.conn != nil {
			_ = stale.conn.SetDeadline(time.Now())
			_ = stale.conn.Close()
		}
	}
	go b.readLoop(session)
	go b.idleWatch(session, idle)
	return session
}

func enableTCPKeepAlive(conn net.Conn) {
	tc, ok := conn.(*net.TCPConn)
	if !ok {
		return
	}
	_ = tc.SetKeepAlive(true)
	_ = tc.SetKeepAlivePeriod(tcpKeepAlivePeriod)
}

func (b *TCPBind) dialOnce(dst netip.AddrPort) (net.Conn, error) {
	cfg := b.getReconnectConfig()
	ctx := b.dialContext()
	if ctx == nil {
		return nil, net.ErrClosed
	}
	dialer := &net.Dialer{Timeout: cfg.dialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", dst.String())
	if err != nil {
		return nil, err
	}
	enableTCPKeepAlive(conn)
	b.trackPending(conn)
	defer b.untrackPending(conn)

	if err := b.performMCCamouflageClient(conn, dst); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if b.dialToNAT.Load() {
		if _, err := conn.Write([]byte(toNATMagic)); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("tonat announce: %w", err)
		}
	}
	if b.isClosed() {
		_ = conn.Close()
		return nil, net.ErrClosed
	}
	return conn, nil
}

func (b *TCPBind) dialContext() context.Context {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ctx
}

func (b *TCPBind) trackPending(c net.Conn) {
	b.pendingMu.Lock()
	defer b.pendingMu.Unlock()
	if b.pending == nil {
		_ = c.Close()
		return
	}
	b.pending[c] = struct{}{}
}

func (b *TCPBind) untrackPending(c net.Conn) {
	b.pendingMu.Lock()
	defer b.pendingMu.Unlock()
	if b.pending != nil {
		delete(b.pending, c)
	}
}

func (b *TCPBind) dialWithRetries(dst netip.AddrPort, maxAttempts int) (net.Conn, error) {
	if maxAttempts <= 0 {
		maxAttempts = maxSendDialAttempts
	}
	cfg := b.getReconnectConfig()
	backoff := cfg.initial
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if b.isClosed() {
			return nil, net.ErrClosed
		}
		conn, err := b.dialOnce(dst)
		if err == nil {
			return conn, nil
		}
		lastErr = err
		if b.isClosed() {
			return nil, net.ErrClosed
		}
		if attempt == maxAttempts-1 {
			break
		}
		timer := time.NewTimer(backoff)
		done := b.getDone()
		if done == nil {
			timer.Stop()
			return nil, net.ErrClosed
		}
		select {
		case <-done:
			timer.Stop()
			return nil, net.ErrClosed
		case <-timer.C:
		}
		backoff *= 2
		if backoff > cfg.max {
			backoff = cfg.max
		}
	}
	if lastErr == nil {
		lastErr = errors.New("dial failed")
	}
	return nil, lastErr
}

func (b *TCPBind) dialMuFor(key string) *sync.Mutex {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.dialMu == nil {
		b.dialMu = make(map[string]*sync.Mutex)
	}
	mu, ok := b.dialMu[key]
	if !ok {
		mu = &sync.Mutex{}
		b.dialMu[key] = mu
	}
	return mu
}

func (b *TCPBind) dropSession(key string, session *tcpSession) {
	if session == nil {
		return
	}
	session.alive = false
	b.mu.Lock()
	if b.sessions != nil {
		if cur, ok := b.sessions[key]; ok && cur == session {
			delete(b.sessions, key)
		}
	}
	b.mu.Unlock()
	if session.conn != nil {
		_ = session.conn.SetDeadline(time.Now())
		_ = session.conn.Close()
	}
}

func (b *TCPBind) getReconnectConfig() reconnectConfig {
	b.mu.Lock()
	defer b.mu.Unlock()
	cfg := b.reconnect
	if cfg.initial <= 0 {
		cfg.initial = defaultReconnectInitial
	}
	if cfg.max <= 0 {
		cfg.max = defaultReconnectMax
	}
	if cfg.dialTimeout <= 0 {
		cfg.dialTimeout = defaultDialTimeout
	}
	return cfg
}

func (b *TCPBind) acceptLoop(listener net.Listener) {
	defer b.wg.Done()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if b.isClosed() {
				return
			}
			continue
		}
		toNATFromMC, err := b.performMCCamouflageServer(conn)
		if err != nil {
			_ = conn.Close()
			continue
		}
		conn, toNATMagic, err := peekToNATMagic(conn)
		if err != nil {
			_ = conn.Close()
			continue
		}
		toNAT := toNATFromMC || toNATMagic
		remote, ok := addrPortFromNetAddr(conn.RemoteAddr())
		if !ok {
			_ = conn.Close()
			continue
		}
		enableTCPKeepAlive(conn)
		if b.installSessionToNAT(remote, conn, true, toNAT) == nil {
			_ = conn.Close()
			continue
		}
	}
}

// peekToNATMagic reads an optional ToNAT announcement after MC camouflage.
func peekToNATMagic(conn net.Conn) (net.Conn, bool, error) {
	br := bufio.NewReader(conn)
	_ = conn.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
	hdr, err := br.Peek(len(toNATMagic))
	_ = conn.SetReadDeadline(time.Time{})
	if err == nil && string(hdr) == toNATMagic {
		_, _ = br.Discard(len(toNATMagic))
		return &bufConn{Reader: br, Conn: conn}, true, nil
	}
	// No magic (or timeout/short read): keep buffered bytes for WG frames.
	return &bufConn{Reader: br, Conn: conn}, false, nil
}

type bufConn struct {
	*bufio.Reader
	net.Conn
}

func (c *bufConn) Read(p []byte) (int, error) {
	return c.Reader.Read(p)
}

func (b *TCPBind) readLoop(session *tcpSession) {
	defer b.wg.Done()
	defer b.dropSession(session.key, session)

	for {
		var header [tcpFrameHeaderSize]byte
		if _, err := io.ReadFull(session.conn, header[:]); err != nil {
			return
		}
		size := int(binary.BigEndian.Uint16(header[:]))
		payload := make([]byte, size)
		if size > 0 {
			if _, err := io.ReadFull(session.conn, payload); err != nil {
				return
			}
		}
		session.lastRX.Store(time.Now().UnixNano())

		dst, ok := addrPortFromNetAddr(session.conn.RemoteAddr())
		if !ok {
			return
		}
		src, _ := addrPortFromNetAddr(session.conn.LocalAddr())
		packet := tcpPacket{
			payload: payload,
			endpoint: &TCPEndpoint{
				dst:   dst,
				src:   src,
				toNAT: session.toNAT,
			},
		}

		recvCh := b.getRecvCh()
		done := b.getDone()
		if recvCh == nil || done == nil {
			return
		}
		select {
		case <-done:
			return
		case recvCh <- packet:
		}
	}
}

// idleWatch closes the TCP session if no framed payload arrives within idle.
// Pair with PersistentKeepalive ≈ rxIdleTimeout on both peers so a dead link
// is detected quickly and the client redials.
func (b *TCPBind) idleWatch(session *tcpSession, idle time.Duration) {
	defer b.wg.Done()
	if idle <= 0 {
		idle = defaultRXIdleTimeout
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if !session.alive {
			return
		}
		done := b.getDone()
		if done == nil {
			return
		}
		select {
		case <-done:
			return
		case <-ticker.C:
			last := session.lastRX.Load()
			if last == 0 {
				continue
			}
			if time.Since(time.Unix(0, last)) > idle {
				if session.conn != nil {
					_ = session.conn.SetDeadline(time.Now())
					_ = session.conn.Close()
				}
				return
			}
		}
	}
}

func (b *TCPBind) receive(packets [][]byte, sizes []int, eps []Endpoint) (int, error) {
	done := b.getDone()
	recvCh := b.getRecvCh()
	if done == nil || recvCh == nil {
		return 0, net.ErrClosed
	}

	select {
	case <-done:
		return 0, net.ErrClosed
	case packet := <-recvCh:
		n := copy(packets[0], packet.payload)
		sizes[0] = n
		eps[0] = packet.endpoint
		if n != len(packet.payload) {
			return 1, io.ErrShortBuffer
		}
		return 1, nil
	}
}

func (b *TCPBind) isClosed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.isOpen
}

func (b *TCPBind) getDone() <-chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.done
}

func (b *TCPBind) getRecvCh() chan tcpPacket {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.recvCh
}

func (b *TCPBind) getMCConfig() mcCamouflageConfig {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.mcConfig
}

func addrPortFromNetAddr(addr net.Addr) (netip.AddrPort, bool) {
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok {
		return netip.AddrPort{}, false
	}
	ip, ok := netip.AddrFromSlice(tcpAddr.IP)
	if !ok {
		return netip.AddrPort{}, false
	}
	return netip.AddrPortFrom(ip.Unmap(), uint16(tcpAddr.Port)), true
}

func writeAll(conn net.Conn, data []byte) error {
	for len(data) > 0 {
		n, err := conn.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrUnexpectedEOF
		}
		data = data[n:]
	}
	return nil
}

func loadMCCamouflageConfig(fileCfg mcConfigFile) (mcCamouflageConfig, error) {
	enabled := true
	if fileCfg.Enabled != nil {
		enabled = *fileCfg.Enabled
	}
	deep := true
	if fileCfg.DeepCamouflage != nil {
		deep = *fileCfg.DeepCamouflage
	}
	loginUser := strings.TrimSpace(fileCfg.LoginUsername)
	if loginUser == "" {
		loginUser = defaultLoginUsername
	}
	pluginChannel := strings.TrimSpace(fileCfg.LoginPluginChannel)
	if pluginChannel == "" {
		pluginChannel = defaultLoginPluginChannel
	}
	pluginSecret := strings.TrimSpace(fileCfg.LoginPluginSecret)
	if pluginSecret == "" {
		pluginSecret = defaultLoginPluginSecret
	}
	rejectMessage := strings.TrimSpace(fileCfg.RejectMessage)
	if rejectMessage == "" {
		rejectMessage = defaultRejectDisconnectMsg
	}
	cfg := mcCamouflageConfig{
		enabled:       enabled,
		deep:          deep,
		timeout:       parseDurationWithDefault(fileCfg.HandshakeTimeout, defaultMCHandshakeTime),
		loginUsername: loginUser,
		pluginChannel: pluginChannel,
		pluginSecret:  pluginSecret,
		rejectMessage: rejectMessage,
	}
	if cfg.timeout <= 0 {
		cfg.timeout = defaultMCHandshakeTime
	}
	return cfg, nil
}

func writeMCPacket(conn net.Conn, payload []byte) error {
	var packet bytes.Buffer
	writeMCVarInt(&packet, len(payload))
	packet.Write(payload)
	return writeAll(conn, packet.Bytes())
}

func readMCPacket(conn net.Conn) ([]byte, error) {
	size, err := readMCVarIntFromConn(conn)
	if err != nil {
		return nil, err
	}
	if size < 0 || size > 1<<20 {
		return nil, fmt.Errorf("invalid mc packet size %d", size)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func writeMCVarInt(w io.Writer, value int) {
	u := uint32(value)
	for {
		if (u & ^uint32(0x7F)) == 0 {
			_, _ = w.Write([]byte{byte(u)})
			return
		}
		_, _ = w.Write([]byte{byte(u&0x7F | 0x80)})
		u >>= 7
	}
}

func readMCVarIntFromConn(conn net.Conn) (int, error) {
	var numRead int
	var result int
	for {
		if numRead > 5 {
			return 0, errors.New("varint too long")
		}
		var one [1]byte
		if _, err := io.ReadFull(conn, one[:]); err != nil {
			return 0, err
		}
		value := int(one[0] & 0x7F)
		result |= value << (7 * numRead)
		numRead++
		if (one[0] & 0x80) == 0 {
			return result, nil
		}
	}
}

func readMCVarInt(r *bytes.Reader) (int, error) {
	var numRead int
	var result int
	for {
		if numRead > 5 {
			return 0, errors.New("varint too long")
		}
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		value := int(b & 0x7F)
		result |= value << (7 * numRead)
		numRead++
		if (b & 0x80) == 0 {
			return result, nil
		}
	}
}

func writeMCString(w io.Writer, s string) {
	writeMCVarInt(w, len(s))
	_, _ = w.Write([]byte(s))
}

func readMCString(r *bytes.Reader) (string, error) {
	size, err := readMCVarInt(r)
	if err != nil {
		return "", err
	}
	if size < 0 || size > r.Len() {
		return "", errors.New("invalid string size")
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(r, data); err != nil {
		return "", err
	}
	return string(data), nil
}

func loadReconnectConfig(fileCfg tcpConfigFile) reconnectConfig {
	allowOutbound := true
	if fileCfg.AllowOutboundDial != nil {
		allowOutbound = *fileCfg.AllowOutboundDial
	}
	cfg := reconnectConfig{
		initial:       parseDurationWithDefault(fileCfg.ReconnectInitialBackoff, defaultReconnectInitial),
		max:           parseDurationWithDefault(fileCfg.ReconnectMaxBackoff, defaultReconnectMax),
		dialTimeout:   parseDurationWithDefault(fileCfg.DialTimeout, defaultDialTimeout),
		rxIdleTimeout: parseDurationWithDefault(fileCfg.RXIdleTimeout, defaultRXIdleTimeout),
		allowOutbound: allowOutbound,
	}
	if cfg.initial <= 0 {
		cfg.initial = defaultReconnectInitial
	}
	if cfg.max < cfg.initial {
		cfg.max = cfg.initial
	}
	if cfg.dialTimeout <= 0 {
		cfg.dialTimeout = defaultDialTimeout
	}
	if cfg.rxIdleTimeout <= 0 {
		cfg.rxIdleTimeout = defaultRXIdleTimeout
	}
	return cfg
}

func loadTransportConfigFile() (transportConfigFile, error) {
	path := defaultTransportConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return transportConfigFile{}, nil
		}
		return transportConfigFile{}, fmt.Errorf("read transport config %q: %w", path, err)
	}
	data = stripJSONComments(data)
	var cfg transportConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return transportConfigFile{}, fmt.Errorf("parse transport config %q: %w", path, err)
	}
	return cfg, nil
}

// stripJSONComments removes // line comments and /* */ block comments outside JSON strings.
func stripJSONComments(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inString := false
	escaped := false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if inString {
			out = append(out, c)
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			out = append(out, c)
			continue
		}
		if c == '/' && i+1 < len(data) {
			switch data[i+1] {
			case '/':
				i += 2
				for i < len(data) && data[i] != '\n' {
					i++
				}
				if i < len(data) {
					out = append(out, '\n')
				}
				continue
			case '*':
				i += 2
				for i+1 < len(data) && !(data[i] == '*' && data[i+1] == '/') {
					i++
				}
				i++
				continue
			}
		}
		out = append(out, c)
	}
	return out
}

func defaultTransportConfigPath() string {
	// Prefer /etc/wireguard on Unix; fall back to executable directory on Windows.
	if runtime.GOOS != "windows" {
		return filepath.Join("/etc/wireguard", transportConfigFileName)
	}
	executablePath, err := os.Executable()
	if err != nil || executablePath == "" {
		return transportConfigFileName
	}
	return filepath.Join(filepath.Dir(executablePath), transportConfigFileName)
}

func parseDurationWithDefault(v string, defaultVal time.Duration) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return defaultVal
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return defaultVal
	}
	return d
}
