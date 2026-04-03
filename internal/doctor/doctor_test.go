package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/johnnyicon/anito/internal/registry"
)

// mockStatusFetcher implements StatusFetcher for tests.
type mockStatusFetcher struct {
	services map[string]*registry.Service
	err      error // returned for any name not in the map
}

func (m *mockStatusFetcher) Status(name string) (*registry.Service, error) {
	if svc, ok := m.services[name]; ok {
		return svc, nil
	}
	if m.err != nil {
		return nil, m.err
	}
	return nil, fmt.Errorf("service %q not found", name)
}

// writeConfig writes a YAML config file into dir/.anito/<filename>.
func writeConfig(t *testing.T, dir, filename, content string) string {
	t.Helper()
	anitoDir := filepath.Join(dir, ".anito")
	if err := os.MkdirAll(anitoDir, 0o755); err != nil {
		t.Fatalf("creating .anito dir: %v", err)
	}
	path := filepath.Join(anitoDir, filename)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing config %s: %v", filename, err)
	}
	return path
}

// minimalConfig returns a valid minimal YAML config that will pass config.Load.
func minimalConfig(name, output string) string {
	return fmt.Sprintf("name: %s\nport: 3000\ntype: binary\noutput: %s\nhealth_check: /health\n", name, output)
}

// ---------- Check: .anito/ directory missing ----------

func TestCheck_NoAnitoDir(t *testing.T) {
	dir := t.TempDir()
	_, err := Check(dir, nil)
	if err == nil {
		t.Fatal("expected error when .anito/ does not exist")
	}
	if got := err.Error(); !contains(got, "no .anito/ directory found") {
		t.Errorf("unexpected error message: %s", got)
	}
}

// ---------- Check: .anito/ exists but no YAML files ----------

func TestCheck_NoYAMLFiles(t *testing.T) {
	dir := t.TempDir()
	anitoDir := filepath.Join(dir, ".anito")
	if err := os.MkdirAll(anitoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write a non-yaml file so the directory is not empty.
	if err := os.WriteFile(filepath.Join(anitoDir, "readme.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Check(dir, nil)
	if err == nil {
		t.Fatal("expected error when no .yaml files exist")
	}
	if got := err.Error(); !contains(got, "no .yaml config files found") {
		t.Errorf("unexpected error message: %s", got)
	}
}

// ---------- Check: valid config, result populated ----------

func TestCheck_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	// Create the output binary so the output-missing check does not fire.
	binPath := filepath.Join(dir, "test-binary")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, dir, "config.yaml", minimalConfig("test-svc", binPath))

	result, err := Check(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.Configs) != 1 {
		t.Fatalf("expected 1 config result, got %d", len(result.Configs))
	}
	if result.Configs[0].Name != "test-svc" {
		t.Errorf("expected name test-svc, got %s", result.Configs[0].Name)
	}
	if !result.Healthy {
		t.Errorf("expected healthy=true, got false; errors=%d issues=%v",
			result.Errors, result.Configs[0].Issues)
	}
}

// ---------- Check: config parse error ----------

func TestCheck_ConfigParseError(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "bad.yaml", "{{not valid yaml}}")

	result, err := Check(dir, nil)
	if err != nil {
		t.Fatalf("Check should not return error for parse failures; got: %v", err)
	}
	if len(result.Configs) != 1 {
		t.Fatalf("expected 1 config result, got %d", len(result.Configs))
	}
	cr := result.Configs[0]
	if cr.ParseError == "" {
		t.Error("expected ParseError to be set")
	}
	if cr.Errors != 1 {
		t.Errorf("expected 1 error for parse failure, got %d", cr.Errors)
	}
	if result.Healthy {
		t.Error("expected healthy=false when there are parse errors")
	}
}

// ---------- Check: nil StatusFetcher skips registry alignment ----------

