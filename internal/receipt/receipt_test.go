package receipt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- filePath tests ---

func TestFilePath_EmptyConfigPath(t *testing.T) {
	got := filePath("")
	if got != "" {
		t.Fatalf("filePath(\"\") = %q, want \"\"", got)
	}
}

func TestFilePath_DirNotDotAnito(t *testing.T) {
	// A config path whose parent directory is not ".anito" should return "".
	got := filePath("/some/repo/configs/config.yaml")
	if got != "" {
		t.Fatalf("filePath with non-.anito parent = %q, want \"\"", got)
	}
}

func TestFilePath_ValidAnitoConfig(t *testing.T) {
	input := "/Users/dev/myrepo/.anito/config.yaml"
	want := "/Users/dev/myrepo/.anito/deployed.json"
	got := filePath(input)
	if got != want {
		t.Fatalf("filePath(%q) = %q, want %q", input, got, want)
	}
}

// --- Write tests ---

func TestWrite_CreatesFileWithSingleEntry(t *testing.T) {
	tmp := t.TempDir()
	anitoDir := filepath.Join(tmp, ".anito")
	if err := os.Mkdir(anitoDir, 0755); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(anitoDir, "config.yaml")
	deployedPath := filepath.Join(anitoDir, "deployed.json")

	entry := Entry{
		Name:       "svc-a",
		StablePort: 8100,
		Address:    "http://localhost:8100",
		BinaryPath: "/usr/local/bin/svc-a",
		ConfigPath: configPath,
		Version:    "v1.0.0",
		DeployedAt: time.Date(2026, 3, 20, 14, 0, 0, 0, time.UTC),
	}

	if err := Write(entry); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// The file should exist with exactly one entry.
	b, err := os.ReadFile(deployedPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(f.Services) != 1 {
		t.Fatalf("got %d services, want 1", len(f.Services))
	}
	got := f.Services["svc-a"]
	if got.StablePort != 8100 {
		t.Errorf("StablePort = %d, want 8100", got.StablePort)
	}
	if got.Address != "http://localhost:8100" {
		t.Errorf("Address = %q, want %q", got.Address, "http://localhost:8100")
	}
}

func TestWrite_UpdatesExistingEntry(t *testing.T) {
	tmp := t.TempDir()
	anitoDir := filepath.Join(tmp, ".anito")
	if err := os.Mkdir(anitoDir, 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(anitoDir, "config.yaml")

	e1 := Entry{
		Name:       "svc-a",
		StablePort: 8100,
		Address:    "http://localhost:8100",
		BinaryPath: "/bin/svc-a-v1",
		ConfigPath: configPath,
		Version:    "v1.0.0",
		DeployedAt: time.Date(2026, 3, 20, 14, 0, 0, 0, time.UTC),
	}
	if err := Write(e1); err != nil {
		t.Fatalf("Write v1: %v", err)
	}

	// Update same name with new version and binary.
	e2 := Entry{
		Name:       "svc-a",
		StablePort: 8100,
		Address:    "http://localhost:8100",
		BinaryPath: "/bin/svc-a-v2",
		ConfigPath: configPath,
		Version:    "v2.0.0",
		DeployedAt: time.Date(2026, 3, 21, 10, 0, 0, 0, time.UTC),
	}
	if err := Write(e2); err != nil {
		t.Fatalf("Write v2: %v", err)
	}

	deployedPath := filepath.Join(anitoDir, "deployed.json")
	b, err := os.ReadFile(deployedPath)
	if err != nil {
		t.Fatal(err)
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Services) != 1 {
		t.Fatalf("got %d services after update, want 1", len(f.Services))
	}
	got := f.Services["svc-a"]
	if got.Version != "v2.0.0" {
		t.Errorf("Version = %q, want %q", got.Version, "v2.0.0")
	}
	if got.BinaryPath != "/bin/svc-a-v2" {
		t.Errorf("BinaryPath = %q, want %q", got.BinaryPath, "/bin/svc-a-v2")
	}
}

func TestWrite_AddsSecondEntry(t *testing.T) {
	tmp := t.TempDir()
	anitoDir := filepath.Join(tmp, ".anito")
	if err := os.Mkdir(anitoDir, 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(anitoDir, "config.yaml")

	e1 := Entry{
		Name:       "svc-a",
		StablePort: 8100,
		Address:    "http://localhost:8100",
		BinaryPath: "/bin/svc-a",
		ConfigPath: configPath,
		DeployedAt: time.Now(),
	}
	e2 := Entry{
		Name:       "svc-b",
		StablePort: 8101,
		Address:    "http://localhost:8101",
		BinaryPath: "/bin/svc-b",
		ConfigPath: configPath,
		DeployedAt: time.Now(),
	}
	if err := Write(e1); err != nil {
		t.Fatalf("Write svc-a: %v", err)
	}
	if err := Write(e2); err != nil {
		t.Fatalf("Write svc-b: %v", err)
	}

	deployedPath := filepath.Join(anitoDir, "deployed.json")
	b, err := os.ReadFile(deployedPath)
	if err != nil {
		t.Fatal(err)
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Services) != 2 {
		t.Fatalf("got %d services, want 2", len(f.Services))
	}
	if _, ok := f.Services["svc-a"]; !ok {
		t.Error("svc-a missing from services map")
	}
	if _, ok := f.Services["svc-b"]; !ok {
		t.Error("svc-b missing from services map")
	}
}

func TestWrite_NoopWhenConfigPathEmpty(t *testing.T) {
	entry := Entry{
		Name:       "svc-a",
		StablePort: 8100,
		ConfigPath: "", // empty -- should be a no-op
	}
	if err := Write(entry); err != nil {
		t.Fatalf("Write with empty ConfigPath returned error: %v", err)
	}
}

// --- Clear tests ---

func TestClear_RemovesEntryKeepsFile(t *testing.T) {
	tmp := t.TempDir()
	anitoDir := filepath.Join(tmp, ".anito")
	if err := os.Mkdir(anitoDir, 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(anitoDir, "config.yaml")

	// Write two entries.
	for _, name := range []string{"svc-a", "svc-b"} {
		if err := Write(Entry{
			Name:       name,
			StablePort: 8100,
			Address:    "http://localhost:8100",
			BinaryPath: "/bin/" + name,
			ConfigPath: configPath,
			DeployedAt: time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Clear one.
	if err := Clear("svc-a", configPath); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	deployedPath := filepath.Join(anitoDir, "deployed.json")
	b, err := os.ReadFile(deployedPath)
	if err != nil {
		t.Fatalf("file should still exist: %v", err)
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Services) != 1 {
		t.Fatalf("got %d services, want 1", len(f.Services))
	}
	if _, ok := f.Services["svc-a"]; ok {
		t.Error("svc-a should have been removed")
	}
	if _, ok := f.Services["svc-b"]; !ok {
		t.Error("svc-b should still be present")
	}
}

func TestClear_RemovesFileWhenLastEntry(t *testing.T) {
	tmp := t.TempDir()
	anitoDir := filepath.Join(tmp, ".anito")
	if err := os.Mkdir(anitoDir, 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(anitoDir, "config.yaml")

	if err := Write(Entry{
		Name:       "only-svc",
		StablePort: 8100,
		Address:    "http://localhost:8100",
		BinaryPath: "/bin/only-svc",
		ConfigPath: configPath,
		DeployedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	if err := Clear("only-svc", configPath); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	deployedPath := filepath.Join(anitoDir, "deployed.json")
	if _, err := os.Stat(deployedPath); !os.IsNotExist(err) {
		t.Fatalf("deployed.json should be deleted when last entry is cleared, stat err: %v", err)
	}
}

func TestClear_NoopForEmptyConfigPath(t *testing.T) {
	if err := Clear("svc-a", ""); err != nil {
		t.Fatalf("Clear with empty configPath returned error: %v", err)
	}
}

func TestClear_NoopForNonExistentName(t *testing.T) {
	tmp := t.TempDir()
	anitoDir := filepath.Join(tmp, ".anito")
	if err := os.Mkdir(anitoDir, 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(anitoDir, "config.yaml")

	// Write one entry, then clear a different name.
	if err := Write(Entry{
		Name:       "svc-a",
		StablePort: 8100,
		Address:    "http://localhost:8100",
		BinaryPath: "/bin/svc-a",
		ConfigPath: configPath,
		DeployedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	if err := Clear("does-not-exist", configPath); err != nil {
		t.Fatalf("Clear non-existent name returned error: %v", err)
	}

	// Original entry should still be there.
	deployedPath := filepath.Join(anitoDir, "deployed.json")
	b, err := os.ReadFile(deployedPath)
	if err != nil {
		t.Fatal(err)
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Services) != 1 {
		t.Fatalf("got %d services, want 1", len(f.Services))
	}
}

// --- Load tests ---

func TestLoad_ReturnsEmptyFileWhenMissing(t *testing.T) {
	tmp := t.TempDir()
	// .anito dir does not even exist -- Load should return empty File, no error.
	f, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if f.Services != nil && len(f.Services) != 0 {
		t.Fatalf("expected empty services, got %d", len(f.Services))
	}
}

func TestLoad_ReadsValidFile(t *testing.T) {
	tmp := t.TempDir()
	anitoDir := filepath.Join(tmp, ".anito")
	if err := os.Mkdir(anitoDir, 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(anitoDir, "config.yaml")

	entry := Entry{
		Name:       "svc-a",
		StablePort: 8100,
		Address:    "http://localhost:8100",
		BinaryPath: "/bin/svc-a",
		ConfigPath: configPath,
		Version:    "sha:abc123",
		DeployedAt: time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
	}
	if err := Write(entry); err != nil {
		t.Fatal(err)
	}

	f, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(f.Services) != 1 {
		t.Fatalf("got %d services, want 1", len(f.Services))
	}
	got := f.Services["svc-a"]
	if got.StablePort != 8100 {
		t.Errorf("StablePort = %d, want 8100", got.StablePort)
	}
	if got.Version != "sha:abc123" {
		t.Errorf("Version = %q, want %q", got.Version, "sha:abc123")
	}
}

// --- DeleteAll tests ---

func TestDeleteAll_RemovesFile(t *testing.T) {
	tmp := t.TempDir()
	anitoDir := filepath.Join(tmp, ".anito")
	if err := os.Mkdir(anitoDir, 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(anitoDir, "config.yaml")

	if err := Write(Entry{
		Name:       "svc-a",
		StablePort: 8100,
		Address:    "http://localhost:8100",
		BinaryPath: "/bin/svc-a",
		ConfigPath: configPath,
		DeployedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	if err := DeleteAll(tmp); err != nil {
		t.Fatalf("DeleteAll: %v", err)
	}

	deployedPath := filepath.Join(anitoDir, "deployed.json")
	if _, err := os.Stat(deployedPath); !os.IsNotExist(err) {
		t.Fatalf("deployed.json should be deleted, stat err: %v", err)
	}
}

func TestDeleteAll_NoopWhenFileMissing(t *testing.T) {
	tmp := t.TempDir()
	// No .anito dir, no deployed.json -- should not error.
	if err := DeleteAll(tmp); err != nil {
		t.Fatalf("DeleteAll on missing file returned error: %v", err)
	}
}

// --- Round-trip test ---

func TestRoundTrip_WriteLoadPreservesAllFields(t *testing.T) {
	tmp := t.TempDir()
	anitoDir := filepath.Join(tmp, ".anito")
	if err := os.Mkdir(anitoDir, 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(anitoDir, "config.yaml")

	deployTime := time.Date(2026, 4, 2, 9, 30, 0, 0, time.UTC)
	entry := Entry{
		Name:       "my-daemon",
		StablePort: 7172,
		Address:    "http://localhost:7172",
		StablePorts: map[string]int{
			"ws":   7172,
			"http": 7173,
		},
		Addresses: map[string]string{
			"ws":   "http://localhost:7172",
			"http": "http://localhost:7173",
		},
		BinaryPath: "/Users/dev/myapp/dist/my-daemon",
		ConfigPath: configPath,
		Version:    "sha:deadbeef",
		DeployedAt: deployTime,
	}

	if err := Write(entry); err != nil {
		t.Fatalf("Write: %v", err)
	}

	f, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(f.Services) != 1 {
		t.Fatalf("got %d services, want 1", len(f.Services))
	}

	got := f.Services["my-daemon"]

	if got.Name != entry.Name {
		t.Errorf("Name = %q, want %q", got.Name, entry.Name)
	}
	if got.StablePort != entry.StablePort {
		t.Errorf("StablePort = %d, want %d", got.StablePort, entry.StablePort)
	}
	if got.Address != entry.Address {
		t.Errorf("Address = %q, want %q", got.Address, entry.Address)
	}
	if got.BinaryPath != entry.BinaryPath {
		t.Errorf("BinaryPath = %q, want %q", got.BinaryPath, entry.BinaryPath)
	}
	if got.ConfigPath != entry.ConfigPath {
		t.Errorf("ConfigPath = %q, want %q", got.ConfigPath, entry.ConfigPath)
	}
	if got.Version != entry.Version {
		t.Errorf("Version = %q, want %q", got.Version, entry.Version)
	}
	if !got.DeployedAt.Equal(entry.DeployedAt) {
		t.Errorf("DeployedAt = %v, want %v", got.DeployedAt, entry.DeployedAt)
	}

	// Check StablePorts map.
	if len(got.StablePorts) != len(entry.StablePorts) {
		t.Fatalf("StablePorts len = %d, want %d", len(got.StablePorts), len(entry.StablePorts))
	}
	for k, wantPort := range entry.StablePorts {
		if gotPort, ok := got.StablePorts[k]; !ok {
			t.Errorf("StablePorts missing key %q", k)
		} else if gotPort != wantPort {
			t.Errorf("StablePorts[%q] = %d, want %d", k, gotPort, wantPort)
		}
	}

	// Check Addresses map.
	if len(got.Addresses) != len(entry.Addresses) {
		t.Fatalf("Addresses len = %d, want %d", len(got.Addresses), len(entry.Addresses))
	}
	for k, wantAddr := range entry.Addresses {
		if gotAddr, ok := got.Addresses[k]; !ok {
			t.Errorf("Addresses missing key %q", k)
		} else if gotAddr != wantAddr {
			t.Errorf("Addresses[%q] = %q, want %q", k, gotAddr, wantAddr)
		}
	}
}
