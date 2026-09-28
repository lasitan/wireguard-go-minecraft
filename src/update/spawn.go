package update

import (
	"errors"
	"os"
	"path/filepath"
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

// SpawnDetached starts `wireguard-go update` in a process that outlives this
// one: the updater stops and restarts our own service, which would otherwise
// kill it midway. It returns a human-readable note on where to follow it.
func SpawnDetached(force bool) (string, error) {
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
	if force {
		args = append(args, "--force")
	}
	note, err := spawnDetached(exe, args)
	if err != nil {
		return "", err
	}
	spawnedAt = time.Now()
	if !ServiceManaged() {
		note += "；当前为前台运行，升级完成后需手动重启进程"
	}
	return note, nil
}
