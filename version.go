package main

import (
	_ "embed"
	"strings"
)

//go:embed .github/build/version.txt
var versionFile string

// Version comes from .github/build/version.txt (embedded at build time).
// Debian packaging may override with -X main.Version=...
var Version = strings.TrimSpace(versionFile)
