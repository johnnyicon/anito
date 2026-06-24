package process

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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

// TestHelperAspnetMockService is invoked as a subprocess helper when
// TEST_HELPER=aspnet_mock_service. It mimics how ASP.NET Core behaves:
// it reads ASPNETCORE_HTTP_PORTS (not PORT) and serves /health on that port.
// This lets us verify Anito's env var injection without needing the .NET SDK.
func TestHelperAspnetMockService(t *testing.T) {
	if os.Getenv("TEST_HELPER") != "aspnet_mock_service" {
		t.Skip("not a subprocess helper")
	}
	portStr := os.Getenv("ASPNETCORE_HTTP_PORTS")
	port, err := strconv.Atoi(portStr)
	if err != nil || port == 0 {
		fmt.Fprintf(os.Stderr, "TestHelperAspnetMockService: invalid ASPNETCORE_HTTP_PORTS=%q\n", portStr)
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
		fmt.Fprintf(os.Stderr, "TestHelperAspnetMockService: ListenAndServe: %v\n", err)
		os.Exit(1)
	}
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

	ports, err := mgr.Start(svc)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop(name) })
	// Return the primary (default) port for test compatibility.
	for _, p := range ports {
		return p
	}
	return 0
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

// TestStartInjectsASPNETEnvVars verifies that Anito injects ASPNETCORE_HTTP_PORTS
// and ASPNETCORE_URLS alongside PORT. The mock service reads ASPNETCORE_HTTP_PORTS
// (not PORT), mimicking ASP.NET Core's default behaviour. If the health check
// succeeds, the env var was injected and used correctly.
func TestStartInjectsASPNETEnvVars(t *testing.T) {
	mgr, reg := newTestManager(t)
	internalPort := registerAndStart(t, mgr, reg, "aspnet", "aspnet_mock_service")

	addr := fmt.Sprintf("http://localhost:%d/health", internalPort)
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := http.Get(addr) //nolint:noctx
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return // pass — process bound to ASPNETCORE_HTTP_PORTS
			}
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("ASP.NET mock did not bind to ASPNETCORE_HTTP_PORTS %d within 5s: %v", internalPort, lastErr)
}

func TestReserveInternalPortsHoldsListenersUntilReleased(t *testing.T) {
	svc := &registry.Service{
		Name:        "multi",
		StablePorts: map[string]int{"http": 8080, "ws": 8081},
	}

	ports, reservations, err := reserveInternalPorts(svc)
	if err != nil {
		t.Fatalf("reserveInternalPorts: %v", err)
	}
	defer closePortReservations(reservations)
	if len(ports) != 2 {
		t.Fatalf("ports len = %d, want 2", len(ports))
	}
	if ports["http"] == ports["ws"] {
		t.Fatalf("reserved duplicate internal ports: %v", ports)
	}
	for name, port := range ports {
		listener, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", port))
		if err == nil {
			_ = listener.Close()
			t.Fatalf("port %s=%d was rebindable while reservation was held", name, port)
		}
	}

	closePortReservations(reservations)
	reservations = nil
	for name, port := range ports {
		listener, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", port))
		if err != nil {
			t.Fatalf("port %s=%d was not rebindable after release: %v", name, port, err)
		}
		_ = listener.Close()
	}
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

// ---------------------------------------------------------------------------
// Tests for uncovered functions
// ---------------------------------------------------------------------------

// TestMarkDrainingProcess_RequiresStartedCommand verifies that callers cannot
// create stale draining entries from nil or unstarted commands.
func TestMarkDrainingProcess_RequiresStartedCommand(t *testing.T) {
	mgr, _ := newTestManager(t)
	if got := mgr.MarkDrainingProcess(nil); got != 0 {
		t.Fatalf("MarkDrainingProcess(nil) = %d, want 0", got)
	}
	if got := mgr.MarkDrainingProcess(&exec.Cmd{}); got != 0 {
		t.Fatalf("MarkDrainingProcess(unstarted) = %d, want 0", got)
	}
	if len(mgr.draining) != 0 {
		t.Fatalf("draining entries = %+v, want none", mgr.draining)
	}
}

// TestIsRunning returns true after Start, false after Stop.
func TestIsRunning(t *testing.T) {
	mgr, reg := newTestManager(t)

	if mgr.IsRunning("nope") {
		t.Error("IsRunning returned true for unknown service")
	}

	registerAndStart(t, mgr, reg, "isrunning", "fake_service")
	if !mgr.IsRunning("isrunning") {
		t.Error("IsRunning returned false after Start")
	}

	_ = mgr.Stop("isrunning")
	if mgr.IsRunning("isrunning") {
		t.Error("IsRunning returned true after Stop")
	}
}

// TestDeregister removes a running process from the table and returns its PID.
func TestDeregister(t *testing.T) {
	mgr, reg := newTestManager(t)
	registerAndStart(t, mgr, reg, "dereg", "fake_service")

	pid, cmd, done := mgr.Deregister("dereg")
	if pid == 0 {
		t.Error("Deregister returned pid=0 for running process")
	}
	if cmd == nil {
		t.Error("Deregister returned nil cmd")
	}
	if done == nil {
		t.Error("Deregister returned nil done channel")
	}

	// Process is no longer tracked.
	if mgr.IsRunning("dereg") {
		t.Error("IsRunning still true after Deregister")
	}

	// Clean up: kill the deregistered process.
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		<-done
	}
}

