//go:build unix

package portfwd

import "syscall"

func raiseNOFILE(need int) {
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		return
	}
	want := uint64(need)
	if lim.Cur >= want {
		return
	}
	lim.Cur = want
	if lim.Max < want {
		lim.Max = want
	}
	_ = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &lim)
}
