package conn

// NewUDPBind returns a native WireGuard UDP bind (RIO when available).
func NewUDPBind() Bind {
	return NewWinRingBind()
}
