package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/johnnyicon/anito/internal/client"
	"github.com/johnnyicon/anito/internal/config"
	"github.com/johnnyicon/anito/internal/process"
	"github.com/johnnyicon/anito/internal/proxy"
	"github.com/johnnyicon/anito/internal/registry"
	"github.com/johnnyicon/anito/internal/server"
)

const defaultDaemonPort = 7700

func main() {
	daemonCmd := flag.NewFlagSet("daemon", flag.ExitOnError)
	daemonPort := daemonCmd.Int("port", defaultDaemonPort, "port for the Anito management API")
	dataDir := daemonCmd.String("data", defaultDataDir(), "directory for registry and logs")

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cli := client.New(defaultDaemonPort)

	switch os.Args[1] {
	case "daemon":
		_ = daemonCmd.Parse(os.Args[2:])
		runDaemon(*daemonPort, *dataDir)

	case "deploy":
		configPath := defaultConfigPath()
		if len(os.Args) >= 3 {
			configPath = os.Args[2]
		}
		runDeploy(cli, configPath)

	case "services":
		runServices(cli)

	case "status":
		requireArg("status", os.Args)
		runStatus(cli, os.Args[2])

	case "stop":
		requireArg("stop", os.Args)
		if err := cli.Stop(os.Args[2]); err != nil {
			fatal(err)
		}
		fmt.Printf("stopped %s\n", os.Args[2])

	case "restart":
		requireArg("restart", os.Args)
		if err := cli.Restart(os.Args[2]); err != nil {
			fatal(err)
		}
		fmt.Printf("restarted %s\n", os.Args[2])

	case "remove":
		requireArg("remove", os.Args)
		if err := cli.Remove(os.Args[2]); err != nil {
			fatal(err)
		}
		fmt.Printf("removed %s\n", os.Args[2])

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func runDeploy(cli *client.Client, configPath string) {
	cfg, err := config.Load(configPath)
	if err != nil {
		fatal(err)
	}

	// Resolve output path to absolute so the daemon can find it regardless of cwd.
	absOutput, err := filepath.Abs(cfg.Output)
	if err != nil {
		fatal(err)
	}

	if cfg.Build != "" {
		fmt.Printf("building %s...\n", cfg.Name)
		parts := strings.Fields(cfg.Build)
		cmd := exec.Command(parts[0], parts[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			log.Fatalf("build failed: %v", err)
		}
	}

	fmt.Printf("deploying %s → localhost:%d...\n", cfg.Name, cfg.Port)
	svc, err := cli.Deploy(client.DeployRequest{
		Name:        cfg.Name,
		Type:        registry.ServiceType(cfg.Type),
		Path:        absOutput,
		StablePort:  cfg.Port,
		EnvFile:     cfg.EnvFile,
		HealthCheck: cfg.HealthCheck,
	})
	if err != nil {
		fatal(err)
	}

	fmt.Printf("✓ %s running on localhost:%d\n", svc.Name, svc.StablePort)
}

func runServices(cli *client.Client) {
	svcs, err := cli.Services()
	if err != nil {
		fatal(err)
	}
	if len(svcs) == 0 {
		fmt.Println("no services registered")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tPORT\tSTATUS\tPID\tDEPLOYED")
	for _, s := range svcs {
		pid := "-"
		if s.PID > 0 {
			pid = fmt.Sprintf("%d", s.PID)
		}
		fmt.Fprintf(w, "%s\t:%d\t%s\t%s\t%s\n",
			s.Name, s.StablePort, s.Status, pid,
			s.DeployedAt.Format(time.DateTime),
		)
	}
	w.Flush()
}

func runStatus(cli *client.Client, name string) {
	svc, err := cli.Status(name)
	if err != nil {
		fatal(err)
	}

	fmt.Printf("name:          %s\n", svc.Name)
	fmt.Printf("type:          %s\n", svc.Type)
	fmt.Printf("port:          localhost:%d\n", svc.StablePort)
	fmt.Printf("status:        %s\n", svc.Status)
	if svc.PID > 0 {
		fmt.Printf("pid:           %d (internal :%d)\n", svc.PID, svc.InternalPort)
	}
	fmt.Printf("binary:        %s\n", svc.BinaryPath)
	fmt.Printf("deployed:      %s\n", svc.DeployedAt.Format(time.DateTime))
	fmt.Printf("updated:       %s\n", svc.UpdatedAt.Format(time.DateTime))
}

func runDaemon(apiPort int, dataDir string) {
	logDir := filepath.Join(dataDir, "logs")

	reg, err := registry.New(dataDir)
	if err != nil {
		log.Fatalf("registry: %v", err)
	}

	mgr, err := process.New(logDir, reg)
	if err != nil {
		log.Fatalf("process manager: %v", err)
	}

	prx := proxy.NewManager()

	// Restore services that were running before the daemon last stopped.
	for _, svc := range reg.All() {
		if svc.StablePort == 0 {
			continue
		}
		if err := prx.Register(svc.Name, svc.StablePort); err != nil {
			log.Printf("warn: could not re-register proxy for %s: %v", svc.Name, err)
			continue
		}
		if svc.Status == registry.StatusRunning {
			log.Printf("restoring %s on localhost:%d", svc.Name, svc.StablePort)
			if svc.Type == registry.TypeStatic {
				if err := prx.SwapStatic(svc.Name, svc.BinaryPath); err != nil {
					log.Printf("warn: static swap failed for %s: %v", svc.Name, err)
				}
				continue
			}
			internalPort, err := mgr.Start(svc)
			if err != nil {
				log.Printf("warn: could not restore %s: %v", svc.Name, err)
				continue
			}
			if err := prx.Swap(svc.Name, internalPort); err != nil {
				log.Printf("warn: proxy swap failed for %s: %v", svc.Name, err)
			}
		}
	}

	srv := server.New(reg, mgr, prx, apiPort)
	log.Fatal(srv.Start())
}

func requireArg(cmd string, args []string) {
	if len(args) < 3 {
		fmt.Fprintf(os.Stderr, "usage: anito %s <name>\n", cmd)
		os.Exit(1)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".anito"
	}
	return filepath.Join(home, ".anito")
}

// defaultConfigPath looks for .anito/config.yaml in the current directory.
func defaultConfigPath() string {
	return filepath.Join(".anito", "config.yaml")
}

func printUsage() {
	fmt.Println(`anito — local production service manager

Usage:
  anito daemon [flags]          start the anito daemon
  anito deploy [config]         build + deploy (default: .anito/config.yaml)
  anito services                list all running services
  anito status <name>           show status and port for a service
  anito stop <name>             stop a service
  anito restart <name>          restart a service
  anito remove <name>           stop and remove a service

Daemon flags:
  --port   management API port (default 7700)
  --data   data directory      (default ~/.anito)`)
}
