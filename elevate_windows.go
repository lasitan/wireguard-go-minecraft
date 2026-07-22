//go:build windows

/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

package main

import (
	"fmt"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shell32              = windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteExW  = shell32.NewProc("ShellExecuteExW")
)

// SHELLEXECUTEINFOW
type shellExecuteInfoW struct {
	CbSize       uint32
	FMask        uint32
	Hwnd         windows.Handle
	LpVerb       *uint16
	LpFile       *uint16
	LpParameters *uint16
	LpDirectory  *uint16
	NShow        int32
	HInstApp     windows.Handle
	LpIDList     uintptr
	LpClass      *uint16
	HKeyClass    windows.Handle
	DwHotKey     uint32
	HIconOrMon   windows.Handle
	HProcess     windows.Handle
}

const (
	seeMaskNoCloseProcess = 0x00000040
	swHide                = 0
)

// ensureElevated re-launches this process with a UAC "runas" prompt when not
// already elevated, then exits with the elevated child's status.
// If the user is already an admin (or UAC auto-elevates), this is seamless.
func ensureElevated() error {
	elevated, err := currentlyElevated()
	if err != nil {
		return err
	}
	if elevated {
		return nil
	}
	code, err := reexecElevatedWindows()
	if err != nil {
		return fmt.Errorf("need Administrator to manage the system service: %w", err)
	}
	os.Exit(code)
	return nil
}

func currentlyElevated() (bool, error) {
	var token windows.Token
	err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token)
	if err != nil {
		return false, err
	}
	defer token.Close()
	return token.IsElevated(), nil
}

func reexecElevatedWindows() (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 1, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}

	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return 1, err
	}
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return 1, err
	}
	var params *uint16
	if len(os.Args) > 1 {
		quoted := make([]string, 0, len(os.Args)-1)
		for _, a := range os.Args[1:] {
			if strings.ContainsAny(a, " \t\"") {
				quoted = append(quoted, windowsQuoteArg(a))
			} else {
				quoted = append(quoted, a)
			}
		}
		params, err = windows.UTF16PtrFromString(strings.Join(quoted, " "))
		if err != nil {
			return 1, err
		}
	}
	var dir *uint16
	if cwd != "" {
		dir, err = windows.UTF16PtrFromString(cwd)
		if err != nil {
			return 1, err
		}
	}

	fmt.Fprintln(os.Stderr, "wireguard-go: requesting Administrator elevation…")

	sei := shellExecuteInfoW{
		CbSize:       uint32(unsafe.Sizeof(shellExecuteInfoW{})),
		FMask:        seeMaskNoCloseProcess,
		LpVerb:       verb,
		LpFile:       file,
		LpParameters: params,
		LpDirectory:  dir,
		NShow:        swHide,
	}
	r1, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&sei)))
	if r1 == 0 {
		if callErr != nil {
			return 1, callErr
		}
		return 1, fmt.Errorf("ShellExecuteEx failed")
	}
	if sei.HProcess == 0 {
		return 1, fmt.Errorf("elevation canceled or failed")
	}
	defer windows.CloseHandle(sei.HProcess)

	event, err := windows.WaitForSingleObject(sei.HProcess, windows.INFINITE)
	if err != nil {
		return 1, err
	}
	if event != windows.WAIT_OBJECT_0 {
		return 1, fmt.Errorf("wait for elevated process: %#x", event)
	}
	var code uint32
	if err := windows.GetExitCodeProcess(sei.HProcess, &code); err != nil {
		return 1, err
	}
	return int(code), nil
}

func windowsQuoteArg(s string) string {
	if s == "" {
		return `""`
	}
	var b strings.Builder
	b.WriteByte('"')
	nSlash := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			nSlash++
		case '"':
			b.WriteString(strings.Repeat(`\`, nSlash*2+1))
			b.WriteByte('"')
			nSlash = 0
		default:
			if nSlash > 0 {
				b.WriteString(strings.Repeat(`\`, nSlash))
				nSlash = 0
			}
			b.WriteByte(c)
		}
	}
	if nSlash > 0 {
		b.WriteString(strings.Repeat(`\`, nSlash*2))
	}
	b.WriteByte('"')
	return b.String()
}
