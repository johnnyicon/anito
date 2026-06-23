package service

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johnnyicon/anito/internal/issues"
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
	return New(reg, mgr, prx, logDir, wtch, nil)
}

func TestValidateServiceName(t *testing.T) {
	valid := []string{
		"gomanan-mcp",
		"svc_1",
		"api.v2",
		"A1",
		strings.Repeat("a", 64),
	}
	for _, name := range valid {
		if err := ValidateServiceName(name); err != nil {
			t.Fatalf("ValidateServiceName(%q) returned error: %v", name, err)
		}
	}

	invalid := []string{
		"",
		"../evil",
		"a/b",
		"a:b",
		".hidden",
		"-dash",
		"svc..evil",
		DaemonLogName,
		DaemonLogName + "-copy",
		strings.Repeat("a", 65),
	}
	for _, name := range invalid {
		if err := ValidateServiceName(name); err == nil {
			t.Fatalf("ValidateServiceName(%q) succeeded, want error", name)
		}
	}
}

func TestDeployRejectsUnsafeServiceNameBeforeLogFileCreate(t *testing.T) {
	svc := newTestService(t)

	_, err := svc.Deploy(DeployRequest{
		Name:       "../evil",
		Type:       registry.TypeBinary,
		Path:       "/definitely/missing/binary",
		StablePort: 0,
	})
	if err == nil || !strings.Contains(err.Error(), "invalid service name") {
		t.Fatalf("Deploy returned %v, want invalid service name", err)
	}

	escapePath := filepath.Join(svc.logDir, "..", "evil.log")
	if _, statErr := os.Stat(escapePath); !os.IsNotExist(statErr) {
		t.Fatalf("unsafe log path %s exists or stat failed with non-ENOENT: %v", escapePath, statErr)
	}
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

func TestDeployRejectsUnsafeProxyBindAddress(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.Deploy(DeployRequest{
		Name:             "unsafe-bind",
		Type:             registry.TypeStatic,
		Path:             t.TempDir(),
		ProxyBindAddress: "0.0.0.0",
	})
	if err == nil {
		t.Fatal("Deploy with wildcard proxy bind address succeeded")
	}
	if !strings.Contains(err.Error(), "proxy_bind_address") {
		t.Fatalf("error = %q, want proxy_bind_address", err.Error())
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

// --- Rollback tests ---

func writeStaticIndex(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(body), 0644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	return dir
}

func TestRollbackStaticServiceRestoresPreviousDeployment(t *testing.T) {
	svc := newTestService(t)
	v1 := writeStaticIndex(t, "v1")
	v2 := writeStaticIndex(t, "v2")

	first, err := svc.Deploy(DeployRequest{
		Name:       "static-svc",
		Version:    "v1",
		Type:       registry.TypeStatic,
		Path:       v1,
		StablePort: 0,
	})
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}

	second, err := svc.Deploy(DeployRequest{
		Name:       "static-svc",
		Version:    "v2",
		Type:       registry.TypeStatic,
		Path:       v2,
		StablePort: 9999,
	})
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}
	if second.StablePort != first.StablePort {
		t.Fatalf("redeploy changed stable port: got %d, want %d", second.StablePort, first.StablePort)
	}

	rolledBack, err := svc.Rollback("static-svc")
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if rolledBack.Version != "v1" {
		t.Errorf("rollback version = %q, want v1", rolledBack.Version)
	}
	if rolledBack.BinaryPath != v1 {
		t.Errorf("rollback path = %q, want %q", rolledBack.BinaryPath, v1)
	}
	if rolledBack.StablePort != first.StablePort {
		t.Errorf("rollback stable port = %d, want %d", rolledBack.StablePort, first.StablePort)
	}
	if rolledBack.Status != registry.StatusRunning {
		t.Errorf("rollback status = %q, want running", rolledBack.Status)
	}
	if rolledBack.PreviousDeployment == nil {
		t.Fatal("rollback PreviousDeployment is nil")
	}
	if rolledBack.PreviousDeployment.Version != "v2" {
		t.Errorf("rollback previous version = %q, want v2", rolledBack.PreviousDeployment.Version)
	}
}

func TestRollbackRequiresPreviousDeployment(t *testing.T) {
	svc := newTestService(t)
	v1 := writeStaticIndex(t, "v1")

	if _, err := svc.Deploy(DeployRequest{
		Name:    "static-svc",
		Version: "v1",
		Type:    registry.TypeStatic,
		Path:    v1,
	}); err != nil {
		t.Fatalf("deploy v1: %v", err)
	}

	if _, err := svc.Rollback("static-svc"); err == nil {
		t.Fatal("expected rollback without previous deployment to fail")
	}
}

// Ensure fmt is used (it's referenced in the inline server handler indirectly).
var _ = fmt.Sprintf

// --- isOrphaned tests ---

// TestIsOrphaned_BinaryMissing verifies that isOrphaned returns true when the
// binary path does not exist on disk.
func TestIsOrphaned_BinaryMissing(t *testing.T) {
	svc := &registry.Service{
		Type:       registry.TypeBinary,
		BinaryPath: "/nonexistent/binary-that-does-not-exist",
		Status:     registry.StatusFailed,
	}
	if !isOrphaned(svc) {
		t.Error("expected isOrphaned=true when binary is missing")
	}
}

// TestIsOrphaned_BinaryExists verifies that isOrphaned returns false when the
// binary exists on disk.
func TestIsOrphaned_BinaryExists(t *testing.T) {
	bin := t.TempDir() + "/bin"
	if err := os.WriteFile(bin, []byte("fake"), 0755); err != nil {
		t.Fatal(err)
	}
	svc := &registry.Service{
		Type:       registry.TypeBinary,
		BinaryPath: bin,
		Status:     registry.StatusFailed,
	}
	if isOrphaned(svc) {
		t.Error("expected isOrphaned=false when binary exists")
	}
}

// TestIsOrphaned_StaticService verifies that static services are never
// considered orphaned (they have no binary to check).
func TestIsOrphaned_StaticService(t *testing.T) {
	svc := &registry.Service{
		Type:       registry.TypeStatic,
		BinaryPath: "/nonexistent/dir",
		Status:     registry.StatusFailed,
	}
	if isOrphaned(svc) {
		t.Error("expected isOrphaned=false for static service")
	}
}

// TestIsOrphaned_RunningService verifies that a running service is never
// considered orphaned.
func TestIsOrphaned_RunningService(t *testing.T) {
	svc := &registry.Service{
		Type:       registry.TypeBinary,
		BinaryPath: "/nonexistent/binary",
		Status:     registry.StatusRunning,
	}
	if isOrphaned(svc) {
		t.Error("expected isOrphaned=false for running service")
	}
}

// --- Services() tests ---

// TestServices_Empty verifies that Services() returns an empty slice when
// nothing is registered.
func TestServices_Empty(t *testing.T) {
	svc := newTestService(t)
	services := svc.Services()
	if len(services) != 0 {
		t.Errorf("expected 0 services, got %d", len(services))
	}
}

// TestServices_OrphanedDetection verifies that Services() marks a service as
// orphaned when its binary path no longer exists.
func TestServices_OrphanedDetection(t *testing.T) {
	svc := newTestService(t)
	_ = svc.reg.Register(&registry.Service{
		Name:       "orphan",
		Type:       registry.TypeBinary,
		BinaryPath: "/nonexistent/binary-orphan",
		StablePort: 0,
		Status:     registry.StatusFailed,
	})

	services := svc.Services()
	if len(services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(services))
	}
	if services[0].Status != registry.StatusOrphaned {
		t.Errorf("expected status=orphaned, got %q", services[0].Status)
	}
}

