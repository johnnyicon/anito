package main

import (
	"fmt"
	"net/http"
	"os"
	"text/tabwriter"
	"time"

	"github.com/johnnyicon/anito/internal/client"
	"github.com/johnnyicon/anito/internal/doctor"
)

func runDoctor(cli *client.Client, repoPath string) {
	var svc doctor.StatusFetcher
	if isDaemonReachable() {
		svc = cli
	}

	result, err := doctor.Check(repoPath, svc)
	if err != nil {
		fatal(err)
	}

	for i, cr := range result.Configs {
		if i > 0 {
			fmt.Println()
		}
		printConfigResult(cr, svc != nil)
	}

	fmt.Println()
	if result.Errors == 0 && result.Warnings == 0 {
		fmt.Println("✓ all checks passed")
	} else if result.Errors > 0 {
		fmt.Printf("%d error(s), %d warning(s)\n", result.Errors, result.Warnings)
	} else {
		fmt.Printf("0 errors, %d warning(s)\n", result.Warnings)
	}

	if result.Errors > 0 {
		os.Exit(1)
	}
}

func printConfigResult(cr doctor.ConfigResult, daemonReachable bool) {
	fmt.Printf("anito doctor — %s\n\n", cr.ConfigFile)

	if cr.ParseError != "" {
		fmt.Printf("  ✗ could not parse config: %s\n", cr.ParseError)
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "  name:\t%s\n", cr.Name)
	_ = w.Flush()

	if len(cr.Issues) == 0 {
		fmt.Println("  ✓ no issues found")
		return
	}

	for _, iss := range cr.Issues {
		switch iss.Severity {
		case "error":
			fmt.Printf("  ✗ [error] %s: %s\n", iss.Field, iss.Message)
		case "warning":
			fmt.Printf("  ⚠ [warn]  %s: %s\n", iss.Field, iss.Message)
		case "info":
			fmt.Printf("  ℹ [info]  %s: %s\n", iss.Field, iss.Message)
		}
		if iss.Action != "" {
			fmt.Printf("    → %s\n", iss.Action)
		}
	}
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
