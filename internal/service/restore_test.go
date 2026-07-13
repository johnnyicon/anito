package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

func TestRestoreAllMixedFixturesPreserveRegistryAndProxyOutcomes(t *testing.T) {
	resetRestoreHooks(t)
	svc := newTestService(t)

	healthyEnv := writeRestoreHelperEnv(t, "healthy", "healthy")
	unhealthyEnv := writeRestoreHelperEnv(t, "unhealthy", "unhealthy")
	hangEnv := writeRestoreHelperEnv(t, "hang", "hang")
	staleEnv := writeRestoreHelperEnv(t, "healthy", "stale")
	multiEnv := writeRestoreHelperEnv(t, "healthy", "multi")

	registerRestoreService(t, svc, newRestoreBinaryRecord("healthy", healthyEnv, 800*time.Millisecond))
	registerRestoreService(t, svc, newRestoreBinaryRecord("unhealthy", unhealthyEnv, 350*time.Millisecond))
	registerRestoreService(t, svc, newRestoreBinaryRecord("hang", hangEnv, 350*time.Millisecond))

	stale := newRestoreBinaryRecord("stale", staleEnv, 800*time.Millisecond)
	stale.PID = 43210
	stale.InternalPorts = map[string]int{"default": 43211}
	stale.InternalPort = 43211
	registerRestoreService(t, svc, stale)

	stopped := newRestoreBinaryRecord("stopped", healthyEnv, 8001)
	stopped.Status = registry.StatusStopped
	registerRestoreService(t, svc, stopped)

	registerRestoreService(t, svc, &registry.Service{
		Name:               "orphaned",
		Type:               registry.TypeBinary,
		BinaryPath:         filepath.Join(t.TempDir(), "missing-binary"),
		Args:               helperLifecycleArgs(),
		EnvFile:            healthyEnv,
		HealthCheck:        "/health",
		HealthCheckTimeout: 350 * time.Millisecond,
		Status:             registry.StatusRunning,
	})

	multi := &registry.Service{
		Name:             "multi",
		Type:             registry.TypeBinary,
		BinaryPath:       os.Args[0],
		Args:             helperLifecycleArgs(),
		EnvFile:          multiEnv,
		HealthCheck:      "/health",
		HealthCheckPort:  "api",
		StablePorts:      map[string]int{"api": freeStablePort(t), "ws": freeStablePort(t)},
		ProxyBindAddress: registry.DefaultProxyBindAddress,
		Status:           registry.StatusRunning,
	}
	registerRestoreService(t, svc, multi)

	for _, name := range []string{"healthy", "stale", "multi"} {
		name := name
		t.Cleanup(func() { _ = svc.Stop(name) })
	}

	result, err := svc.RestoreAll(context.Background(), RestoreAllOptions{MaxParallel: 3})
	if err != nil {
		t.Fatalf("RestoreAll: %v", err)
	}

	gotOutcome := map[string]RestoreOutcome{}
	for _, serviceResult := range result.Services {
		gotOutcome[serviceResult.Name] = serviceResult.Outcome
	}
	wantOutcome := map[string]RestoreOutcome{
		"healthy":   RestoreRunning,
		"unhealthy": RestoreFailed,
		"hang":      RestoreFailed,
		"stale":     RestoreRunning,
		"stopped":   RestoreSkipped,
		"orphaned":  RestoreOrphaned,
		"multi":     RestoreRunning,
	}
	for name, want := range wantOutcome {
		if got := gotOutcome[name]; got != want {
			t.Fatalf("%s outcome = %q, want %q", name, got, want)
		}
	}

	assertStatus(t, svc, "healthy", registry.StatusRunning)
	assertStatus(t, svc, "unhealthy", registry.StatusFailed)
	assertStatus(t, svc, "hang", registry.StatusFailed)
	assertStatus(t, svc, "stale", registry.StatusRunning)
	assertStatus(t, svc, "stopped", registry.StatusStopped)
	assertStatus(t, svc, "orphaned", registry.StatusOrphaned)
	assertStatus(t, svc, "multi", registry.StatusRunning)

	staleStatus, err := svc.Status("stale")
	if err != nil {
		t.Fatal(err)
	}
	if staleStatus.PID == 0 || staleStatus.PID == 43210 {
		t.Fatalf("stale PID = %d, want non-zero replacement PID", staleStatus.PID)
	}
	if staleStatus.InternalPorts["default"] == 43211 {
		t.Fatalf("stale internal port = %d, want replacement port", staleStatus.InternalPorts["default"])
	}

	multiStatus, err := svc.Status("multi")
	if err != nil {
		t.Fatal(err)
	}
	if len(multiStatus.InternalPorts) != 2 {
		t.Fatalf("multi internal ports = %v, want two restored ports", multiStatus.InternalPorts)
	}

	assertProxyBodyContains(t, proxyURLFor("healthy", svc, "default"), http.StatusOK, "healthy:default")
	assertProxyBodyContains(t, proxyURLFor("stale", svc, "default"), http.StatusOK, "stale:default")
	assertProxyStatus(t, proxyURLFor("unhealthy", svc, "default"), http.StatusServiceUnavailable)
	assertProxyStatus(t, proxyURLFor("hang", svc, "default"), http.StatusServiceUnavailable)
	assertProxyStatus(t, proxyURLFor("stopped", svc, "default"), http.StatusServiceUnavailable)
	assertProxyStatus(t, proxyURLFor("orphaned", svc, "default"), http.StatusServiceUnavailable)
	assertProxyBodyContains(t, proxyURLFor("multi", svc, "api"), http.StatusOK, "multi:api")
	assertProxyBodyContains(t, proxyURLFor("multi", svc, "ws"), http.StatusOK, "multi:ws")
}

