package config

import (
	"os"
	"path/filepath"
	"runtime"
)

const confDirDefaultUnix = "/etc/wireguard"

// ConfDir returns the wireguard config directory: WG_CONF_DIR when set; on
// Windows the executable's own directory once it holds a config (installer
// layout, portable use), else %ProgramData%\wireguard; /etc/wireguard elsewhere.
func ConfDir() string {
	if d := os.Getenv("WG_CONF_DIR"); d != "" {
		return d
	}
	if runtime.GOOS == "windows" {
		if d := ExeDir(); d != "" && HasConfig(d) {
			return d
		}
		if pd := os.Getenv("ProgramData"); pd != "" {
			return filepath.Join(pd, "wireguard")
		}
		return `C:\ProgramData\wireguard`
	}
	return confDirDefaultUnix
}

// ExeDir is the directory of the running executable (symlinks resolved).
func ExeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return filepath.Dir(exe)
}

// HasConfig reports whether dir holds an agent / master config or a role lock.
func HasConfig(dir string) bool {
	for _, name := range []string{AgentFileName, MasterFileName, RoleLockName} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}
