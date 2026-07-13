package service

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/johnnyicon/anito/internal/registry"
)

func resetRestoreHooks(t *testing.T) {
	t.Helper()
	testRestoreHooks = restoreHooks{}
	t.Cleanup(func() { testRestoreHooks = restoreHooks{} })
}

func freeStablePort(t *testing.T) int {
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

func registerRestoreService(t *testing.T, svc *Service, rs *registry.Service) {
	t.Helper()
	if rs.HealthCheck == "" {
		rs.HealthCheck = "/health"
	}
	if len(rs.StablePorts) == 0 && rs.StablePort == 0 {
		rs.StablePorts = map[string]int{"default": freeStablePort(t)}
	}
	rs.NormalizePorts()
	if err := svc.reg.Register(rs); err != nil {
		t.Fatalf("register %s: %v", rs.Name, err)
	}
	if err := svc.reg.UpdateStatus(rs.Name, rs.Status, rs.PID); err != nil {
		t.Fatalf("status %s: %v", rs.Name, err)
	}
}

func TestRestoreAllListenerFirstBeforeWorkers(t *testing.T) {
	resetRestoreHooks(t)
	svc := newTestService(t)
	for _, name := range []string{"charlie", "alpha", "bravo"} {
		dir := writeStaticIndex(t, name)
		registerRestoreService(t, svc, &registry.Service{
			Name:       name,
			Type:       registry.TypeStatic,
			BinaryPath: dir,
			Status:     registry.StatusRunning,
		})
	}

	var mu sync.Mutex
	var binds []string
	var workerChecks int
	testRestoreHooks.afterBind = func(name string) {
		mu.Lock()
		defer mu.Unlock()
		binds = append(binds, name)
	}
	testRestoreHooks.beforeWorker = func(name string) {
		mu.Lock()
		defer mu.Unlock()
		workerChecks++
		if len(binds) != 3 {
			t.Fatalf("worker for %s started after %d binds, want all 3", name, len(binds))
		}
	}

	result, err := svc.RestoreAll(context.Background(), RestoreAllOptions{MaxParallel: 2})
	if err != nil {
		t.Fatalf("RestoreAll: %v", err)
	}
	if result.Restored != 3 {
		t.Fatalf("restored = %d, want 3", result.Restored)
	}
	if workerChecks != 3 {
		t.Fatalf("worker checks = %d, want 3", workerChecks)
	}
	want := []string{"alpha", "bravo", "charlie"}
	if !equalStrings(binds, want) {
		t.Fatalf("bind order = %v, want %v", binds, want)
	}
}

func TestRestoreAllStartupGateBlocksMutationsDuringReconcile(t *testing.T) {
	resetRestoreHooks(t)
	svc := newTestService(t)
	dir := writeStaticIndex(t, "blocked")
	registerRestoreService(t, svc, &registry.Service{
		Name:       "blocked-static",
		Type:       registry.TypeStatic,
		BinaryPath: dir,
		Status:     registry.StatusRunning,
	})

	entered := make(chan struct{})
	release := make(chan struct{})
	testRestoreHooks.beforeWorker = func(name string) {
		close(entered)
		<-release
	}

	done := make(chan error, 1)
	go func() {
		_, err := svc.RestoreAll(context.Background(), RestoreAllOptions{MaxParallel: 1})
		done <- err
	}()
	<-entered

	_, err := svc.Deploy(DeployRequest{})
	assertGateError(t, err)
	assertGateError(t, svc.Restart("blocked-static"))
	assertGateError(t, svc.Stop("blocked-static"))
	_, err = svc.Rollback("blocked-static")
	assertGateError(t, err)
	assertGateError(t, svc.Remove("blocked-static"))
	_, err = svc.Reserve("new-service", 0)
	assertGateError(t, err)
	_, err = svc.ReservePorts("new-service", map[string]int{"default": 0})
	assertGateError(t, err)

	if got := svc.StartupState(); !got.MutationsBlocked || got.Phase != StartPhaseReconciling {
		t.Fatalf("startup state = phase %s blocked %v, want reconciling/blocked", got.Phase, got.MutationsBlocked)
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("RestoreAll: %v", err)
	}
	if got := svc.StartupState(); got.MutationsBlocked || got.Phase != StartPhaseReady {
		t.Fatalf("startup state after restore = phase %s blocked %v, want ready/unblocked", got.Phase, got.MutationsBlocked)
	}
}

func TestRestoreAllConcurrencyBound(t *testing.T) {
	resetRestoreHooks(t)
	svc := newTestService(t)
	for _, name := range []string{"one", "two", "three", "four"} {
		dir := writeStaticIndex(t, name)
		registerRestoreService(t, svc, &registry.Service{
			Name:       name,
			Type:       registry.TypeStatic,
			BinaryPath: dir,
			Status:     registry.StatusRunning,
		})
	}

	entered := make(chan string, 4)
	release := make(chan struct{})
	var mu sync.Mutex
	active := 0
	maxActive := 0
	testRestoreHooks.beforeWorker = func(name string) {
		mu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()
		entered <- name
		<-release
		mu.Lock()
		active--
		mu.Unlock()
	}

	done := make(chan error, 1)
	go func() {
		_, err := svc.RestoreAll(context.Background(), RestoreAllOptions{MaxParallel: 2})
		done <- err
	}()

	<-entered
	<-entered
	select {
	case name := <-entered:
		t.Fatalf("third worker %s entered before first wave was released", name)
	default:
	}
	release <- struct{}{}
	release <- struct{}{}
	<-entered
	<-entered
	release <- struct{}{}
	release <- struct{}{}

	if err := <-done; err != nil {
		t.Fatalf("RestoreAll: %v", err)
	}
	if maxActive > 2 {
		t.Fatalf("max active workers = %d, want <= 2", maxActive)
	}
}

func TestRestoreAllOutcomesAreIsolated(t *testing.T) {
	resetRestoreHooks(t)
	svc := newTestService(t)
	occupied, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = occupied.Close() })
	occupiedPort := occupied.Addr().(*net.TCPAddr).Port

	staticDir := writeStaticIndex(t, "ok")
	registerRestoreService(t, svc, &registry.Service{
		Name:        "bind-fail",
		Type:        registry.TypeStatic,
		BinaryPath:  staticDir,
		StablePorts: map[string]int{"default": occupiedPort},
		Status:      registry.StatusRunning,
	})
	registerRestoreService(t, svc, &registry.Service{
		Name:       "orphan",
		Type:       registry.TypeBinary,
		BinaryPath: filepath.Join(t.TempDir(), "missing"),
		Status:     registry.StatusRunning,
	})
	registerRestoreService(t, svc, &registry.Service{
		Name:       "static-ok",
		Type:       registry.TypeStatic,
		BinaryPath: staticDir,
		Status:     registry.StatusRunning,
	})
	registerRestoreService(t, svc, &registry.Service{
		Name:       "stopped",
		Type:       registry.TypeStatic,
		BinaryPath: staticDir,
		Status:     registry.StatusStopped,
	})

	result, err := svc.RestoreAll(context.Background(), RestoreAllOptions{MaxParallel: 2})
	if err != nil {
		t.Fatalf("RestoreAll: %v", err)
	}
	outcomes := map[string]RestoreOutcome{}
	for _, serviceResult := range result.Services {
		outcomes[serviceResult.Name] = serviceResult.Outcome
	}
	if outcomes["bind-fail"] != RestoreBindFail {
		t.Fatalf("bind-fail outcome = %q, want %q", outcomes["bind-fail"], RestoreBindFail)
	}
	if outcomes["orphan"] != RestoreOrphaned {
		t.Fatalf("orphan outcome = %q, want %q", outcomes["orphan"], RestoreOrphaned)
	}
	if outcomes["static-ok"] != RestoreStatic {
		t.Fatalf("static-ok outcome = %q, want %q", outcomes["static-ok"], RestoreStatic)
	}
	if outcomes["stopped"] != RestoreSkipped {
		t.Fatalf("stopped outcome = %q, want %q", outcomes["stopped"], RestoreSkipped)
	}

	assertStatus(t, svc, "bind-fail", registry.StatusFailed)
	assertStatus(t, svc, "orphan", registry.StatusOrphaned)
	assertStatus(t, svc, "static-ok", registry.StatusRunning)
	assertStatus(t, svc, "stopped", registry.StatusStopped)
}

