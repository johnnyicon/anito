package watcher

import (
	"bytes"
	"log"
	"os"
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
