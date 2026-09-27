//go:build windows

package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"golang.zx2c4.com/wireguard/internal/update"
)

func applyUpdate(ctx context.Context, rel *update.Release, exe string) error {
	name, err := update.BinaryAssetName(rel.Version)
	if err != nil {
		return err
	}
	tmp, err := downloadAsset(ctx, rel, name, filepath.Dir(exe))
	if err != nil {
		return err
	}

	m, err := mgr.Connect()
	if err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("connect service manager: %w", err)
	}
	defer m.Disconnect()

	running := stopOwnServices(m)
	if err := update.ReplaceExecutable(exe, tmp); err != nil {
		_ = os.Remove(tmp)
		startServices(m, running)
		return err
	}
	fmt.Fprintf(os.Stderr, "wireguard-go: 已替换 %s\n", exe)
	startServices(m, running)
	return nil
}

// stopOwnServices stops every running wireguard-go-* service and returns their names.
func stopOwnServices(m *mgr.Mgr) []string {
	names, err := m.ListServices()
	if err != nil {
		return nil
	}
	var stopped []string
	for _, name := range names {
		if !strings.HasPrefix(name, windowsServicePrefix) {
			continue
		}
		s, err := m.OpenService(name)
		if err != nil {
			continue
		}
		st, err := s.Query()
		if err != nil || st.State != svc.Running {
			s.Close()
			continue
		}
		_, _ = s.Control(svc.Stop)
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			if st, err := s.Query(); err != nil || st.State == svc.Stopped {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		s.Close()
		stopped = append(stopped, name)
		fmt.Fprintf(os.Stderr, "wireguard-go: 已停止 %s\n", name)
	}
	return stopped
}

func startServices(m *mgr.Mgr, names []string) {
	for _, name := range names {
		if err := startWindowsService(m, name); err != nil {
			fmt.Fprintf(os.Stderr, "wireguard-go: %v\n", err)
		}
	}
}
