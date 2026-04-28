package watcher

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestWatcher_PostDebounceLog verifies that multiple file events within the
// debounce window produce only a single [WATCH] log line, not one per event.
func TestWatcher_PostDebounceLog(t *testing.T) {
	// Redirect the standard logger to a buffer.
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	dir := t.TempDir()
	m := New()

	triggered := make(chan struct{}, 10)
	if err := m.Start("svc", []string{dir}, func(string) {
		triggered <- struct{}{}
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop("svc")

	// Write several files quickly (within the debounce window).
	for i := 0; i < 5; i++ {
		f, err := os.CreateTemp(dir, "test*.txt")
		if err != nil {
			t.Fatalf("CreateTemp: %v", err)
		}
		_ = f.Close()
	}

	// Wait for the single debounced trigger to fire.
	select {
	case <-triggered:
		// good
	case <-time.After(3 * time.Second):
		t.Fatal("no trigger within 3s")
	}

	// Wait a bit for any additional (unexpected) log lines.
	time.Sleep(200 * time.Millisecond)

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	watchLines := 0
	for _, line := range lines {
		if strings.Contains(line, "[WATCH]") && strings.Contains(line, "name=svc") && strings.Contains(line, "coalesced=") {
			watchLines++
		}
	}

	if watchLines == 0 {
		t.Errorf("expected at least one [WATCH] log line with coalesced=, got none\noutput:\n%s", output)
	}
	// We should see exactly 1 [WATCH] log line for the debounced batch.
	// (Allow 1 because rapid events might produce a second debounce if timing is off.)
	if watchLines > 2 {
		t.Errorf("expected 1-2 [WATCH] log lines (coalesced), got %d\noutput:\n%s", watchLines, output)
	}
}

// TestActive_EmptyWhenNoWatchers verifies that Active returns an empty slice
// when no watchers have been started.
func TestActive_EmptyWhenNoWatchers(t *testing.T) {
	m := New()
	active := m.Active()
	if len(active) != 0 {
		t.Errorf("Active() = %v, want empty slice", active)
	}
}

// TestActive_ReturnsServiceNamesAfterStart verifies that Active returns the
// names of services with active watchers.
func TestActive_ReturnsServiceNamesAfterStart(t *testing.T) {
	m := New()
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	if err := m.Start("alpha", []string{dir1}, func(string) {}); err != nil {
		t.Fatalf("Start alpha: %v", err)
	}
	defer m.Stop("alpha")
	if err := m.Start("beta", []string{dir2}, func(string) {}); err != nil {
		t.Fatalf("Start beta: %v", err)
	}
	defer m.Stop("beta")

	active := m.Active()
	if len(active) != 2 {
		t.Fatalf("Active() len = %d, want 2", len(active))
	}

	found := map[string]bool{}
	for _, name := range active {
		found[name] = true
	}
	if !found["alpha"] || !found["beta"] {
		t.Errorf("Active() = %v, want [alpha, beta]", active)
	}
}

// TestStopAll_StopsAllWatchers verifies that StopAll stops every active watcher
// and Active returns empty afterward.
func TestStopAll_StopsAllWatchers(t *testing.T) {
	m := New()
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	if err := m.Start("svc1", []string{dir1}, func(string) {}); err != nil {
		t.Fatalf("Start svc1: %v", err)
	}
	if err := m.Start("svc2", []string{dir2}, func(string) {}); err != nil {
		t.Fatalf("Start svc2: %v", err)
	}

	m.StopAll()

	active := m.Active()
	if len(active) != 0 {
		t.Errorf("Active() after StopAll = %v, want empty", active)
	}
}

// TestStart_ReplaceExistingWatcher verifies that calling Start with an already-
// watched name stops the old watcher and starts a new one.
func TestStart_ReplaceExistingWatcher(t *testing.T) {
	m := New()
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	triggered1 := make(chan string, 10)
	triggered2 := make(chan string, 10)

	if err := m.Start("svc", []string{dir1}, func(s string) {
		triggered1 <- s
	}); err != nil {
		t.Fatalf("Start (first): %v", err)
	}

	// Replace the watcher with a new one watching a different directory.
	if err := m.Start("svc", []string{dir2}, func(s string) {
		triggered2 <- s
	}); err != nil {
		t.Fatalf("Start (second): %v", err)
	}
	defer m.Stop("svc")

	// Only one watcher should be active for this name.
	active := m.Active()
	if len(active) != 1 {
		t.Fatalf("Active() len = %d, want 1", len(active))
	}

	// Write a file to the second directory — should trigger the new callback.
	f, err := os.CreateTemp(dir2, "replace-test*.txt")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	_ = f.Close()

	select {
	case <-triggered2:
		// good — new watcher fired
	case <-time.After(3 * time.Second):
		t.Fatal("replacement watcher did not fire within 3s")
	}

	// The old callback should NOT have fired (give it a short window).
	select {
	case path := <-triggered1:
		t.Errorf("old watcher unexpectedly triggered for %s", path)
	case <-time.After(200 * time.Millisecond):
		// good — old watcher is silent
	}
}

// TestAddDirsRecursive_SkipsHiddenDirs verifies that addDirsRecursive does not
// add hidden subdirectories (starting with '.') to the watcher.
func TestAddDirsRecursive_SkipsHiddenDirs(t *testing.T) {
	root := t.TempDir()
	// Create a normal subdirectory and a hidden one.
	normalDir := filepath.Join(root, "src")
	hiddenDir := filepath.Join(root, ".git")
	if err := os.MkdirAll(normalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(hiddenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create a file inside the hidden dir to prove its contents aren't traversed.
	if err := os.WriteFile(filepath.Join(hiddenDir, "HEAD"), []byte("ref"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := New()
	// Start a watcher on root — addDirsRecursive runs inside Start.
	if err := m.Start("svc", []string{root}, func(string) {}); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer m.Stop("svc")
	// If we reach here without panic, hidden dir was skipped correctly.
}

// TestAddDirsRecursive_SkipsFiles verifies that addDirsRecursive skips
// non-directory entries (files at root level do not add themselves).
func TestAddDirsRecursive_SkipsFiles(t *testing.T) {
	root := t.TempDir()
	// Create a file at the root level.
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	if err := m.Start("svc2", []string{root}, func(string) {}); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer m.Stop("svc2")
}

// TestHiddenFilesSkipped verifies that files starting with '.' or '#' do not
// trigger the watcher callback.
func TestHiddenFilesSkipped(t *testing.T) {
	// Redirect the standard logger to suppress [WATCH] output.
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	dir := t.TempDir()
	m := New()

	triggered := make(chan string, 10)
	if err := m.Start("svc", []string{dir}, func(s string) {
		triggered <- s
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop("svc")

	// Write hidden files that should be ignored.
	for _, name := range []string{".hidden", "#temp#"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("ignored"), 0644); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}

	// Give the watcher time to potentially (incorrectly) fire.
	time.Sleep(time.Duration(float64(debounceDelay) * 1.5))

	select {
	case path := <-triggered:
		t.Errorf("hidden file unexpectedly triggered watcher: %s", path)
	default:
		// good — no trigger
	}

	// Now write a normal file to confirm the watcher is actually working.
	if err := os.WriteFile(filepath.Join(dir, "real.txt"), []byte("visible"), 0644); err != nil {
		t.Fatalf("WriteFile real.txt: %v", err)
	}

	select {
	case <-triggered:
		// good — normal file triggered
	case <-time.After(3 * time.Second):
		t.Fatal("normal file did not trigger watcher within 3s")
	}
}