func TestCheck_NilStatusFetcher_SkipsRegistry(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "svc-bin")
	if err := os.WriteFile(binPath, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, dir, "config.yaml", minimalConfig("test-svc", binPath))

	result, err := Check(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With nil StatusFetcher and a valid config whose output exists, there
	// should be no issues at all (no registry-related warnings).
	for _, iss := range result.Configs[0].Issues {
		if iss.Field == "port" && contains(iss.Message, "config says port") {
			t.Errorf("registry alignment check ran with nil StatusFetcher: %v", iss)
		}
		if iss.Field == "status" {
			t.Errorf("registry status check ran with nil StatusFetcher: %v", iss)
		}
	}
}

// ---------- checkConfig: missing output (no build command -> error) ----------

func TestCheckConfig_MissingOutput_NoBuild(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, "config.yaml", minimalConfig("svc", "./nonexistent"))

	cr := checkConfig(cfgPath, ".anito/config.yaml", dir, nil)
	found := findIssue(cr.Issues, "output", "error")
	if found == nil {
		t.Fatal("expected error-level issue for missing output without build command")
	}
	if !contains(found.Action, "build the binary") {
		t.Errorf("unexpected action: %s", found.Action)
	}
}

// ---------- checkConfig: missing output (has build command -> warning) ----------

func TestCheckConfig_MissingOutput_WithBuild(t *testing.T) {
	dir := t.TempDir()
	cfg := "name: svc\nport: 3000\ntype: binary\noutput: ./nonexistent\nbuild: go build -o ./nonexistent\nhealth_check: /health\n"
	cfgPath := writeConfig(t, dir, "config.yaml", cfg)

	cr := checkConfig(cfgPath, ".anito/config.yaml", dir, nil)
	found := findIssue(cr.Issues, "output", "warning")
	if found == nil {
		t.Fatal("expected warning-level issue for missing output with build command")
	}
	// Should not be an error.
	errFound := findIssue(cr.Issues, "output", "error")
	if errFound != nil {
		t.Error("should be a warning, not an error, when build command exists")
	}
}

// ---------- checkConfig: missing env_file ----------

func TestCheckConfig_MissingEnvFile(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "svc-bin")
	if err := os.WriteFile(binPath, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf("name: svc\nport: 3000\ntype: binary\noutput: %s\nenv_file: ./missing.env\nhealth_check: /health\n", binPath)
	cfgPath := writeConfig(t, dir, "config.yaml", cfg)

	cr := checkConfig(cfgPath, ".anito/config.yaml", dir, nil)
	found := findIssue(cr.Issues, "env_file", "error")
	if found == nil {
		t.Fatal("expected error for missing env_file")
	}
}

// ---------- checkConfig: invalid type ----------

func TestCheckConfig_InvalidType(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "svc-bin")
	if err := os.WriteFile(binPath, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	// config.Load defaults empty type to "binary", so we must test the
	// doctor's own check. config.Load does NOT reject invalid types -- it only
	// defaults empty. So "docker" would pass Load and hit the doctor check.
	cfg := fmt.Sprintf("name: svc\nport: 3000\ntype: docker\noutput: %s\nhealth_check: /health\n", binPath)
	cfgPath := writeConfig(t, dir, "config.yaml", cfg)

	cr := checkConfig(cfgPath, ".anito/config.yaml", dir, nil)
	found := findIssue(cr.Issues, "type", "error")
	if found == nil {
		t.Fatal("expected error for invalid type")
	}
	if !contains(found.Message, "docker") {
		t.Errorf("expected message to mention the invalid type, got: %s", found.Message)
	}
}

// ---------- checkConfig: invalid restart_policy ----------

