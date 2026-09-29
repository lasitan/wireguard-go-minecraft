package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"golang.zx2c4.com/wireguard/src/core"
	"golang.zx2c4.com/wireguard/src/core/wire"
)

const (
	uiSendQueue     = 64
	uiHelloTimeout  = 10 * time.Second
	uiWriteTimeout  = 10 * time.Second
	uiReadTimeout   = 60 * time.Second
	uiTickEvery     = 2 * time.Second
	uiFlushDebounce = 120 * time.Millisecond
)

const (
	uiDirtyMesh uint32 = 1 << iota
	uiDirtyMeta
	uiDirtyVersion
	uiDirtyPresence
)

// uiHub pushes Master state to admin browsers over a binary WebSocket:
// snapshots on change, presence/traffic every uiTickEvery, and detailed
// stats for the node each browser has open.
type uiHub struct {
	s *Server

	mu    sync.Mutex
	conns map[*uiConn]struct{}

	dirty    atomic.Uint32
	kick     chan struct{}
	meshHash uint64 // run goroutine only
}

type uiConn struct {
	ws     *websocket.Conn
	send   chan []byte
	ctx    context.Context
	cancel context.CancelFunc
	sub    atomic.Pointer[string]
}

func newUIHub(s *Server) *uiHub {
	return &uiHub{s: s, conns: map[*uiConn]struct{}{}, kick: make(chan struct{}, 1)}
}

func (u *uiHub) mark(bits uint32) {
	u.dirty.Or(bits)
	select {
	case u.kick <- struct{}{}:
	default:
	}
}

func (u *uiHub) meshChanged()     { u.mark(uiDirtyMesh | uiDirtyVersion) }
func (u *uiHub) metaChanged()     { u.mark(uiDirtyMeta) }
func (u *uiHub) versionChanged()  { u.mark(uiDirtyVersion) }
func (u *uiHub) presenceChanged() { u.mark(uiDirtyPresence | uiDirtyVersion) }

func (u *uiHub) count() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.conns)
}

func (u *uiHub) broadcast(b []byte) {
	u.mu.Lock()
	defer u.mu.Unlock()
	for c := range u.conns {
		c.enqueue(b)
	}
}

// enqueue never blocks; a browser that cannot keep up is dropped and will
// reconnect to a fresh snapshot.
func (c *uiConn) enqueue(b []byte) {
	select {
	case c.send <- b:
	default:
		c.cancel()
	}
}

func (u *uiHub) run(stop <-chan struct{}) {
	tick := time.NewTicker(uiTickEvery)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-u.kick:
			select {
			case <-stop:
				return
			case <-time.After(uiFlushDebounce):
			}
			u.flush(u.dirty.Swap(0))
		case <-tick.C:
			u.periodic()
		}
	}
}

func (u *uiHub) flush(bits uint32) {
	if bits == 0 || u.count() == 0 {
		return
	}
	if bits&uiDirtyMesh != 0 {
		m := u.s.store.Snapshot()
		u.broadcastMeshIfChanged(m)
	}
	if bits&uiDirtyMeta != 0 {
		if b := u.metaFrame(); b != nil {
			u.broadcast(b)
		}
	}
	if bits&uiDirtyVersion != 0 {
		u.broadcast(u.versionFrame())
	}
	if bits&uiDirtyPresence != 0 {
		u.broadcast(u.presenceFrame(u.s.store.Snapshot()))
	}
}

// periodic pushes live traffic/presence and catches mesh changes made
// outside the admin API (public IPs, GeoIP, cluster standby).
func (u *uiHub) periodic() {
	if u.count() == 0 {
		return
	}
	m := u.s.store.Snapshot()
	u.broadcastMeshIfChanged(m)
	u.broadcast(u.presenceFrame(m))
	u.pushSubscribedStats()
}

func (u *uiHub) broadcastMeshIfChanged(m core.Mesh) {
	h := meshFingerprint(m)
	if h == u.meshHash {
		return
	}
	u.meshHash = h
	if b := meshFrame(m); b != nil {
		u.broadcast(b)
	}
}

// meshFingerprint ignores LastSeen, which moves every few seconds and is
// already carried by presence frames.
func meshFingerprint(m core.Mesh) uint64 {
	cp := m.WithoutPrivateKeys()
	for i := range cp.Nodes {
		cp.Nodes[i].LastSeen = time.Time{}
	}
	b, err := json.Marshal(cp)
	if err != nil {
		return 0
	}
	f := fnv.New64a()
	_, _ = f.Write(b)
	return f.Sum64()
}

func (u *uiHub) pushSubscribedStats() {
	u.mu.Lock()
	subs := make(map[*uiConn]string, len(u.conns))
	for c := range u.conns {
		if id := c.sub.Load(); id != nil && *id != "" {
			subs[c] = *id
		}
	}
	u.mu.Unlock()
	frames := map[string][]byte{}
	for c, id := range subs {
		b, ok := frames[id]
		if !ok {
			b = u.nodeStatsFrame(id)
			frames[id] = b
		}
		if b != nil {
			c.enqueue(b)
		}
	}
}

func jsonFrame(t wire.MsgType, v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return wire.EncodeFrame(t, 0, b)
}

