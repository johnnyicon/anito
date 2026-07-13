package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/johnnyicon/anito/internal/registry"
	"github.com/johnnyicon/anito/internal/service"
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

func TestRunDaemonStartsAPIMCPBeforeRestoreFinishes(t *testing.T) {
	apiPort := freeTestPort(t)
	mcpPort := freeTestPort(t)
	dataDir := t.TempDir()

	restoreStarted := make(chan struct{})
	releaseRestore := make(chan struct{})
	restoreDone := make(chan struct{})
	originalRestore := daemonRestoreAll
	daemonRestoreAll = func(ctx context.Context, svc *service.Service, opts service.RestoreAllOptions) (*service.RestoreAllResult, error) {
		close(restoreStarted)
		<-releaseRestore
		close(restoreDone)
		return &service.RestoreAllResult{Phase: service.StartPhaseReady}, nil
	}
	t.Cleanup(func() {
		daemonRestoreAll = originalRestore
		close(releaseRestore)
	})

	go runDaemon(apiPort, mcpPort, dataDir)
	<-restoreStarted

	waitHTTPStatus(t, "http://localhost:"+strconv.Itoa(apiPort)+"/health", http.StatusOK)
	waitTCP(t, net.JoinHostPort("localhost", strconv.Itoa(mcpPort)))

	select {
	case <-restoreDone:
		t.Fatal("restore finished before liveness checks completed")
	default:
	}
}

func freeTestPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func waitHTTPStatus(t *testing.T, url string, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url) //nolint:noctx
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == want {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s did not return %d before deadline", url, want)
}

func waitTCP(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s did not accept TCP before deadline", addr)
}
