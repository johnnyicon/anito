// Package notify sends macOS desktop notifications.
//
// Uses the AnitoNotify.app Swift helper if installed (custom icon, native UNUserNotificationCenter).
// Falls back to osascript if the helper is not found.
// All calls are best-effort and never block the caller.
package notify

import (
	"os"
	"os/exec"
	"path/filepath"
)

// execCommand creates OS commands. Swapped in tests to avoid spawning real processes.
var execCommand func(name string, arg ...string) *exec.Cmd = exec.Command

// Candidate paths for the Swift notification helper (checked in order).
var helperPaths = []string{
	"/Applications/AnitoNotify.app/Contents/MacOS/anito-notify",
}

func init() {
	// Also check ~/Applications
	if home, err := os.UserHomeDir(); err == nil {
		helperPaths = append(helperPaths,
			filepath.Join(home, "Applications", "AnitoNotify.app", "Contents", "MacOS", "anito-notify"),
		)
	}
}

// resolveHelper returns the path to the Swift helper if it exists, or empty string.
func resolveHelper() string {
	for _, p := range helperPaths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// Send fires a macOS notification with the given title and message.
func Send(title, message string) {
	go send(title, message, false)
}

// SendWithSound fires a notification with the default system sound.
func SendWithSound(title, message string) {
	go send(title, message, true)
}

func send(title, message string, sound bool) {
	if helper := resolveHelper(); helper != "" {
		args := []string{"--title", title, "--message", message}
		if sound {
			args = append(args, "--sound")
		}
		// Use `open` to launch as a proper app (required for notification permissions)
		appPath := filepath.Dir(filepath.Dir(filepath.Dir(helper))) // .../AnitoNotify.app
		openArgs := append([]string{appPath, "--args"}, args...)
		_ = execCommand("open", openArgs...).Run()
		return
	}

	// Fallback: osascript (works but shows Script Editor icon)
	script := `display notification "` + escapeAS(message) + `" with title "` + escapeAS(title) + `"`
	if sound {
		script += ` sound name "default"`
	}
	_ = execCommand("osascript", "-e", script).Run()
}

// escapeAS escapes double quotes and backslashes for AppleScript strings.
func escapeAS(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '"' || s[i] == '\\' {
			out = append(out, '\\')
		}
		out = append(out, s[i])
	}
	return string(out)
}
