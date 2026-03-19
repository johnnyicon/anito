package main

import (
	"flag"
	"fmt"
	"io"
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
	"github.com/johnnyicon/anito/internal/issues"
	mcpserver "github.com/johnnyicon/anito/internal/mcp"
	"github.com/johnnyicon/anito/internal/process"
	"github.com/johnnyicon/anito/internal/proxy"
	"github.com/johnnyicon/anito/internal/registry"
	"github.com/johnnyicon/anito/internal/server"
	"github.com/johnnyicon/anito/internal/service"
	"github.com/johnnyicon/anito/internal/setup"
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
	case "install":
		binDir := ""
		for i, arg := range os.Args[2:] {
			if arg == "--bin-dir" && i+1 < len(os.Args[2:]) {
				binDir = os.Args[3+i]
			}
		}
		runInstall(binDir)

	case "daemon":
		_ = daemonCmd.Parse(os.Args[2:])
		runDaemon(*daemonPort, *daemonMCPPort, *dataDir)

	case "setup":
		path := "."
		if len(os.Args) >= 3 {
			path = os.Args[2]
		}
		runSetup(path)

	case "doctor":
		path := "."
		if len(os.Args) >= 3 {
			path = os.Args[2]
		}
		runDoctor(cli, path)

	case "deploy":
		configPath := defaultConfigPath()
		if len(os.Args) >= 3 {
			configPath = os.Args[2]
		}
		runDeploy(cli, configPath)

	case "promote":
		// promote <stable-config> [dev-name]
		// Builds the stable binary (runs config.Build) and deploys it as the
		// stable service.  Functionally identical to `deploy` but named to
		// make the "dev → stable" promotion intent explicit.
		// Optional second arg is the dev service name — displayed in output
		// for clarity but not used mechanically.
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "usage: anito promote <stable-config.yaml> [dev-service-name]\n")
			os.Exit(1)
		}
		devName := ""
		if len(os.Args) >= 4 {
			devName = os.Args[3]
		}
		runPromote(cli, os.Args[2], devName)

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
		logName := os.Args[2]
		// "daemon" is the CLI alias for the daemon log — "~daemon" starts with a tilde
		// which the shell expands before Go sees it, so we map the safe alias here.
		if logName == "daemon" {
			logName = service.DaemonLogName
		}
		follow := false
		for _, arg := range os.Args[3:] {
			if arg == "-f" || arg == "--follow" {
				follow = true
			}
		}
		if follow {
			if err := cli.LogsFollow(logName, 100, os.Stdout); err != nil {
				fatal(err)
			}
		} else {
			runLogs(cli, logName)
		}

	case "issues":
		n := 20
		src := ""
		for i, arg := range os.Args[2:] {
			if arg == "--lines" || arg == "-n" {
				if i+1 < len(os.Args[2:]) {
					fmt.Sscanf(os.Args[3+i], "%d", &n)
				}
			}
			if arg == "--source" {
				if i+1 < len(os.Args[2:]) {
					src = os.Args[3+i]
				}
			}
		}
		runIssues(cli, n, src)

	case "report":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "usage: anito report <message> [--source <name>] [--context <text>] [--repo <path>]\n")
			os.Exit(1)
		}
		msg := os.Args[2]
		src := ""
		ctx := ""
		repo := ""
		for i, arg := range os.Args[3:] {
			if arg == "--source" && i+1 < len(os.Args[3:]) {
				src = os.Args[4+i]
			}
			if arg == "--context" && i+1 < len(os.Args[3:]) {
				ctx = os.Args[4+i]
			}
			if arg == "--repo" && i+1 < len(os.Args[3:]) {
				repo = os.Args[4+i]
			}
		}
		if src == "" {
			src = "cli:report"
		}
		if err := cli.Report(issues.Issue{
			Source:   src,
			Tool:     "report",
			Error:    msg,
			Context:  ctx,
			RepoPath: repo,
			Severity: "info",
		}); err != nil {
			fatal(err)
		}
		fmt.Println("reported")

	case "teardown":
		repoPath := "."
		if len(os.Args) >= 3 {
			repoPath = os.Args[2]
		}
		abs, err := filepath.Abs(repoPath)
		if err != nil {
			fatal(err)
		}
		removed, err := cli.Teardown(abs)
		if err != nil {
			fatal(err)
		}
		if len(removed) == 0 {
			fmt.Println("no services found in .anito/deployed.json")
		} else {
			for _, name := range removed {
				fmt.Printf("removed %s\n", name)
			}
			fmt.Printf("teardown complete (%d services removed)\n", len(removed))
		}

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

