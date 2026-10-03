package agent

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.zx2c4.com/wireguard/src/core/config"
)

func agentLockPath() string {
	return filepath.Join(config.ConfDir(), "agent.lock")
}

func lockFail(err error) error {
	return fmt.Errorf("another lasitan-cluster agent already holds %s: %w", agentLockPath(), err)
}

func openLockFile() (*os.File, error) {
	if err := os.MkdirAll(config.ConfDir(), 0755); err != nil {
		return nil, err
	}
	return os.OpenFile(agentLockPath(), os.O_CREATE|os.O_RDWR, 0600)
}
