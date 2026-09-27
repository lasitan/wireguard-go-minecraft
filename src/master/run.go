// Package master wires the control-plane HTTP server behind `wireguard-go master`.
package master

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.zx2c4.com/wireguard/src/core"
	"golang.zx2c4.com/wireguard/src/core/config"
	"golang.zx2c4.com/wireguard/src/master/api"
)

// HandleCommand runs the Master HTTP server when invoked as `master`.
// It returns false when os.Args is not a master invocation.
func HandleCommand() bool {
	if len(os.Args) < 2 || os.Args[1] != "master" {
		return false
	}
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "Usage: wireguard-go master")
		os.Exit(core.ExitSetupFailed)
	}

	cfgPath := filepath.Join(config.ConfDir(), config.MasterFileName)
	var cfg config.MasterConfig
	if err := config.LoadJSON(cfgPath, &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "master: load %s: %v\n", cfgPath, err)
		os.Exit(core.ExitSetupFailed)
	}
	srv, err := api.NewServer(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "master: %v\n", err)
		os.Exit(core.ExitSetupFailed)
	}
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintf(os.Stderr, "master: listen: %v\n", err)
		os.Exit(core.ExitSetupFailed)
	}
	return true
}
