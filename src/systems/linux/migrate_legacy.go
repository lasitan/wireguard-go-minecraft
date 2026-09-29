//go:build linux

package linux

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	legacyBinName      = "wireguard-go"
	legacyPkgName      = "wireguard-mc"
	legacyConfDir      = "/etc/wireguard"
	legacyDataDir      = "/var/lib/wireguard-mc"
	legacyRunDir       = "/var/run/wireguard"
	legacyAgentFile    = "wireguard-go-agent.json"
	legacyMasterFile   = "wireguard-go-master.json"
	legacyTransportFile = "wireguard-go-transport.json"
	legacyUnitAgentLib = "/lib/systemd/system/wireguard-go@.service"
	legacyUnitAgentEtc = "/etc/systemd/system/wireguard-go@.service"
	legacyUnitMasterLib = "/lib/systemd/system/wireguard-go-master.service"
	legacyUnitMasterEtc = "/etc/systemd/system/wireguard-go-master.service"
	legacyMasterUnit   = "wireguard-go-master.service"

	newConfDir      = "/etc/lasitan-cluster"
	newDataDir      = "/var/lib/lasitan-cluster"
	newAgentFile    = "lasitan-cluster-agent.json"
	newMasterFile   = "lasitan-cluster-master.json"
	newTransportFile = "lasitan-cluster-transport.json"
	migrateStateFile = ".legacy-migrate.json"
)

var legacyBinCandidates = []string{
	"/usr/bin/wireguard-go",
	"/usr/local/bin/wireguard-go",
	"/usr/sbin/wireguard-go",
	"/usr/local/sbin/wireguard-go",
}

// legacyMigrateState records what was running so we can start the new units
// after the new binary is installed (install.sh mid-flight).
type legacyMigrateState struct {
	Master  bool     `json:"master"`
	Ifaces  []string `json:"ifaces"`
	Pending bool     `json:"pending"`
}

// MigrateLegacy detects leftover wireguard-go / wireguard-mc installs, copies
// configs into /etc/lasitan-cluster and /var/lib/lasitan-cluster, stops and
// removes the old binary + systemd units, then optionally starts the new
// services. Safe when the old PATH entry is already gone but the binary or
// configs remain (interrupted upgrade).
func MigrateLegacy(start bool) error {
	if err := ensureElevated(); err != nil {
		return err
	}

	bins := findLegacyBins()
	units := listLegacyUnits()
	confPresent := dirHasLegacyConfig(legacyConfDir)
	dataPresent := dirExists(legacyDataDir)
	pkgPresent := dpkgInstalled(legacyPkgName)

	if len(bins) == 0 && len(units) == 0 && !confPresent && !dataPresent && !pkgPresent {
		fmt.Fprintln(os.Stderr, "lasitan-cluster: 未检测到旧版 wireguard-go / wireguard-mc 残留")
		if start {
			return startFromMigrateState()
		}
		return nil
	}

	fmt.Fprintln(os.Stderr, "lasitan-cluster: 检测到旧版 wireguard-go，开始无损迁移…")
	for _, b := range bins {
		fmt.Fprintf(os.Stderr, "  · 二进制 %s\n", b)
	}
	for _, u := range units {
		fmt.Fprintf(os.Stderr, "  · systemd %s\n", u)
	}
	if confPresent {
		fmt.Fprintf(os.Stderr, "  · 配置目录 %s\n", legacyConfDir)
	}
	if dataPresent {
		fmt.Fprintf(os.Stderr, "  · 数据目录 %s\n", legacyDataDir)
	}
	if pkgPresent {
		fmt.Fprintf(os.Stderr, "  · dpkg 包 %s\n", legacyPkgName)
	}

	st := legacyMigrateState{Pending: true}
	agentIfaces := collectLegacyIfaces(units)
	st.Master = unitActiveOrEnabled(legacyMasterUnit) || roleIsMaster(legacyConfDir)
	if !st.Master && len(agentIfaces) == 0 && fileExists(filepath.Join(legacyConfDir, legacyMasterFile)) {
		// Units already gone mid-upgrade; master json alone implies Master role.
		st.Master = true
	}
	if st.Master {
		st.Ifaces = uniqueStrings(agentIfaces)
	} else {
		st.Ifaces = uniqueStrings(append(agentIfaces, confIfaces(legacyConfDir)...))
	}

	// 1) Stop old services / processes first so configs are quiescent.
	stopLegacy(units)

	// 2) Preserve configs + data into the new project dirs (never overwrite).
	if err := os.MkdirAll(newConfDir, 0755); err != nil {
		return err
	}
	if err := migrateConfigs(); err != nil {
		return err
	}
	if err := migrateDataDir(); err != nil {
		return err
	}
	_ = saveMigrateState(st)

	// 3) Remove old binary, units, package leftovers.
	removeLegacyUnits()
	removeLegacyBins(bins)
	_ = exec.Command("pkill", "-x", legacyBinName).Run()
	removeLegacyPackage()
	_ = runSystemctl("daemon-reload")

	fmt.Fprintln(os.Stderr, "lasitan-cluster: 旧版已停止并清理；配置已保存到新目录")

	if !start {
		fmt.Fprintln(os.Stderr, "lasitan-cluster: 已跳过启动（--no-start）；安装新二进制后执行 lasitan-cluster migrate --start")
		return nil
	}
	return startMigratedServices(st)
}

