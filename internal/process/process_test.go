package process

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/johnnyicon/anito/internal/registry"
)

// ---------------------------------------------------------------------------
// Subprocess helpers — skipped unless the correct TEST_HELPER env var is set.
// These run as child processes launched by Start() in the tests below.
// ---------------------------------------------------------------------------

// TestHelperFakeService is invoked as a subprocess helper when
// TEST_HELPER=fake_service. It reads PORT from the environment, starts an HTTP
// server that serves /health → 200, and blocks until it is killed.
func TestHelperFakeService(t *testing.T) {
	if os.Getenv("TEST_HELPER") != "fake_service" {
		t.Skip("not a subprocess helper")
	}
	portStr := os.Getenv("PORT")
	port, err := strconv.Atoi(portStr)
	if err != nil || port == 0 {
		fmt.Fprintf(os.Stderr, "TestHelperFakeService: invalid PORT=%q\n", portStr)
		os.Exit(1)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := &http.Server{
		Addr:    fmt.Sprintf("localhost:%d", port),
		Handler: mux,
	}
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "TestHelperFakeService: ListenAndServe: %v\n", err)
		os.Exit(1)
	}
	// Block forever — parent will SIGTERM.
	select {}
}

// TestHelperCrashImmediately is invoked as a subprocess helper when
// TEST_HELPER=crash. It exits immediately with status 1.
func TestHelperCrashImmediately(t *testing.T) {
	if os.Getenv("TEST_HELPER") != "crash" {
		t.Skip("not a subprocess helper")
	}
	os.Exit(1)
}

// ---------------------------------------------------------------------------
// Helper to build a registry.Service pointing at os.Args[0] (the test binary).
// ---------------------------------------------------------------------------

func capitalise(s string) string {
	if len(s) == 0 {
		return s
	}
	// "fake_service" → "FakeService", "crash" → "Crash"
	result := []byte(s)
	result[0] -= 32 // to upper
	for i := 1; i < len(result); i++ {
		if result[i] == '_' && i+1 < len(result) {
			copy(result[i:], result[i+1:])
			result = result[:len(result)-1]
			result[i] -= 32
		}
	}
	return string(result)
}

// newTestManager creates a process.Manager with a temp log dir and registry.
func newTestManager(t *testing.T) (*Manager, *registry.Registry) {
	t.Helper()
	logDir := t.TempDir()
	reg, err := registry.New(t.TempDir())
	if err != nil {
		t.Fatalf("registry.New: %v", err)
	}
	mgr, err := New(logDir, reg)
	if err != nil {
		t.Fatalf("process.New: %v", err)
	}
	return mgr, reg
}

// registerAndStart registers svc in reg, then calls mgr.Start.
// It also appends the given extraEnv vars to os.Environ via the registry's
// env injection by patching the manager's logDir env through a temp env file.
// For simplicity we achieve the same thing by setting the env vars in the test
// process before Start and restoring them after — Start inherits os.Environ.
func registerAndStart(t *testing.T, mgr *Manager, reg *registry.Registry, name, helper string) int {
	t.Helper()

	// Set TEST_HELPER in the current process's environment so that os.Environ()
	// — which buildCmd uses — carries it into the child.
	t.Setenv("TEST_HELPER", helper)

	svc := &registry.Service{
		Name:        name,
		Type:        registry.TypeBinary,
		BinaryPath:  os.Args[0],
		Args:        []string{"-test.run=TestHelper" + capitalise(helper), "-test.v"},
		Status:      registry.StatusStopped,
		HealthCheck: "/health",
	}
	if err := reg.Register(svc); err != nil {
		t.Fatalf("reg.Register: %v", err)
	}

	port, err := mgr.Start(svc)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop(name) })
	return port
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestStartInjectsPORTEnv starts a fake-service subprocess and verifies the
// process bound to the injected PORT by polling /health.
func TestStartInjectsPORTEnv(t *testing.T) {
	mgr, reg := newTestManager(t)
	internalPort := registerAndStart(t, mgr, reg, "fake", "fake_service")

	addr := fmt.Sprintf("http://localhost:%d/health", internalPort)
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := http.Get(addr) //nolint:noctx
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return // pass
			}
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("process did not bind to injected PORT %d within 5s: %v", internalPort, lastErr)
}

// TestCrashSetsOnCrashCallback starts a crashing subprocess and verifies that
// OnCrash is called with the service name within 2 seconds.
func TestCrashSetsOnCrashCallback(t *testing.T) {
	mgr, reg := newTestManager(t)

	called := make(chan string, 1)
	mgr.OnCrash = func(name string) {
		called <- name
	}

	t.Setenv("TEST_HELPER", "crash")
	svc := &registry.Service{
		Name:        "crasher",
		Type:        registry.TypeBinary,
		BinaryPath:  os.Args[0],
		Args:        []string{"-test.run=TestHelperCrashImmediately", "-test.v"},
		Status:      registry.StatusStopped,
		HealthCheck: "/health",
	}
	if err := reg.Register(svc); err != nil {
		t.Fatalf("reg.Register: %v", err)
	}
	if _, err := mgr.Start(svc); err != nil {
		t.Fatalf("Start: %v", err)
	}

	select {
	case name := <-called:
		if name != "crasher" {
			t.Errorf("OnCrash called with %q, want %q", name, "crasher")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OnCrash not called within 5s after process crash")
	}
}

// TestStopPreventsOnCrash starts a fake-service subprocess, stops it
// intentionally, and verifies OnCrash is NOT called (because it is a clean
// stop, not a crash).
func TestStopPreventsOnCrash(t *testing.T) {
	mgr, reg := newTestManager(t)

	called := make(chan string, 1)
	mgr.OnCrash = func(name string) {
		called <- name
	}

	internalPort := registerAndStart(t, mgr, reg, "healthy", "fake_service")

	// Wait for the process to be up before stopping it.
	addr := fmt.Sprintf("http://localhost:%d/health", internalPort)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(addr) //nolint:noctx
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	if err := mgr.Stop("healthy"); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Wait long enough for an OnCrash callback to have fired if it were going to.
	select {
	case name := <-called:
		t.Errorf("OnCrash called unexpectedly with %q after intentional Stop", name)
	case <-time.After(300 * time.Millisecond):
		// pass — no crash callback fired
	}
}

// Ensure net is referenced (used by freePort in helpers above, but let's keep
// the import used directly here too).
var _ net.Listener