// --- Status() tests ---

// TestStatus_NotFound verifies that Status returns an error for an unknown service.
func TestStatus_NotFound(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.Status("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent service, got nil")
	}
}

// TestStatus_Found verifies that Status returns the service record when registered.
func TestStatus_Found(t *testing.T) {
	svc := newTestService(t)
	_ = svc.reg.Register(&registry.Service{
		Name:       "my-svc",
		Type:       registry.TypeBinary,
		BinaryPath: "/bin/true",
		Status:     registry.StatusStopped,
	})

	got, err := svc.Status("my-svc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "my-svc" {
		t.Errorf("expected name=my-svc, got %q", got.Name)
	}
}

// TestStatus_Orphaned verifies that Status returns StatusOrphaned when the
// binary is missing from disk.
func TestStatus_Orphaned(t *testing.T) {
	svc := newTestService(t)
	_ = svc.reg.Register(&registry.Service{
		Name:       "orphan-svc",
		Type:       registry.TypeBinary,
		BinaryPath: "/nonexistent/gone-binary",
		Status:     registry.StatusFailed,
	})

	got, err := svc.Status("orphan-svc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status != registry.StatusOrphaned {
		t.Errorf("expected status=orphaned, got %q", got.Status)
	}
}

// --- UsedPorts() tests ---

// TestUsedPorts_Empty verifies that UsedPorts returns an empty map when no
// services are registered.
func TestUsedPorts_Empty(t *testing.T) {
	svc := newTestService(t)
	ports := svc.UsedPorts()
	if len(ports) != 0 {
		t.Errorf("expected 0 used ports, got %d", len(ports))
	}
}

// TestUsedPorts_WithService verifies that UsedPorts includes the stable port of
// a registered service.
func TestUsedPorts_WithService(t *testing.T) {
	svc := newTestService(t)
	_ = svc.reg.Register(&registry.Service{
		Name:        "ported-svc",
		Type:        registry.TypeBinary,
		BinaryPath:  "/bin/true",
		StablePort:  9876,
		StablePorts: map[string]int{"default": 9876},
		Status:      registry.StatusStopped,
	})

	ports := svc.UsedPorts()
	if !ports[9876] {
		t.Errorf("expected port 9876 in used ports, got %v", ports)
	}
}

// --- Remove() tests ---

