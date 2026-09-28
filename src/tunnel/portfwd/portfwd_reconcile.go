package portfwd

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"golang.zx2c4.com/wireguard/src/tunnel/spec"
)

// forwardEntry is one running forward: its listener and the connections it
// proxies, so it can be stopped without touching the others.
type forwardEntry struct {
	key    string
	closer io.Closer
	conns  map[net.Conn]struct{}
	closed bool
}

func forwardKey(s spec.PortForwardSpec) string {
	return s.Proto + "/" + s.ListenAddr() + ">" + s.DestAddr()
}

func (m *PortForwardManager) running(s spec.PortForwardSpec) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.forwards[forwardKey(s)]
	return ok
}

func (m *PortForwardManager) register(s spec.PortForwardSpec, closer io.Closer) (*forwardEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		_ = closer.Close()
		return nil, fmt.Errorf("forward manager closed")
	}
	e := &forwardEntry{key: forwardKey(s), closer: closer, conns: make(map[net.Conn]struct{})}
	m.forwards[e.key] = e
	return e, nil
}

func (m *PortForwardManager) track(e *forwardEntry, c net.Conn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || e.closed {
		_ = c.Close()
		return
	}
	e.conns[c] = struct{}{}
}

func (m *PortForwardManager) untrack(e *forwardEntry, c net.Conn) {
	m.mu.Lock()
	delete(e.conns, c)
	m.mu.Unlock()
}

func (m *PortForwardManager) closeEntry(e *forwardEntry) {
	m.mu.Lock()
	e.closed = true
	conns := e.conns
	e.conns = make(map[net.Conn]struct{})
	m.mu.Unlock()
	_ = e.closer.Close()
	for c := range conns {
		_ = c.SetDeadline(time.Now())
		_ = c.Close()
	}
}

// Reconcile hot-switches the running forwards to exactly those of peers:
// unchanged forwards (and their live connections) are kept, removed ones are
// stopped first so a changed destination can reuse the same listen port.
// Peer OnUp/OnDown hooks are not run.
func (m *PortForwardManager) Reconcile(peers []spec.PeerHookConfig) error {
	type want struct {
		spec  spec.PortForwardSpec
		label string
	}
	wanted := make(map[string]want)
	for _, p := range peers {
		for _, fw := range p.Forwards {
			wanted[forwardKey(fw)] = want{fw, p.Label}
		}
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return fmt.Errorf("forward manager closed")
	}
	var stale []*forwardEntry
	for key, e := range m.forwards {
		if _, ok := wanted[key]; !ok {
			stale = append(stale, e)
			delete(m.forwards, key)
		}
	}
	m.mu.Unlock()
	for _, e := range stale {
		m.closeEntry(e)
		fmt.Fprintf(os.Stderr, "lasitan-cluster: stopped forward %s\n", e.key)
	}

	var errs []error
	for _, w := range wanted {
		var err error
		switch w.spec.Proto {
		case spec.ProtoTCP:
			err = m.startTCPForward(w.spec, w.label)
		case spec.ProtoUDP:
			err = m.startUDPForward(w.spec, w.label)
		default:
			err = fmt.Errorf("unknown forward proto %q", w.spec.Proto)
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
