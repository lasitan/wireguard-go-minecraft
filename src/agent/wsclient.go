package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"golang.zx2c4.com/wireguard/src/core"
	"golang.zx2c4.com/wireguard/src/core/config"
	"golang.zx2c4.com/wireguard/src/core/wire"
	"golang.zx2c4.com/wireguard/src/tunnel/ipcounter"
	"golang.zx2c4.com/wireguard/src/tunnel/portfwd"
	"golang.zx2c4.com/wireguard/src/update"
	"golang.zx2c4.com/wireguard/src/wireguard/device"
)

const (
	wsDialTimeout      = 10 * time.Second
	wsWriteTimeout     = 10 * time.Second
	wsReadTimeout      = 45 * time.Second
	wsDefaultStatsTick = 2 * time.Second
	publicIPRefresh    = 30 * time.Minute
	applyRetryMin      = 2 * time.Second
	applyRetryMax      = 30 * time.Second
)

type wsClient struct {
	boot    *config.AgentBootstrap
	dev     *device.Device
	counter *ipcounter.IPCounter
	ap      *applier

	seq atomic.Uint32
	// retrying is the revision retryApply is working on (-1 = none).
	retrying atomic.Int64
	// coder/websocket allows one writer; statsLoop and the read loop
	// (pong / config ack) would otherwise race and abort the socket.
	writeMu sync.Mutex

	ipMu  sync.Mutex
	ipAt  time.Time
	pubV4 netip.Addr
	pubV6 netip.Addr
}

func wsURL(masterURL string) string {
	u := stringsTrimSlash(masterURL)
	switch {
	case strings.HasPrefix(u, "https://"):
		u = "wss://" + strings.TrimPrefix(u, "https://")
	case strings.HasPrefix(u, "http://"):
		u = "ws://" + strings.TrimPrefix(u, "http://")
	}
	return u + "/api/agent/ws"
}

func (c *wsClient) publicIPs() (netip.Addr, netip.Addr) {
	c.ipMu.Lock()
	defer c.ipMu.Unlock()
	if c.ipAt.IsZero() || time.Since(c.ipAt) > publicIPRefresh {
		c.pubV4, c.pubV6 = detectPublicIPs(c.boot.MasterURL)
		c.ipAt = time.Now()
	}
	return c.pubV4, c.pubV6
}

func (c *wsClient) write(ctx context.Context, ws *websocket.Conn, t wire.MsgType, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	wctx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
	defer cancel()
	return ws.Write(wctx, websocket.MessageBinary, wire.EncodeFrame(t, c.seq.Add(1), payload))
}

// run holds one WS session. connected reports whether the handshake
// succeeded, which callers use to choose reconnect vs. HTTP fallback.
func (c *wsClient) run(ctx context.Context) (connected bool, err error) {
	v4, v6 := c.publicIPs()

	dctx, cancel := context.WithTimeout(ctx, wsDialTimeout)
	ws, _, err := websocket.Dial(dctx, wsURL(c.boot.MasterURL), nil)
	cancel()
	if err != nil {
		return false, err
	}
	defer ws.CloseNow()
	ws.SetReadLimit(wire.MaxFrameBytes)

	if err := c.write(ctx, ws, wire.TypeHello, wire.Hello{Token: c.boot.Key, Version: update.Current(), PublicV4: v4, PublicV6: v6}.Marshal()); err != nil {
		return false, err
	}
	hctx, hcancel := context.WithTimeout(ctx, wsDialTimeout)
	f, err := readWire(hctx, ws)
	hcancel()
	if err != nil {
		return false, err
	}
	switch f.Type {
	case wire.TypeHelloAck:
	case wire.TypeError:
		var em wire.ErrorMsg
		_ = em.Unmarshal(f.Payload)
		return false, fmt.Errorf("master rejected: %d %s", em.Code, em.Message)
	default:
		return false, fmt.Errorf("unexpected frame %d during handshake", f.Type)
	}
	var ack wire.HelloAck
	if err := ack.Unmarshal(f.Payload); err != nil {
		return false, err
	}
	tick := wsDefaultStatsTick
	if ack.StatsIntervalMs >= 500 {
		tick = time.Duration(ack.StatsIntervalMs) * time.Millisecond
	}
	fmt.Fprintf(os.Stderr, "lasitan-cluster: master link up (ws, node %s)\n", ack.NodeID)
	c.ap.resetSession()

	sctx, scancel := context.WithCancel(ctx)
	defer scancel()
	go c.statsLoop(sctx, ws, tick)

	for {
		rctx, rcancel := context.WithTimeout(sctx, wsReadTimeout)
		f, err := readWire(rctx, ws)
		rcancel()
		if err != nil {
			return true, err
		}
		switch f.Type {
		case wire.TypePing:
			if err := c.write(sctx, ws, wire.TypePong, f.Payload); err != nil {
				return true, err
			}
		case wire.TypeConfigPush:
			var desired core.DesiredConfig
			ack := wire.ConfigAck{OK: true}
			if err := json.Unmarshal(f.Payload, &desired); err != nil {
				ack.OK, ack.Error = false, err.Error()
			} else {
				ack.Revision = uint32(desired.Revision)
				if err := c.ap.apply(&desired); err != nil {
					ack.OK, ack.Error = false, err.Error()
					fmt.Fprintf(os.Stderr, "lasitan-cluster: %v\n", err)
					go c.retryApply(sctx, ws, &desired)
				}
			}
			if err := c.write(sctx, ws, wire.TypeConfigAck, ack.Marshal()); err != nil {
				return true, err
			}
		case wire.TypeUpdate:
			var cmd wire.UpdateCmd
			_ = cmd.Unmarshal(f.Payload)
			ack := wire.UpdateAck{OK: true}
			note, err := update.SpawnDetached(update.SpawnOptions{Force: cmd.Force, Proxy: cmd.Proxy, Release: cmd.Release})
			if err != nil {
				ack.OK, ack.Message = false, err.Error()
			} else {
				ack.Message = note
			}
			fmt.Fprintf(os.Stderr, "lasitan-cluster: master requested update: ok=%v %s\n", ack.OK, ack.Message)
			if err := c.write(sctx, ws, wire.TypeUpdateAck, ack.Marshal()); err != nil {
				return true, err
			}
		case wire.TypeError:
			var em wire.ErrorMsg
			_ = em.Unmarshal(f.Payload)
			return true, fmt.Errorf("master error: %d %s", em.Code, em.Message)
		}
	}
}