func startFromMigrateState() error {
	st, err := loadMigrateState()
	if err != nil || st == nil || !st.Pending {
		return nil
	}
	return startMigratedServices(*st)
}

func startMigratedServices(st legacyMigrateState) error {
	exe, err := resolveInstalledLasitan()
	if err != nil {
		fmt.Fprintf(os.Stderr, "lasitan-cluster: 新二进制尚未安装到 PATH（%v）；配置已就绪，安装后执行 lasitan-cluster migrate --start\n", err)
		return nil
	}
	fmt.Fprintf(os.Stderr, "lasitan-cluster: 使用 %s 启动新服务…\n", exe)

	// Ensure unit templates exist (packaged or written under /etc).
	if st.Master {
		if err := ensureNewMasterUnit(exe); err != nil {
			return err
		}
	}
	if len(st.Ifaces) > 0 || (!st.Master && agentConfigExists()) {
		if err := ensureNewAgentUnit(exe); err != nil {
			return err
		}
	}
	_ = runSystemctl("daemon-reload")

	if st.Master {
		if err := runSystemctl("enable", "--now", "lasitan-cluster-master"); err != nil {
			fmt.Fprintf(os.Stderr, "lasitan-cluster: 启动 master 失败: %v\n", err)
		} else {
			fmt.Fprintln(os.Stderr, "lasitan-cluster: 已启动 lasitan-cluster-master")
		}
	}

	ifaces := st.Ifaces
	if len(ifaces) == 0 && !st.Master && agentConfigExists() {
		ifaces = []string{defaultIface}
	}
	for _, iface := range ifaces {
		if err := validateIfaceName(iface); err != nil {
			continue
		}
		unit := fmt.Sprintf("lasitan-cluster@%s", iface)
		if err := runSystemctl("enable", "--now", unit); err != nil {
			fmt.Fprintf(os.Stderr, "lasitan-cluster: 启动 %s 失败: %v\n", unit, err)
			continue
		}
		fmt.Fprintf(os.Stderr, "lasitan-cluster: 已启动 %s\n", unit)
	}

	st.Pending = false
	_ = saveMigrateState(st)
	fmt.Fprintln(os.Stderr, "lasitan-cluster: 无损迁移完成")
	return nil
}

func findLegacyBins() []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, p := range legacyBinCandidates {
		add(p)
	}
	if p, err := exec.LookPath(legacyBinName); err == nil {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			add(r)
		} else {
			add(p)
		}
	}
	return out
}

func listLegacyUnits() []string {
	var out []string
	if _, err := exec.LookPath("systemctl"); err != nil {
		return out
	}
	cmds := [][]string{
		{"list-units", "--type=service", "--all", "--no-legend", "--plain", "wireguard-go@*", legacyMasterUnit},
		{"list-unit-files", "--type=service", "--no-legend", "--plain", "wireguard-go@*", legacyMasterUnit},
	}
	seen := map[string]bool{}
	for _, args := range cmds {
		b, err := exec.Command("systemctl", args...).Output()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) == 0 {
				continue
			}
			name := f[0]
			if !strings.HasPrefix(name, "wireguard-go") {
				continue
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	// Unit files on disk even if systemctl list is empty (partial uninstall).
	for _, p := range []string{legacyUnitAgentLib, legacyUnitAgentEtc, legacyUnitMasterLib, legacyUnitMasterEtc} {
		if fileExists(p) {
			base := filepath.Base(p)
			if !seen[base] {
				seen[base] = true
				out = append(out, base)
			}
		}
	}
	return out
}

func collectLegacyIfaces(units []string) []string {
	var ifaces []string
	for _, u := range units {
		if strings.HasPrefix(u, "wireguard-go@") && strings.HasSuffix(u, ".service") {
			iface := strings.TrimSuffix(strings.TrimPrefix(u, "wireguard-go@"), ".service")
			if iface != "" && iface != "*" {
				ifaces = append(ifaces, iface)
			}
		}
	}
	ifaces = append(ifaces, confIfaces(legacyConfDir)...)
	return ifaces
}

func confIfaces(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".conf") || strings.HasSuffix(name, ".example") {
			continue
		}
		out = append(out, strings.TrimSuffix(name, ".conf"))
	}
	return out
}

