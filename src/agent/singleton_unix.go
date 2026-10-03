//go:build unix

package agent

import "golang.org/x/sys/unix"

// LockAgentInstance makes sure only one agent process uses this config
// directory (lc0 and wg0 sharing lasitan-cluster-agent.json would otherwise
// kick each other's Master websocket forever).
func LockAgentInstance() (unlock func(), err error) {
	f, err := openLockFile()
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, lockFail(err)
	}
	return func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		_ = f.Close()
	}, nil
}
