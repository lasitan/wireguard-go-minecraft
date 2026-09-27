// Package role manages the host-wide master/agent role lock file.
package role

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.zx2c4.com/wireguard/src/core/config"
)

func lockPath() string {
	return filepath.Join(config.ConfDir(), config.RoleLockName)
}

func Read() (string, error) {
	data, err := os.ReadFile(lockPath())
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func Write(role string) error {
	if err := os.MkdirAll(config.ConfDir(), 0755); err != nil {
		return err
	}
	return os.WriteFile(lockPath(), []byte(role+"\n"), 0644)
}

func Clear() error {
	err := os.Remove(lockPath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// AssertCanInstall rejects switching between master and agent on one host.
func AssertCanInstall(want string) error {
	cur, err := Read()
	if err != nil {
		return err
	}
	if cur == "" {
		return nil
	}
	if cur == want {
		return nil // reinstall same role
	}
	if cur == config.RoleMaster {
		return fmt.Errorf("this host is locked as master; uninstall master first (cannot switch to %s)", want)
	}
	if want == config.RoleMaster {
		return fmt.Errorf("this host is locked as %s; uninstall agent first before install master", cur)
	}
	return nil
}