func stopLegacy(units []string) {
	for _, u := range units {
		// Skip template unit wireguard-go@.service — stop concrete instances only.
		if u == "wireguard-go@.service" {
			continue
		}
		_ = runSystemctl("disable", "--now", u)
		_ = runSystemctl("stop", u)
	}
	// Catch any remaining instances.
	if b, err := exec.Command("systemctl", "list-units", "--type=service", "--state=active",
		"--no-legend", "--plain", "wireguard-go@*").Output(); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) == 0 {
				continue
			}
			_ = runSystemctl("disable", "--now", f[0])
		}
	}
	_ = runSystemctl("disable", "--now", legacyMasterUnit)
	_ = exec.Command("pkill", "-x", legacyBinName).Run()
	// Best-effort: drop leftover TUN devices named in old confs.
	for _, iface := range confIfaces(legacyConfDir) {
		_ = exec.Command("ip", "link", "set", "dev", iface, "down").Run()
		// Keep the TUN if new service will reuse the same iface name — only down.
		_ = os.Remove(filepath.Join(legacyRunDir, iface+".sock"))
	}
}

func migrateConfigs() error {
	if !dirExists(legacyConfDir) {
		return nil
	}
	pairs := [][2]string{
		{legacyAgentFile, newAgentFile},
		{legacyMasterFile, newMasterFile},
		{legacyTransportFile, newTransportFile},
		{".role", ".role"},
	}
	for _, p := range pairs {
		src := filepath.Join(legacyConfDir, p[0])
		dst := filepath.Join(newConfDir, p[1])
		if !fileExists(src) {
			continue
		}
		if fileExists(dst) {
			fmt.Fprintf(os.Stderr, "lasitan-cluster: 保留已有 %s（不覆盖）\n", dst)
			continue
		}
		if err := copyFile(src, dst, 0600); err != nil {
			return fmt.Errorf("copy %s → %s: %w", src, dst, err)
		}
		fmt.Fprintf(os.Stderr, "lasitan-cluster: 已保存 %s → %s\n", src, dst)
		if p[0] == legacyMasterFile {
			if err := rewriteMasterDataDir(dst); err != nil {
				fmt.Fprintf(os.Stderr, "lasitan-cluster: 调整 dataDir 警告: %v\n", err)
			}
		}
	}
	// Interface .conf files (legacy local mode).
	entries, err := os.ReadDir(legacyConfDir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".conf") {
			continue
		}
		src := filepath.Join(legacyConfDir, name)
		dst := filepath.Join(newConfDir, name)
		if fileExists(dst) {
			fmt.Fprintf(os.Stderr, "lasitan-cluster: 保留已有 %s（不覆盖）\n", dst)
			continue
		}
		if err := copyFile(src, dst, 0600); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "lasitan-cluster: 已保存 %s → %s\n", src, dst)
	}
	return nil
}

func rewriteMasterDataDir(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	changed := false
	if v, ok := m["dataDir"].(string); ok {
		if v == legacyDataDir || v == "" {
			m["dataDir"] = newDataDir
			changed = true
		}
	} else if _, ok := m["dataDir"]; !ok {
		m["dataDir"] = newDataDir
		changed = true
	}
	if !changed {
		return nil
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	return os.WriteFile(path, out, 0600)
}

func migrateDataDir() error {
	if !dirExists(legacyDataDir) {
		return nil
	}
	if err := os.MkdirAll(newDataDir, 0750); err != nil {
		return err
	}
	entries, err := os.ReadDir(legacyDataDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		src := filepath.Join(legacyDataDir, e.Name())
		dst := filepath.Join(newDataDir, e.Name())
		if fileExists(dst) {
			fmt.Fprintf(os.Stderr, "lasitan-cluster: 保留已有 %s（不覆盖）\n", dst)
			continue
		}
		if e.IsDir() {
			if err := copyDir(src, dst); err != nil {
				return err
			}
		} else {
			if err := copyFile(src, dst, 0640); err != nil {
				return err
			}
		}
		fmt.Fprintf(os.Stderr, "lasitan-cluster: 已保存 %s → %s\n", src, dst)
	}
	return nil
}

func removeLegacyUnits() {
	for _, p := range []string{legacyUnitAgentLib, legacyUnitAgentEtc, legacyUnitMasterLib, legacyUnitMasterEtc} {
		if err := os.Remove(p); err == nil {
			fmt.Fprintf(os.Stderr, "lasitan-cluster: 已删除 %s\n", p)
		}
	}
	// Drop enablement symlinks.
	wants, _ := filepath.Glob("/etc/systemd/system/*.wants/wireguard-go*")
	for _, p := range wants {
		_ = os.Remove(p)
	}
}

func removeLegacyBins(bins []string) {
	for _, b := range bins {
		if err := os.Remove(b); err == nil {
			fmt.Fprintf(os.Stderr, "lasitan-cluster: 已删除 %s\n", b)
		} else if err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "lasitan-cluster: 删除 %s 失败: %v\n", b, err)
		}
	}
}

