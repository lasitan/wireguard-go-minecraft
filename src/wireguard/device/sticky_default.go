//go:build !linux

package device

import (
	"golang.zx2c4.com/wireguard/src/wireguard/conn"
	"golang.zx2c4.com/wireguard/src/wireguard/utils/rwcancel"
)

func (device *Device) startRouteListener(_ conn.Bind) (*rwcancel.RWCancel, error) {
	return nil, nil
}
