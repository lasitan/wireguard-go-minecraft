package main

import "golang.zx2c4.com/wireguard/src/systems/windows"

func versionText() string { return windows.VersionText() }

func platformMain() { windows.Main() }
