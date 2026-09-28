package wire

// UpdateCmd asks an agent to install the latest release.
type UpdateCmd struct {
	Force bool
}

func (m UpdateCmd) Marshal() []byte {
	var w writer
	if m.Force {
		w.u8(1)
	} else {
		w.u8(0)
	}
	return w.b
}

func (m *UpdateCmd) Unmarshal(b []byte) error {
	r := reader{b: b}
	m.Force = r.u8() == 1
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
