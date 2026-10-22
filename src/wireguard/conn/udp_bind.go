//go:build !windows

package conn

// NewUDPBind returns a native WireGuard UDP bind (std sockets).
func NewUDPBind() Bind {
	return NewStdNetBind()
}