func TestCheckConfig_InvalidRestartPolicy(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "svc-bin")
	if err := os.WriteFile(binPath, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	// config.Load validates restart_policy and returns an error for unknown values.
	// So an invalid restart_policy causes a ParseError in checkConfig.
	cfg := fmt.Sprintf("name: svc\nport: 3000\ntype: binary\noutput: %s\nrestart_policy: bogus\nhealth_check: /health\n", binPath)
	cfgPath := writeConfig(t, dir, "config.yaml", cfg)

	cr := checkConfig(cfgPath, ".anito/config.yaml", dir, nil)
	if cr.ParseError == "" {
		// If config.Load passes it through, doctor should catch it.
		found := findIssue(cr.Issues, "restart_policy", "error")
		if found == nil {
			t.Fatal("expected error for invalid restart_policy")
		}
	}
	// Either way there must be at least 1 error.
	if cr.Errors < 1 {
		t.Error("expected at least 1 error for invalid restart_policy")
	}
}

// ---------- checkConfig: suspicious drain_window ----------

func TestCheckConfig_LargeDrainWindow(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "svc-bin")
	if err := os.WriteFile(binPath, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 10 minutes expressed as a duration string.
	cfg := fmt.Sprintf("name: svc\nport: 3000\ntype: binary\noutput: %s\ndrain_window: 10m\nhealth_check: /health\n", binPath)
	cfgPath := writeConfig(t, dir, "config.yaml", cfg)

	cr := checkConfig(cfgPath, ".anito/config.yaml", dir, nil)
	found := findIssue(cr.Issues, "drain_window", "warning")
	if found == nil {
		t.Fatal("expected warning for drain_window > 5 minutes")
	}
}

// ---------- checkConfig: normal drain_window is fine ----------

func TestCheckConfig_NormalDrainWindow_NoWarning(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "svc-bin")
	if err := os.WriteFile(binPath, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf("name: svc\nport: 3000\ntype: binary\noutput: %s\ndrain_window: 3s\nhealth_check: /health\n", binPath)
	cfgPath := writeConfig(t, dir, "config.yaml", cfg)

	cr := checkConfig(cfgPath, ".anito/config.yaml", dir, nil)
	found := findIssue(cr.Issues, "drain_window", "warning")
	if found != nil {
		t.Errorf("did not expect drain_window warning for 3s, got: %v", found)
	}
}

// ---------- checkConfig: missing watch paths ----------

func TestCheckConfig_MissingWatchPaths(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "svc-bin")
	if err := os.WriteFile(binPath, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf("name: svc\nport: 3000\ntype: binary\noutput: %s\nwatch:\n  - ./nonexistent-dir\nhealth_check: /health\n", binPath)
	cfgPath := writeConfig(t, dir, "config.yaml", cfg)

	cr := checkConfig(cfgPath, ".anito/config.yaml", dir, nil)
	found := findIssue(cr.Issues, "watch", "warning")
	if found == nil {
		t.Fatal("expected warning for non-existent watch path")
	}
	if !contains(found.Message, "does not exist") {
		t.Errorf("unexpected message: %s", found.Message)
	}
}

// ---------- checkConfig: watch path is a file, not a directory ----------

