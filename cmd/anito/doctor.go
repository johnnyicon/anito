package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/johnnyicon/anito/internal/client"
	"github.com/johnnyicon/anito/internal/config"
)

// assetExtensions are file types that cause spurious watch-mode restarts when
// included in a watch path.
var assetExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".svg": true, ".webp": true, ".ico": true, ".bmp": true,
	".woff": true, ".woff2": true, ".ttf": true, ".otf": true,
	".mp4": true, ".mov": true, ".avi": true, ".webm": true,
	".pdf": true, ".zip": true,
}

type issue struct {
	severity string // "error" | "warning" | "info"
	field    string
	message  string
	action   string // what the developer should do
}

func runDoctor(cli *client.Client, repoPath string) {
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		fatal(fmt.Errorf("resolving path: %w", err))
	}

	// Collect all .yaml config files in .anito/
	anitoDir := filepath.Join(abs, ".anito")
	entries, err := os.ReadDir(anitoDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("✗ no .anito/ directory found at %s\n", abs)
			fmt.Println("  Run `anito setup` to initialise this repo.")
			os.Exit(1)
		}
		fatal(err)
	}

	var configFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
			configFiles = append(configFiles, filepath.Join(anitoDir, e.Name()))
		}
	}
	if len(configFiles) == 0 {
		fmt.Printf("✗ no .yaml config files found in %s\n", anitoDir)
		fmt.Println("  Run `anito setup` to generate a config.")
		os.Exit(1)
	}

	// Check whether the daemon is reachable for registry alignment checks.
	daemonReachable := isDaemonReachable()

	totalErrors := 0
	totalWarnings := 0

	for i, cfgPath := range configFiles {
		if i > 0 {
			fmt.Println()
		}
		e, w := checkConfig(cli, cfgPath, abs, daemonReachable)
		totalErrors += e
		totalWarnings += w
	}

	fmt.Println()
	if totalErrors == 0 && totalWarnings == 0 {
		fmt.Println("✓ all checks passed")
	} else {
		if totalErrors > 0 {
			fmt.Printf("%d error(s), %d warning(s)\n", totalErrors, totalWarnings)
		} else {
			fmt.Printf("0 errors, %d warning(s)\n", totalWarnings)
		}
	}

	if totalErrors > 0 {
		os.Exit(1)
	}
}

