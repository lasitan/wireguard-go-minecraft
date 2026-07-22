//go:build !windows

/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/device"
)

const (
	udpSessionIdle   = 60 * time.Second
	udpMaxPacket     = 65535
	forwardCloseWait = 1 * time.Second
)

type portForwardManager struct {
	logger  *device.Logger
	mu      sync.Mutex
	closers []io.Closer
	active  map[net.Conn]struct{}
	onDown  []struct {
		label string
		cmds  []string
		env   map[string]string
	}
	wg     sync.WaitGroup
	closed bool
}

func newPortForwardManager(logger *device.Logger) *portForwardManager {
	return &portForwardManager{
		logger: logger,
		active: make(map[net.Conn]struct{}),
	}
}

func (m *portForwardManager) StartFromPeers(peers []peerHookConfig) error {
	tcpN, udpN := 0, 0
	for _, p := range peers {
		env := map[string]string{
			"WG_PEER":       p.label,
			"WG_PEER_HOST":  p.allowedIP,
			"WG_ALLOWED_IP": p.allowedIP,
		}
		for _, fw := range p.forwards {
			var err error
			switch fw.Proto {
			case protoTCP:
				err = m.startTCPForward(fw, p.label)
				if err == nil {
					tcpN++
				}
			case protoUDP:
				err = m.startUDPForward(fw, p.label)
				if err == nil {
					udpN++
				}
			default:
				err = fmt.Errorf("unknown forward proto %q", fw.Proto)
			}
			if err != nil {
				m.Close()
				return err
			}
		}
		if err := runPeerHooks(p.onUp, env, m.logger, "OnUp("+p.label+")"); err != nil {
			m.Close()
			return err
		}
		if len(p.onDown) > 0 {
			m.mu.Lock()
			m.onDown = append(m.onDown, struct {
				label string
				cmds  []string
				env   map[string]string
			}{label: p.label, cmds: append([]string{}, p.onDown...), env: env})
			m.mu.Unlock()
		}
	}
	if tcpN+udpN > 0 {
		fmt.Fprintf(os.Stderr, "wireguard-go: started %d TCP + %d UDP forward(s)\n", tcpN, udpN)
	}
	return nil
}

func (m *portForwardManager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.closers)
}

func (m *portForwardManager) track(c net.Conn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		_ = c.Close()
		return
	}
	m.active[c] = struct{}{}
}

func (m *portForwardManager) untrack(c net.Conn) {
	m.mu.Lock()
	delete(m.active, c)
	m.mu.Unlock()
}

func (m *portForwardManager) startTCPForward(spec portForwardSpec, peerLabel string) error {
	ln, err := net.Listen("tcp", spec.ListenAddr())
	if err != nil {
		return fmt.Errorf("ForwardTCP listen %s: %w", spec.ListenAddr(), err)
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		_ = ln.Close()
		return fmt.Errorf("forward manager closed")
	}
	m.closers = append(m.closers, ln)
	m.mu.Unlock()

	m.logger.Verbosef("ForwardTCP %s (peer %s)", spec.String(), peerLabel)
	fmt.Fprintf(os.Stderr, "wireguard-go: ForwardTCP %s (peer %s)\n", spec.ListenAddr()+" -> "+spec.DestAddr(), peerLabel)

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		for {
			client, err := ln.Accept()
			if err != nil {
				return
			}
			m.wg.Add(1)
			go func(c net.Conn) {
				defer m.wg.Done()
				m.proxyTCP(c, spec)
			}(client)
		}
	}()
	return nil
}

func (m *portForwardManager) proxyTCP(client net.Conn, spec portForwardSpec) {
	m.track(client)
	defer func() {
		_ = client.Close()
		m.untrack(client)
	}()
	_ = client.SetDeadline(time.Now().Add(30 * time.Second))

	dialer := net.Dialer{Timeout: 10 * time.Second}
	upstream, err := dialer.Dial("tcp", spec.DestAddr())
	if err != nil {
		m.logger.Verbosef("ForwardTCP dial %s failed: %v", spec.DestAddr(), err)
		return
	}
	m.track(upstream)
	defer func() {
		_ = upstream.Close()
		m.untrack(upstream)
	}()
	_ = client.SetDeadline(time.Time{})
	_ = upstream.SetDeadline(time.Time{})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(upstream, client)
		if tc, ok := upstream.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(client, upstream)
		if tc, ok := client.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
	}()
	wg.Wait()
}

type udpSession struct {
	clientAddr *net.UDPAddr
	upstream   *net.UDPConn
	lastActive time.Time
}

