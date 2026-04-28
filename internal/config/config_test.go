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

func TestProxyBindAddressParsed(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "name: svc\noutput: ./bin/svc\nproxy_bind_address: 100.94.58.29\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ProxyBindAddress != "100.94.58.29" {
		t.Errorf("ProxyBindAddress = %q, want %q", cfg.ProxyBindAddress, "100.94.58.29")
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

// TestPortAndPortsMutuallyExclusive verifies that setting both port and ports
// returns an error mentioning "mutually exclusive".
func TestPortAndPortsMutuallyExclusive(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, `
name: svc
output: ./bin/svc
port: 3000
ports:
  ws: 7172
  http: 7173
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error when both port and ports are set, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error %q does not mention 'mutually exclusive'", err.Error())
	}
}

// TestHealthCheckPortMustReferenceValidPortName verifies that health_check_port
// referencing a non-existent port name returns an error.
func TestHealthCheckPortMustReferenceValidPortName(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, `
name: svc
output: ./bin/svc
ports:
  ws: 7172
  http: 7173
health_check_port: grpc
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid health_check_port, got nil")
	}
	if !strings.Contains(err.Error(), "health_check_port") {
		t.Errorf("error %q does not mention health_check_port", err.Error())
	}
}

// TestRelativeEnvFileResolvedAgainstConfigDir verifies that a relative env_file
// is resolved against the config file's directory.
func TestRelativeEnvFileResolvedAgainstConfigDir(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, `
name: svc
output: ./bin/svc
env_file: .env
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := filepath.Join(dir, ".env")
	if cfg.EnvFile != want {
		t.Errorf("EnvFile = %q, want %q", cfg.EnvFile, want)
	}
}

// TestAbsoluteEnvFileLeftUnchanged verifies that an absolute env_file path
// passes through Load unchanged.
func TestAbsoluteEnvFileLeftUnchanged(t *testing.T) {
	dir := t.TempDir()
	absEnv := "/etc/anito/service.env"
	path := writeConfig(t, dir, "name: svc\noutput: ./bin/svc\nenv_file: "+absEnv+"\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.EnvFile != absEnv {
		t.Errorf("EnvFile = %q, want %q", cfg.EnvFile, absEnv)
	}
}

// TestPortNormalizationToDefaultMap verifies that a singular port: value is
// normalized into a Ports map with key "default".
func TestPortNormalizationToDefaultMap(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, `
name: svc
output: ./bin/svc
port: 3000
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Ports) != 1 {
		t.Fatalf("Ports len = %d, want 1", len(cfg.Ports))
	}
	if cfg.Ports["default"] != 3000 {
		t.Errorf("Ports[\"default\"] = %d, want 3000", cfg.Ports["default"])
	}
}

// TestMultiPortPreserved verifies that a ports map is preserved as-is when
// port is omitted (zero).
func TestMultiPortPreserved(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, `
name: svc
output: ./bin/svc
ports:
  ws: 7172
  http: 7173
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Ports) != 2 {
		t.Fatalf("Ports len = %d, want 2", len(cfg.Ports))
	}
	if cfg.Ports["ws"] != 7172 {
		t.Errorf("Ports[\"ws\"] = %d, want 7172", cfg.Ports["ws"])
	}
	if cfg.Ports["http"] != 7173 {
		t.Errorf("Ports[\"http\"] = %d, want 7173", cfg.Ports["http"])
	}
}

// TestTypeDefaultsToBinary verifies that omitting type yields "binary".
func TestTypeDefaultsToBinary(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "name: svc\noutput: ./bin/svc\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Type != "binary" {
		t.Errorf("Type = %q, want \"binary\"", cfg.Type)
	}
}

// TestInvalidYAMLReturnsParseError verifies that malformed YAML produces an
// error mentioning "parsing".
func TestInvalidYAMLReturnsParseError(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "name: svc\n\t\tbad: [unclosed\n")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected parse error for invalid YAML, got nil")
	}
	if !strings.Contains(err.Error(), "parsing") {
		t.Errorf("error %q does not mention 'parsing'", err.Error())
	}
}

// TestFileNotFoundReturnsReadError verifies that a missing file produces an
// error mentioning "reading".
func TestFileNotFoundReturnsReadError(t *testing.T) {
	_, err := Load("/tmp/nonexistent-anito-test/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if !strings.Contains(err.Error(), "reading") {
		t.Errorf("error %q does not mention 'reading'", err.Error())
	}
}

// TestVersionFieldPreserved verifies that the version field is loaded into the
// Config struct.
func TestVersionFieldPreserved(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, `
name: svc
output: ./bin/svc
version: v1.2.3
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Version != "v1.2.3" {
		t.Errorf("Version = %q, want \"v1.2.3\"", cfg.Version)
	}
}

// TestArgsFieldPreserved verifies that the args field is loaded into the Config
// struct.
func TestArgsFieldPreserved(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, `
name: svc
output: ./bin/svc
args:
  - --mode
  - test
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Args) != 2 {
		t.Fatalf("Args len = %d, want 2", len(cfg.Args))
	}
	if cfg.Args[0] != "--mode" || cfg.Args[1] != "test" {
		t.Errorf("Args = %v, want [--mode test]", cfg.Args)
	}
}

// TestBuildFieldPreserved verifies that the build field is loaded into the
// Config struct.
func TestBuildFieldPreserved(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, `
name: svc
output: ./bin/svc
build: go build -o ./bin/svc ./cmd/svc
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := "go build -o ./bin/svc ./cmd/svc"
	if cfg.Build != want {
		t.Errorf("Build = %q, want %q", cfg.Build, want)
	}
}
