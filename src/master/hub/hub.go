package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"net"
	"net/http"
	"net/netip"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"golang.zx2c4.com/wireguard/src/master/stats"
	"golang.zx2c4.com/wireguard/src/master/store"
	"golang.zx2c4.com/wireguard/src/utils/netaddr"

	"github.com/coder/websocket"

	"golang.zx2c4.com/wireguard/src/core"
	"golang.zx2c4.com/wireguard/src/core/wire"
	"golang.zx2c4.com/wireguard/src/update"
)

const (
	hubShards          = 64
	hubSendQueue       = 32
	hubHelloTimeout    = 10 * time.Second
	hubWriteTimeout    = 10 * time.Second
	hubPingInterval    = 15 * time.Second
	hubReadTimeout     = 45 * time.Second
	hubTouchThrottle   = 5 * time.Second
	hubStatsIntervalMs = 2000
)

// Hub holds one long-lived WebSocket per agent. Connections are spread over
// shards so registration and broadcast do not contend on a single lock.
type Hub struct {
	store  *store.Store
	stats  *stats.StatsService
	shards [hubShards]hubShard

	// OnPublicIPs is called (in its own goroutine) when an agent reports
	// public addresses; used to trigger GeoIP resolution.
	OnPublicIPs func(nodeID, v4, v6 string)
	// OnConnChange is called after an agent connects or disconnects.
	OnConnChange func()
}

func (h *Hub) connChanged() {
	if h.OnConnChange != nil {
		h.OnConnChange()
	}
}

type hubShard struct {
	mu    sync.RWMutex
	conns map[string]*agentConn
}

type agentConn struct {
	nodeID  string
	version string // build version from Hello
	ws      *websocket.Conn
	send    chan []byte
	cfg     atomic.Pointer[core.DesiredConfig]
	cfgSig  chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc
	seq     atomic.Uint32

	lastTouch time.Time
	ackedRev  atomic.Uint32

	updMu  sync.Mutex // one RequestUpdate at a time per connection
	updAck chan wire.UpdateAck
}

func NewHub(st *store.Store, statsSvc *stats.StatsService) *Hub {
	h := &Hub{store: st, stats: statsSvc}
	for i := range h.shards {
		h.shards[i].conns = make(map[string]*agentConn)
	}
	return h
}

func (h *Hub) shard(nodeID string) *hubShard {
	f := fnv.New32a()
	_, _ = f.Write([]byte(nodeID))
	return &h.shards[f.Sum32()%hubShards]
}

// Count returns the number of connected agents.
func (h *Hub) Count() int {
	n := 0
	for i := range h.shards {
		s := &h.shards[i]
		s.mu.RLock()
		n += len(s.conns)
		s.mu.RUnlock()
	}
	return n
}

func (h *Hub) IsConnected(nodeID string) bool {
	s := h.shard(nodeID)
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.conns[nodeID]
	return ok
}

// AgentVersion returns the build version a connected agent reported.
func (h *Hub) AgentVersion(nodeID string) (string, bool) {
	s := h.shard(nodeID)
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.conns[nodeID]
	if !ok {
		return "", false
	}
	return c.version, true
}

// AgentVersions snapshots nodeID -> reported version for every connection.
func (h *Hub) AgentVersions() map[string]string {
	out := make(map[string]string)
	for i := range h.shards {
		s := &h.shards[i]
		s.mu.RLock()
		for id, c := range s.conns {
			out[id] = c.version
		}
		s.mu.RUnlock()
	}
	return out
}

func (h *Hub) register(c *agentConn) {
	s := h.shard(c.nodeID)
	s.mu.Lock()
	old := s.conns[c.nodeID]
	s.conns[c.nodeID] = c
	s.mu.Unlock()
	if old != nil {
		old.cancel()
	}
}

