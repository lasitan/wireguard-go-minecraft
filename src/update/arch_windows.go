package update

import (
	"runtime"

	"golang.org/x/sys/windows"
)

const (
	imageFileMachineAMD64 = 0x8664
	imageFileMachineARM64 = 0xAA64
)

func hostArch() string {
	var process, native uint16
	if err := windows.IsWow64Process2(windows.CurrentProcess(), &process, &native); err != nil {
		return runtime.GOARCH
	}
	switch native {
	case imageFileMachineARM64:
		return "arm64"
	case imageFileMachineAMD64:
		return "amd64"
	}
	return runtime.GOARCH
}