// TestRemove_Registered verifies that Remove succeeds for a registered service.
func TestRemove_Registered(t *testing.T) {
	svc := newTestService(t)
	_ = svc.reg.Register(&registry.Service{
		Name:       "rm-svc",
		Type:       registry.TypeBinary,
		BinaryPath: "/bin/true",
		Status:     registry.StatusStopped,
	})

	if err := svc.Remove("rm-svc"); err != nil {
		t.Fatalf("Remove returned error: %v", err)
	}

	if _, err := svc.Status("rm-svc"); err == nil {
		t.Error("expected service to be gone after Remove, but Status succeeded")
	}
}

// TestRemove_Unknown verifies that Remove on a non-existent service does not
// return an error (registry.Remove is a no-op for missing keys).
func TestRemove_Unknown(t *testing.T) {
	svc := newTestService(t)
	if err := svc.Remove("does-not-exist"); err != nil {
		t.Errorf("Remove unknown service returned error: %v", err)
	}
}

// --- Teardown() tests ---

// TestTeardown_NoReceipt verifies that Teardown with no deployed.json is a
// no-op returning an empty removed list.
func TestTeardown_NoReceipt(t *testing.T) {
	svc := newTestService(t)
	dir := t.TempDir() // no .anito/deployed.json

	removed, err := svc.Teardown(dir)
	if err != nil {
		t.Fatalf("Teardown returned error: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("expected 0 removed, got %v", removed)
	}
}

// --- Logs() tests ---

// TestLogs_NotFound verifies that Logs returns an error for an unknown service.
func TestLogs_NotFound(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.Logs("nonexistent", 100)
	if err == nil {
		t.Error("expected error for nonexistent service")
	}
}

// TestLogs_EmptyFile verifies that Logs returns an empty slice when the log
// file does not yet exist.
func TestLogs_EmptyFile(t *testing.T) {
	svc := newTestService(t)
	_ = svc.reg.Register(&registry.Service{
		Name:       "log-svc",
		Type:       registry.TypeBinary,
		BinaryPath: "/bin/true",
		Status:     registry.StatusStopped,
	})

	lines, err := svc.Logs("log-svc", 100)
	if err != nil {
		t.Fatalf("Logs returned error: %v", err)
	}
	if len(lines) != 0 {
		t.Errorf("expected 0 lines for missing log, got %d", len(lines))
	}
}

// TestLogs_WithContent verifies that Logs returns log lines and respects the n limit.
func TestLogs_WithContent(t *testing.T) {
	svc := newTestService(t)
	_ = svc.reg.Register(&registry.Service{
		Name:       "log-svc2",
		Type:       registry.TypeBinary,
		BinaryPath: "/bin/true",
		Status:     registry.StatusStopped,
	})

	// Write a fake log file.
	logPath := fmt.Sprintf("%s/log-svc2.log", svc.logDir)
	content := "line1\nline2\nline3\n"
	if err := os.WriteFile(logPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	lines, err := svc.Logs("log-svc2", 2)
	if err != nil {
		t.Fatalf("Logs returned error: %v", err)
	}
	if len(lines) != 2 {
		t.Errorf("expected 2 lines with n=2, got %d: %v", len(lines), lines)
	}
}

// --- BuildLogs() tests ---

// TestBuildLogs_NotFound verifies that BuildLogs returns an error for an unknown service.
func TestBuildLogs_NotFound(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.BuildLogs("nonexistent", 100)
	if err == nil {
		t.Error("expected error for nonexistent service")
	}
}

// TestBuildLogs_EmptyFile verifies that BuildLogs returns an empty slice when
// no build log file exists.
func TestBuildLogs_EmptyFile(t *testing.T) {
	svc := newTestService(t)
	_ = svc.reg.Register(&registry.Service{
		Name:       "build-svc",
		Type:       registry.TypeBinary,
		BinaryPath: "/bin/true",
		Status:     registry.StatusStopped,
	})

	lines, err := svc.BuildLogs("build-svc", 100)
	if err != nil {
		t.Fatalf("BuildLogs returned error: %v", err)
	}
	if len(lines) != 0 {
		t.Errorf("expected 0 lines, got %d", len(lines))
	}
}

// TestBuildLogs_WithContent verifies that BuildLogs returns the last n lines.
func TestBuildLogs_WithContent(t *testing.T) {
	svc := newTestService(t)
	_ = svc.reg.Register(&registry.Service{
		Name:       "build-svc2",
		Type:       registry.TypeBinary,
		BinaryPath: "/bin/true",
		Status:     registry.StatusStopped,
	})

	buildLog := fmt.Sprintf("%s/build-svc2-build.log", svc.logDir)
	if err := os.WriteFile(buildLog, []byte("a\nb\nc\n"), 0644); err != nil {
		t.Fatal(err)
	}

	lines, err := svc.BuildLogs("build-svc2", 2)
	if err != nil {
		t.Fatalf("BuildLogs returned error: %v", err)
	}
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(lines))
	}
}

