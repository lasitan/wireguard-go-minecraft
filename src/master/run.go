package main

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.zx2c4.com/wireguard/master"
	"golang.zx2c4.com/wireguard/meshcfg"
)

// handleMasterCommand runs the control-plane Master HTTP server.
func handleMasterCommand() bool {
	if len(os.Args) < 2 || os.Args[1] != "master" {
		return false
	}
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "Usage: wireguard-go master")
		os.Exit(ExitSetupFailed)
	}

	cfgPath := filepath.Join(meshcfg.ConfDir(), meshcfg.MasterFileName)
	var cfg meshcfg.MasterConfig
	if err := meshcfg.LoadJSON(cfgPath, &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "master: load %s: %v\n", cfgPath, err)
		os.Exit(ExitSetupFailed)
	}
	srv, err := master.NewServer(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "master: %v\n", err)
		os.Exit(ExitSetupFailed)
	}
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintf(os.Stderr, "master: listen: %v\n", err)
		os.Exit(ExitSetupFailed)
	}
	return true
}
