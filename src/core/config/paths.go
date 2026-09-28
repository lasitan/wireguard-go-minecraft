package config

import (
	"os"
	"path/filepath"
	"runtime"
)

const (
	// DefaultIface is the TUN name used when none is specified.
	DefaultIface = "lc0"

	// ConfDirEnv overrides the config directory (install / portable layouts).
	ConfDirEnv = "LASITAN_CONF_DIR"

	confDirDefaultUnix = "/etc/lasitan-cluster"
	confDirWinLeaf     = "lasitan-cluster"
)

// ConfDir returns the Lasitan-Cluster config directory: LASITAN_CONF_DIR when
// set; on Windows the executable's own directory once it holds a config
// (installer / portable), else %ProgramData%\lasitan-cluster; elsewhere
// /etc/lasitan-cluster.
func ConfDir() string {
	if d := os.Getenv(ConfDirEnv); d != "" {
		return d
	}
	if runtime.GOOS == "windows" {
		if d := ExeDir(); d != "" && HasConfig(d) {
			return d
		}
		if pd := os.Getenv("ProgramData"); pd != "" {
			return filepath.Join(pd, confDirWinLeaf)
		}
		return `C:\ProgramData\lasitan-cluster`
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
