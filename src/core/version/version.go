// Package version exposes the release version embedded from version.txt,
// the single source of truth that CI also reads to tag releases.
package version

import (
	_ "embed"
	"strings"
)

//go:embed version.txt
var versionFile string

var Version = strings.TrimSpace(versionFile)
