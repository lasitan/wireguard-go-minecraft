/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

package main

import (
	"fmt"
	"os"

	"golang.zx2c4.com/wireguard/src/commands/keygen"
	"golang.zx2c4.com/wireguard/src/core/version"
	"golang.zx2c4.com/wireguard/src/master"
	"golang.zx2c4.com/wireguard/src/update"
)

func main() {
	update.SetCurrent(version.Version)
	update.CleanupOld()
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Print(versionText())
		return
	}
	if keygen.HandleKeyCommand() {
		return
	}
	if master.HandleCommand() {
		return
	}
	platformMain()
}