func (m *portForwardManager) startUDPForward(spec portForwardSpec, peerLabel string) error {
	listenAddr, err := net.ResolveUDPAddr("udp", spec.ListenAddr())
	if err != nil {
		return fmt.Errorf("ForwardUDP resolve listen %s: %w", spec.ListenAddr(), err)
	}
	pc, err := net.ListenUDP("udp", listenAddr)
	if err != nil {
		return fmt.Errorf("ForwardUDP listen %s: %w", spec.ListenAddr(), err)
	}
	destAddr, err := net.ResolveUDPAddr("udp", spec.DestAddr())
	if err != nil {
		_ = pc.Close()
		return fmt.Errorf("ForwardUDP resolve dest %s: %w", spec.DestAddr(), err)
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		_ = pc.Close()
		return fmt.Errorf("forward manager closed")
	}
	m.closers = append(m.closers, pc)
	m.mu.Unlock()

	m.logger.Verbosef("ForwardUDP %s (peer %s)", spec.String(), peerLabel)
	fmt.Fprintf(os.Stderr, "wireguard-go: ForwardUDP %s (peer %s)\n", spec.ListenAddr()+" -> "+spec.DestAddr(), peerLabel)

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.serveUDP(pc, destAddr, peerLabel)
	}()
	return nil
}

func (m *portForwardManager) serveUDP(pc *net.UDPConn, destAddr *net.UDPAddr, peerLabel string) {
	sessions := make(map[string]*udpSession)
	var sessMu sync.Mutex

	stopGC := make(chan struct{})
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stopGC:
				return
			case <-t.C:
				now := time.Now()
				sessMu.Lock()
				for k, s := range sessions {
					if now.Sub(s.lastActive) > udpSessionIdle {
						_ = s.upstream.Close()
						delete(sessions, k)
					}
				}
				sessMu.Unlock()
			}
		}
	}()
	defer close(stopGC)

	buf := make([]byte, udpMaxPacket)
	for {
		n, clientAddr, err := pc.ReadFromUDP(buf)
		if err != nil {
			sessMu.Lock()
			for _, s := range sessions {
				_ = s.upstream.Close()
			}
			clear(sessions)
			sessMu.Unlock()
			return
		}
		key := clientAddr.String()
		payload := make([]byte, n)
		copy(payload, buf[:n])

		sessMu.Lock()
		sess, ok := sessions[key]
		if !ok {
			up, dialErr := net.DialUDP("udp", nil, destAddr)
			if dialErr != nil {
				sessMu.Unlock()
				m.logger.Verbosef("ForwardUDP dial %s failed: %v", destAddr, dialErr)
				continue
			}
			sess = &udpSession{
				clientAddr: clientAddr,
				upstream:   up,
				lastActive: time.Now(),
			}
			sessions[key] = sess
			m.wg.Add(1)
			go func(s *udpSession, k string) {
				defer m.wg.Done()
				rbuf := make([]byte, udpMaxPacket)
				for {
					_ = s.upstream.SetReadDeadline(time.Now().Add(udpSessionIdle + time.Second))
					rn, err := s.upstream.Read(rbuf)
					if err != nil {
						sessMu.Lock()
						if cur, exists := sessions[k]; exists && cur == s {
							delete(sessions, k)
						}
						sessMu.Unlock()
						_ = s.upstream.Close()
						return
					}
					sessMu.Lock()
					s.lastActive = time.Now()
					sessMu.Unlock()
					if _, err := pc.WriteToUDP(rbuf[:rn], s.clientAddr); err != nil {
						return
					}
				}
			}(sess, key)
			m.logger.Verbosef("ForwardUDP new session %s -> %s (peer %s)", key, destAddr, peerLabel)
		}
		sess.lastActive = time.Now()
		up := sess.upstream
		sessMu.Unlock()

		if _, err := up.Write(payload); err != nil {
			sessMu.Lock()
			if cur, exists := sessions[key]; exists && cur.upstream == up {
				_ = up.Close()
				delete(sessions, key)
			}
			sessMu.Unlock()
		}
	}
}

func (m *portForwardManager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	closers := m.closers
	m.closers = nil
	active := m.active
	m.active = make(map[net.Conn]struct{})
	downs := m.onDown
	m.onDown = nil
	m.mu.Unlock()

	for c := range active {
		_ = c.SetDeadline(time.Now())
		_ = c.Close()
	}
	for _, c := range closers {
		_ = c.Close()
	}

	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(forwardCloseWait):
		if m.logger != nil {
			m.logger.Verbosef("Port forward shutdown timed out after %s", forwardCloseWait)
		}
		fmt.Fprintf(os.Stderr, "wireguard-go: port forward shutdown timed out after %s\n", forwardCloseWait)
	}

	for i := len(downs) - 1; i >= 0; i-- {
		d := downs[i]
		_ = runPeerHooks(d.cmds, d.env, m.logger, "OnDown("+d.label+")")
	}
}