func removeLegacyPackage() {
	if !dpkgInstalled(legacyPkgName) {
		return
	}
	// Configs already copied; remove package without purging foreign files we care about.
	cmd := exec.Command("dpkg", "--remove", "--force-depends", legacyPkgName)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "lasitan-cluster: 移除包 %s 警告: %v（可忽略）\n", legacyPkgName, err)
		return
	}
	fmt.Fprintf(os.Stderr, "lasitan-cluster: 已移除 dpkg 包 %s\n", legacyPkgName)
}

func ensureNewAgentUnit(exe string) error {
	if fileExists(systemdUnitPathLib) {
		return nil
	}
	if fileExists(systemdUnitPathEtc) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(systemdUnitPathEtc), 0755); err != nil {
		return err
	}
	return os.WriteFile(systemdUnitPathEtc, []byte(systemdUnitTemplate(exe)), 0644)
}

func ensureNewMasterUnit(exe string) error {
	if fileExists(systemdMasterUnitLib) {
		return nil
	}
	if fileExists(systemdMasterUnitEtc) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(systemdMasterUnitEtc), 0755); err != nil {
		return err
	}
	return os.WriteFile(systemdMasterUnitEtc, []byte(systemdMasterUnitTemplate(exe)), 0644)
}

func resolveInstalledLasitan() (string, error) {
	candidates := []string{"/usr/bin/lasitan-cluster", "/usr/local/bin/lasitan-cluster"}
	if self, err := os.Executable(); err == nil {
		if r, err := filepath.EvalSymlinks(self); err == nil {
			self = r
		}
		// Prefer a non-temp install path.
		if !strings.Contains(self, "/tmp/") && !strings.HasPrefix(self, "/var/tmp/") {
			candidates = append([]string{self}, candidates...)
		}
	}
	if p, err := exec.LookPath("lasitan-cluster"); err == nil {
		candidates = append(candidates, p)
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			if r, err := filepath.EvalSymlinks(p); err == nil {
				return r, nil
			}
			return p, nil
		}
	}
	return "", fmt.Errorf("找不到已安装的 lasitan-cluster")
}

func saveMigrateState(st legacyMigrateState) error {
	if err := os.MkdirAll(newConfDir, 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(filepath.Join(newConfDir, migrateStateFile), b, 0600)
}

func loadMigrateState() (*legacyMigrateState, error) {
	b, err := os.ReadFile(filepath.Join(newConfDir, migrateStateFile))
	if err != nil {
		return nil, err
	}
	var st legacyMigrateState
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func unitActiveOrEnabled(unit string) bool {
	if err := exec.Command("systemctl", "is-active", "--quiet", unit).Run(); err == nil {
		return true
	}
	if err := exec.Command("systemctl", "is-enabled", "--quiet", unit).Run(); err == nil {
		return true
	}
	return false
}

func dirHasLegacyConfig(dir string) bool {
	for _, name := range []string{legacyAgentFile, legacyMasterFile, legacyTransportFile, ".role"} {
		if fileExists(filepath.Join(dir, name)) {
			return true
		}
	}
	return len(confIfaces(dir)) > 0
}

func roleIsMaster(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, ".role"))
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(b)) == "master"
}

func agentConfigExists() bool {
	return fileExists(filepath.Join(newConfDir, newAgentFile))
}

func dpkgInstalled(name string) bool {
	if _, err := exec.LookPath("dpkg-query"); err != nil {
		return false
	}
	out, err := exec.Command("dpkg-query", "-W", "-f=${Status}", name).Output()
	if err != nil {
		return false
	}
	s := string(out)
	return strings.Contains(s, "install ok installed") || strings.Contains(s, "installed")
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if st, err := os.Stat(src); err == nil {
		_ = os.Chmod(tmp, st.Mode().Perm())
	}
	return os.Rename(tmp, dst)
}

func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0750); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyDir(s, d); err != nil {
				return err
			}
			continue
		}
		if fileExists(d) {
			continue
		}
		if err := copyFile(s, d, 0640); err != nil {
			return err
		}
	}
	return nil
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
