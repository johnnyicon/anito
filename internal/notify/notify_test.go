package notify

import (
	"os"
	"path/filepath"
	"testing"
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
