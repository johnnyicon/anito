package notify

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEscapeAS(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain string",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "string with double quotes",
			input: `say "hello"`,
			want:  `say \"hello\"`,
		},
		{
			name:  "string with backslashes",
			input: `path\to\file`,
			want:  `path\\to\\file`,
		},
		{
			name:  "string with both quotes and backslashes",
			input: `he said "it's a \"path\\"`,
			want:  `he said \"it's a \\\"path\\\\\"`,
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapeAS(tt.input)
			if got != tt.want {
				t.Errorf("escapeAS(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestResolveHelper_NoHelperInstalled(t *testing.T) {
	// Save original helperPaths and restore after test.
	origPaths := helperPaths
	t.Cleanup(func() { helperPaths = origPaths })

	// Point helperPaths at paths that definitely do not exist.
	helperPaths = []string{
		filepath.Join(os.TempDir(), "anito-test-nonexistent", "AnitoNotify.app", "Contents", "MacOS", "anito-notify"),
		filepath.Join(os.TempDir(), "anito-test-nonexistent-2", "AnitoNotify.app", "Contents", "MacOS", "anito-notify"),
	}

	got := resolveHelper()
	if got != "" {
		t.Errorf("resolveHelper() = %q, want empty string when no helper exists", got)
	}
}

func TestResolveHelper_HelperExists(t *testing.T) {
	// Save original helperPaths and restore after test.
	origPaths := helperPaths
	t.Cleanup(func() { helperPaths = origPaths })

	// Create a temporary fake helper binary.
	dir := t.TempDir()
	fakePath := filepath.Join(dir, "AnitoNotify.app", "Contents", "MacOS", "anito-notify")
	if err := os.MkdirAll(filepath.Dir(fakePath), 0o755); err != nil {
		t.Fatalf("failed to create fake helper dir: %v", err)
	}
	if err := os.WriteFile(fakePath, []byte("fake"), 0o755); err != nil {
		t.Fatalf("failed to write fake helper: %v", err)
	}

	helperPaths = []string{
		filepath.Join(os.TempDir(), "anito-test-nonexistent", "no-such-binary"),
		fakePath,
	}

	got := resolveHelper()
	if got != fakePath {
		t.Errorf("resolveHelper() = %q, want %q", got, fakePath)
	}
}

// --- send() tests ---

// swapExecCommand replaces execCommand with fn for the duration of the test.
func swapExecCommand(t *testing.T, fn func(name string, arg ...string) *exec.Cmd) {
	t.Helper()
	orig := execCommand
	execCommand = fn
	t.Cleanup(func() { execCommand = orig })
}

// swapHelperPaths replaces helperPaths with paths for the duration of the test.
func swapHelperPaths(t *testing.T, paths []string) {
	t.Helper()
	orig := helperPaths
	helperPaths = paths
	t.Cleanup(func() { helperPaths = orig })
}

// noCmdHelper returns an execCommand replacement that records which command was
// called and what args were passed, then executes a no-op.
type cmdRecord struct {
	name string
	args []string
}

func recordingExecCommand(records *[]cmdRecord) func(string, ...string) *exec.Cmd {
	return func(name string, arg ...string) *exec.Cmd {
		*records = append(*records, cmdRecord{name: name, args: arg})
		return exec.Command("true") // fast no-op
	}
}

// TestSend_OsascriptFallback_NoSound verifies that send() uses osascript when
// no helper is installed and does not include "sound name" in the script.
func TestSend_OsascriptFallback_NoSound(t *testing.T) {
	swapHelperPaths(t, []string{"/nonexistent/AnitoNotify.app/Contents/MacOS/anito-notify"})

	var records []cmdRecord
	swapExecCommand(t, recordingExecCommand(&records))

	send("My Title", "My Message", false)

	if len(records) != 1 {
		t.Fatalf("expected 1 command, got %d", len(records))
	}
	if records[0].name != "osascript" {
		t.Errorf("expected osascript, got %q", records[0].name)
	}
	script := strings.Join(records[0].args, " ")
	if !strings.Contains(script, "My Title") {
		t.Errorf("script does not contain title: %q", script)
	}
	if !strings.Contains(script, "My Message") {
		t.Errorf("script does not contain message: %q", script)
	}
	if strings.Contains(script, "sound name") {
		t.Errorf("script should not contain 'sound name' when sound=false: %q", script)
	}
}

// TestSend_OsascriptFallback_WithSound verifies that the sound flag adds
// "sound name" to the AppleScript when using the osascript fallback.
func TestSend_OsascriptFallback_WithSound(t *testing.T) {
	swapHelperPaths(t, []string{"/nonexistent/AnitoNotify.app/Contents/MacOS/anito-notify"})

	var records []cmdRecord
	swapExecCommand(t, recordingExecCommand(&records))

	send("Title", "Message", true)

	if len(records) != 1 {
		t.Fatalf("expected 1 command, got %d", len(records))
	}
	script := strings.Join(records[0].args, " ")
	if !strings.Contains(script, "sound name") {
		t.Errorf("script should contain 'sound name' when sound=true: %q", script)
	}
}

// TestSend_HelperPath_NoSound verifies that send() uses `open` when the Swift
// helper exists, passing --title and --message but not --sound.
func TestSend_HelperPath_NoSound(t *testing.T) {
	dir := t.TempDir()
	fakePath := filepath.Join(dir, "AnitoNotify.app", "Contents", "MacOS", "anito-notify")
	if err := os.MkdirAll(filepath.Dir(fakePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fakePath, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	swapHelperPaths(t, []string{fakePath})

	var records []cmdRecord
	swapExecCommand(t, recordingExecCommand(&records))

	send("Title", "Message", false)

	if len(records) != 1 {
		t.Fatalf("expected 1 command, got %d", len(records))
	}
	if records[0].name != "open" {
		t.Errorf("expected 'open' command, got %q", records[0].name)
	}
	args := strings.Join(records[0].args, " ")
	if !strings.Contains(args, "--title") {
		t.Errorf("args should contain --title: %q", args)
	}
	if !strings.Contains(args, "--message") {
		t.Errorf("args should contain --message: %q", args)
	}
	if strings.Contains(args, "--sound") {
		t.Errorf("args should not contain --sound when sound=false: %q", args)
	}
}

// TestSend_HelperPath_WithSound verifies that send() passes --sound when using
// the Swift helper and sound=true.
func TestSend_HelperPath_WithSound(t *testing.T) {
	dir := t.TempDir()
	fakePath := filepath.Join(dir, "AnitoNotify.app", "Contents", "MacOS", "anito-notify")
	if err := os.MkdirAll(filepath.Dir(fakePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fakePath, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	swapHelperPaths(t, []string{fakePath})

	var records []cmdRecord
	swapExecCommand(t, recordingExecCommand(&records))

	send("Title", "Message", true)

	if len(records) != 1 {
		t.Fatalf("expected 1 command, got %d", len(records))
	}
	args := strings.Join(records[0].args, " ")
	if !strings.Contains(args, "--sound") {
		t.Errorf("args should contain --sound when sound=true: %q", args)
	}
}

// TestSend_Public verifies that Send() launches a goroutine that invokes the
// command runner exactly once. Uses a WaitGroup to synchronize with the goroutine.
func TestSend_Public(t *testing.T) {
	swapHelperPaths(t, []string{"/nonexistent/AnitoNotify.app/Contents/MacOS/anito-notify"})

	var wg sync.WaitGroup
	wg.Add(1)
	swapExecCommand(t, func(name string, arg ...string) *exec.Cmd {
		wg.Done()
		return exec.Command("true")
	})

	Send("title", "message")

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Send goroutine did not complete in time")
	}
}

// TestSendWithSound_Public verifies that SendWithSound() launches a goroutine
// that invokes the command runner exactly once.
func TestSendWithSound_Public(t *testing.T) {
	swapHelperPaths(t, []string{"/nonexistent/AnitoNotify.app/Contents/MacOS/anito-notify"})

	var wg sync.WaitGroup
	wg.Add(1)
	swapExecCommand(t, func(name string, arg ...string) *exec.Cmd {
		wg.Done()
		return exec.Command("true")
	})

	SendWithSound("title", "message")

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SendWithSound goroutine did not complete in time")
	}
}
