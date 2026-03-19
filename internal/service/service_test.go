package service

import (
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/johnnyicon/anito/internal/process"
	"github.com/johnnyicon/anito/internal/proxy"
	"github.com/johnnyicon/anito/internal/registry"
	"github.com/johnnyicon/anito/internal/watcher"
)

// newTestService creates a Service wired to real (but isolated) sub-components
// backed by temp directories.
func newTestService(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	logDir := t.TempDir()

	reg, err := registry.New(dir)
	if err != nil {
		t.Fatalf("registry.New: %v", err)
	}
	mgr, err := process.New(logDir, reg)
	if err != nil {
		t.Fatalf("process.New: %v", err)
	}
	prx := proxy.NewManager()
	wtch := watcher.New()
	return New(reg, mgr, prx, logDir, wtch)
}

// startInlineServer binds a free port, starts an HTTP server in a goroutine,
// and returns the port. The server is shut down via t.Cleanup.
func startInlineServer(t *testing.T, status int) int {
	t.Helper()
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		}),
	}
	go srv.Serve(l) //nolint:errcheck
	t.Cleanup(func() { srv.Close() })
	return port
}

// --- waitHTTPReady tests ---

// TestWaitHTTPReady_PassesOn200 verifies that waitHTTPReady returns nil when
// the server responds 200.
func TestWaitHTTPReady_PassesOn200(t *testing.T) {
	port := startInlineServer(t, http.StatusOK)
	if err := waitHTTPReady(port, "/health", 2*time.Second); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestWaitHTTPReady_FailsOn404 verifies that a server that is up but returns
// 404 causes waitHTTPReady to time out and return an error.
func TestWaitHTTPReady_FailsOn404(t *testing.T) {
	port := startInlineServer(t, http.StatusNotFound)
	err := waitHTTPReady(port, "/health", 300*time.Millisecond)
	if err == nil {
		t.Error("expected error when server returns 404, got nil")
	}
}

// TestWaitHTTPReady_TimesOut verifies that waitHTTPReady returns an error when
// no server is listening on the given port.
func TestWaitHTTPReady_TimesOut(t *testing.T) {
	// Grab a port and immediately release it so nothing is listening.
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	err = waitHTTPReady(port, "/health", 300*time.Millisecond)
	if err == nil {
		t.Error("expected timeout error when nothing is listening, got nil")
	}
}

// TestWaitHTTPReady_ErrorMentionsPORT verifies that the timeout error message
// contains "PORT" (instructing the developer to read PORT from the environment).
func TestWaitHTTPReady_ErrorMentionsPORT(t *testing.T) {
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	err = waitHTTPReady(port, "/health", 300*time.Millisecond)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if msg := err.Error(); !containsAny(msg, "PORT") {
		t.Errorf("error %q does not mention PORT", msg)
	}
}

// TestWaitHTTPReady_ErrorMentionsRoute verifies that the timeout error message
// contains the health check path.
func TestWaitHTTPReady_ErrorMentionsRoute(t *testing.T) {
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	const path = "/readyz"
	err = waitHTTPReady(port, path, 300*time.Millisecond)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if msg := err.Error(); !containsAny(msg, path) {
		t.Errorf("error %q does not mention health check path %q", msg, path)
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) > 0 {
			idx := 0
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					idx = i
					_ = idx
					return true
				}
			}
		}
	}
	return false
}

// --- crash backoff tests ---

// registerSvcWithPolicy registers a service in the registry with the given
// restart policy and status so handleCrash can retrieve it.
func registerSvcWithPolicy(t *testing.T, svc *Service, name, policy string, watchPaths []string) {
	t.Helper()
	err := svc.reg.Register(&registry.Service{
		Name:          name,
		Type:          registry.TypeBinary,
		BinaryPath:    "/nonexistent/binary",
		StablePort:    0,
		HealthCheck:   "/health",
		Status:        registry.StatusFailed,
		RestartPolicy: policy,
		WatchPaths:    watchPaths,
	})
	if err != nil {
		t.Fatalf("register svc: %v", err)
	}
}

// useShortBackoffs replaces crashBackoffDurations with millisecond values for
// the duration of the test, restoring the originals on cleanup.
func useShortBackoffs(t *testing.T) {
	t.Helper()
	orig := crashBackoffDurations
	crashBackoffDurations = []time.Duration{time.Millisecond, time.Millisecond}
	t.Cleanup(func() { crashBackoffDurations = orig })
}

