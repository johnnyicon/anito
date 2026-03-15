package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/johnnyicon/anito/internal/process"
	"github.com/johnnyicon/anito/internal/registry"
	"github.com/johnnyicon/anito/internal/server"
)

func main() {
	var (
		daemonCmd = flag.NewFlagSet("daemon", flag.ExitOnError)
		port      = daemonCmd.Int("port", 6660, "port for the Anito API server")
		dataDir   = daemonCmd.String("data", defaultDataDir(), "directory for registry and logs")
		portMin   = daemonCmd.Int("port-min", 8100, "start of port allocation range")
		portMax   = daemonCmd.Int("port-max", 8200, "end of port allocation range")
	)

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "daemon":
		_ = daemonCmd.Parse(os.Args[2:])
		runDaemon(*port, *dataDir, *portMin, *portMax)

	default:
		// All other commands (deploy, stop, restart, etc.) are handled
		// by the CLI client in pkg/client — this binary delegates to it.
		// For now print usage.
		printUsage()
		os.Exit(1)
	}
}

func runDaemon(apiPort int, dataDir string, portMin, portMax int) {
	logDir := filepath.Join(dataDir, "logs")

	reg, err := registry.New(dataDir, portMin, portMax)
	if err != nil {
		log.Fatalf("registry: %v", err)
	}

	mgr, err := process.New(logDir, reg)
	if err != nil {
		log.Fatalf("process manager: %v", err)
	}

	// Restore previously running services on startup
	for _, svc := range reg.All() {
		if svc.Status == registry.StatusRunning {
			log.Printf("restoring %s on port %d", svc.Name, svc.Port)
			if err := mgr.Start(svc); err != nil {
				log.Printf("warn: could not restore %s: %v", svc.Name, err)
			}
		}
	}

	srv := server.New(reg, mgr, apiPort)
	log.Fatal(srv.Start())
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".anito"
	}
	return filepath.Join(home, ".anito")
}

func printUsage() {
	fmt.Println(`anito — local production service manager

Usage:
  anito daemon [flags]     start the anito daemon
  anito deploy <config>    deploy a service from an anito.yaml
  anito services           list all registered services
  anito stop <name>        stop a service
  anito restart <name>     restart a service
  anito status <name>      get service status
  anito remove <name>      stop and remove a service

Daemon flags:
  --port      API server port (default 6660)
  --data      data directory  (default ~/.anito)
  --port-min  start of port range (default 8100)
  --port-max  end of port range   (default 8200)`)
}
