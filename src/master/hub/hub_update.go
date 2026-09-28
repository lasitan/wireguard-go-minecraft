package hub

import (
	"errors"
	"time"

	"golang.zx2c4.com/wireguard/src/core/wire"
)

const hubUpdateAckTimeout = 15 * time.Second

var (
	ErrAgentOffline = errors.New("agent 不在线")
	// Agents built before remote update ignore TypeUpdate and never ack.
	ErrUpdateNoAck = errors.New("agent 未响应（版本过旧不支持远程升级，请在该机器上手动执行升级命令）")
)

// RequestUpdate asks a connected agent to start its self-updater and waits
// for it to confirm. The upgrade itself finishes after the agent restarts and
// reconnects with the new version.
func (h *Hub) RequestUpdate(nodeID string, force bool) (wire.UpdateAck, error) {
	s := h.shard(nodeID)
	s.mu.RLock()
	c := s.conns[nodeID]
	s.mu.RUnlock()
	if c == nil {
		return wire.UpdateAck{}, ErrAgentOffline
	}
	c.updMu.Lock()
	defer c.updMu.Unlock()
	select {
	case <-c.updAck: // drop a stale ack from an earlier timed-out request
	default:
	}
	c.enqueue(c.frame(wire.TypeUpdate, wire.UpdateCmd{Force: force}.Marshal()))
	t := time.NewTimer(hubUpdateAckTimeout)
	defer t.Stop()
	select {
	case a := <-c.updAck:
		return a, nil
	case <-c.ctx.Done():
		return wire.UpdateAck{}, ErrAgentOffline
	case <-t.C:
		return wire.UpdateAck{}, ErrUpdateNoAck
	}
}

func (c *agentConn) deliverUpdateAck(payload []byte) {
	var a wire.UpdateAck
	if a.Unmarshal(payload) != nil {
		return
	}
	select {
	case c.updAck <- a:
	default:
	}
}
