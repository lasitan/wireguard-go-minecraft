package update

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"time"
)

const updateUsage = "Usage: wireguard-go update [--check] [--force]"

// ApplyFunc installs rel over the running executable exe (platform specific).
type ApplyFunc func(ctx context.Context, rel *Release, exe string) error

// RunCommand implements `wireguard-go update`: check GitHub Releases and,
// unless --check, elevate and install the newer build via apply.
func RunCommand(args []string, ensureElevated func() error, apply ApplyFunc) error {
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
	rel, err := Latest(lookup)
	if err != nil {
		return fmt.Errorf("check %s: %w", ReleasesURL, err)
	}

	cur := Current()
	fmt.Fprintf(os.Stderr, "wireguard-go: 当前版本 %s，最新版本 %s（%s）\n", orUnknown(cur), rel.Version, rel.URL)
	newer := Newer(rel.Version, cur)
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
	if err := apply(ctx, rel, exe); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wireguard-go: 已升级到 %s\n", rel.Version)
	return nil
}

// DownloadAsset fetches the named release asset into dir, logging the URL.
func DownloadAsset(ctx context.Context, rel *Release, name, dir string) (string, error) {
	a, ok := rel.Asset(name)
	if !ok {
		return "", fmt.Errorf("release %s has no asset %s", rel.Tag, name)
	}
	fmt.Fprintf(os.Stderr, "wireguard-go: 下载 %s\n", Proxied(a.URL))
	return Download(ctx, a, dir, os.Stderr)
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
