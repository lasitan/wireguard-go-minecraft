//go:build unix

package update

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// ServiceManaged reports whether systemd started us (the updater restarts units).
func ServiceManaged() bool {
	return os.Getenv("INVOCATION_ID") != ""
}

func spawnDetached(exe string, args []string) (string, error) {
	if os.Geteuid() != 0 {
		return "", errors.New("升级需要 root 权限运行 lasitan-cluster")
	}
	// A plain child stays in our unit's cgroup and dies when the updater
	// restarts that unit, so run it as its own transient unit.
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		if systemdRun, err := exec.LookPath("systemd-run"); err == nil {
			unit := "lasitan-cluster-update-" + strconv.FormatInt(time.Now().Unix(), 10)
			cmd := exec.Command(systemdRun, append([]string{"--unit=" + unit, "--collect", "--quiet", exe}, args...)...)
			if out, err := cmd.CombinedOutput(); err != nil {
				return "", fmt.Errorf("systemd-run: %v (%s)", err, out)
			}
			return "已启动升级进程，日志：journalctl -u " + unit, nil
		}
	}

	logPath := filepath.Join(os.TempDir(), "lasitan-cluster-update.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", logPath, err)
	}
	defer logFile.Close()
	cmd := exec.Command(exe, args...)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start updater: %w", err)
	}
	_ = cmd.Process.Release()
	return "已启动升级进程，日志：" + logPath, nil
}
