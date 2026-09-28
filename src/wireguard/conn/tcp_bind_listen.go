package conn

import "net"

var _ ListenPortUpdater = (*TCPBind)(nil)

// UpdateListenPort swaps the accept listeners to port while keeping every
// established session, so a node can become (or stop being) a server without
// dropping its tunnels. The new port is bound before the old one is released.
func (b *TCPBind) UpdateListenPort(port uint16) (uint16, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.isOpen {
		return 0, net.ErrClosed
	}
	if port != 0 && port == b.listenPortLocked() {
		return port, nil
	}
	listener4, listener6, actual, err := openListeners(port)
	if err != nil {
		return 0, err
	}
	old4, old6 := b.listener4, b.listener6
	b.startAcceptLocked(listener4, listener6)
	if old4 != nil {
		_ = old4.Close()
	}
	if old6 != nil {
		_ = old6.Close()
	}
	return uint16(actual), nil
}

func (b *TCPBind) startAcceptLocked(listener4, listener6 net.Listener) {
	b.listener4, b.listener6 = listener4, listener6
	if listener4 != nil {
		b.wg.Add(1)
		go b.acceptLoop(listener4)
	}
	if listener6 != nil {
		b.wg.Add(1)
		go b.acceptLoop(listener6)
	}
}

func (b *TCPBind) listenPortLocked() uint16 {
	for _, l := range []net.Listener{b.listener4, b.listener6} {
		if l == nil {
			continue
		}
		if a, ok := l.Addr().(*net.TCPAddr); ok {
			return uint16(a.Port)
		}
	}
	return 0
}
