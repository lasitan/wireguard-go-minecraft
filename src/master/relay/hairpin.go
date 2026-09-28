package relay

import (
	"os"
	"sync"

	"golang.zx2c4.com/wireguard/src/wireguard/tun"
)

const (
	hairpinMTU   = 1420
	hairpinBatch = 16
	hairpinQueue = 1024
)

// hairpinTUN is a TUN with no OS side: every decrypted packet the device
// writes comes straight back out of Read, so WireGuard's own AllowedIPs table
// forwards it to the owning peer.
type hairpinTUN struct {
	q      chan []byte
	events chan tun.Event
	done   chan struct{}
	once   sync.Once
}

func newHairpinTUN() *hairpinTUN {
	t := &hairpinTUN{
		q:      make(chan []byte, hairpinQueue),
		events: make(chan tun.Event, 1),
		done:   make(chan struct{}),
	}
	t.events <- tun.EventUp
	return t
}

func (t *hairpinTUN) File() *os.File { return nil }

func (t *hairpinTUN) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	var pkt []byte
	select {
	case pkt = <-t.q:
	case <-t.done:
		return 0, os.ErrClosed
	}
	n := 0
	for {
		sizes[n] = copy(bufs[n][offset:], pkt)
		n++
		if n == len(bufs) {
			return n, nil
		}
		select {
		case pkt = <-t.q:
		default:
			return n, nil
		}
	}
}

func (t *hairpinTUN) Write(bufs [][]byte, offset int) (int, error) {
	for _, b := range bufs {
		pkt := append([]byte(nil), b[offset:]...)
		select {
		case t.q <- pkt:
		case <-t.done:
			return 0, os.ErrClosed
		default: // queue full: drop like a congested link
		}
	}
	return len(bufs), nil
}

func (t *hairpinTUN) MTU() (int, error)        { return hairpinMTU, nil }
func (t *hairpinTUN) Name() (string, error)    { return "master-relay", nil }
func (t *hairpinTUN) Events() <-chan tun.Event { return t.events }
func (t *hairpinTUN) BatchSize() int           { return hairpinBatch }

func (t *hairpinTUN) Close() error {
	t.once.Do(func() {
		close(t.done)
		close(t.events)
	})
	return nil
}