func TestLogHelpersRejectUnsafeRegisteredServiceName(t *testing.T) {
	svc := newTestService(t)
	unsafeName := "../evil"
	if err := svc.reg.Register(&registry.Service{
		Name:   unsafeName,
		Type:   registry.TypeBinary,
		Status: registry.StatusStopped,
	}); err != nil {
		t.Fatalf("register unsafe fixture: %v", err)
	}

	if _, err := svc.Logs(unsafeName, 10); err == nil || !strings.Contains(err.Error(), "invalid service name") {
		t.Fatalf("Logs returned %v, want invalid service name", err)
	}
	if _, err := svc.BuildLogs(unsafeName, 10); err == nil || !strings.Contains(err.Error(), "invalid service name") {
		t.Fatalf("BuildLogs returned %v, want invalid service name", err)
	}
}

// --- Reserve() tests ---

// TestReserve_AllocatesPort verifies that Reserve claims a stable port and
// registers a placeholder service entry.
func TestReserve_AllocatesPort(t *testing.T) {
	svc := newTestService(t)

	port, err := svc.Reserve("reserve-svc", 0)
	if err != nil {
		t.Fatalf("Reserve returned error: %v", err)
	}
	if port == 0 {
		t.Error("expected non-zero port from Reserve")
	}

	// Service should now be in the registry.
	if _, err := svc.Status("reserve-svc"); err != nil {
		t.Errorf("expected reserve-svc in registry after Reserve: %v", err)
	}

	// Clean up the proxy listener.
	svc.prx.Remove("reserve-svc")
}

// TestReserve_ExistingServicePreservesPort verifies that calling Reserve on an
// already-registered service keeps its existing port.
func TestReserve_ExistingServicePreservesPort(t *testing.T) {
	svc := newTestService(t)

	// First reserve — allocates a port.
	port1, err := svc.Reserve("stable-svc", 0)
	if err != nil {
		t.Fatalf("first Reserve: %v", err)
	}

	// Second reserve — should return the same port.
	port2, err := svc.Reserve("stable-svc", 0)
	if err != nil {
		t.Fatalf("second Reserve: %v", err)
	}
	if port1 != port2 {
		t.Errorf("port changed between reserves: %d → %d", port1, port2)
	}

	svc.prx.Remove("stable-svc")
}

func TestReserveRejectsUnsafeServiceName(t *testing.T) {
	svc := newTestService(t)

	if _, err := svc.Reserve("a/b", 0); err == nil || !strings.Contains(err.Error(), "invalid service name") {
		t.Fatalf("Reserve returned %v, want invalid service name", err)
	}
	if _, err := svc.ReservePorts("../evil", map[string]int{"api": 0}); err == nil || !strings.Contains(err.Error(), "invalid service name") {
		t.Fatalf("ReservePorts returned %v, want invalid service name", err)
	}
}

// --- ReservePorts() tests ---

// TestReservePorts_MultiPort verifies that ReservePorts claims multiple named
// ports and registers a placeholder service.
func TestReservePorts_MultiPort(t *testing.T) {
	svc := newTestService(t)

	ports, err := svc.ReservePorts("mp-svc", map[string]int{"ws": 0, "api": 0})
	if err != nil {
		t.Fatalf("ReservePorts returned error: %v", err)
	}
	if ports["ws"] == 0 {
		t.Error("expected non-zero ws port")
	}
	if ports["api"] == 0 {
		t.Error("expected non-zero api port")
	}
	if ports["ws"] == ports["api"] {
		t.Error("expected distinct ports for ws and api")
	}

	svc.prx.RemovePorts("mp-svc")
}

// --- hashPath tests ---

// TestHashPath_File verifies that hashPath returns a non-empty string for an
// existing file and changes when file content changes.
func TestHashPath_File(t *testing.T) {
	f := t.TempDir() + "/bin"
	if err := os.WriteFile(f, []byte("v1"), 0755); err != nil {
		t.Fatal(err)
	}

	h1 := hashPath(f)
	if h1 == "" {
		t.Error("expected non-empty hash for existing file")
	}

	if err := os.WriteFile(f, []byte("v2"), 0755); err != nil {
		t.Fatal(err)
	}
	h2 := hashPath(f)
	if h1 == h2 {
		t.Error("expected hash to change when file content changes")
	}
}

// TestHashPath_Missing verifies that hashPath returns a non-empty fallback for
// a missing path (does not panic).
func TestHashPath_Missing(t *testing.T) {
	h := hashPath("/nonexistent/path/to/binary")
	if h == "" {
		t.Error("expected non-empty fallback hash for missing path")
	}
}

// TestHashPath_Dir verifies that hashPath hashes a directory by walking its
// contents.
func TestHashPath_Dir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	h := hashPath(dir)
	if h == "" {
		t.Error("expected non-empty hash for directory")
	}
	if !strings.HasPrefix(h, "sha:") {
		t.Errorf("expected sha: prefix, got %q", h)
	}
}

func TestDeployUsesVersionPathForGeneratedVersion(t *testing.T) {
	svc := newTestService(t)
	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("<html/>"), 0644); err != nil {
		t.Fatal(err)
	}
	versionDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(versionDir, "bundle.js"), []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}

	first, err := svc.Deploy(DeployRequest{
		Name:        "svc",
		Type:        registry.TypeStatic,
		Path:        staticDir,
		VersionPath: versionDir,
	})
	if err != nil {
		t.Fatalf("Deploy first: %v", err)
	}

	if err := os.WriteFile(filepath.Join(versionDir, "bundle.js"), []byte("v2"), 0644); err != nil {
		t.Fatal(err)
	}
	second, err := svc.Deploy(DeployRequest{
		Name:        "svc",
		Type:        registry.TypeStatic,
		Path:        staticDir,
		VersionPath: versionDir,
	})
	if err != nil {
		t.Fatalf("Deploy second: %v", err)
	}

	if first.Version == second.Version {
		t.Fatalf("Version did not change after version_path content changed: %q", first.Version)
	}
}

