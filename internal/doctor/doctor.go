// Package doctor validates a repo's Anito configuration and checks registry
// alignment against the running daemon. It is the shared logic layer used by
// both the CLI (anito doctor) and MCP (anito_doctor) surfaces.
package doctor

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/johnnyicon/anito/internal/config"
	"github.com/johnnyicon/anito/internal/registry"
)

// Issue is a single finding from a config check.
type Issue struct {
	Severity string // "error" | "warning" | "info"
	Field    string
	Message  string
	Action   string // suggested remediation (may be empty)
}

// ConfigResult is the doctor report for one config file.
type ConfigResult struct {
	ConfigFile string  // relative path from repo root
	Name       string  // service name (empty if parse failed)
	ParseError string  // non-empty if config.Load failed
	Issues     []Issue
	Errors     int
	Warnings   int
}

// Result is the full doctor report for a repo.
type Result struct {
	RepoPath string
	Configs  []ConfigResult
	Errors   int
	Warnings int
	Healthy  bool
}

// StatusFetcher retrieves a registered service's state from the daemon.
// Decoupled so callers can pass a real client or a stub in tests.
type StatusFetcher interface {
	Status(name string) (*registry.Service, error)
}

// assetExtensions are file types that cause spurious watch-mode restarts.
var assetExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".svg": true, ".webp": true, ".ico": true, ".bmp": true,
	".woff": true, ".woff2": true, ".ttf": true, ".otf": true,
	".mp4": true, ".mov": true, ".avi": true, ".webm": true,
	".pdf": true, ".zip": true,
}

// Check runs all doctor checks for every .yaml config found in repoPath/.anito/.
// Pass a non-nil StatusFetcher to include registry alignment checks; pass nil
// to skip them (e.g. when the daemon is not running).
func Check(repoPath string, svc StatusFetcher) (*Result, error) {
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, fmt.Errorf("resolving path: %w", err)
	}

	anitoDir := filepath.Join(abs, ".anito")
	entries, err := os.ReadDir(anitoDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no .anito/ directory found at %s — run `anito setup` to initialise", abs)
		}
		return nil, err
	}

	var configFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
			configFiles = append(configFiles, filepath.Join(anitoDir, e.Name()))
		}
	}
	if len(configFiles) == 0 {
		return nil, fmt.Errorf("no .yaml config files found in %s — run `anito setup` to generate one", anitoDir)
	}

	result := &Result{RepoPath: abs}
	for _, cfgPath := range configFiles {
		rel, _ := filepath.Rel(abs, cfgPath)
		cr := checkConfig(cfgPath, rel, abs, svc)
		result.Configs = append(result.Configs, cr)
		result.Errors += cr.Errors
		result.Warnings += cr.Warnings
	}
	result.Healthy = result.Errors == 0

	return result, nil
}