// checkConfig runs all checks for one config file and prints a report.
// Returns the number of errors and warnings found.
func checkConfig(cli *client.Client, cfgPath, repoRoot string, daemonReachable bool) (errors, warnings int) {
	rel, _ := filepath.Rel(repoRoot, cfgPath)
	fmt.Printf("anito doctor — %s\n\n", rel)

	var issues []issue

	// --- Parse config ---
	cfg, parseErr := config.Load(cfgPath)
	if parseErr != nil {
		fmt.Printf("  ✗ could not parse config: %v\n", parseErr)
		return 1, 0
	}

	// --- Schema checks ---

	// Output file must exist. Paths in config.yaml are resolved relative to the
	// repo root (where `anito deploy` is run from), not the config file's dir.
	absOutput := cfg.Output
	if !filepath.IsAbs(absOutput) {
		absOutput = filepath.Join(repoRoot, absOutput)
	}
	if _, err := os.Stat(absOutput); os.IsNotExist(err) {
		sev := "error"
		action := "build the binary and run `anito deploy`"
		if cfg.Build != "" {
			sev = "warning"
			action = "run `anito deploy` — it will execute the build command automatically"
		}
		issues = append(issues, issue{
			severity: sev,
			field:    "output",
			message:  fmt.Sprintf("file not found: %s", cfg.Output),
			action:   action,
		})
	}

	// Valid type.
	if cfg.Type != "binary" && cfg.Type != "static" {
		issues = append(issues, issue{
			severity: "error",
			field:    "type",
			message:  fmt.Sprintf("unknown type %q — must be \"binary\" or \"static\"", cfg.Type),
		})
	}

	// Valid restart_policy.
	switch cfg.RestartPolicy {
	case "always", "on-watch", "never", "":
	default:
		issues = append(issues, issue{
			severity: "error",
			field:    "restart_policy",
			message:  fmt.Sprintf("unknown value %q — must be \"always\", \"on-watch\", or \"never\"", cfg.RestartPolicy),
		})
	}

	// --- Watch path checks ---
	for _, wp := range cfg.Watch {
		info, err := os.Stat(wp)
		if os.IsNotExist(err) {
			issues = append(issues, issue{
				severity: "warning",
				field:    "watch",
				message:  fmt.Sprintf("path does not exist: %s", wp),
				action:   "remove or correct this watch path; missing paths are silently ignored by the watcher",
			})
			continue
		}
		if err != nil {
			continue
		}
		if !info.IsDir() {
			issues = append(issues, issue{
				severity: "warning",
				field:    "watch",
				message:  fmt.Sprintf("watch path is a file, not a directory: %s", wp),
				action:   "watch paths must be directories",
			})
			continue
		}

		// Walk for asset files.
		assetExts, assetCount := findAssets(wp)
		if assetCount > 0 {
			extList := strings.Join(assetExts, ", ")
			issues = append(issues, issue{
				severity: "warning",
				field:    "watch",
				message: fmt.Sprintf(
					"watch path %s contains %d asset file(s) (%s) — these trigger spurious restarts",
					shortPath(wp, repoRoot), assetCount, extList,
				),
				action: "narrow the watch path to source directories only (e.g. ./src, ./cmd) to exclude asset trees",
			})
		}
	}

	// --- Drain window sanity check ---
	// Flag suspiciously large values that suggest the user may have entered
	// a value in milliseconds or seconds instead of a proper duration.
	// (config.yaml uses time.Duration which yaml.v3 parses from strings like
	// "3s" correctly — a bare integer is treated as nanoseconds by Go.)
	if cfg.DrainWindow > 5*time.Minute {
		issues = append(issues, issue{
			severity: "warning",
			field:    "drain_window",
			message:  fmt.Sprintf("drain_window is %v — this seems very large", cfg.DrainWindow),
			action:   "use a duration string like \"3s\" or \"500ms\" in config.yaml",
		})
	}

	// --- Registry alignment (requires daemon) ---
	if daemonReachable {
		svc, err := cli.Status(cfg.Name)
		if err == nil {
			// Port mismatch: config says X, registry says Y.
			if cfg.Port != 0 && svc.StablePort != cfg.Port {
				issues = append(issues, issue{
					severity: "warning",
					field:    "port",
					message: fmt.Sprintf(
						"config says port %d but service is registered on port %d",
						cfg.Port, svc.StablePort,
					),
					action: fmt.Sprintf(
						"the registry is source of truth for ports — update config.yaml port to %d, "+
							"or remove the port field to accept the assigned port",
						svc.StablePort,
					),
				})
			}

			// Binary path mismatch: registry has a different path than config output.
			if absOutput != "" && svc.BinaryPath != absOutput {
				issues = append(issues, issue{
					severity: "info",
					field:    "output",
					message: fmt.Sprintf(
						"registered binary: %s\n    config output:     %s",
						svc.BinaryPath, absOutput,
					),
					action: "paths differ — this is expected if you deploy from different worktrees; redeploy to update",
				})
			}

			// Status check.
			if svc.Status == "failed" {
				issues = append(issues, issue{
					severity: "warning",
					field:    "status",
					message:  "service is in failed state",
					action:   "run `anito restart " + cfg.Name + "` or `anito deploy` to recover",
				})
			}
		}
	}

	// --- Print results ---
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "  name:\t%s\n", cfg.Name)
	if cfg.Build != "" {
		fmt.Fprintf(w, "  build:\t%s\n", cfg.Build)
	}
	fmt.Fprintf(w, "  output:\t%s\n", cfg.Output)
	if cfg.Port != 0 {
		fmt.Fprintf(w, "  port:\t%d\n", cfg.Port)
	} else {
		fmt.Fprintf(w, "  port:\tauto-allocated\n")
	}
	fmt.Fprintf(w, "  type:\t%s\n", cfg.Type)
	fmt.Fprintf(w, "  restart_policy:\t%s\n", cfg.RestartPolicy)
	if len(cfg.Watch) > 0 {
		fmt.Fprintf(w, "  watch:\t%s", cfg.Watch[0])
		for _, wp := range cfg.Watch[1:] {
			fmt.Fprintf(w, "\n       \t%s", wp)
		}
		fmt.Fprintln(w)
	}
	if daemonReachable {
		fmt.Fprintf(w, "  daemon:\treachable\n")
	} else {
		fmt.Fprintf(w, "  daemon:\tnot running (registry checks skipped)\n")
	}
	_ = w.Flush()
	fmt.Println()

	if len(issues) == 0 {
		fmt.Println("  ✓ no issues found")
		return 0, 0
	}

	for _, iss := range issues {
		switch iss.severity {
		case "error":
			fmt.Printf("  ✗ [error] %s: %s\n", iss.field, iss.message)
			errors++
		case "warning":
			fmt.Printf("  ⚠ [warn]  %s: %s\n", iss.field, iss.message)
			warnings++
		case "info":
			fmt.Printf("  ℹ [info]  %s: %s\n", iss.field, iss.message)
		}
		if iss.action != "" {
			fmt.Printf("    → %s\n", iss.action)
		}
	}

	return errors, warnings
}

// findAssets walks a directory and returns the unique asset extensions found
// and a total count of asset files. Caps the walk at 2000 files to stay fast.
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

// shortPath returns path relative to base if possible, otherwise absolute.
func shortPath(path, base string) string {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return path
	}
	return rel
}

// isDaemonReachable returns true if the Anito management API is up.
func isDaemonReachable() bool {
	c := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := c.Get(fmt.Sprintf("http://localhost:%d/health", defaultDaemonPort))
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == 200
}