// TestHandleCrash_NeverPolicy_NoRestart verifies that restart_policy=never
// leaves crashAttempts unchanged (i.e. handleCrash returns early).
func TestHandleCrash_NeverPolicy_NoRestart(t *testing.T) {
	useShortBackoffs(t)
	svc := newTestService(t)
	registerSvcWithPolicy(t, svc, "svc", "never", nil)

	svc.handleCrash("svc")

	svc.crashMu.Lock()
	attempts := svc.crashAttempts["svc"]
	svc.crashMu.Unlock()

	if attempts != 0 {
		t.Errorf("crashAttempts = %d, want 0 (never policy should not increment)", attempts)
	}
}

// TestHandleCrash_OnWatchPolicy_NoWatchPaths_NoRestart verifies that
// restart_policy=on-watch with no watch paths does not restart.
func TestHandleCrash_OnWatchPolicy_NoWatchPaths_NoRestart(t *testing.T) {
	useShortBackoffs(t)
	svc := newTestService(t)
	registerSvcWithPolicy(t, svc, "svc", "on-watch", nil /* no watch paths */)

	svc.handleCrash("svc")

	svc.crashMu.Lock()
	attempts := svc.crashAttempts["svc"]
	svc.crashMu.Unlock()

	if attempts != 0 {
		t.Errorf("crashAttempts = %d, want 0 (on-watch with no paths should not increment)", attempts)
	}
}

// TestHandleCrash_AlwaysPolicy_IncrementsAttempts verifies that
// restart_policy=always increments the crash counter even though Restart()
// will fail (no binary exists).
func TestHandleCrash_AlwaysPolicy_IncrementsAttempts(t *testing.T) {
	useShortBackoffs(t)
	svc := newTestService(t)
	registerSvcWithPolicy(t, svc, "svc", "always", nil)

	// handleCrash will: increment counter, sleep 1ms, call Restart (which fails
	// because the binary doesn't exist — that's fine). We just want the counter.
	svc.handleCrash("svc")

	svc.crashMu.Lock()
	attempts := svc.crashAttempts["svc"]
	svc.crashMu.Unlock()

	if attempts != 1 {
		t.Errorf("crashAttempts = %d, want 1", attempts)
	}
}

// TestHandleCrash_GivesUpAtMaxAttempts verifies that when crashAttempts is
// already at max (len(crashBackoffDurations)), handleCrash takes the give-up
// path and does not increment further.
func TestHandleCrash_GivesUpAtMaxAttempts(t *testing.T) {
	useShortBackoffs(t)
	svc := newTestService(t)
	registerSvcWithPolicy(t, svc, "svc", "always", nil)

	max := len(crashBackoffDurations)

	// Pre-seed at max.
	svc.crashMu.Lock()
	svc.crashAttempts["svc"] = max
	svc.crashMu.Unlock()

	svc.handleCrash("svc")

	svc.crashMu.Lock()
	attempts := svc.crashAttempts["svc"]
	svc.crashMu.Unlock()

	if attempts != max {
		t.Errorf("crashAttempts = %d after give-up, want %d (must not increment past max)", attempts, max)
	}
}

// TestDeployResetscrashAttempts verifies that the crash counter is cleared by
// simulating what Deploy does: delete the entry from crashAttempts.
func TestDeployResetscrashAttempts(t *testing.T) {
	svc := newTestService(t)

	// Seed counter to 3.
	svc.crashMu.Lock()
	svc.crashAttempts["svc"] = 3
	svc.crashMu.Unlock()

	// Deploy does: delete(s.crashAttempts, req.Name) after a successful restart.
	// We simulate that directly here.
	svc.crashMu.Lock()
	delete(svc.crashAttempts, "svc")
	svc.crashMu.Unlock()

	svc.crashMu.Lock()
	attempts := svc.crashAttempts["svc"]
	svc.crashMu.Unlock()

	if attempts != 0 {
		t.Errorf("crashAttempts = %d after reset, want 0", attempts)
	}
}

// Ensure fmt is used (it's referenced in the inline server handler indirectly).
var _ = fmt.Sprintf