func TestCheckConfig_WatchPathIsFile(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "svc-bin")
	if err := os.WriteFile(binPath, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Create a file that will be used as a watch path.
	watchFile := filepath.Join(dir, "some-file.go")
	if err := os.WriteFile(watchFile, []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Use absolute path so config.Load does not re-resolve it.
	cfg := fmt.Sprintf("name: svc\nport: 3000\ntype: binary\noutput: %s\nwatch:\n  - %s\nhealth_check: /health\n", binPath, watchFile)
	cfgPath := writeConfig(t, dir, "config.yaml", cfg)

	cr := checkConfig(cfgPath, ".anito/config.yaml", dir, nil)
	found := findIssue(cr.Issues, "watch", "warning")
	if found == nil {
		t.Fatal("expected warning for watch path that is a file")
	}
	if !contains(found.Message, "file, not a directory") {
		t.Errorf("unexpected message: %s", found.Message)
	}
}

// ---------- checkConfig: watch path contains asset files ----------

func TestCheckConfig_WatchPathWithAssets(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "svc-bin")
	if err := os.WriteFile(binPath, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Create a watch directory with some asset files.
	watchDir := filepath.Join(dir, "assets")
	if err := os.MkdirAll(watchDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"logo.png", "icon.svg", "photo.jpg"} {
		if err := os.WriteFile(filepath.Join(watchDir, name), []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := fmt.Sprintf("name: svc\nport: 3000\ntype: binary\noutput: %s\nwatch:\n  - %s\nhealth_check: /health\n", binPath, watchDir)
	cfgPath := writeConfig(t, dir, "config.yaml", cfg)

	cr := checkConfig(cfgPath, ".anito/config.yaml", dir, nil)
	found := findIssue(cr.Issues, "watch", "warning")
	if found == nil {
		t.Fatal("expected warning for watch path with asset files")
	}
	if !contains(found.Message, "asset file") {
		t.Errorf("expected message to mention asset files, got: %s", found.Message)
	}
}

// ---------- isWorktreePath ----------

func TestIsWorktreePath_True(t *testing.T) {
	cases := []string{
		"/Users/dev/project/.claude/worktrees/agent-abc123/foo",
		"/home/user/repo/worktrees/feature-x/.anito/config.yaml",
		"/worktrees/something",
	}
	for _, p := range cases {
		if !isWorktreePath(p) {
			t.Errorf("expected isWorktreePath(%q) = true", p)
		}
	}
}

func TestIsWorktreePath_False(t *testing.T) {
	cases := []string{
		"/Users/dev/project/src/main.go",
		"/home/user/repo/.anito/config.yaml",
		"/tmp/worktree-stuff/config.yaml", // no "/worktrees/"
		"",
	}
	for _, p := range cases {
		if isWorktreePath(p) {
			t.Errorf("expected isWorktreePath(%q) = false", p)
		}
	}
}

// ---------- findAssets ----------

func TestFindAssets_WithAssets(t *testing.T) {
	dir := t.TempDir()
	files := map[string]bool{
		"logo.png":   true,
		"icon.svg":   true,
		"font.woff2": true,
		"main.go":    false,
		"readme.md":  false,
		"style.css":  false,
	}
	for name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	exts, count := findAssets(dir)
	if count != 3 {
		t.Errorf("expected 3 asset files, got %d", count)
	}
	if len(exts) != 3 {
		t.Errorf("expected 3 unique extensions, got %d: %v", len(exts), exts)
	}
	// Verify the extensions are the expected ones.
	extSet := map[string]bool{}
	for _, e := range exts {
		extSet[e] = true
	}
	for _, want := range []string{".png", ".svg", ".woff2"} {
		if !extSet[want] {
			t.Errorf("expected extension %s in result, got %v", want, exts)
		}
	}
}

func TestFindAssets_NoAssets(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"main.go", "util.go", "README.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	exts, count := findAssets(dir)
	if count != 0 {
		t.Errorf("expected 0 asset files, got %d", count)
	}
	if len(exts) != 0 {
		t.Errorf("expected 0 extensions, got %v", exts)
	}
}

func TestFindAssets_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	exts, count := findAssets(dir)
	if count != 0 {
		t.Errorf("expected 0 count for empty dir, got %d", count)
	}
	if len(exts) != 0 {
		t.Errorf("expected no extensions for empty dir, got %v", exts)
	}
}

func TestFindAssets_Subdirectories(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "images")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "photo.jpeg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	exts, count := findAssets(dir)
	if count != 1 {
		t.Errorf("expected 1 asset in subdirectory, got %d", count)
	}
	if len(exts) != 1 || exts[0] != ".jpeg" {
		t.Errorf("expected [.jpeg], got %v", exts)
	}
}

// ---------- Registry alignment: port mismatch ----------

func TestCheckConfig_RegistryPortMismatch(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "svc-bin")
	if err := os.WriteFile(binPath, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeConfig(t, dir, "config.yaml", minimalConfig("test-svc", binPath))

	fetcher := &mockStatusFetcher{
		services: map[string]*registry.Service{
			"test-svc": {
				Name:       "test-svc",
				StablePort: 4000, // config says 3000
				BinaryPath: binPath,
				Status:     registry.StatusRunning,
				ConfigPath: cfgPath,
			},
		},
	}

	cr := checkConfig(cfgPath, ".anito/config.yaml", dir, fetcher)
	found := findIssue(cr.Issues, "port", "warning")
	if found == nil {
		t.Fatal("expected warning for port mismatch between config and registry")
	}
	if !contains(found.Message, "3000") || !contains(found.Message, "4000") {
		t.Errorf("expected message to mention both ports, got: %s", found.Message)
	}
}

// ---------- Registry alignment: failed status ----------

func TestCheckConfig_RegistryFailedStatus(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "svc-bin")
	if err := os.WriteFile(binPath, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeConfig(t, dir, "config.yaml", minimalConfig("fail-svc", binPath))

	fetcher := &mockStatusFetcher{
		services: map[string]*registry.Service{
			"fail-svc": {
				Name:       "fail-svc",
				StablePort: 3000,
				BinaryPath: binPath,
				Status:     registry.StatusFailed,
				ConfigPath: cfgPath,
			},
		},
	}

	cr := checkConfig(cfgPath, ".anito/config.yaml", dir, fetcher)
	found := findIssue(cr.Issues, "status", "warning")
	if found == nil {
		t.Fatal("expected warning for failed status")
	}
	if !contains(found.Message, "failed state") {
		t.Errorf("unexpected message: %s", found.Message)
	}
}

// ---------- Registry alignment: orphaned status ----------

func TestCheckConfig_RegistryOrphanedStatus(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "svc-bin")
	if err := os.WriteFile(binPath, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeConfig(t, dir, "config.yaml", minimalConfig("orphan-svc", binPath))

	fetcher := &mockStatusFetcher{
		services: map[string]*registry.Service{
			"orphan-svc": {
				Name:       "orphan-svc",
				StablePort: 3000,
				BinaryPath: "/gone/binary",
				Status:     registry.StatusOrphaned,
				ConfigPath: cfgPath,
			},
		},
	}

	cr := checkConfig(cfgPath, ".anito/config.yaml", dir, fetcher)
	found := findIssue(cr.Issues, "binary_path", "error")
	if found == nil {
		t.Fatal("expected error for orphaned status")
	}
	if !contains(found.Message, "no longer exists") {
		t.Errorf("unexpected message: %s", found.Message)
	}
}

// ---------- Registry alignment: relative env_file in registry ----------

func TestCheckConfig_RegistryRelativeEnvFile(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "svc-bin")
	if err := os.WriteFile(binPath, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeConfig(t, dir, "config.yaml", minimalConfig("env-svc", binPath))

	fetcher := &mockStatusFetcher{
		services: map[string]*registry.Service{
			"env-svc": {
				Name:       "env-svc",
				StablePort: 3000,
				BinaryPath: binPath,
				EnvFile:    ".env", // relative -- should trigger error
				Status:     registry.StatusRunning,
				ConfigPath: cfgPath,
			},
		},
	}

	cr := checkConfig(cfgPath, ".anito/config.yaml", dir, fetcher)
	found := findIssue(cr.Issues, "env_file", "error")
	if found == nil {
		t.Fatal("expected error for relative env_file in registry")
	}
	if !contains(found.Message, "relative path") {
		t.Errorf("unexpected message: %s", found.Message)
	}
}

// ---------- Registry alignment: missing config_path ----------

func TestCheckConfig_RegistryMissingConfigPath(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "svc-bin")
	if err := os.WriteFile(binPath, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeConfig(t, dir, "config.yaml", minimalConfig("nocfg-svc", binPath))

	fetcher := &mockStatusFetcher{
		services: map[string]*registry.Service{
			"nocfg-svc": {
				Name:       "nocfg-svc",
				StablePort: 3000,
				BinaryPath: binPath,
				ConfigPath: "", // missing
				Status:     registry.StatusRunning,
			},
		},
	}

	cr := checkConfig(cfgPath, ".anito/config.yaml", dir, fetcher)
	found := findIssue(cr.Issues, "config_path", "error")
	if found == nil {
		t.Fatal("expected error for missing config_path in registry")
	}
}

// ---------- Registry alignment: config_path points to deleted file ----------

func TestCheckConfig_RegistryDeletedConfigPath(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "svc-bin")
	if err := os.WriteFile(binPath, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeConfig(t, dir, "config.yaml", minimalConfig("del-svc", binPath))

	fetcher := &mockStatusFetcher{
		services: map[string]*registry.Service{
			"del-svc": {
				Name:       "del-svc",
				StablePort: 3000,
				BinaryPath: binPath,
				ConfigPath: "/does/not/exist/config.yaml",
				Status:     registry.StatusRunning,
			},
		},
	}

	cr := checkConfig(cfgPath, ".anito/config.yaml", dir, fetcher)
	found := findIssue(cr.Issues, "config_path", "error")
	if found == nil {
		t.Fatal("expected error for deleted config_path")
	}
	if !contains(found.Message, "no longer exists") {
		t.Errorf("unexpected message: %s", found.Message)
	}
}

// ---------- Registry alignment: binary path mismatch (info) ----------

func TestCheckConfig_RegistryBinaryPathMismatch(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "svc-bin")
	if err := os.WriteFile(binPath, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeConfig(t, dir, "config.yaml", minimalConfig("bin-svc", binPath))

	fetcher := &mockStatusFetcher{
		services: map[string]*registry.Service{
			"bin-svc": {
				Name:       "bin-svc",
				StablePort: 3000,
				BinaryPath: "/other/path/bin", // different from config output
				Status:     registry.StatusRunning,
				ConfigPath: cfgPath,
			},
		},
	}

	cr := checkConfig(cfgPath, ".anito/config.yaml", dir, fetcher)
	found := findIssue(cr.Issues, "output", "info")
	if found == nil {
		t.Fatal("expected info issue for binary path mismatch")
	}
	if !contains(found.Message, "registered binary differs") {
		t.Errorf("unexpected message: %s", found.Message)
	}
}

// ---------- Registry alignment: service not found (no issues) ----------

func TestCheckConfig_RegistryServiceNotFound(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "svc-bin")
	if err := os.WriteFile(binPath, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeConfig(t, dir, "config.yaml", minimalConfig("new-svc", binPath))

	fetcher := &mockStatusFetcher{
		services: map[string]*registry.Service{},
	}

	cr := checkConfig(cfgPath, ".anito/config.yaml", dir, fetcher)
	// Service not in registry is fine -- it just hasn't been deployed yet.
	if cr.Errors != 0 {
		t.Errorf("expected 0 errors for unregistered service, got %d; issues: %+v", cr.Errors, cr.Issues)
	}
}

// ---------- Check: multiple configs processed ----------

func TestCheck_MultipleConfigs(t *testing.T) {
	dir := t.TempDir()
	bin1 := filepath.Join(dir, "svc1-bin")
	bin2 := filepath.Join(dir, "svc2-bin")
	if err := os.WriteFile(bin1, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin2, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, dir, "svc1.yaml", minimalConfig("svc1", bin1))
	writeConfig(t, dir, "svc2.yaml", minimalConfig("svc2", bin2))

	result, err := Check(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Configs) != 2 {
		t.Fatalf("expected 2 config results, got %d", len(result.Configs))
	}
}

// ---------- Check: error + warning counts aggregate ----------

func TestCheck_AggregateErrorsAndWarnings(t *testing.T) {
	dir := t.TempDir()
	// First config: valid output exists.
	bin1 := filepath.Join(dir, "svc1-bin")
	if err := os.WriteFile(bin1, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, dir, "svc1.yaml", minimalConfig("svc1", bin1))
	// Second config: output missing -> 1 error.
	writeConfig(t, dir, "svc2.yaml", minimalConfig("svc2", "./missing-binary"))

	result, err := Check(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Errors < 1 {
		t.Errorf("expected at least 1 total error, got %d", result.Errors)
	}
	if result.Healthy {
		t.Error("expected healthy=false when there are errors")
	}
}

// ---------- checkConfig: valid type "static" is accepted ----------

func TestCheckConfig_ValidTypeStatic(t *testing.T) {
	dir := t.TempDir()
	staticDir := filepath.Join(dir, "public")
	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf("name: spa\nport: 3000\ntype: static\noutput: %s\nhealth_check: /health\n", staticDir)
	cfgPath := writeConfig(t, dir, "config.yaml", cfg)

	cr := checkConfig(cfgPath, ".anito/config.yaml", dir, nil)
	found := findIssue(cr.Issues, "type", "error")
	if found != nil {
		t.Errorf("did not expect error for valid type=static, got: %v", found)
	}
}

// ---------- checkConfig: env_file exists -- no error ----------

func TestCheckConfig_EnvFileExists_NoError(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "svc-bin")
	if err := os.WriteFile(binPath, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	anitoDir := filepath.Join(dir, ".anito")
	if err := os.MkdirAll(anitoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(anitoDir, "test.env")
	if err := os.WriteFile(envPath, []byte("FOO=bar"), 0o644); err != nil {
		t.Fatal(err)
	}
	// env_file is relative to config dir (.anito/)
	cfg := fmt.Sprintf("name: svc\nport: 3000\ntype: binary\noutput: %s\nenv_file: test.env\nhealth_check: /health\n", binPath)
	cfgPath := writeConfig(t, dir, "config.yaml", cfg)

	cr := checkConfig(cfgPath, ".anito/config.yaml", dir, nil)
	found := findIssue(cr.Issues, "env_file", "error")
	if found != nil {
		t.Errorf("did not expect env_file error when file exists, got: %v", found)
	}
}

// ---------- Registry alignment: config deployed from different location (info) ----------

func TestCheckConfig_RegistryConfigPathDiffers(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "svc-bin")
	if err := os.WriteFile(binPath, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeConfig(t, dir, "config.yaml", minimalConfig("moved-svc", binPath))

	// Create a different config file that the registry points to.
	otherCfg := filepath.Join(dir, "other-config.yaml")
	if err := os.WriteFile(otherCfg, []byte(minimalConfig("moved-svc", binPath)), 0o644); err != nil {
		t.Fatal(err)
	}

	fetcher := &mockStatusFetcher{
		services: map[string]*registry.Service{
			"moved-svc": {
				Name:       "moved-svc",
				StablePort: 3000,
				BinaryPath: binPath,
				ConfigPath: otherCfg,
				Status:     registry.StatusRunning,
			},
		},
	}

	cr := checkConfig(cfgPath, ".anito/config.yaml", dir, fetcher)
	found := findIssue(cr.Issues, "config_path", "info")
	if found == nil {
		t.Fatal("expected info issue for config deployed from different location")
	}
	if !contains(found.Message, "different location") {
		t.Errorf("unexpected message: %s", found.Message)
	}
}

// ---------- ConfigResult.add: severity counting ----------

func TestConfigResult_Add_SeverityCounting(t *testing.T) {
	cr := ConfigResult{}
	cr.add(Issue{Severity: "error", Field: "a", Message: "err1"})
	cr.add(Issue{Severity: "warning", Field: "b", Message: "warn1"})
	cr.add(Issue{Severity: "info", Field: "c", Message: "info1"})
	cr.add(Issue{Severity: "error", Field: "d", Message: "err2"})

	if cr.Errors != 2 {
		t.Errorf("expected 2 errors, got %d", cr.Errors)
	}
	if cr.Warnings != 1 {
		t.Errorf("expected 1 warning, got %d", cr.Warnings)
	}
	if len(cr.Issues) != 4 {
		t.Errorf("expected 4 total issues, got %d", len(cr.Issues))
	}
}

// ---------- detectPortConflict: no listener -> empty string ----------

func TestDetectPortConflict_NoListener(t *testing.T) {
	// Use a very high ephemeral port unlikely to be in use.
	result := detectPortConflict(59123)
	if result != "" {
		t.Errorf("expected empty string for port with no listener, got: %s", result)
	}
}

// ---------- Check: drain_window as nanosecond integer (very large) ----------

func TestCheckConfig_DrainWindowNanoseconds(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "svc-bin")
	if err := os.WriteFile(binPath, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 3 billion nanoseconds = 3 seconds as a bare integer in YAML.
	// Go's time.Duration treats bare integers as nanoseconds.
	// 3000000000ns = 3s, which is under 5min, so no warning.
	cfg := fmt.Sprintf("name: svc\nport: 3000\ntype: binary\noutput: %s\ndrain_window: 3000000000\nhealth_check: /health\n", binPath)
	cfgPath := writeConfig(t, dir, "config.yaml", cfg)

	cr := checkConfig(cfgPath, ".anito/config.yaml", dir, nil)
	found := findIssue(cr.Issues, "drain_window", "warning")
	// 3s as nanoseconds should be fine -- it is 3 seconds.
	if found != nil {
		t.Errorf("did not expect drain_window warning for 3s worth of nanoseconds: %v", found)
	}
}

// ---------- Worktree detection: config_path from worktree in registry ----------

func TestCheckConfig_RegistryConfigFromWorktree(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "svc-bin")
	if err := os.WriteFile(binPath, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeConfig(t, dir, "config.yaml", minimalConfig("wt-svc", binPath))

	// Create the worktree config so Stat doesn't fail.
	wtDir := filepath.Join(dir, ".claude", "worktrees", "agent-abc", ".anito")
	if err := os.MkdirAll(wtDir, 0o755); err != nil {
		t.Fatal(err)
	}
	wtCfg := filepath.Join(wtDir, "config.yaml")
	if err := os.WriteFile(wtCfg, []byte(minimalConfig("wt-svc", binPath)), 0o644); err != nil {
		t.Fatal(err)
	}

	fetcher := &mockStatusFetcher{
		services: map[string]*registry.Service{
			"wt-svc": {
				Name:       "wt-svc",
				StablePort: 3000,
				BinaryPath: binPath,
				ConfigPath: wtCfg, // deployed from a worktree
				Status:     registry.StatusRunning,
			},
		},
	}

	cr := checkConfig(cfgPath, ".anito/config.yaml", dir, fetcher)
	found := findIssue(cr.Issues, "config_path", "info")
	if found == nil {
		t.Fatal("expected info issue for config deployed from worktree")
	}
	if !contains(found.Message, "worktree") {
		t.Errorf("expected message to mention worktree, got: %s", found.Message)
	}
}

// ---------- Check: config missing name (Load returns error) ----------

func TestCheck_ConfigMissingName(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "config.yaml", "port: 3000\noutput: ./bin\n")

	result, err := Check(dir, nil)
	if err != nil {
		t.Fatalf("Check should not return error for invalid configs: %v", err)
	}
	if result.Configs[0].ParseError == "" {
		t.Error("expected ParseError for config without name")
	}
	if result.Healthy {
		t.Error("expected healthy=false")
	}
}

// ---------- Helpers ----------

// contains reports whether s contains substr (case-sensitive).
func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// findIssue returns the first issue matching the given field and severity, or nil.
func findIssue(issues []Issue, field, severity string) *Issue {
	for i := range issues {
		if issues[i].Field == field && issues[i].Severity == severity {
			return &issues[i]
		}
	}
	return nil
}

// Ensure the test uses the time import to silence unused import warnings.
var _ = time.Second
