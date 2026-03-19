package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeConfig writes content to <dir>/config.yaml and returns the full path.
func writeConfig(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeConfig: %v", err)
	}
	return path
}

// TestRelativeWatchPathsResolvedAgainstConfigDir verifies that a relative watch
// path like "./src" in a config at /tmp/svc/.anito/config.yaml resolves to
// /tmp/svc/.anito/src.
func TestRelativeWatchPathsResolvedAgainstConfigDir(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, `
name: my-svc
output: ./bin/my-svc
watch:
  - ./src
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Watch) != 1 {
		t.Fatalf("Watch len = %d, want 1", len(cfg.Watch))
	}
	want := filepath.Join(dir, "src")
	if cfg.Watch[0] != want {
		t.Errorf("Watch[0] = %q, want %q", cfg.Watch[0], want)
	}
}

// TestAbsoluteWatchPathsUnchanged verifies that an absolute watch path passes
// through Load unchanged.
func TestAbsoluteWatchPathsUnchanged(t *testing.T) {
	dir := t.TempDir()
	absPath := "/abs/path/to/src"
	path := writeConfig(t, dir, "name: svc\noutput: ./bin/svc\nwatch:\n  - "+absPath+"\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Watch) != 1 {
		t.Fatalf("Watch len = %d, want 1", len(cfg.Watch))
	}
	if cfg.Watch[0] != absPath {
		t.Errorf("Watch[0] = %q, want %q", cfg.Watch[0], absPath)
	}
}

// TestRestartPolicyDefault verifies that omitting restart_policy yields "on-watch".
func TestRestartPolicyDefault(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "name: svc\noutput: ./bin/svc\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RestartPolicy != "on-watch" {
		t.Errorf("RestartPolicy = %q, want \"on-watch\"", cfg.RestartPolicy)
	}
}

// TestRestartPolicyValidation verifies that an invalid restart_policy returns
// an error containing "restart_policy".
func TestRestartPolicyValidation(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "name: svc\noutput: ./bin/svc\nrestart_policy: bad-value\n")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid restart_policy, got nil")
	}
	if !strings.Contains(err.Error(), "restart_policy") {
		t.Errorf("error %q does not mention restart_policy", err.Error())
	}
}

// TestHealthCheckDefault verifies that omitting health_check yields "/health".
func TestHealthCheckDefault(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "name: svc\noutput: ./bin/svc\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HealthCheck != "/health" {
		t.Errorf("HealthCheck = %q, want \"/health\"", cfg.HealthCheck)
	}
}

// TestHealthCheckTimeoutParsed verifies that health_check_timeout: 30s is
// parsed as 30 * time.Second.
func TestHealthCheckTimeoutParsed(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "name: svc\noutput: ./bin/svc\nhealth_check_timeout: 30s\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := 30 * time.Second
	if cfg.HealthCheckTimeout != want {
		t.Errorf("HealthCheckTimeout = %v, want %v", cfg.HealthCheckTimeout, want)
	}
}

// TestRequiresName verifies that a config without a name field returns an error.
func TestRequiresName(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "output: ./bin/svc\n")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing name, got nil")
	}
}

// TestRequiresOutput verifies that a config without an output field returns an error.
func TestRequiresOutput(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "name: svc\n")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing output, got nil")
	}
}
