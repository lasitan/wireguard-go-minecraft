package service

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"time"

	"golang.zx2c4.com/wireguard/internal/update"
)

const updateUsage = "Usage: wireguard-go update [--check] [--force]"

// runUpdate implements `wireguard-go update`: check GitHub Releases and,
// unless --check, install the newer build and restart running services.
func runUpdate(args []string) error {
	check, force := false, false
	for _, a := range args {
		switch a {
		case "--check", "-c":
			check = true
		case "--force", "-f":
			force = true
		default:
			return fmt.Errorf("unknown flag %q\n%s", a, updateUsage)
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	lookup, lcancel := context.WithTimeout(ctx, 30*time.Second)
	defer lcancel()
	rel, err := update.Latest(lookup)
	if err != nil {
		return fmt.Errorf("check %s: %w", update.ReleasesURL, err)
	}

	cur := update.Current()
	fmt.Fprintf(os.Stderr, "wireguard-go: 当前版本 %s，最新版本 %s（%s）\n", orUnknown(cur), rel.Version, rel.URL)
	newer := update.Newer(rel.Version, cur)
	if !newer && !force {
		fmt.Fprintln(os.Stderr, "wireguard-go: 已是最新版本")
		return nil
	}
	if check {
		if newer {
			fmt.Fprintf(os.Stderr, "wireguard-go: 有可用更新，执行 `%s` 升级\n", updateHint())
		}
		return nil
	}

	if err := ensureElevated(); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	if err := applyUpdate(ctx, rel, exe); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wireguard-go: 已升级到 %s\n", rel.Version)
	return nil
}

func downloadAsset(ctx context.Context, rel *update.Release, name, dir string) (string, error) {
	a, ok := rel.Asset(name)
	if !ok {
		return "", fmt.Errorf("release %s has no asset %s", rel.Tag, name)
	}
	fmt.Fprintf(os.Stderr, "wireguard-go: 下载 %s\n", update.Proxied(a.URL))
	return update.Download(ctx, a, dir, os.Stderr)
}

func updateHint() string {
	if runtime.GOOS == "windows" {
		return "wireguard-go update"
	}
	return "sudo wireguard-go update"
}

func orUnknown(v string) string {
	if v == "" {
		return "未知"
	}
	return v
}