func TestRestoreAllRestoresRunningBinary(t *testing.T) {
	resetRestoreHooks(t)
	t.Setenv("TEST_HELPER", "fake_service_lifecycle")
	svc := newTestService(t)
	registerRestoreService(t, svc, &registry.Service{
		Name:               "binary-ok",
		Type:               registry.TypeBinary,
		BinaryPath:         os.Args[0],
		Args:               []string{"-test.run=TestHelperFakeServiceLifecycle", "-test.v"},
		HealthCheck:        "/health",
		HealthCheckTimeout: 5 * time.Second,
		Status:             registry.StatusRunning,
	})
	t.Cleanup(func() { _ = svc.Stop("binary-ok") })

	result, err := svc.RestoreAll(context.Background(), RestoreAllOptions{MaxParallel: 1})
	if err != nil {
		t.Fatalf("RestoreAll: %v", err)
	}
	if result.Restored != 1 || result.Services[0].Outcome != RestoreRunning {
		t.Fatalf("result = restored %d services %+v, want one running", result.Restored, result.Services)
	}
	got, err := svc.Status("binary-ok")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != registry.StatusRunning || got.PID == 0 {
		t.Fatalf("status=%q pid=%d, want running with pid", got.Status, got.PID)
	}
}

func assertGateError(t *testing.T, err error) {
	t.Helper()
	var gate *StartupGateError
	if !errors.As(err, &gate) {
		t.Fatalf("error = %v, want StartupGateError", err)
	}
}

func assertStatus(t *testing.T, svc *Service, name string, want registry.ServiceStatus) {
	t.Helper()
	got, err := svc.Status(name)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != want {
		t.Fatalf("%s status = %q, want %q", name, got.Status, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
