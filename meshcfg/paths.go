package meshcfg

import (
	"os"
	"path/filepath"
	"runtime"
)

const confDirDefaultUnix = "/etc/wireguard"

// ConfDir returns the wireguard config directory (overridable via WG_CONF_DIR).
func ConfDir() string {
	if d := os.Getenv("WG_CONF_DIR"); d != "" {
		return d
	}
	if runtime.GOOS == "windows" {
		if pd := os.Getenv("ProgramData"); pd != "" {
			return filepath.Join(pd, "wireguard")
		}
		return `C:\ProgramData\wireguard`
	}
	return confDirDefaultUnix
}
