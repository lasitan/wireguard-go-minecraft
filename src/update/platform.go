package update

import (
	"fmt"
	"runtime"
)

// Friendly names mirror .github/build/friendly-filenames.json.
var friendly = map[string]string{
	"linux/amd64":   "linux-amd64",
	"linux/386":     "linux-386",
	"linux/arm64":   "linux-arm64",
	"linux/arm":     "linux-armv7",
	"windows/amd64": "windows10-amd64",
	"windows/arm64": "windows10-arm64",
}

var debArch = map[string]string{
	"amd64": "amd64",
	"386":   "i386",
	"arm64": "arm64",
	"arm":   "armhf",
}

// BinaryAssetName is the standalone binary for this host in a release. It
// follows the native CPU, so an emulated x64 build on ARM64 Windows upgrades
// to the arm64 build.
func BinaryAssetName(version string) (string, error) {
	key := runtime.GOOS + "/" + hostArch()
	f, ok := friendly[key]
	if !ok {
		return "", fmt.Errorf("no prebuilt binary for %s", key)
	}
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	return fmt.Sprintf("lasitan-cluster-%s-%s%s", f, Normalize(version), ext), nil
}

// DebAssetName is the Debian package for this architecture in a release.
func DebAssetName(version string) (string, error) {
	a, ok := debArch[runtime.GOARCH]
	if !ok || runtime.GOOS != "linux" {
		return "", fmt.Errorf("no .deb for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return fmt.Sprintf("lasitan-cluster_%s-1_%s.deb", Normalize(version), a), nil
}
