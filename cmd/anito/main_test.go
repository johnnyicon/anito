package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/johnnyicon/anito/internal/registry"
)

func TestRestoreStartBlockedReasonAllowsMissingRecordedPID(t *testing.T) {
	for _, pid := range []int{0, -1} {
		svc := &registry.Service{Name: "svc", PID: pid}
		if reason := restoreStartBlockedReason(svc); reason != "" {
			t.Fatalf("restoreStartBlockedReason(pid=%d) = %q, want empty", pid, reason)
		}
	}
	if reason := restoreStartBlockedReason(nil); reason != "" {
		t.Fatalf("restoreStartBlockedReason(nil) = %q, want empty", reason)
	}
}

func TestRestoreStartBlockedReasonBlocksLiveRecordedPID(t *testing.T) {
	svc := &registry.Service{Name: "svc", PID: os.Getpid()}
	reason := restoreStartBlockedReason(svc)
	if !strings.Contains(reason, "recorded pid") || !strings.Contains(reason, "refusing to start duplicate") {
		t.Fatalf("reason = %q, want duplicate-start block", reason)
	}
}

func TestRestoreStartBlockedReasonAllowsExitedRecordedPID(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start: %v", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatalf("cmd.Wait: %v", err)
	}

	svc := &registry.Service{Name: "svc", PID: pid}
	if reason := restoreStartBlockedReason(svc); reason != "" {
		t.Fatalf("restoreStartBlockedReason(exited pid %d) = %q, want empty", pid, reason)
	}
}
