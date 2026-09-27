//go:build linux

package linux

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.zx2c4.com/wireguard/src/update"
)

const dpkgList = "/var/lib/dpkg/info/wireguard-mc.list"

func applyUpdate(ctx context.Context, rel *update.Release, exe string) error {
	if dpkgManaged(exe) {
		name, err := update.DebAssetName(rel.Version)
		if err != nil {
			return err
		}
		deb, err := update.DownloadAsset(ctx, rel, name, os.TempDir())
		if err != nil {
			return err
		}
		defer os.Remove(deb)
		// postinst restarts wireguard-go@* and the master unit.
		cmd := exec.CommandContext(ctx, "dpkg", "-i", deb)
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("dpkg -i: %w", err)
		}
		return nil
	}

	name, err := update.BinaryAssetName(rel.Version)
	if err != nil {
		return err
	}
	tmp, err := update.DownloadAsset(ctx, rel, name, filepath.Dir(exe))
	if err != nil {
		return err
	}
	if err := update.ReplaceExecutable(exe, tmp); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	fmt.Fprintf(os.Stderr, "wireguard-go: 已替换 %s\n", exe)
	restartActiveUnits()
	return nil
}

func dpkgManaged(exe string) bool {
	if exe != "/usr/bin/wireguard-go" {
		return false
	}
	_, err := os.Stat(dpkgList)
	return err == nil
}

func restartActiveUnits() {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return
	}
	out, err := exec.Command("systemctl", "list-units", "--type=service", "--state=active",
		"--no-legend", "--plain", "wireguard-go@*", "wireguard-go-master.service").Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if err := runSystemctl("restart", f[0]); err != nil {
			fmt.Fprintf(os.Stderr, "wireguard-go: %v\n", err)
			continue
		}
		fmt.Fprintf(os.Stderr, "wireguard-go: 已重启 %s\n", f[0])
	}
}
