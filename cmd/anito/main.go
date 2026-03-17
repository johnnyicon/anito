package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/johnnyicon/anito/internal/client"
	"github.com/johnnyicon/anito/internal/config"
	mcpserver "github.com/johnnyicon/anito/internal/mcp"
	"github.com/johnnyicon/anito/internal/process"
	"github.com/johnnyicon/anito/internal/proxy"
	"github.com/johnnyicon/anito/internal/registry"
	"github.com/johnnyicon/anito/internal/server"
	"github.com/johnnyicon/anito/internal/service"
	"github.com/johnnyicon/anito/internal/watcher"
)

// version is set at build time:
//
//	go build -ldflags "-X main.version=v1.0.0" ./cmd/anito/
var version = "dev"

const (
	defaultDaemonPort = 7700
	defaultMCPPort    = 7701
)

func main() {
	daemonCmd := flag.NewFlagSet("daemon", flag.ExitOnError)
	daemonPort := daemonCmd.Int("port", defaultDaemonPort, "port for the Anito management API")
	daemonMCPPort := daemonCmd.Int("mcp-port", defaultMCPPort, "port for the Anito MCP server")
	dataDir := daemonCmd.String("data", defaultDataDir(), "directory for registry and logs")

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cli := client.New(defaultDaemonPort)

	switch os.Args[1] {
	case "daemon":
		_ = daemonCmd.Parse(os.Args[2:])
		runDaemon(*daemonPort, *daemonMCPPort, *dataDir)

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

	case "logs":
		requireArg("logs", os.Args)
		runLogs(cli, os.Args[2])

	case "mcp":
		runMCPInfo(defaultMCPPort)

	case "reload":
		runReload()

	case "version":
		fmt.Printf("anito %s\n", version)

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

	portDesc := "auto"
	if cfg.Port != 0 {
		portDesc = fmt.Sprintf("localhost:%d", cfg.Port)
	}
	fmt.Printf("deploying %s → %s...\n", cfg.Name, portDesc)

	svc, err := cli.Deploy(client.DeployRequest{
		Name:        cfg.Name,
		Version:     cfg.Version,
		Type:        registry.ServiceType(cfg.Type),
		Path:        absOutput,
		Args:        cfg.Args,
		StablePort:  cfg.Port,
		EnvFile:     cfg.EnvFile,
		HealthCheck: cfg.HealthCheck,
		WatchPaths:  cfg.Watch,
	})
	if err != nil {
		fatal(err)
	}

	versionStr := ""
	if svc.Version != "" {
		versionStr = fmt.Sprintf(" (%s)", svc.Version)
	}
	fmt.Printf("✓ %s%s running on localhost:%d\n", svc.Name, versionStr, svc.StablePort)
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
	fmt.Fprintln(w, "NAME\tPORT\tSTATUS\tPID\tVERSION\tDEPLOYED")
	for _, s := range svcs {
		pid := "-"
		if s.PID > 0 {
			pid = fmt.Sprintf("%d", s.PID)
		}
		ver := s.Version
		if ver == "" {
			ver = "-"
		}
		fmt.Fprintf(w, "%s\t:%d\t%s\t%s\t%s\t%s\n",
			s.Name, s.StablePort, s.Status, pid, ver,
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
	fmt.Printf("version:       %s\n", svc.Version)
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

func runLogs(cli *client.Client, name string) {
	lines, err := cli.Logs(name, 100)
	if err != nil {
		fatal(err)
	}
	for _, line := range lines {
		fmt.Println(line)
	}
}

func runMCPInfo(mcpPort int) {
	fmt.Printf("Anito MCP server: http://localhost:%d\n\n", mcpPort)
	fmt.Println("Add to Claude Code:")
	fmt.Printf("  claude mcp add --transport http anito http://localhost:%d\n\n", mcpPort)
	fmt.Println("Or add to .mcp.json / ~/.claude.json under mcpServers:")
	fmt.Printf(`  {
    "anito": {
      "type": "http",
      "url": "http://localhost:%d"
    }
  }`+"\n", mcpPort)
}

// runReload rebuilds the daemon binary and reloads the launchd agent.
// It answers the question "how do I get the running daemon to the latest build?"
func runReload() {
	plist := launchdPlistPath()
	if _, err := os.Stat(plist); err != nil {
		fatal(fmt.Errorf("launchd plist not found at %s — is Anito installed as a daemon?", plist))
	}

	fmt.Println("unloading daemon...")
	if out, err := exec.Command("launchctl", "unload", plist).CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "warn: unload: %s\n", strings.TrimSpace(string(out)))
	}
	time.Sleep(500 * time.Millisecond)

	fmt.Println("loading daemon...")
	if out, err := exec.Command("launchctl", "load", plist).CombinedOutput(); err != nil {
		fatal(fmt.Errorf("load failed: %s", strings.TrimSpace(string(out))))
	}

	fmt.Print("waiting for daemon...")
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(300 * time.Millisecond)
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/health", defaultDaemonPort))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				fmt.Println(" ✓")
				// Show running version
				cli := client.New(defaultDaemonPort)
				if v, err := cli.DaemonVersion(); err == nil {
					fmt.Printf("daemon running version %s\n", v)
				}
				return
			}
		}
		fmt.Print(".")
	}
	fmt.Println()
	fatal(fmt.Errorf("daemon did not become healthy within 10s — check ~/.anito/logs/anito.log"))
}

func runDaemon(apiPort, mcpPort int, dataDir string) {
	log.SetOutput(os.Stdout)
	log.SetFlags(log.Ldate | log.Ltime)

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
	wtch := watcher.New()

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

	log.Printf("[STARTUP] version=%s data=%s api=:%d mcp=:%d", version, dataDir, apiPort, mcpPort)

	svc := service.New(reg, mgr, prx, logDir, wtch)
	svc.StartWatchers()

	mcpSrv := mcpserver.New(svc, mcpPort)
	go func() {
		if err := mcpSrv.Start(); err != nil {
			log.Printf("MCP server error: %v", err)
		}
	}()

	srv := server.New(svc, apiPort, version)
	log.Fatal(srv.Start())
}

func launchdPlistPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "LaunchAgents", "com.anito.daemon.plist")
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
  anito logs <name>             print recent log output
  anito stop <name>             stop a service
  anito restart <name>          restart a service
  anito remove <name>           stop and remove a service
  anito reload                  reload the daemon with the current binary (launchd)
  anito version                 print the daemon binary version
  anito mcp                     show MCP server connection info

Daemon flags:
  --port      management API port (default 7700)
  --mcp-port  MCP server port     (default 7701)
  --data      data directory      (default ~/.anito)`)
}
