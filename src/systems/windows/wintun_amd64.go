//go:build windows && amd64

package main

import _ "embed"

//go:embed third_party/wintun/amd64/wintun.dll
var embeddedWintunDLL []byte
