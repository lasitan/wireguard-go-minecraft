//go:build windows && amd64

package windows

import _ "embed"

//go:embed wintun/amd64/wintun.dll
var embeddedWintunDLL []byte