// runSetup inspects a repo for Anito compatibility and writes the generated
// config to disk. It is the CLI counterpart of the anito_setup MCP tool.
// For composite apps (multiple services), use the MCP tool or see docs/mcp.md.
func runSetup(path string) {
	result, err := setup.Inspect(path)
	if err != nil {
		fatal(err)
	}

	fmt.Printf("inspecting %s...\n\n", result.RepoPath)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	check := func(ok bool) string {
		if ok {
			return "✓"
		}
		return "✗"
	}
	fmt.Fprintf(w, "  language:\t%s\n", result.Language)
	fmt.Fprintf(w, "  PORT env var:\t%s\n", check(result.HasPORT))
	fmt.Fprintf(w, "  /health route:\t%s\n", check(result.HasHealthRoute))
	fmt.Fprintf(w, "  .anito/config.yaml:\t%s\n", check(result.HasAnitoConfig))
	_ = w.Flush()
	fmt.Println()

	if len(result.Issues) > 0 {
		fmt.Println("issues:")
		for _, iss := range result.Issues {
			fmt.Printf("  [%s] %s\n    → %s\n", iss.Severity, iss.What, iss.Fix)
		}
		fmt.Println()
	}

	// Write the generated config unless one already exists.
	configPath := filepath.Join(result.RepoPath, ".anito", "config.yaml")
	if result.SuggestedConfig != "" && !result.HasAnitoConfig {
		if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
			fatal(err)
		}
		if err := os.WriteFile(configPath, []byte(result.SuggestedConfig), 0644); err != nil {
			fatal(err)
		}
		fmt.Printf("wrote %s\n\n", configPath)
	} else if result.HasAnitoConfig {
		fmt.Printf("config already exists at %s — skipping write\n\n", configPath)
	}

	fmt.Println("next steps:")
	for _, step := range result.Instructions {
		fmt.Printf("  %s\n", step)
	}

	if len(result.Issues) == 0 {
		fmt.Println("\n✓ repo is Anito-ready. Run `anito deploy` to start.")
	}
}

// runPromote builds and deploys a stable service from its config YAML.
// It is semantically identical to runDeploy but uses "promote" terminology
// to signal that this is a dev → stable promotion.  devName is optional
// display context.
func runPromote(cli *client.Client, configPath string, devName string) {
	cfg, err := config.Load(configPath)
	if err != nil {
		fatal(err)
	}
	if cfg.Build == "" {
		fatal(fmt.Errorf("config %s has no build command — nothing to promote", configPath))
	}

	if devName != "" {
		fmt.Printf("promoting %s → %s...\n", devName, cfg.Name)
	} else {
		fmt.Printf("promoting %s...\n", cfg.Name)
	}

	// Delegate to runDeploy — it handles build + health-check + proxy swap.
	runDeploy(cli, configPath)
}