// retryApply keeps re-applying a pushed revision that failed (e.g. the OS
// refused the new interface address) until it sticks, a newer revision
// supersedes it, or the session ends. Master only pushes on change, so
// without this the node would sit on its old address indefinitely.
func (c *wsClient) retryApply(ctx context.Context, ws *websocket.Conn, d *core.DesiredConfig) {
	rev := int64(d.Revision)
	if c.retrying.Swap(rev) == rev {
		return
	}
	defer c.retrying.CompareAndSwap(rev, -1)
	delay := applyRetryMin
	for {
		if !sleepCtx(ctx, delay) {
			return
		}
		if c.retrying.Load() != rev {
			return
		}
		err := c.ap.apply(d)
		if err == nil {
			if c.ap.revision() == d.Revision {
				ack := wire.ConfigAck{OK: true, Revision: uint32(d.Revision)}
				_ = c.write(ctx, ws, wire.TypeConfigAck, ack.Marshal())
			}
			return
		}
		fmt.Fprintf(os.Stderr, "lasitan-cluster: retry: %v\n", err)
		if delay *= 2; delay > applyRetryMax {
			delay = applyRetryMax
		}
	}
}

func readWire(ctx context.Context, ws *websocket.Conn) (wire.Frame, error) {
	typ, b, err := ws.Read(ctx)
	if err != nil {
		return wire.Frame{}, err
	}
	if typ != websocket.MessageBinary {
		return wire.Frame{}, errors.New("non-binary frame")
	}
	return wire.DecodeFrame(b)
}

func (c *wsClient) statsLoop(ctx context.Context, ws *websocket.Conn, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if err := c.write(ctx, ws, wire.TypeStats, c.collect().Marshal()); err != nil {
			if ctx.Err() == nil {
				ws.CloseNow()
			}
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (c *wsClient) collect() wire.Stats {
	rx, tx, ips := c.counter.Snapshot()
	st := wire.Stats{UnixMilli: time.Now().UnixMilli(), RxBytes: rx, TxBytes: tx}
	for _, p := range c.dev.PeerStats() {
		rtt := p.HandshakeRTTNano / 1000
		if rtt > 1<<32-1 {
			rtt = 0
		}
		st.Peers = append(st.Peers, wire.PeerStat{
			PublicKey:          p.PublicKey,
			RxBytes:            p.RxBytes,
			TxBytes:            p.TxBytes,
			LastHandshakeNano:  p.LastHandshakeNano,
			HandshakeRTTMicros: uint32(rtt),
		})
	}
	for _, s := range ips {
		st.IPs = append(st.IPs, wire.IPStat{IP: s.IP, RxBytes: s.Rx, TxBytes: s.Tx})
	}
	for _, f := range portfwd.ForwardCounters() {
		frx, ftx := f.Bytes()
		st.Forwards = append(st.Forwards, wire.ForwardStat{Protocol: f.Protocol, Listen: f.Listen, RxBytes: frx, TxBytes: ftx})
	}
	return st
}
