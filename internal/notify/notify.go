// Package notify sends macOS desktop notifications via terminal-notifier.
// All calls are best-effort — if terminal-notifier is not installed or fails,
// the error is silently discarded. Never block the caller.
package notify

import "os/exec"

const appID = "com.anito.daemon"

// Send fires a macOS notification with the given title and message.
// It is a no-op if terminal-notifier is not on PATH.
func Send(title, message string) {
	go func() {
		_ = exec.Command("terminal-notifier",
			"-title", title,
			"-message", message,
			"-sender", appID,
		).Run()
	}()
}
