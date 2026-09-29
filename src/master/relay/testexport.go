package relay

import (
	"golang.zx2c4.com/wireguard/src/core"
	"golang.zx2c4.com/wireguard/src/master/store"
)

func NewHairpinTUN() *hairpinTUN {
	return newHairpinTUN()
}

func B64hex(k string) (string, error) {
	return b64hex(k)
}

func (s *Service) ApplyLocked(rc store.RelayConfig, want []core.DesiredPeer) error {
	return s.applyLocked(rc, want)
}

func (s *Service) CloseLocked() {
	s.closeLocked()
}
