package update

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const updateUsage = "Usage: lasitan-cluster update [--check] [--force] [--proxy=URL]"

// ApplyFunc installs rel over the running executable exe (platform specific).
type ApplyFunc func(ctx context.Context, rel *Release, exe string) error

// RunCommand implements `lasitan-cluster update`: check GitHub Releases and,
// unless --check, elevate and install the newer build via apply.
func RunCommand(args []string, ensureElevated func() error, apply ApplyFunc) error {
	check, force := false, false
	manifestPath := ""
	for _, a := range args {
		switch {
		case a == "--check" || a == "-c":
			check = true
		case a == "--force" || a == "-f":
			force = true
		case strings.HasPrefix(a, "--proxy="):
			if p := strings.TrimSpace(strings.TrimPrefix(a, "--proxy=")); p != "" {
				_ = os.Setenv(ProxyEnv, p)
			}
		case strings.HasPrefix(a, "--release="):
			manifestPath = strings.TrimPrefix(a, "--release=")
		default:
			return fmt.Errorf("unknown flag %q\n%s", a, updateUsage)
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	rel := loadManifest(manifestPath)
	if rel == nil {
		lookup, lcancel := context.WithTimeout(ctx, 30*time.Second)
		defer lcancel()
		var err error
		if rel, err = Latest(lookup); err != nil {
			return fmt.Errorf("check %s: %w", ReleasesURL, err)
		}
	}

	cur := Current()
	fmt.Fprintf(os.Stderr, "lasitan-cluster: 当前版本 %s，最新版本 %s（%s）\n", orUnknown(cur), rel.Version, rel.URL)
	newer := Newer(rel.Version, cur)
	if !newer && !force {
		fmt.Fprintln(os.Stderr, "lasitan-cluster: 已是最新版本")
		return nil
	}
	if check {
		if newer {
			fmt.Fprintf(os.Stderr, "lasitan-cluster: 有可用更新，执行 `%s` 升级\n", updateHint())
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
	fmt.Fprintf(os.Stderr, "lasitan-cluster: 已升级到 %s\n", rel.Version)
	return nil
}

// DownloadAsset fetches the named release asset into dir, logging the URL.
func DownloadAsset(ctx context.Context, rel *Release, name, dir string) (string, error) {
	a, ok := rel.Asset(name)
	if !ok {
		return "", fmt.Errorf("release %s has no asset %s", rel.Tag, name)
	}
	fmt.Fprintf(os.Stderr, "lasitan-cluster: 下载 %s\n", Proxied(a.URL))
	return Download(ctx, a, dir, os.Stderr)
}

// loadManifest reads and removes a manifest written by SpawnDetached.
func loadManifest(path string) *Release {
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	_ = os.Remove(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lasitan-cluster: 读取版本清单失败，改为在线查询：%v\n", err)
		return nil
	}
	rel, err := ParseManifest(b)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lasitan-cluster: %v，改为在线查询\n", err)
		return nil
	}
	return rel
}

func updateHint() string {
	if runtime.GOOS == "windows" {
		return "lasitan-cluster update"
	}
	return "sudo lasitan-cluster update"
}

func orUnknown(v string) string {
	if v == "" {
		return "未知"
	}
	return v
}
