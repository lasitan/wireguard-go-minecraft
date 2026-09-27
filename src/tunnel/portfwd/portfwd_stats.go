package tunnel

import (
	"io"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
)

// ForwardCounter counts bytes through one forward. Rx = from the connecting
// client toward the destination, Tx = back to the client.
type ForwardCounter struct {
	Protocol string
	Listen   string
	rx, tx   atomic.Uint64
}

func (c *ForwardCounter) Bytes() (rx, tx uint64) { return c.rx.Load(), c.tx.Load() }

// forwardCounters outlives PortForwardManager instances so counters stay
// monotonic across config reloads.
var forwardCounters sync.Map // "proto listen" -> *ForwardCounter

func forwardListenLabel(spec PortForwardSpec) string {
	if spec.ListenHost == "" || spec.ListenHost == "0.0.0.0" {
		return strconv.Itoa(spec.ListenPort)
	}
	return spec.ListenAddr()
}

func forwardCounterFor(spec PortForwardSpec) *ForwardCounter {
	listen := forwardListenLabel(spec)
	key := spec.Proto + " " + listen
	if v, ok := forwardCounters.Load(key); ok {
		return v.(*ForwardCounter)
	}
	v, _ := forwardCounters.LoadOrStore(key, &ForwardCounter{Protocol: spec.Proto, Listen: listen})
	return v.(*ForwardCounter)
}

// ForwardCounters returns all forward counters seen by this process.
func ForwardCounters() []*ForwardCounter {
	var out []*ForwardCounter
	forwardCounters.Range(func(_, v any) bool {
		out = append(out, v.(*ForwardCounter))
		return true
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].Protocol != out[j].Protocol {
			return out[i].Protocol < out[j].Protocol
		}
		return out[i].Listen < out[j].Listen
	})
	return out
}

type countingWriter struct {
	w io.Writer
	n *atomic.Uint64
}

func (c countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n.Add(uint64(n))
	return n, err
}
