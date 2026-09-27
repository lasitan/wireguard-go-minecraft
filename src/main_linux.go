package main

import "golang.zx2c4.com/wireguard/src/systems/linux"

func versionText() string { return linux.VersionText() }

func platformMain() { linux.Main() }
