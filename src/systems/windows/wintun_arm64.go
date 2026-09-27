//go:build windows && arm64

package windows

import _ "embed"

//go:embed wintun/arm64/wintun.dll
var embeddedWintunDLL []byte
