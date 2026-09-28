//go:build windows

package update

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
)

// ServiceManaged reports whether we run under the SCM (restarted by the updater).
func ServiceManaged() bool {
	ok, _ := svc.IsWindowsService()
	return ok
}

func spawnDetached(exe string, args []string) (string, error) {
	logPath := filepath.Join(filepath.Dir(exe), "wireguard-go-update.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", logPath, err)
	}
	defer logFile.Close()

	start := func(flags uint32) error {
		cmd := exec.Command(exe, args...)
		cmd.Dir = filepath.Dir(exe)
		cmd.Stdout, cmd.Stderr = logFile, logFile
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags, HideWindow: true}
		if err := cmd.Start(); err != nil {
			return err
		}
		return cmd.Process.Release()
	}
	base := uint32(windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP)
	// Leave the service's job object when allowed, so stopping it cannot take the updater down.
	if err := start(base | windows.CREATE_BREAKAWAY_FROM_JOB); err != nil {
		if err := start(base); err != nil {
			return "", fmt.Errorf("start updater: %w", err)
		}
	}
	return "已启动升级进程，日志：" + logPath, nil
}