func meshFrame(m core.Mesh) []byte {
	return jsonFrame(wire.TypeUIMesh, m.WithoutPrivateKeys())
}

func (u *uiHub) metaFrame() []byte {
	meta, err := u.s.metaView()
	if err != nil {
		return nil
	}
	return jsonFrame(wire.TypeUIMeta, meta)
}

func (u *uiHub) versionFrame() []byte {
	return jsonFrame(wire.TypeUIVersion, u.s.versionView())
}

func (u *uiHub) nodeStatsFrame(id string) []byte {
	v, ok := u.s.nodeStats(id)
	if !ok {
		return nil
	}
	return jsonFrame(wire.TypeUINodeStats, v)
}

func (u *uiHub) presenceFrame(m core.Mesh) []byte {
	now := time.Now()
	p := wire.Presence{NowMs: now.UnixMilli(), Nodes: make([]wire.PresenceNode, 0, len(m.Nodes))}
	for i := range m.Nodes {
		n := &m.Nodes[i]
		link, _ := u.s.nodeLink(n, now)
		e := wire.PresenceNode{NodeID: n.ID, Link: link}
		if !n.LastSeen.IsZero() {
			e.LastSeenMs = n.LastSeen.UnixMilli()
		}
		live, _ := u.s.stats.Live(n.ID)
		e.RxRate, e.TxRate = live.RxRate, live.TxRate
		if live.RTTMillis > 0 {
			e.RTTMicros = uint32(live.RTTMillis * 1000)
		}
		p.Nodes = append(p.Nodes, e)
	}
	return wire.EncodeFrame(wire.TypeUIPresence, 0, p.Marshal())
}

func (u *uiHub) serveWS(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	ws.SetReadLimit(wire.MaxFrameBytes)
	defer ws.CloseNow()

	helloCtx, cancelHello := context.WithTimeout(r.Context(), uiHelloTimeout)
	err = u.readHello(helloCtx, ws)
	cancelHello()
	if err != nil {
		code := wire.ErrCodeProtocol
		if errors.Is(err, errUIUnauthorized) {
			code = wire.ErrCodeAuth
		}
		wctx, wc := context.WithTimeout(context.Background(), uiWriteTimeout)
		_ = ws.Write(wctx, websocket.MessageBinary, wire.EncodeFrame(wire.TypeUIError, 0, wire.ErrorMsg{Code: code, Message: err.Error()}.Marshal()))
		wc()
		ws.Close(websocket.StatusPolicyViolation, err.Error())
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &uiConn{ws: ws, send: make(chan []byte, uiSendQueue), ctx: ctx, cancel: cancel}

	m := u.s.store.Snapshot()
	initial := [][]byte{
		wire.EncodeFrame(wire.TypeUIHelloAck, 0, wire.UIHelloAck{PresenceIntervalMs: uint32(uiTickEvery / time.Millisecond)}.Marshal()),
		meshFrame(m),
		u.metaFrame(),
		u.versionFrame(),
		u.presenceFrame(m),
	}
	for _, b := range initial {
		if b != nil {
			c.send <- b
		}
	}

	u.mu.Lock()
	u.conns[c] = struct{}{}
	u.mu.Unlock()
	defer func() {
		u.mu.Lock()
		delete(u.conns, c)
		u.mu.Unlock()
	}()

	go u.writeLoop(c)
	u.readLoop(c)
	ws.Close(websocket.StatusNormalClosure, "")
}

var errUIUnauthorized = errors.New("unauthorized")

func (u *uiHub) readHello(ctx context.Context, ws *websocket.Conn) error {
	typ, b, err := ws.Read(ctx)
	if err != nil {
		return err
	}
	if typ != websocket.MessageBinary {
		return fmt.Errorf("expected binary frame")
	}
	f, err := wire.DecodeFrame(b)
	if err != nil {
		return err
	}
	if f.Type != wire.TypeUIHello {
		return fmt.Errorf("expected ui hello, got type %d", f.Type)
	}
	var h wire.UIHello
	if err := h.Unmarshal(f.Payload); err != nil {
		return err
	}
	if !u.s.sessions.valid(h.Token) {
		return errUIUnauthorized
	}
	return nil
}

func (u *uiHub) readLoop(c *uiConn) {
	defer c.cancel()
	for {
		rctx, cancel := context.WithTimeout(c.ctx, uiReadTimeout)
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
		switch f.Type {
		case wire.TypePing:
			c.enqueue(wire.EncodeFrame(wire.TypePong, f.Seq, f.Payload))
		case wire.TypeUISubscribe:
			var s wire.UISubscribe
			if s.Unmarshal(f.Payload) != nil {
				return
			}
			c.sub.Store(&s.NodeID)
			if s.NodeID != "" {
				if fb := u.nodeStatsFrame(s.NodeID); fb != nil {
					c.enqueue(fb)
				}
			}
		}
	}
}

func (u *uiHub) writeLoop(c *uiConn) {
	defer c.cancel()
	for {
		select {
		case <-c.ctx.Done():
			return
		case b := <-c.send:
			wctx, cancel := context.WithTimeout(c.ctx, uiWriteTimeout)
			err := c.ws.Write(wctx, websocket.MessageBinary, b)
			cancel()
			if err != nil {
				return
			}
		}
	}
}