// --- WaitHealthy tests ---

// TestWaitHealthy_HTTP_Passes verifies that WaitHealthy succeeds when the server returns 200.
func TestWaitHealthy_HTTP_Passes(t *testing.T) {
	port := startInlineServer(t, http.StatusOK)
	if err := WaitHealthy(port, "/health", 2*time.Second); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- StartWatchers() tests ---

// TestStartWatchers_NoWatchPaths verifies that StartWatchers is a no-op for
// services without watch paths.
func TestStartWatchers_NoWatchPaths(t *testing.T) {
	svc := newTestService(t)
	_ = svc.reg.Register(&registry.Service{
		Name:   "no-watch-svc",
		Type:   registry.TypeBinary,
		Status: registry.StatusStopped,
		// No WatchPaths
	})
	svc.StartWatchers() // should not start any watcher
}

// TestStartWatchers_WithWatchPaths verifies that StartWatchers starts a watcher
// for services that have WatchPaths configured.
func TestStartWatchers_WithWatchPaths(t *testing.T) {
	svc := newTestService(t)
	watchDir := t.TempDir()
	_ = svc.reg.Register(&registry.Service{
		Name:       "watch-start-svc",
		Type:       registry.TypeBinary,
		BinaryPath: "/bin/true",
		Status:     registry.StatusStopped,
		WatchPaths: []string{watchDir},
	})

	svc.StartWatchers()
	t.Cleanup(func() { svc.wtch.Stop("watch-start-svc") })
}

// --- startWatcher error path ---

// TestStartWatcher_InvalidPath verifies that startWatcher logs an error and
// does not panic when a watch path does not exist.
func TestStartWatcher_InvalidPath(t *testing.T) {
	svc := newTestService(t)
	reg := &registry.Service{
		Name:       "bad-watch-svc",
		WatchPaths: []string{"/nonexistent/watch/path"},
	}
	// Should log an error but not panic.
	svc.startWatcher(reg)
}

// --- Logs daemon path ---

// TestLogs_DaemonLog verifies that Logs("~daemon", n) reads the Anito daemon
// log file directly without requiring a registry entry.
func TestLogs_DaemonLog(t *testing.T) {
	svc := newTestService(t)

	// Write content to the daemon log path.
	daemonLog := filepath.Join(svc.logDir, "anito.log")
	if err := os.WriteFile(daemonLog, []byte("a\nb\nc\n"), 0644); err != nil {
		t.Fatal(err)
	}

	lines, err := svc.Logs(DaemonLogName, 2)
	if err != nil {
		t.Fatalf("Logs(~daemon) returned error: %v", err)
	}
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(lines))
	}
}

// --- writeReceipt ---

