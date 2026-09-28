package wire

import "fmt"

// maxReleaseBytes bounds the release manifest carried by UpdateCmd.
const maxReleaseBytes = 256 << 10

// UpdateCmd asks an agent to install the latest release.
//
// Proxy and Release are optional trailing fields; older agents ignore them.
// Proxy prefixes GitHub download URLs (e.g. https://ghfast.top/). Release is
// the JSON manifest the Master already fetched, so the agent can skip
// api.github.com, which is often unreachable where a proxy is needed.
type UpdateCmd struct {
	Force   bool
	Proxy   string
	Release []byte
}

func (m UpdateCmd) Marshal() []byte {
	var w writer
	if m.Force {
		w.u8(1)
	} else {
		w.u8(0)
	}
	rel := m.Release
	if len(rel) > maxReleaseBytes {
		rel = nil
	}
	if m.Proxy == "" && len(rel) == 0 {
		return w.b
	}
	w.str(m.Proxy)
	w.u32(uint32(len(rel)))
	w.b = append(w.b, rel...)
	return w.b
}

func (m *UpdateCmd) Unmarshal(b []byte) error {
	r := reader{b: b}
	m.Force = r.u8() == 1
	if r.err != nil || len(r.b) == 0 {
		return r.err
	}
	m.Proxy = r.str()
	n := int(r.u32())
	if n > maxReleaseBytes && r.err == nil {
		r.err = fmt.Errorf("wire: release manifest too large (%d)", n)
	}
	if rel := r.bytes(n); len(rel) > 0 {
		m.Release = append([]byte(nil), rel...)
	}
	return r.err
}

// UpdateAck reports whether the agent started its updater. Completion shows up
// as a new version in the Hello of the next connection.
type UpdateAck struct {
	OK      bool
	Message string
}

func (m UpdateAck) Marshal() []byte {
	var w writer
	if m.OK {
		w.u8(1)
	} else {
		w.u8(0)
	}
	w.str(m.Message)
	return w.b
}

func (m *UpdateAck) Unmarshal(b []byte) error {
	r := reader{b: b}
	m.OK = r.u8() == 1
	m.Message = r.str()
	return r.err
}
