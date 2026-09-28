//go:build windows

package config

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/src/utils/pwsh"
	"golang.zx2c4.com/wireguard/src/wireguard/device"
)

func psList(items []string) string {
	q := make([]string, len(items))
	for i, s := range items {
		q[i] = "'" + pwsh.Quote(s) + "'"
	}
	return "@(" + strings.Join(q, ",") + ")"
}

// syncIfaceRoutes adds/removes on-link routes through the tunnel interface.
func syncIfaceRoutes(iface string, add, del []string, logger *device.Logger) error {
	script := fmt.Sprintf(`
$idx = (Get-NetAdapter -Name '%s' -ErrorAction Stop).ifIndex
foreach ($p in %s) { Remove-NetRoute -InterfaceIndex $idx -DestinationPrefix $p -Confirm:$false -ErrorAction SilentlyContinue }
foreach ($p in %s) {
  if (-not (Get-NetRoute -InterfaceIndex $idx -DestinationPrefix $p -ErrorAction SilentlyContinue)) {
    New-NetRoute -InterfaceIndex $idx -DestinationPrefix $p -PolicyStore ActiveStore -ErrorAction Stop | Out-Null
  }
}`, pwsh.Quote(iface), psList(del), psList(add))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("routes on %s: %w (%s)", iface, err, strings.TrimSpace(string(out)))
	}
	logger.Verbosef("Routes on %s: +%v -%v", iface, add, del)
	return nil
}