// unregister removes c only if it is still the current connection for its node.
func (h *Hub) unregister(c *agentConn) bool {
	s := h.shard(c.nodeID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conns[c.nodeID] == c {
		delete(s.conns, c.nodeID)
		return true
	}
	return false
}

func (h *Hub) snapshotConns() []*agentConn {
	var out []*agentConn
	for i := range h.shards {
		s := &h.shards[i]
		s.mu.RLock()
		for _, c := range s.conns {
			out = append(out, c)
		}
		s.mu.RUnlock()
	}
	return out
}

// Disconnect drops the agent connection for nodeID (e.g. node deleted).
func (h *Hub) Disconnect(nodeID string) {
	s := h.shard(nodeID)
	s.mu.RLock()
	c := s.conns[nodeID]
	s.mu.RUnlock()
	if c != nil {
		c.cancel()
	}
}

// PushAll recompiles and pushes config to every connected agent. Pushes are
// coalesced per connection: only the latest config is kept if the writer lags.
func (h *Hub) PushAll() {
	conns := h.snapshotConns()
	if len(conns) == 0 {
		return
	}
	ids := make([]string, len(conns))
	for i, c := range conns {
		ids[i] = c.nodeID
	}
	cfgs, err := h.store.DesiredForNodes(ids)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lasitan-cluster master: push config: %v\n", err)
		return
	}
	for _, c := range conns {
		d := cfgs[c.nodeID]
		if d == nil {
			c.cancel()
			continue
		}
		c.queueConfig(d)
	}
}

func (c *agentConn) queueConfig(d *core.DesiredConfig) {
	c.cfg.Store(d)
	select {
	case c.cfgSig <- struct{}{}:
	default:
	}
}

func (c *agentConn) frame(t wire.MsgType, payload []byte) []byte {
	return wire.EncodeFrame(t, c.seq.Add(1), payload)
}

// enqueue never blocks; a full queue means the agent cannot keep up, so the
// connection is dropped rather than letting memory grow.
func (c *agentConn) enqueue(b []byte) {
	select {
	case c.send <- b:
	default:
		c.cancel()
	}
}

// ServeWS upgrades an agent to the binary long connection.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	ws.SetReadLimit(wire.MaxFrameBytes)
	defer ws.CloseNow()

	helloCtx, cancelHello := context.WithTimeout(r.Context(), hubHelloTimeout)
	node, hello, err := h.readHello(helloCtx, ws)
	cancelHello()
	if err != nil {
		code := wire.ErrCodeProtocol
		if errors.Is(err, store.ErrUnauthorized) {
			code = wire.ErrCodeAuth
		}
		wctx, wc := context.WithTimeout(context.Background(), hubWriteTimeout)
		_ = ws.Write(wctx, websocket.MessageBinary, wire.EncodeFrame(wire.TypeError, 0, wire.ErrorMsg{Code: code, Message: err.Error()}.Marshal()))
		wc()
		ws.Close(websocket.StatusPolicyViolation, err.Error())
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	c := &agentConn{
		nodeID:  node.ID,
		version: update.Valid(hello.Version),
		ws:      ws,
		send:    make(chan []byte, hubSendQueue),
		cfgSig:  make(chan struct{}, 1),
		updAck:  make(chan wire.UpdateAck, 1),
		ctx:     ctx,
		cancel:  cancel,
	}
	now := time.Now()
	h.register(c)
	h.stats.MarkConnected(node.ID, now)
	_ = h.store.TouchLastSeen(node.ID, now)
	c.lastTouch = now
	h.RecordPublicIPs(node, hello, r.RemoteAddr)

	ackCtx, ackCancel := context.WithTimeout(ctx, hubWriteTimeout)
	err = ws.Write(ackCtx, websocket.MessageBinary, c.frame(wire.TypeHelloAck, wire.HelloAck{NodeID: node.ID, StatsIntervalMs: hubStatsIntervalMs}.Marshal()))
	ackCancel()
	if err != nil {
		cancel()
		if h.unregister(c) {
			h.stats.MarkDisconnected(node.ID)
		}
		return
	}
	if d, err := h.store.DesiredForNode(node.ID); err == nil {
		c.queueConfig(d)
	}

	h.connChanged()
	go h.writeLoop(c)
	h.readLoop(c)

	cancel()
	if h.unregister(c) {
		h.stats.MarkDisconnected(node.ID)
		h.connChanged()
	}
	ws.Close(websocket.StatusNormalClosure, "")
}

func (h *Hub) readHello(ctx context.Context, ws *websocket.Conn) (core.Node, wire.Hello, error) {
	var hello wire.Hello
	typ, b, err := ws.Read(ctx)
	if err != nil {
		return core.Node{}, hello, err
	}
	if typ != websocket.MessageBinary {
		return core.Node{}, hello, fmt.Errorf("expected binary frame")
	}
	f, err := wire.DecodeFrame(b)
	if err != nil {
		return core.Node{}, hello, err
	}
	if f.Type != wire.TypeHello {
		return core.Node{}, hello, fmt.Errorf("expected hello, got type %d", f.Type)
	}
	if err := hello.Unmarshal(f.Payload); err != nil {
		return core.Node{}, hello, err
	}
	if hello.Token == "" {
		return core.Node{}, hello, store.ErrUnauthorized
	}
	node, err := h.store.NodeByToken(hello.Token)
	if err != nil {
		return core.Node{}, hello, err
	}
	return node, hello, nil
}