// TestDeregister_Unknown returns zero values for unknown service.
func TestDeregister_Unknown(t *testing.T) {
	mgr, _ := newTestManager(t)
	pid, cmd, done := mgr.Deregister("nope")
	if pid != 0 || cmd != nil || done != nil {
		t.Errorf("Deregister(unknown) = (%d, %v, %v), want (0, nil, nil)", pid, cmd, done)
	}
}

// TestPID returns non-zero after Start, zero after Stop.
func TestPID(t *testing.T) {
	mgr, reg := newTestManager(t)

	if mgr.PID("nope") != 0 {
		t.Error("PID returned non-zero for unknown service")
	}

	registerAndStart(t, mgr, reg, "pid-test", "fake_service")
	if mgr.PID("pid-test") == 0 {
		t.Error("PID returned 0 after Start")
	}
}

func TestPIDAlive(t *testing.T) {
	if !PIDAlive(os.Getpid()) {
		t.Fatal("PIDAlive(current pid) = false, want true")
	}
	if PIDAlive(0) || PIDAlive(-1) {
		t.Fatal("PIDAlive should reject non-positive PIDs")
	}

	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start: %v", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatalf("cmd.Wait: %v", err)
	}
	if PIDAlive(pid) {
		t.Fatalf("PIDAlive(exited pid %d) = true, want false", pid)
	}
}

// TestInternalPort returns port after Start, 0 for unknown.
func TestInternalPort(t *testing.T) {
	mgr, reg := newTestManager(t)

	if mgr.InternalPort("nope") != 0 {
		t.Error("InternalPort returned non-zero for unknown")
	}

	port := registerAndStart(t, mgr, reg, "iport", "fake_service")
	got := mgr.InternalPort("iport")
	if got != port {
		t.Errorf("InternalPort = %d, want %d", got, port)
	}
}

// TestInternalPorts returns all named ports after Start.
func TestInternalPorts(t *testing.T) {
	mgr, reg := newTestManager(t)

	if mgr.InternalPorts("nope") != nil {
		t.Error("InternalPorts returned non-nil for unknown")
	}

	registerAndStart(t, mgr, reg, "iports", "fake_service")
	ports := mgr.InternalPorts("iports")
	if len(ports) == 0 {
		t.Error("InternalPorts returned empty map after Start")
	}
}

