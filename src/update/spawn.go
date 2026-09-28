package update

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// spawnCooldown stops repeated clicks from racing several updaters.
const spawnCooldown = 3 * time.Minute

var (
	spawnMu   sync.Mutex
	spawnedAt time.Time
)

// ErrUpdateInProgress is returned while a recently spawned updater may still run.
var ErrUpdateInProgress = errors.New("升级已在进行中，请稍候")

// SpawnOptions tunes a detached updater run.
type SpawnOptions struct {
	Force bool
	// Proxy prefixes GitHub download URLs (see ProxyEnv).
	Proxy string
	// Release is a manifest from Release.Manifest; when set the updater skips
	// the api.github.com lookup.
	Release []byte
}

// SpawnDetached starts `wireguard-go update` in a process that outlives this
// one: the updater stops and restarts our own service, which would otherwise
// kill it midway. It returns a human-readable note on where to follow it.
func SpawnDetached(opts SpawnOptions) (string, error) {
	spawnMu.Lock()
	defer spawnMu.Unlock()
	if !spawnedAt.IsZero() && time.Since(spawnedAt) < spawnCooldown {
		return "", ErrUpdateInProgress
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	args := []string{"update"}
	if opts.Force {
		args = append(args, "--force")
	}
	if p := strings.TrimSpace(opts.Proxy); p != "" {
		args = append(args, "--proxy="+p)
	}
	if len(opts.Release) > 0 {
		if path, err := writeManifest(opts.Release); err == nil {
			args = append(args, "--release="+path)
		}
	}
	note, err := spawnDetached(exe, args)
	if err != nil {
		return "", err
	}
	spawnedAt = time.Now()
	if p := strings.TrimSpace(opts.Proxy); p != "" {
		note += "（经代理 " + p + "）"
	}
	if !ServiceManaged() {
		note += "；当前为前台运行，升级完成后需手动重启进程"
	}
	return note, nil
}

func writeManifest(b []byte) (string, error) {
	f, err := os.CreateTemp("", "wg-mc-release-*.json")
	if err != nil {
		return "", err
	}
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