// RecordPublicIPs stores agent-reported public IPs, falling back to the
// connection's remote address when it is a public address.
func (h *Hub) RecordPublicIPs(node core.Node, hello wire.Hello, remote string) {
	v4, v6 := "", ""
	if hello.PublicV4.IsValid() {
		v4 = hello.PublicV4.Unmap().String()
	}
	if hello.PublicV6.IsValid() {
		v6 = hello.PublicV6.String()
	}
	if v4 == "" && v6 == "" {
		if host, _, err := net.SplitHostPort(remote); err == nil {
			if a, err := netip.ParseAddr(host); err == nil && netaddr.IsPublicAddr(a) {
				a = a.Unmap()
				if a.Is4() {
					v4 = a.String()
				} else {
					v6 = a.String()
				}
			}
		}
	}
	if v4 == "" && v6 == "" {
		return
	}
	if v4 != node.PublicV4 || v6 != node.PublicV6 {
		if bumped, _ := h.store.SetPublicIPs(node.ID, v4, v6); bumped {
			go h.PushAll()
		}
	}
	if h.OnPublicIPs != nil {
		go h.OnPublicIPs(node.ID, v4, v6)
	}
}

func (h *Hub) readLoop(c *agentConn) {
	for {
		rctx, cancel := context.WithTimeout(c.ctx, hubReadTimeout)
		typ, b, err := c.ws.Read(rctx)
		cancel()
		if err != nil {
			return
		}
		if typ != websocket.MessageBinary {
			continue
		}
		f, err := wire.DecodeFrame(b)
		if err != nil {
			return
		}
		now := time.Now()
		switch f.Type {
		case wire.TypeStats:
			var st wire.Stats
			if err := st.Unmarshal(f.Payload); err != nil {
				return
			}
			h.stats.Ingest(c.nodeID, &st, now)
		case wire.TypePing:
			c.enqueue(wire.EncodeFrame(wire.TypePong, f.Seq, f.Payload))
		case wire.TypePong:
			var p wire.Ping
			if p.Unmarshal(f.Payload) == nil && p.UnixNano > 0 {
				if rtt := now.Sub(time.Unix(0, p.UnixNano)); rtt >= 0 {
					h.stats.SetRTT(c.nodeID, rtt)
				}
			}
		case wire.TypeConfigAck:
			var a wire.ConfigAck
			if a.Unmarshal(f.Payload) == nil {
				if a.OK {
					c.ackedRev.Store(a.Revision)
				} else {
					fmt.Fprintf(os.Stderr, "lasitan-cluster master: node %s failed to apply revision %d: %s\n", c.nodeID, a.Revision, a.Error)
				}
			}
		case wire.TypeUpdateAck:
			c.deliverUpdateAck(f.Payload)
		}
		if now.Sub(c.lastTouch) >= hubTouchThrottle {
			c.lastTouch = now
			_ = h.store.TouchLastSeen(c.nodeID, now)
		}
	}
}

func (h *Hub) writeLoop(c *agentConn) {
	ping := time.NewTicker(hubPingInterval)
	defer ping.Stop()
	defer c.cancel()
	write := func(b []byte) bool {
		wctx, cancel := context.WithTimeout(c.ctx, hubWriteTimeout)
		defer cancel()
		return c.ws.Write(wctx, websocket.MessageBinary, b) == nil
	}
	for {
		select {
		case <-c.ctx.Done():
			return
		case b := <-c.send:
			if !write(b) {
				return
			}
		case <-c.cfgSig:
			d := c.cfg.Load()
			if d == nil {
				continue
			}
			payload, err := json.Marshal(d)
			if err != nil {
				continue
			}
			if !write(c.frame(wire.TypeConfigPush, payload)) {
				return
			}
		case <-ping.C:
			if !write(c.frame(wire.TypePing, wire.Ping{UnixNano: time.Now().UnixNano()}.Marshal())) {
				return
			}
		}
	}
}
