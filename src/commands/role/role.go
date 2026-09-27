package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.zx2c4.com/wireguard/meshcfg"
)

func roleLockPath() string {
	return filepath.Join(meshcfg.ConfDir(), meshcfg.RoleLockName)
}

func readRoleLock() (string, error) {
	data, err := os.ReadFile(roleLockPath())
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func writeRoleLock(role string) error {
	if err := os.MkdirAll(meshcfg.ConfDir(), 0755); err != nil {
		return err
	}
	return os.WriteFile(roleLockPath(), []byte(role+"\n"), 0644)
}

func clearRoleLock() error {
	err := os.Remove(roleLockPath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func assertCanInstall(want string) error {
	cur, err := readRoleLock()
	if err != nil {
		return err
	}
	if cur == "" {
		return nil
	}
	if cur == want {
		return nil // reinstall same role
	}
	if cur == meshcfg.RoleMaster {
		return fmt.Errorf("this host is locked as master; uninstall master first (cannot switch to %s)", want)
	}
	if want == meshcfg.RoleMaster {
		return fmt.Errorf("this host is locked as %s; uninstall agent first before install master", cur)
	}
	return nil
}
