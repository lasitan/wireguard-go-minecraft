//go:build !windows

package update

import "runtime"

func hostArch() string { return runtime.GOARCH }
