// Package receipt manages the deployed.json file written into a consuming
// repo's .anito/ directory after every successful deploy. It is the repo's
// local record of what it has registered with Anito — the single source of
// truth for cleanup, teardown, and agent re-entry.
//
// File location: <repo>/.anito/deployed.json
// Written by: Anito after every successful deploy (when ConfigPath is set)
// Read by: any agent in the consuming repo that needs to know what ports/names
//           are registered, or what to remove before deleting a worktree.
package receipt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Entry records one deployed service.
type Entry struct {
	Name       string    `json:"name"`
	StablePort int       `json:"stable_port"`
	Address    string    `json:"address"`              // "http://localhost:<port>"
	BinaryPath string    `json:"binary_path"`
	ConfigPath string    `json:"config_path"`
	Version    string    `json:"version,omitempty"`
	DeployedAt time.Time `json:"deployed_at"`
}

// File is the structure written to deployed.json.
type File struct {
	Services map[string]Entry `json:"services"` // keyed by service name
}

var mu sync.Mutex // guards all file operations (cross-goroutine safety)

// filePath returns the deployed.json path for a given .anito/config.yaml path.
// If configPath is empty or does not contain ".anito", returns "".
func filePath(configPath string) string {
	if configPath == "" {
		return ""
	}
	dir := filepath.Dir(configPath)
	if filepath.Base(dir) != ".anito" {
		return ""
	}
	return filepath.Join(dir, "deployed.json")
}

// Write adds or updates the entry for e.Name in the deployed.json located
// next to the config file at e.ConfigPath. No-op if ConfigPath is empty.
func Write(e Entry) error {
	path := filePath(e.ConfigPath)
	if path == "" {
		return nil
	}

	mu.Lock()
	defer mu.Unlock()

	f := read(path)
	if f.Services == nil {
		f.Services = make(map[string]Entry)
	}
	f.Services[e.Name] = e
	return write(path, f)
}

// Clear removes the entry for name from the deployed.json located next to
// configPath. No-op if configPath is empty or name is not in the file.
func Clear(name, configPath string) error {
	path := filePath(configPath)
	if path == "" {
		return nil
	}

	mu.Lock()
	defer mu.Unlock()

	f := read(path)
	if _, ok := f.Services[name]; !ok {
		return nil
	}
	delete(f.Services, name)
	if len(f.Services) == 0 {
		// Remove the file entirely when empty — nothing left to clean up.
		_ = os.Remove(path)
		return nil
	}
	return write(path, f)
}

// Load reads the deployed.json from a repo's .anito/ directory.
// repoPath is the repo root (must contain a .anito/ subdirectory).
// Returns an empty File (not an error) if the file does not exist.
func Load(repoPath string) (File, error) {
	path := filepath.Join(repoPath, ".anito", "deployed.json")

	mu.Lock()
	defer mu.Unlock()

	f := read(path)
	return f, nil
}

// DeleteAll removes deployed.json from the repo's .anito/ directory.
// Called by Teardown after all services have been removed.
func DeleteAll(repoPath string) error {
	path := filepath.Join(repoPath, ".anito", "deployed.json")
	mu.Lock()
	defer mu.Unlock()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("receipt: remove %s: %w", path, err)
	}
	return nil
}

// read parses path into a File. Returns an empty File on any error (missing,
// malformed, etc.) — callers treat a missing receipt as an empty one.
func read(path string) File {
	b, err := os.ReadFile(path)
	if err != nil {
		return File{}
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return File{}
	}
	return f
}

// write marshals f to path with indentation for human readability.
func write(path string, f File) error {
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("receipt: marshal: %w", err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0644); err != nil {
		return fmt.Errorf("receipt: write %s: %w", path, err)
	}
	return nil
}
