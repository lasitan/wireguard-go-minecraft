//go:build linux

package linux

const (
	LegacyDataDir = legacyDataDir
	NewDataDir    = newDataDir
)

func RewriteMasterDataDir(path string) error {
	return rewriteMasterDataDir(path)
}

func UniqueStrings(in []string) []string {
	return uniqueStrings(in)
}