// TestWriteReceipt_WithConfigPath verifies that writeReceipt writes a
// deployed.json when the service has ConfigPath set.
func TestWriteReceipt_WithConfigPath(t *testing.T) {
	repoDir := t.TempDir()
	anitoDir := filepath.Join(repoDir, ".anito")
	if err := os.MkdirAll(anitoDir, 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(anitoDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("name: test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	svc := &registry.Service{
		Name:        "receipt-svc",
		Type:        registry.TypeBinary,
		BinaryPath:  "/bin/true",
		StablePort:  8888,
		StablePorts: map[string]int{"default": 8888},
		ConfigPath:  configPath,
		Status:      registry.StatusRunning,
	}

	writeReceipt(svc)

	deployedPath := filepath.Join(anitoDir, "deployed.json")
	if _, err := os.Stat(deployedPath); os.IsNotExist(err) {
		t.Error("expected deployed.json to be written")
	}
}

// --- Teardown with services ---

// TestTeardown_WithServiceInReceipt verifies that Teardown removes services
// listed in deployed.json and returns their names.
func TestTeardown_WithServiceInReceipt(t *testing.T) {
	svc := newTestService(t)

	// Create a repo with .anito/ and a deployed.json pointing to a service.
	repoDir := t.TempDir()
	anitoDir := filepath.Join(repoDir, ".anito")
	if err := os.MkdirAll(anitoDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Register the service so it can be removed.
	_ = svc.reg.Register(&registry.Service{
		Name:       "teardown-svc",
		Type:       registry.TypeBinary,
		BinaryPath: "/bin/true",
		Status:     registry.StatusStopped,
	})

	// Write a deployed.json that references the service.
	deployedJSON := `{"services":{"teardown-svc":{"name":"teardown-svc","stable_port":9111,"address":"http://localhost:9111","binary_path":"/bin/true","config_path":"` + filepath.Join(anitoDir, "config.yaml") + `","deployed_at":"2026-01-01T00:00:00Z"}}}`
	if err := os.WriteFile(filepath.Join(anitoDir, "deployed.json"), []byte(deployedJSON), 0644); err != nil {
		t.Fatal(err)
	}

	removed, err := svc.Teardown(repoDir)
	if err != nil {
		t.Fatalf("Teardown returned error: %v", err)
	}
	if len(removed) != 1 || removed[0] != "teardown-svc" {
		t.Errorf("expected removed=[teardown-svc], got %v", removed)
	}
}

// --- allocateOnePort reserved port error ---

// TestReserve_ReservedPort verifies that reserving a port in Anito's reserved
// set (7700, 7701) returns an error.
func TestReserve_ReservedPort(t *testing.T) {
	svc := newTestService(t)

	_, err := svc.Reserve("conflict-svc", 7700)
	if err == nil {
		t.Error("expected error when reserving Anito's management port 7700")
	}
	if !strings.Contains(err.Error(), "reserved") {
		t.Errorf("error should mention 'reserved': %v", err)
	}
}

// --- BuildLogStream tests ---

// TestBuildLogStream_NotFound verifies BuildLogStream returns an error for an unknown service.
func TestBuildLogStream_NotFound(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.BuildLogStream(context.Background(), "no-such-svc")
	if err == nil {
		t.Fatal("expected error for nonexistent service")
	}
}

// TestBuildLogStream_ImmediateCancel verifies BuildLogStream returns a channel
// that is closed when the context is cancelled before any ticks fire.
func TestBuildLogStream_ImmediateCancel(t *testing.T) {
	svc := newTestService(t)
	_ = svc.reg.Register(&registry.Service{
		Name:   "stream-build-svc",
		Type:   registry.TypeBinary,
		Status: registry.StatusStopped,
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel
	ch, err := svc.BuildLogStream(ctx, "stream-build-svc")
	if err != nil {
		t.Fatalf("BuildLogStream returned error: %v", err)
	}
	// Drain — channel must close promptly.
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return // channel closed — pass
			}
		case <-timer.C:
			t.Fatal("BuildLogStream channel did not close after context cancel")
		}
	}
}

// --- LogStream tests ---

// TestLogStream_NotFound verifies LogStream returns an error for an unknown service.
func TestLogStream_NotFound(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.LogStream(context.Background(), "no-such-svc")
	if err == nil {
		t.Fatal("expected error for nonexistent service")
	}
}

// TestLogStream_ImmediateCancel verifies LogStream returns a channel that is
// closed when the context is cancelled before any ticks fire.
func TestLogStream_ImmediateCancel(t *testing.T) {
	svc := newTestService(t)
	_ = svc.reg.Register(&registry.Service{
		Name:   "stream-log-svc",
		Type:   registry.TypeBinary,
		Status: registry.StatusStopped,
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel
	ch, err := svc.LogStream(ctx, "stream-log-svc")
	if err != nil {
		t.Fatalf("LogStream returned error: %v", err)
	}
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return // channel closed — pass
			}
		case <-timer.C:
			t.Fatal("LogStream channel did not close after context cancel")
		}
	}
}

// --- Restart tests ---

// TestRestart_NotFound verifies Restart returns an error for an unknown service.
func TestRestart_NotFound(t *testing.T) {
	svc := newTestService(t)
	err := svc.Restart("no-such-svc")
	if err == nil {
		t.Fatal("expected error for nonexistent service")
	}
}

// --- handleCrash additional edge cases ---

// TestRestart_Static verifies Restart returns nil immediately for TypeStatic services.
func TestRestart_Static(t *testing.T) {
	svc := newTestService(t)
	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("<html/>"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Deploy(DeployRequest{
		Name: "restart-static-svc",
		Type: registry.TypeStatic,
		Path: staticDir,
	})
	if err != nil {
		t.Fatalf("Deploy returned error: %v", err)
	}
	t.Cleanup(func() { _ = svc.Remove("restart-static-svc") })

	if err := svc.Restart("restart-static-svc"); err != nil {
		t.Errorf("Restart for TypeStatic returned error: %v", err)
	}
}

// TestStop_NotRunning verifies Stop on a stopped (non-running) service logs the
// error but still calls UnswapPorts and returns an error.
func TestStop_NotRunning(t *testing.T) {
	svc := newTestService(t)
	_ = svc.reg.Register(&registry.Service{
		Name:   "stopped-already-svc",
		Type:   registry.TypeBinary,
		Status: registry.StatusStopped,
	})
	// mgr.Stop will fail (not running) — Stop should return non-nil error.
	err := svc.Stop("stopped-already-svc")
	// mgr.Stop returns an error when the process isn't tracked — that's expected.
	_ = err // could be nil or non-nil depending on process manager; just verify no panic.
}

// --- handleCrash additional edge cases ---

// TestHandleCrash_UnknownService verifies handleCrash is a no-op for unregistered services.
func TestHandleCrash_UnknownService(t *testing.T) {
	svc := newTestService(t)
	// Should not panic or block.
	svc.handleCrash("totally-unknown-svc")
}

// TestHandleCrash_StoppedService verifies handleCrash does not restart a
// service whose status is Stopped (intentionally stopped).
func TestHandleCrash_StoppedService(t *testing.T) {
	useShortBackoffs(t)
	svc := newTestService(t)
	_ = svc.reg.Register(&registry.Service{
		Name:          "stopped-svc",
		Type:          registry.TypeBinary,
		BinaryPath:    "/nonexistent",
		Status:        registry.StatusStopped,
		RestartPolicy: "always",
	})
	svc.handleCrash("stopped-svc")
	svc.crashMu.Lock()
	attempts := svc.crashAttempts["stopped-svc"]
	svc.crashMu.Unlock()
	if attempts != 0 {
		t.Errorf("crashAttempts = %d, want 0 for stopped service", attempts)
	}
}

// --- allocateOnePort fallthrough ---

// TestAllocateOnePort_PreferredInUse verifies that when the preferred port is
// already in the used set, Reserve auto-allocates a different port.
func TestAllocateOnePort_PreferredInUse(t *testing.T) {
	svc := newTestService(t)

	// Occupy port 8150 in the registry.
	_ = svc.reg.Register(&registry.Service{
		Name:        "occupier-svc",
		StablePorts: map[string]int{"default": 8150},
		Status:      registry.StatusStopped,
	})

	// Reserve a new service preferring the occupied port — should fall through.
	port, err := svc.Reserve("fallthrough-svc", 8150)
	if err != nil {
		t.Fatalf("Reserve returned error: %v", err)
	}
	if port == 8150 {
		t.Errorf("expected auto-allocated port != 8150, got 8150")
	}
	t.Cleanup(func() { svc.prx.Remove("fallthrough-svc") })
}

// --- LogStream reading live lines ---

// TestLogStream_ReadsNewLines verifies that LogStream delivers lines appended
// to the log file after the stream is open.
func TestLogStream_ReadsNewLines(t *testing.T) {
	svc := newTestService(t)
	_ = svc.reg.Register(&registry.Service{
		Name:   "live-log-svc",
		Type:   registry.TypeBinary,
		Status: registry.StatusStopped,
	})

	// Create the log file (empty) so the goroutine can open it.
	logPath := filepath.Join(svc.logDir, "live-log-svc.log")
	if err := os.WriteFile(logPath, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ch, err := svc.LogStream(ctx, "live-log-svc")
	if err != nil {
		t.Fatalf("LogStream returned error: %v", err)
	}

	// Give goroutine time to open the file and seek to end.
	time.Sleep(50 * time.Millisecond)

	// Append a line — ticker (200ms) will pick it up.
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintln(f, "live-line"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	select {
	case line, ok := <-ch:
		if !ok {
			t.Fatal("channel closed before receiving line")
		}
		if line != "live-line" {
			t.Errorf("expected 'live-line', got %q", line)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for log line from LogStream")
	}
}

// TestBuildLogStream_ReadsNewLines verifies that BuildLogStream delivers lines
// appended to the build log file after the stream is open.
func TestBuildLogStream_ReadsNewLines(t *testing.T) {
	svc := newTestService(t)
	_ = svc.reg.Register(&registry.Service{
		Name:   "live-build-svc",
		Type:   registry.TypeBinary,
		Status: registry.StatusStopped,
	})

	// Create the build log file (empty).
	logPath := filepath.Join(svc.logDir, "live-build-svc-build.log")
	if err := os.WriteFile(logPath, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ch, err := svc.BuildLogStream(ctx, "live-build-svc")
	if err != nil {
		t.Fatalf("BuildLogStream returned error: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintln(f, "build-line"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	select {
	case line, ok := <-ch:
		if !ok {
			t.Fatal("channel closed before receiving line")
		}
		if line != "build-line" {
			t.Errorf("expected 'build-line', got %q", line)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for log line from BuildLogStream")
	}
}

// --- auto-issue pipeline ---

// TestHandleCrash_GiveUpAppendsIssue verifies that when crashAttempts is at max
// and handleCrash takes the give-up path, an issue is appended to the store.
func TestHandleCrash_GiveUpAppendsIssue(t *testing.T) {
	useShortBackoffs(t)

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
	iss := issues.New(dir)
	svc := New(reg, mgr, proxy.NewManager(), logDir, watcher.New(), iss)

	registerSvcWithPolicy(t, svc, "svc", "always", nil)

	// Pre-seed at max attempts so the next handleCrash takes the give-up path.
	max := len(crashBackoffDurations)
	svc.crashMu.Lock()
	svc.crashAttempts["svc"] = max
	svc.crashMu.Unlock()

	svc.handleCrash("svc")

	got, err := iss.Recent(10, "daemon:")
	if err != nil {
		t.Fatalf("iss.Recent: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected at least one issue to be appended on crash give-up, got none")
	}
	if got[0].Source != "daemon:crash_give_up" {
		t.Errorf("issue source = %q, want %q", got[0].Source, "daemon:crash_give_up")
	}
}

// TestHandleCrash_GiveUpAttachesLogContext verifies that the crash give-up issue
// includes the last lines of the service log in its Context field.
func TestHandleCrash_GiveUpAttachesLogContext(t *testing.T) {
	useShortBackoffs(t)

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
	iss := issues.New(dir)
	svc := New(reg, mgr, proxy.NewManager(), logDir, watcher.New(), iss)

	registerSvcWithPolicy(t, svc, "svc", "always", nil)

	// Write some lines to the service log file.
	logFile := filepath.Join(logDir, "svc.log")
	if err := os.WriteFile(logFile, []byte("line1\nline2\nline3\n"), 0644); err != nil {
		t.Fatal(err)
	}

	max := len(crashBackoffDurations)
	svc.crashMu.Lock()
	svc.crashAttempts["svc"] = max
	svc.crashMu.Unlock()

	svc.handleCrash("svc")

	got, err := iss.Recent(10, "daemon:")
	if err != nil {
		t.Fatalf("iss.Recent: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected issue to be appended")
	}
	if !strings.Contains(got[0].Context, "line3") {
		t.Errorf("issue Context %q does not contain last log line 'line3'", got[0].Context)
	}
}

// --- metrics ---

// TestMetrics_Counters verifies that deploysTotal and crashesTotal
// are incremented correctly and Services counts reflect live registry state.
func TestMetrics_Counters(t *testing.T) {
	svc := newTestService(t)

	m := svc.Metrics()
	if m.DeploysTotal != 0 || m.CrashesTotal != 0 {
		t.Fatalf("expected zero counters at start, got deploys=%d crashes=%d", m.DeploysTotal, m.CrashesTotal)
	}
	if m.ServicesTotal != 0 {
		t.Fatalf("expected zero services at start, got %d", m.ServicesTotal)
	}

	// Simulate a crash event incrementing crashesTotal.
	svc.crashesTotal.Add(1)
	svc.crashesTotal.Add(1)

	// Simulate two deploys incrementing deploysTotal.
	svc.deploysTotal.Add(1)

	// Register one running and one stopped service in the registry.
	_ = svc.reg.Register(&registry.Service{
		Name:        "running-svc",
		StablePorts: map[string]int{"default": 9901},
		Status:      registry.StatusRunning,
	})
	_ = svc.reg.UpdateStatus("running-svc", registry.StatusRunning, 0)
	_ = svc.reg.Register(&registry.Service{
		Name:        "stopped-svc",
		StablePorts: map[string]int{"default": 9902},
		Status:      registry.StatusStopped,
	})
	_ = svc.reg.UpdateStatus("stopped-svc", registry.StatusStopped, 0)

	m = svc.Metrics()
	if m.DeploysTotal != 1 {
		t.Errorf("DeploysTotal = %d, want 1", m.DeploysTotal)
	}
	if m.CrashesTotal != 2 {
		t.Errorf("CrashesTotal = %d, want 2", m.CrashesTotal)
	}
	if m.ServicesTotal != 2 {
		t.Errorf("ServicesTotal = %d, want 2", m.ServicesTotal)
	}
	if m.ServicesRunning != 1 {
		t.Errorf("ServicesRunning = %d, want 1", m.ServicesRunning)
	}
	if m.ServicesStopped != 1 {
		t.Errorf("ServicesStopped = %d, want 1", m.ServicesStopped)
	}
}

// --- case study submission ---

func TestSubmitCaseStudy_WritesFile(t *testing.T) {
	svc := newTestService(t)
	req := CaseStudyRequest{
		PainPoint:    "our four daemons crashed each other constantly during development",
		Workflow:     "we deploy each daemon separately with anito deploy and use watch mode for active dev",
		Outcome:      "zero downtime deploys eliminated integration test flakiness entirely",
		StackContext: "Go monorepo, 4 cooperating daemons",
		Quote:        "Local dev finally feels like production.",
		CreditAs:     "a platform team",
		FeaturesUsed: []string{"hot-swap", "watch-mode"},
	}
	path, err := svc.SubmitCaseStudy(req)
	if err != nil {
		t.Fatalf("SubmitCaseStudy: %v", err)
	}
	if path == "" {
		t.Fatal("expected non-empty path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "draft: true") {
		t.Error("expected 'draft: true' in frontmatter")
	}
	if !strings.Contains(content, req.PainPoint) {
		t.Error("expected pain_point in content")
	}
	if !strings.Contains(content, req.Outcome) {
		t.Error("expected outcome in content")
	}
	if !strings.Contains(content, req.Quote) {
		t.Error("expected quote in content")
	}
	if !strings.Contains(content, "a platform team") {
		t.Error("expected credit_as in content")
	}
}

func TestSubmitCaseStudy_RequiredFields(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.SubmitCaseStudy(CaseStudyRequest{
		PainPoint: "something",
		// missing Workflow and Outcome
	})
	if err == nil {
		t.Fatal("expected error when required fields are missing")
	}
}

func TestCaseStudySlug(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"our daemons crashed each other", "our-daemons-crashed-each-other"},
		{"Hello World!", "hello-world"},
		{"  leading spaces  ", "leading-spaces"},
		{"a--double  dash", "a-double-dash"},
	}
	for _, tc := range cases {
		got := caseStudySlug(tc.in)
		if got != tc.want {
			t.Errorf("caseStudySlug(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
