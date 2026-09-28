//go:build windows

package windows

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"

	"golang.zx2c4.com/wireguard/src/commands/role"
	"golang.zx2c4.com/wireguard/src/core/config"
)

var procGetConsoleProcessList = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleProcessList")

// autoRun handles a bare `lasitan-cluster.exe` (double-click): it picks master or
// agent from the config next to the exe, elevates for the tunnel, and runs the
// real command in a child so the console stays open when it fails.
func autoRun() {
	ownConsole := ownsConsole()
	dir := config.ConfDir()
	args, service, err := autoArgs(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lasitan-cluster: %v\n", err)
		fmt.Fprintf(os.Stderr, "lasitan-cluster: 把 %s 或 %s 放到 %s 后再双击运行（或重新运行安装程序选择模式）\n",
			config.AgentFileName, config.MasterFileName, config.ExeDir())
		pauseIf(ownConsole)
		os.Exit(1)
	}
	if serviceRunning(service) {
		fmt.Fprintf(os.Stderr, "lasitan-cluster: 系统服务 %s 已在运行，无需重复启动\n", service)
		pauseIf(ownConsole)
		return
	}
	if args[0] != "master" {
		if elevated, _ := currentlyElevated(); !elevated {
			if _, err := reexecElevated(swShowNormal, false); err != nil {
				fmt.Fprintf(os.Stderr, "lasitan-cluster: 运行隧道需要管理员权限：%v\n", err)
				pauseIf(ownConsole)
				os.Exit(1)
			}
			return
		}
	}

	fmt.Fprintf(os.Stderr, "lasitan-cluster: 使用 %s 中的配置启动：lasitan-cluster %s\n", dir, strings.Join(args, " "))
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "lasitan-cluster: %v\n", err)
		pauseIf(ownConsole)
		os.Exit(1)
	}
	// Ctrl+C reaches every process on the console; the child shuts down cleanly.
	signal.Ignore(os.Interrupt)
	cmd := exec.Command(exe, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(), "LASITAN_CONF_DIR="+dir)
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "lasitan-cluster: 已退出：%v\n", err)
		pauseIf(ownConsole)
		os.Exit(1)
	}
}

// autoArgs maps the config found in dir to a command line and its service name.
func autoArgs(dir string) (args []string, service string, err error) {
	has := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	master, agent := has(config.MasterFileName), has(config.AgentFileName)
	if master && agent {
		switch lock, _ := role.Read(); lock {
		case config.RoleMaster:
			agent = false
		case config.RoleAgent:
			master = false
		default:
			return nil, "", fmt.Errorf("%s 中同时存在 %s 和 %s，请只保留一个", dir, config.MasterFileName, config.AgentFileName)
		}
	}
	switch {
	case master:
		return []string{"master"}, "lasitan-cluster-master", nil
	case agent:
		iface := config.AgentBootstrap{}.IfaceName()
		return []string{"-f", iface}, WindowsServiceName(iface), nil
	}
	return nil, "", errors.New("没有找到配置文件")
}

func serviceRunning(name string) bool {
	m, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return false
	}
	defer windows.CloseServiceHandle(m)
	n, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return false
	}
	s, err := windows.OpenService(m, n, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return false
	}
	defer windows.CloseServiceHandle(s)
	var st windows.SERVICE_STATUS
	if err := windows.QueryServiceStatus(s, &st); err != nil {
		return false
	}
	return svc.State(st.CurrentState) == svc.Running
}

// ownsConsole reports whether this process is alone on its console, i.e. it
// was started by Explorer / ShellExecute rather than from a terminal.
func ownsConsole() bool {
	var pids [2]uint32
	n, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), 2)
	return n == 1
}

func pauseIf(ownConsole bool) {
	if !ownConsole {
		return
	}
	fmt.Fprint(os.Stderr, "按回车键退出…")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}
