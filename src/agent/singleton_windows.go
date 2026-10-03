//go:build windows

package agent

import "golang.org/x/sys/windows"

func LockAgentInstance() (unlock func(), err error) {
	f, err := openLockFile()
	if err != nil {
		return nil, err
	}
	overlapped := new(windows.Overlapped)
	// Lock 1 byte exclusive, non-blocking (LOCKFILE_FAIL_IMMEDIATELY | EXCLUSIVE).
	if err := windows.LockFileEx(windows.Handle(f.Fd()), 0x0002|0x0001, 0, 1, 0, overlapped); err != nil {
		_ = f.Close()
		return nil, lockFail(err)
	}
	return func() {
		_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, overlapped)
		_ = f.Close()
	}, nil
}