func checkConfig(cfgPath, relPath, repoRoot string, svc StatusFetcher) ConfigResult {
	cr := ConfigResult{ConfigFile: relPath}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		cr.ParseError = err.Error()
		cr.Errors++
		return cr
	}
	cr.Name = cfg.Name

	// Output file existence.
	absOutput := cfg.Output
	if !filepath.IsAbs(absOutput) {
		absOutput = filepath.Join(repoRoot, absOutput)
	}
	if _, err := os.Stat(absOutput); os.IsNotExist(err) {
		sev, action := "error", "build the binary and run `anito deploy`"
		if cfg.Build != "" {
			sev = "warning"
			action = "run `anito deploy` — it will execute the build command automatically"
		}
		cr.add(Issue{Severity: sev, Field: "output",
			Message: fmt.Sprintf("file not found: %s", cfg.Output),
			Action:  action,
		})
	}

	// Valid type.
	if cfg.Type != "binary" && cfg.Type != "static" {
		cr.add(Issue{Severity: "error", Field: "type",
			Message: fmt.Sprintf("unknown type %q — must be \"binary\" or \"static\"", cfg.Type),
		})
	}

	// Valid restart_policy.
	switch cfg.RestartPolicy {
	case "always", "on-watch", "never", "":
	default:
		cr.add(Issue{Severity: "error", Field: "restart_policy",
			Message: fmt.Sprintf("unknown value %q — must be \"always\", \"on-watch\", or \"never\"", cfg.RestartPolicy),
		})
	}

	// Drain window sanity (bare integers in YAML are treated as nanoseconds by Go).
	if cfg.DrainWindow > 5*time.Minute {
		cr.add(Issue{Severity: "warning", Field: "drain_window",
			Message: fmt.Sprintf("value is %v — this seems very large", cfg.DrainWindow),
			Action:  "use a duration string like \"3s\" or \"500ms\" in config.yaml",
		})
	}

	// Watch path checks.
	for _, wp := range cfg.Watch {
		info, err := os.Stat(wp)
		if os.IsNotExist(err) {
			cr.add(Issue{Severity: "warning", Field: "watch",
				Message: fmt.Sprintf("path does not exist: %s", wp),
				Action:  "remove or correct this path — missing watch paths are silently ignored",
			})
			continue
		}
		if err != nil {
			continue
		}
		if !info.IsDir() {
			cr.add(Issue{Severity: "warning", Field: "watch",
				Message: fmt.Sprintf("watch path is a file, not a directory: %s", wp),
				Action:  "watch paths must be directories",
			})
			continue
		}
		exts, count := findAssets(wp)
		if count > 0 {
			rel, _ := filepath.Rel(repoRoot, wp)
			cr.add(Issue{Severity: "warning", Field: "watch",
				Message: fmt.Sprintf("%s contains %d asset file(s) (%s) — these trigger spurious restarts",
					rel, count, strings.Join(exts, ", ")),
				Action: "narrow the watch path to source directories only (e.g. ./src, ./cmd)",
			})
		}
	}

	// Port conflict check — verify nothing else is holding the stable port on
	// any loopback interface. Anito binds both 127.0.0.1 and [::1]; if a third
	// process grabbed either side first it will shadow Anito for IPv6 clients.
	if cfg.Port != 0 {
		if conflict := detectPortConflict(cfg.Port); conflict != "" {
			cr.add(Issue{Severity: "error", Field: "port",
				Message: fmt.Sprintf("port %d has a competing listener: %s", cfg.Port, conflict),
				Action:  "another process is holding this port — find and stop it, then restart the service",
			})
		}
	}

	// Registry alignment.
	if svc != nil {
		reg, err := svc.Status(cfg.Name)
		if err == nil {
			if cfg.Port != 0 && reg.StablePort != cfg.Port {
				cr.add(Issue{Severity: "warning", Field: "port",
					Message: fmt.Sprintf("config says port %d but service is registered on port %d",
						cfg.Port, reg.StablePort),
					Action: fmt.Sprintf("registry is source of truth — update config.yaml port to %d, "+
						"or remove the port field", reg.StablePort),
				})
			}
			if absOutput != "" && reg.BinaryPath != absOutput {
				cr.add(Issue{Severity: "info", Field: "output",
					Message: fmt.Sprintf("registered binary differs from config output "+
						"(registered: %s, config: %s)", reg.BinaryPath, absOutput),
					Action: "expected if deploying from a worktree; redeploy to update",
				})
			}
			if reg.Status == registry.StatusFailed {
				cr.add(Issue{Severity: "warning", Field: "status",
					Message: "service is in failed state",
					Action:  fmt.Sprintf("run `anito restart %s` or `anito deploy` to recover", cfg.Name),
				})
			}
		}
	}

	return cr
}

func (cr *ConfigResult) add(iss Issue) {
	cr.Issues = append(cr.Issues, iss)
	switch iss.Severity {
	case "error":
		cr.Errors++
	case "warning":
		cr.Warnings++
	}
}

// detectPortConflict checks whether anything other than Anito's own proxy is
// holding the given port on any loopback interface. It probes both 127.0.0.1
// and [::1] and returns a description of the conflict, or "" if clean.
// Anito's proxy sets X-Anito-Proxy: 1 on every response — any listener
// missing that header is a foreign process.
func detectPortConflict(port int) string {
	addrs := []string{
		fmt.Sprintf("127.0.0.1:%d", port),
		fmt.Sprintf("[::1]:%d", port),
	}
	client := &http.Client{Timeout: 300 * time.Millisecond}
	for _, addr := range addrs {
		// Quick TCP probe first — skip if nothing is listening.
		conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
		if err != nil {
			continue
		}
		_ = conn.Close()

		// Something is listening. Check for Anito's fingerprint header.
		resp, err := client.Get(fmt.Sprintf("http://%s/", addr))
		if err != nil {
			return fmt.Sprintf("non-HTTP process on %s", addr)
		}
		_ = resp.Body.Close()
		if resp.Header.Get("X-Anito-Proxy") != "1" {
			return fmt.Sprintf("foreign process on %s (HTTP %d, no Anito header)", addr, resp.StatusCode)
		}
	}
	return ""
}

// findAssets walks dir and returns unique asset extensions found and total count.
// Caps at 2000 files to stay fast on large trees.
func findAssets(dir string) (exts []string, count int) {
	seen := map[string]bool{}
	walked := 0
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		walked++
		if walked > 2000 {
			return filepath.SkipAll
		}
		ext := strings.ToLower(filepath.Ext(path))
		if assetExtensions[ext] {
			count++
			seen[ext] = true
		}
		return nil
	})
	for ext := range seen {
		exts = append(exts, ext)
	}
	return exts, count
}