// TestHasDefaultOnly verifies the helper function.
func TestHasDefaultOnly(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]int
		want bool
	}{
		{"default only", map[string]int{"default": 1}, true},
		{"multi port", map[string]int{"ws": 1, "http": 2}, false},
		{"empty", map[string]int{}, false},
		{"non-default single", map[string]int{"other": 1}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasDefaultOnly(tc.in); got != tc.want {
				t.Errorf("hasDefaultOnly(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestPrimaryPortFromMap verifies port selection logic.
func TestPrimaryPortFromMap(t *testing.T) {
	cases := []struct {
		name            string
		ports           map[string]int
		healthCheckPort string
		want            int
	}{
		{"health check port", map[string]int{"ws": 1, "http": 2}, "ws", 1},
		{"default fallback", map[string]int{"default": 3, "ws": 1}, "", 3},
		{"first port", map[string]int{"ws": 1}, "", 1},
		{"empty map", map[string]int{}, "", 0},
		{"missing hc port", map[string]int{"ws": 1}, "missing", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := primaryPortFromMap(tc.ports, tc.healthCheckPort); got != tc.want {
				t.Errorf("primaryPortFromMap = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestLoadEnvFile reads KEY=VALUE lines, skips comments and empty lines.
func TestLoadEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "# comment\nFOO=bar\n\nBAZ=qux\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	vars, err := loadEnvFile(path)
	if err != nil {
		t.Fatalf("loadEnvFile: %v", err)
	}
	if len(vars) != 2 {
		t.Fatalf("len(vars) = %d, want 2", len(vars))
	}
	if vars[0] != "FOO=bar" {
		t.Errorf("vars[0] = %q, want FOO=bar", vars[0])
	}
	if vars[1] != "BAZ=qux" {
		t.Errorf("vars[1] = %q, want BAZ=qux", vars[1])
	}
}

// TestLoadEnvFile_NormalizesEnvSyntax verifies supported dotenv-style syntax.
func TestLoadEnvFile_NormalizesEnvSyntax(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := strings.Join([]string{
		"  # indented comment",
		" export FOO = \"bar baz\" ",
		"BAZ='qux'",
		"EMPTY=",
		"PORT = 3000",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	vars, err := loadEnvFile(path)
	if err != nil {
		t.Fatalf("loadEnvFile: %v", err)
	}
	want := []string{"FOO=bar baz", "BAZ=qux", "EMPTY=", "PORT=3000"}
	if strings.Join(vars, "|") != strings.Join(want, "|") {
		t.Fatalf("vars = %v, want %v", vars, want)
	}
}

func TestLoadEnvFile_RejectsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "GOOD=value\nnot-an-env-line\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadEnvFile(path)
	if err == nil {
		t.Fatal("expected malformed env line error")
	}
	if !strings.Contains(err.Error(), ".env:2") || !strings.Contains(err.Error(), "expected KEY=VALUE") {
		t.Fatalf("error = %q, want file line and parse reason", err.Error())
	}
}

// TestLoadEnvFile_Missing returns error for nonexistent file.
func TestLoadEnvFile_Missing(t *testing.T) {
	_, err := loadEnvFile("/nonexistent/file")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

// TestLoadEnvFile_NoTrailingNewline handles file without trailing newline.
func TestLoadEnvFile_NoTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("KEY=val"), 0644); err != nil {
		t.Fatal(err)
	}
	vars, err := loadEnvFile(path)
	if err != nil {
		t.Fatalf("loadEnvFile: %v", err)
	}
	if len(vars) != 1 || vars[0] != "KEY=val" {
		t.Errorf("vars = %v, want [KEY=val]", vars)
	}
}

// TestSplitLines verifies the line splitter.
func TestSplitLines(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"simple", "a\nb\nc", []string{"a", "b", "c"}},
		{"trailing newline", "a\nb\n", []string{"a", "b"}},
		{"empty", "", nil},
		{"single line", "hello", []string{"hello"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitLines(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("splitLines(%q) = %v (len %d), want %v (len %d)", tc.in, got, len(got), tc.want, len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("splitLines(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
				}
			}
		})
	}
}

// --- MarkDrainingProcess positive command ---

func TestMarkDrainingProcess_StartedCommand(t *testing.T) {
	mgr, _ := newTestManager(t)
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	pid := mgr.MarkDrainingProcess(cmd)
	if pid == 0 {
		t.Fatal("MarkDrainingProcess returned pid=0 for started command")
	}
	mgr.mu.RLock()
	_, ok := mgr.draining[pid]
	mgr.mu.RUnlock()
	if !ok {
		t.Fatalf("MarkDrainingProcess did not set draining[%d]", pid)
	}
}

// --- DrainProc (exported wrapper) ---

// TestDrainProc_NilProcess verifies DrainProc is a no-op for an unstarted cmd.
func TestDrainProc_NilProcess(t *testing.T) {
	cmd := &exec.Cmd{} // no Process — cmd.Process is nil
	err := DrainProc(cmd, nil)
	if err != nil {
		t.Errorf("DrainProc with nil Process returned error: %v", err)
	}
}

// TestDrainProc_NilDone verifies DrainProc does not panic when done is nil.
// Uses a process that exits quickly so the path exercises the no-done branch
// only up to the SIGTERM step (the actual sleep+SIGKILL is skipped because
// the process exits fast and the function returns early on SIGTERM success).
func TestDrainProc_NilDone(t *testing.T) {
	// Use a process that exits immediately so cmd.Process is valid but the
	// process is already gone when DrainProc runs.
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start: %v", err)
	}
	_ = cmd.Wait() // let it finish; Process is still non-nil after Wait
	// drainProc with nil done: sends SIGTERM (ignored since process is gone).
	// The function returns without sleeping because the process is already dead.
	_ = DrainProc(cmd, nil)
}

// TestDrainProc_WithDone verifies DrainProc works when done is provided.
func TestDrainProc_WithDone(t *testing.T) {
	mgr, reg := newTestManager(t)
	_ = registerAndStart(t, mgr, reg, "drainproc2", "fake_service")
	_, cmd, done := mgr.Deregister("drainproc2")
	if cmd == nil || cmd.Process == nil {
		t.Skip("no process to drain")
	}

	err := DrainProc(cmd, done)
	if err != nil {
		t.Errorf("DrainProc returned error: %v", err)
	}
}

func TestDrainProc_ReturnsWhenDoneNeverCloses(t *testing.T) {
	orig := drainTimeout
	drainTimeout = 10 * time.Millisecond
	t.Cleanup(func() { drainTimeout = orig })

	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	err := DrainProc(cmd, make(chan struct{}))
	if err == nil {
		t.Fatal("expected error when done channel never closes")
	}
	if !strings.Contains(err.Error(), "did not exit after SIGKILL") {
		t.Fatalf("error = %q, want SIGKILL timeout", err.Error())
	}
}

// --- Manager.Restart ---

// TestManagerRestart verifies that Restart stops then starts the service.
func TestManagerRestart(t *testing.T) {
	mgr, reg := newTestManager(t)
	registerAndStart(t, mgr, reg, "restart-svc", "fake_service")

	svc, ok := reg.Get("restart-svc")
	if !ok {
		t.Fatal("service not in registry")
	}

	_, err := mgr.Restart(svc)
	if err != nil {
		t.Fatalf("Restart returned error: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("restart-svc") })

	if !mgr.IsRunning("restart-svc") {
		t.Error("expected service to be running after Restart")
	}
}

// --- buildCmd multi-port ---

// TestBuildCmd_MultiPort verifies that multi-port services get PORT_<NAME> env vars.
func TestBuildCmd_MultiPort(t *testing.T) {
	mgr, _ := newTestManager(t)
	svc := &registry.Service{
		Name:        "multi-port-svc",
		Type:        registry.TypeBinary,
		BinaryPath:  "/bin/true",
		StablePorts: map[string]int{"ws": 7172, "http": 7173},
	}
	ports := map[string]int{"ws": 58001, "http": 58002}
	cmd, logFile, err := mgr.buildCmd(svc, ports)
	if logFile != nil {
		logFile.Close()
	}
	if err != nil {
		t.Fatalf("buildCmd returned error: %v", err)
	}
	envStr := strings.Join(cmd.Env, " ")
	if !strings.Contains(envStr, "PORT_WS=58001") && !strings.Contains(envStr, "PORT_WS=58002") {
		if !strings.Contains(envStr, "PORT_WS=") {
			t.Errorf("expected PORT_WS in env, got: %s", envStr)
		}
	}
}

// TestBuildCmd_StaticReturnsError verifies buildCmd returns error for static services.
func TestBuildCmd_StaticReturnsError(t *testing.T) {
	mgr, _ := newTestManager(t)
	svc := &registry.Service{
		Name: "static-svc",
		Type: registry.TypeStatic,
	}
	_, _, err := mgr.buildCmd(svc, map[string]int{"default": 8080})
	if err == nil {
		t.Error("expected error for static service")
	}
}

// TestBuildCmd_WithEnvFile verifies buildCmd includes env vars from an env file.
func TestBuildCmd_WithEnvFile(t *testing.T) {
	mgr, _ := newTestManager(t)
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, []byte("MY_VAR=hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	svc := &registry.Service{
		Name:        "envfile-svc",
		Type:        registry.TypeBinary,
		BinaryPath:  "/bin/true",
		EnvFile:     envFile,
		StablePorts: map[string]int{"default": 8080},
	}
	cmd, logFile, err := mgr.buildCmd(svc, map[string]int{"default": 58100})
	if logFile != nil {
		logFile.Close()
	}
	if err != nil {
		t.Fatalf("buildCmd returned error: %v", err)
	}
	envStr := strings.Join(cmd.Env, " ")
	if !strings.Contains(envStr, "MY_VAR=hello") {
		t.Errorf("expected MY_VAR=hello in env, got: %s", envStr)
	}
}

// Ensure net and filepath are referenced.
var _ net.Listener
