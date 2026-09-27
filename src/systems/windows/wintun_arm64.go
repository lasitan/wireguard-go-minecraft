//go:build windows && arm64

package main

import _ "embed"

//go:embed third_party/wintun/arm64/wintun.dll
var embeddedWintunDLL []byte