func runDeploy(cli *client.Client, configPath string) {
	cfg, err := config.Load(configPath)
	if err != nil {
		fatal(err)
	}

	absConfig, err := filepath.Abs(configPath)
	if err != nil {
		fatal(err)
	}
	absOutput, err := filepath.Abs(cfg.Output)
	if err != nil {
		fatal(err)
	}

	// Resolve env_file to absolute so the daemon can open it regardless of
	// its own working directory. Relative paths are resolved from the caller's
	// CWD, consistent with how output and config_path are handled above.
	absEnvFile := cfg.EnvFile
	if absEnvFile != "" && !filepath.IsAbs(absEnvFile) {
		absEnvFile, err = filepath.Abs(absEnvFile)
		if err != nil {
			fatal(err)
		}
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
		Name:               cfg.Name,
		Version:            cfg.Version,
		Type:               registry.ServiceType(cfg.Type),
		Path:               absOutput,
		Args:               cfg.Args,
		StablePort:         cfg.Port,
		EnvFile:            absEnvFile,
		HealthCheck:        cfg.HealthCheck,
		WatchPaths:         cfg.Watch,
		DrainWindow:        cfg.DrainWindow,
		HealthCheckTimeout: cfg.HealthCheckTimeout,
		RestartPolicy:      cfg.RestartPolicy,
		ConfigPath:         absConfig,
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

	// Port pressure summary — show how many auto-allocation slots are in use.
	const rangeStart, rangeEnd = 8100, 8200
	inRange := 0
	for _, s := range svcs {
		if s.StablePort >= rangeStart && s.StablePort <= rangeEnd {
			inRange++
		}
	}
	fmt.Printf("\nPorts: %d/%d in use (%d–%d)\n", inRange, rangeEnd-rangeStart+1, rangeStart, rangeEnd)
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
	if svc.ConfigPath != "" {
		fmt.Printf("config:        %s\n", svc.ConfigPath)
	} else {
		fmt.Printf("config:        (none recorded — redeploy to track)\n")
	}
	fmt.Printf("deployed:      %s\n", svc.DeployedAt.Format(time.DateTime))
	fmt.Printf("updated:       %s\n", svc.UpdatedAt.Format(time.DateTime))
}

func runIssues(cli *client.Client, n int, source string) {
	list, err := cli.Issues(n, source)
	if err != nil {
		fatal(err)
	}
	if len(list) == 0 {
		fmt.Println("no issues logged")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TIME\tSEVERITY\tSOURCE\tERROR")
	for _, iss := range list {
		errMsg := iss.Error
		if len(errMsg) > 60 {
			errMsg = errMsg[:57] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			iss.Timestamp.Format("2006-01-02 15:04:05"),
			iss.Severity,
			iss.Source,
			errMsg,
		)
	}
	w.Flush()
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

// plistTemplate is the launchd plist for Anito.
// Placeholders (in order): binary path, home dir (×4).
const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.anito.daemon</string>

  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>daemon</string>
    <string>--port</string>
    <string>7700</string>
    <string>--mcp-port</string>
    <string>7701</string>
  </array>

  <key>RunAtLoad</key>
  <true/>

  <key>KeepAlive</key>
  <true/>

  <key>StandardOutPath</key>
  <string>%s/.anito/logs/anito.log</string>

  <key>StandardErrorPath</key>
  <string>%s/.anito/logs/anito.error.log</string>

  <key>EnvironmentVariables</key>
  <dict>
    <key>HOME</key>
    <string>%s</string>
    <key>PATH</key>
    <string>%s/.local/bin:/usr/local/bin:/usr/bin:/bin</string>
  </dict>
</dict>
</plist>
`

// runInstall performs first-time daemon setup on a clean machine:
// copies the binary, creates ~/.anito/logs/, writes the launchd plist, and
// loads the daemon.  It is NOT needed on machines where Anito is already
// running — use `make reload` there.
func runInstall(binDir string) {
	home, err := os.UserHomeDir()
	if err != nil {
		fatal(fmt.Errorf("could not determine home directory: %w", err))
	}

	// Bail out if the daemon is already healthy — nothing to do.
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/health", defaultDaemonPort)) //nolint:noctx
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			fmt.Println("Anito is already installed and running.")
			fmt.Println("Use 'make reload' to update the binary.")
			return
		}
	}

	if binDir == "" {
		binDir = filepath.Join(home, ".local", "bin")
	}
	targetBin := filepath.Join(binDir, "anito")

	self, err := os.Executable()
	if err != nil {
		fatal(fmt.Errorf("could not locate current executable: %w", err))
	}

	// Install binary.
	if err := os.MkdirAll(binDir, 0755); err != nil {
		fatal(fmt.Errorf("creating %s: %w", binDir, err))
	}
	fmt.Printf("installing binary → %s\n", targetBin)
	if err := copyFile(self, targetBin, 0755); err != nil {
		fatal(fmt.Errorf("copying binary: %w", err))
	}

	// Create data + log directories.
	logDir := filepath.Join(home, ".anito", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		fatal(fmt.Errorf("creating %s: %w", logDir, err))
	}

	// Write launchd plist.
	plistPath := filepath.Join(home, "Library", "LaunchAgents", "com.anito.daemon.plist")
	if err := os.MkdirAll(filepath.Dir(plistPath), 0755); err != nil {
		fatal(fmt.Errorf("creating LaunchAgents dir: %w", err))
	}
	plist := fmt.Sprintf(plistTemplate, targetBin, home, home, home, home)
	if err := os.WriteFile(plistPath, []byte(plist), 0644); err != nil {
		fatal(fmt.Errorf("writing plist: %w", err))
	}
	fmt.Printf("wrote %s\n", plistPath)

	// Load the agent.
	fmt.Println("loading daemon...")
	if out, err := exec.Command("launchctl", "load", plistPath).CombinedOutput(); err != nil {
		fatal(fmt.Errorf("launchctl load: %s", strings.TrimSpace(string(out))))
	}

	// Wait for the daemon to become healthy.
	fmt.Print("waiting for daemon")
	deadline := time.Now().Add(10 * time.Second)
	healthy := false
	for time.Now().Before(deadline) {
		time.Sleep(300 * time.Millisecond)
		r, err := http.Get(fmt.Sprintf("http://localhost:%d/health", defaultDaemonPort)) //nolint:noctx
		if err == nil {
			r.Body.Close()
			if r.StatusCode == http.StatusOK {
				healthy = true
				break
			}
		}
		fmt.Print(".")
	}
	fmt.Println()
	if !healthy {
		fatal(fmt.Errorf("daemon did not become healthy within 10s — check %s/anito.log", logDir))
	}

	fmt.Printf("✓ anito installed and running\n\n")
	fmt.Printf("Connect Claude Code:\n")
	fmt.Printf("  claude mcp add --transport http anito http://localhost:%d\n", defaultMCPPort)
}

// copyFile copies src to dst with the given file permissions.
func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
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
					log.Printf("[RESTORE_FAILED] name=%s reason=%v", svc.Name, err)
					_ = reg.UpdateStatus(svc.Name, registry.StatusFailed, 0)
				}
				continue
			}
			// Verify binary exists before attempting to start.
			if _, err := os.Stat(svc.BinaryPath); err != nil {
				log.Printf("[RESTORE_FAILED] name=%s reason=binary not found: %s", svc.Name, svc.BinaryPath)
				_ = reg.UpdateStatus(svc.Name, registry.StatusFailed, 0)
				continue
			}
			internalPort, err := mgr.Start(svc)
			if err != nil {
				log.Printf("[RESTORE_FAILED] name=%s error=%v", svc.Name, err)
				_ = reg.UpdateStatus(svc.Name, registry.StatusFailed, 0)
				continue
			}

			hcTimeout := svc.HealthCheckTimeout
			if hcTimeout == 0 {
				hcTimeout = 15 * time.Second
			}
			if err := service.WaitHealthy(internalPort, svc.HealthCheck, hcTimeout); err != nil {
				log.Printf("[RESTORE_FAILED] name=%s health_check=%v", svc.Name, err)
				_ = mgr.Stop(svc.Name)
				_ = reg.UpdateStatus(svc.Name, registry.StatusFailed, 0)
				continue
			}

			if err := prx.Swap(svc.Name, internalPort); err != nil {
				log.Printf("[RESTORE_FAILED] name=%s proxy_swap=%v", svc.Name, err)
				_ = mgr.Stop(svc.Name)
				_ = reg.UpdateStatus(svc.Name, registry.StatusFailed, 0)
				continue
			}
			newPID := mgr.PID(svc.Name)
			_ = reg.UpdateStatus(svc.Name, registry.StatusRunning, newPID)
		}
	}

	log.Printf("[STARTUP] version=%s data=%s api=:%d mcp=:%d", version, dataDir, apiPort, mcpPort)

	svc := service.New(reg, mgr, prx, logDir, wtch)
	svc.StartWatchers()

	iss := issues.New(dataDir)

	mcpSrv := mcpserver.New(svc, iss, mcpPort)
	go func() {
		if err := mcpSrv.Start(); err != nil {
			log.Printf("MCP server error: %v", err)
		}
	}()

	srv := server.New(svc, iss, apiPort, version)
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
  anito install [--bin-dir <path>]          first-time daemon setup: install binary, write plist, start daemon
  anito daemon [flags]                      start the anito daemon
  anito setup [path]                        inspect repo, check service contract, write .anito/config.yaml
  anito doctor [path]                       validate .anito/config.yaml and check registry alignment
  anito deploy [config]                     build + deploy (default: .anito/config.yaml)
  anito promote <stable-config> [dev-name]  build stable binary and deploy it
  anito services                            list all running services
  anito status <name>                       show status and port for a service
  anito logs <name> [-f|--follow]           print recent log output; -f streams live (use "daemon" for the Anito daemon log)
  anito stop <name>                         stop a service
  anito restart <name>                      restart a service
  anito remove <name>                       stop and remove a service
  anito issues [--lines N] [--source pfx]  show recent logged issues (errors, failures, consumer reports)
  anito report <message> [flags]            manually report an issue (--source name, --context text, --repo path)
  anito reload                              reload the daemon with the current binary (launchd)
  anito version                             print the daemon binary version
  anito mcp                                 show MCP server connection info

Daemon flags:
  --port      management API port (default 7700)
  --mcp-port  MCP server port     (default 7701)
  --data      data directory      (default ~/.anito)

Examples:
  anito promote .anito/gomanan-mcp.yaml gomanan-mcp-dev
    → builds gomanan-daemon binary, deploys it as gomanan-mcp on :8100`)
}
