// Package version exposes the release version. The single source of truth is
// .github/build/version.txt (CI tags releases from it); builds inject it with
//
//	-ldflags "-X golang.zx2c4.com/wireguard/src/core/version.Version=$(cat .github/build/version.txt)"
package version

var Version = "dev"