func TestRestoreAllSlowFailureDoesNotBlockHealthyPeer(t *testing.T) {
	resetRestoreHooks(t)
	svc := newTestService(t)

	healthyEnv := writeRestoreHelperEnv(t, "healthy", "healthy")
	hangEnv := writeRestoreHelperEnv(t, "hang", "hang")

	registerRestoreService(t, svc, newRestoreBinaryRecord("healthy-peer", healthyEnv, 800*time.Millisecond))
	registerRestoreService(t, svc, newRestoreBinaryRecord("slow-fail", hangEnv, 3*time.Second))
	t.Cleanup(func() { _ = svc.Stop("healthy-peer") })

	done := make(chan struct{})
	var result *RestoreAllResult
	var restoreErr error
	go func() {
		result, restoreErr = svc.RestoreAll(context.Background(), RestoreAllOptions{MaxParallel: 2})
		close(done)
	}()

	waitForProxyStatus(t, proxyURLFor("healthy-peer", svc, "default"), http.StatusOK, 5*time.Second)

	select {
	case <-done:
		t.Fatal("RestoreAll finished before the slow failure timed out")
	default:
	}

	if state := svc.StartupState(); state.Phase != StartPhaseReconciling || !state.MutationsBlocked {
		t.Fatalf("startup state during slow failure = %+v, want reconciling with mutations blocked", state)
	}
	assertProxyStatus(t, proxyURLFor("slow-fail", svc, "default"), http.StatusServiceUnavailable)

	<-done
	if restoreErr != nil {
		t.Fatalf("RestoreAll: %v", restoreErr)
	}

	gotOutcome := map[string]RestoreOutcome{}
	for _, serviceResult := range result.Services {
		gotOutcome[serviceResult.Name] = serviceResult.Outcome
	}
	if gotOutcome["healthy-peer"] != RestoreRunning {
		t.Fatalf("healthy-peer outcome = %q, want %q", gotOutcome["healthy-peer"], RestoreRunning)
	}
	if gotOutcome["slow-fail"] != RestoreFailed {
		t.Fatalf("slow-fail outcome = %q, want %q", gotOutcome["slow-fail"], RestoreFailed)
	}
	assertProxyBodyContains(t, proxyURLFor("healthy-peer", svc, "default"), http.StatusOK, "healthy:default")
}

func newRestoreBinaryRecord(name, envFile string, timeout time.Duration) *registry.Service {
	return &registry.Service{
		Name:               name,
		Type:               registry.TypeBinary,
		BinaryPath:         os.Args[0],
		Args:               helperLifecycleArgs(),
		EnvFile:            envFile,
		HealthCheck:        "/health",
		HealthCheckTimeout: timeout,
		Status:             registry.StatusRunning,
	}
}

func helperLifecycleArgs() []string {
	return []string{"-test.run=TestHelperFakeServiceLifecycle", "-test.v"}
}

func writeRestoreHelperEnv(t *testing.T, mode, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), fmt.Sprintf("%s.env", mode))
	content := "TEST_HELPER=fake_service_lifecycle\n" +
		"TEST_SERVICE_MODE=" + mode + "\n" +
		"TEST_RESPONSE_BODY=" + body + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func proxyURLFor(name string, svc *Service, portName string) string {
	entry, ok := svc.reg.Get(name)
	if !ok {
		return ""
	}
	entry.NormalizePorts()
	return registry.AddressFor(entry.ProxyBindAddress, entry.StablePorts[portName])
}

func waitForProxyStatus(t *testing.T, url string, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, _, err := fetchHTTP(url)
		if err == nil && status == want {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("%s did not return %d before deadline", url, want)
}

func assertProxyStatus(t *testing.T, url string, want int) {
	t.Helper()
	status, _, err := fetchHTTP(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	if status != want {
		t.Fatalf("GET %s status = %d, want %d", url, status, want)
	}
}

func assertProxyBodyContains(t *testing.T, url string, wantStatus int, wantSubstring string) {
	t.Helper()
	status, body, err := fetchHTTP(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	if status != wantStatus {
		t.Fatalf("GET %s status = %d, want %d", url, status, wantStatus)
	}
	if !strings.Contains(body, wantSubstring) {
		t.Fatalf("GET %s body = %q, want substring %q", url, body, wantSubstring)
	}
}

func fetchHTTP(url string) (int, string, error) {
	client := &http.Client{Timeout: 300 * time.Millisecond}
	resp, err := client.Get(url) //nolint:noctx
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, "", err
	}
	return resp.StatusCode, string(body), nil
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
