//go:build windows && !amd64 && !arm64

package main

// No Wintun is bundled for this architecture; ensureWintunDLL reports it.
var embeddedWintunDLL []byte
