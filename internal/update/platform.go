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
}

var debArch = map[string]string{
	"amd64": "amd64",
	"386":   "i386",
	"arm64": "arm64",
	"arm":   "armhf",
}

// BinaryAssetName is the standalone binary for this platform in a release.
func BinaryAssetName(version string) (string, error) {
	key := runtime.GOOS + "/" + runtime.GOARCH
	f, ok := friendly[key]
	if !ok {
		return "", fmt.Errorf("no prebuilt binary for %s", key)
	}
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	return fmt.Sprintf("wireguard-mc-%s-%s%s", f, Normalize(version), ext), nil
}

// DebAssetName is the Debian package for this architecture in a release.
func DebAssetName(version string) (string, error) {
	a, ok := debArch[runtime.GOARCH]
	if !ok || runtime.GOOS != "linux" {
		return "", fmt.Errorf("no .deb for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return fmt.Sprintf("wireguard-mc_%s-1_%s.deb", Normalize(version), a), nil
}
