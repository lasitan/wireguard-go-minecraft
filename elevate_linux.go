//go:build !windows

/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// ensureElevated re-execs via sudo -n / pkexec / sudo when not root.
// On success of re-exec it exits the current process with the child's status
// (never returns). Cached sudo credentials make this silent for the user.
func ensureElevated() error {
	if os.Geteuid() == 0 {
		return nil
	}
	code, err := reexecElevatedUnix()
	if err != nil {
		return fmt.Errorf("need root to manage the system service: %w\ntry: sudo %s %s",
			err, os.Args[0], strings.Join(os.Args[1:], " "))
	}
	os.Exit(code)
	return nil
}

func reexecElevatedUnix() (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 1, err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	args := append([]string{}, os.Args[1:]...)

	// Prefer passwordless sudo when credentials are already cached (truly seamless).
	if sudo, err := exec.LookPath("sudo"); err == nil {
		if exec.Command(sudo, "-n", "true").Run() == nil {
			return runElevatedWait(sudo, append([]string{"-n", exe}, args...)...)
		}
	}

	// Graphical polkit prompt when available (desktop; one click / password).
	if pkexec, err := exec.LookPath("pkexec"); err == nil {
		code, err := runElevatedWait(pkexec, append([]string{exe}, args...)...)
		// If pkexec itself is missing/broken, try sudo; otherwise propagate.
		if err != nil && isCommandNotFound(err) {
			// fall through
		} else {
			return code, err
		}
	}

	if sudo, err := exec.LookPath("sudo"); err == nil {
		fmt.Fprintln(os.Stderr, "wireguard-go: elevating with sudo…")
		return runElevatedWait(sudo, append([]string{exe}, args...)...)
	}

	return 1, fmt.Errorf("neither sudo nor pkexec found")
}

func isCommandNotFound(err error) bool {
	if err == nil {
		return false
	}
	if ee, ok := err.(*exec.Error); ok && ee.Err == exec.ErrNotFound {
		return true
	}
	return false
}

func runElevatedWait(name string, arg ...string) (int, error) {
	cmd := exec.Command(name, arg...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	if ee, ok := err.(*exec.ExitError); ok {
		if status, ok := ee.Sys().(syscall.WaitStatus); ok {
			return status.ExitStatus(), nil
		}
		return 1, nil
	}
	return 1, err
}
