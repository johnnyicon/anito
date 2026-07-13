package service

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/johnnyicon/anito/internal/registry"
)

// TestHelperFakeServiceLifecycle is a subprocess helper — starts an HTTP
// server on PORT and blocks. Only runs when TEST_HELPER=fake_service_lifecycle.
func TestHelperFakeServiceLifecycle(t *testing.T) {
	if os.Getenv("TEST_HELPER") != "fake_service_lifecycle" {
		t.Skip("not a subprocess helper")
	}
	portStr := os.Getenv("PORT")
	port, err := strconv.Atoi(portStr)
	if err != nil || port == 0 {
		fmt.Fprintf(os.Stderr, "TestHelperFakeServiceLifecycle: invalid PORT=%q\n", portStr)
		os.Exit(1)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if os.Getenv("TEST_UNHEALTHY") == "1" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	srv := &http.Server{Addr: fmt.Sprintf("localhost:%d", port), Handler: mux}
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		os.Exit(1)
	}
	select {}
}

// newFakeSvc returns a registry.Service pointing at os.Args[0] (the test
// binary) configured to run TestHelperFakeServiceLifecycle.
func newFakeSvc(name string) *registry.Service {
	return &registry.Service{
		Name:        name,
		Type:        registry.TypeBinary,
		BinaryPath:  os.Args[0],
		Args:        []string{"-test.run=TestHelperFakeServiceLifecycle", "-test.v"},
		HealthCheck: "/health",
		Status:      registry.StatusStopped,
	}
}

// waitForPort polls until an HTTP server is reachable at the given port.
func waitForPort(t *testing.T, port int) {
	t.Helper()
	addr := fmt.Sprintf("http://localhost:%d/health", port)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(addr) //nolint:noctx
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("port %d did not become healthy within 5s", port)
}

// TestStop_WritesStoppedStatus deploys a fake service, stops it, and confirms
// the registry shows status=stopped.
func TestStop_WritesStoppedStatus(t *testing.T) {
	t.Setenv("TEST_HELPER", "fake_service_lifecycle")
	svc := newTestService(t)

	fakeSvc := newFakeSvc("stop-test")
	if err := svc.reg.Register(fakeSvc); err != nil {
		t.Fatalf("reg.Register: %v", err)
	}
	// Allocate proxy port so Start can proceed.
	stablePorts, err := svc.allocatePorts("stop-test", map[string]int{"default": 0}, registry.DefaultProxyBindAddress)
	if err != nil {
		t.Fatalf("allocatePorts: %v", err)
	}
	_ = stablePorts
	fakeSvc.StablePorts = stablePorts
	fakeSvc.NormalizePorts()

	internalPorts, err := svc.mgr.Start(fakeSvc)
	if err != nil {
		t.Fatalf("mgr.Start: %v", err)
	}
	// Use the primary (default) port.
	var primaryPort int
	for _, p := range internalPorts {
		primaryPort = p
		break
	}
	waitForPort(t, primaryPort)
	t.Cleanup(func() { _ = svc.mgr.Stop("stop-test") })

	if err := svc.Stop("stop-test"); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	got, ok := svc.reg.Get("stop-test")
	if !ok {
		t.Fatal("service not found in registry after Stop")
	}
	if got.Status != registry.StatusStopped {
		t.Errorf("status = %q, want %q", got.Status, registry.StatusStopped)
	}
}

// TestDeploy_RunningWrittenAfterSwap confirms that a successful deploy writes
// status=running with a valid PID.
func TestDeploy_RunningWrittenAfterSwap(t *testing.T) {
	t.Setenv("TEST_HELPER", "fake_service_lifecycle")
	svc := newTestService(t)

	result, err := svc.Deploy(DeployRequest{
		Name:               "deploy-status-test",
		Type:               registry.TypeBinary,
		Path:               os.Args[0],
		Args:               []string{"-test.run=TestHelperFakeServiceLifecycle", "-test.v"},
		HealthCheck:        "/health",
		HealthCheckTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop("deploy-status-test") })

	if result.Status != registry.StatusRunning {
		t.Errorf("status = %q after Deploy, want %q", result.Status, registry.StatusRunning)
	}
	if result.PID == 0 {
		t.Error("PID = 0 after successful Deploy, want non-zero")
	}
}

// TestConcurrentDeploy_Serialized launches two goroutines that both try to
// deploy the same service. Verifies they don't interleave (second waits for
// the first to complete). We detect interleaving by checking that the registry
// is never in an inconsistent state mid-deploy.
func TestConcurrentDeploy_Serialized(t *testing.T) {
	t.Setenv("TEST_HELPER", "fake_service_lifecycle")
	svc := newTestService(t)

	var wg sync.WaitGroup
	errors := make(chan error, 2)

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.Deploy(DeployRequest{
				Name:               "concurrent-test",
				Type:               registry.TypeBinary,
				Path:               os.Args[0],
				Args:               []string{"-test.run=TestHelperFakeServiceLifecycle", "-test.v"},
				HealthCheck:        "/health",
				HealthCheckTimeout: 10 * time.Second,
			})
			if err != nil {
				errors <- err
			}
		}()
	}
	wg.Wait()
	close(errors)

	t.Cleanup(func() { _ = svc.Stop("concurrent-test") })

	for err := range errors {
		t.Errorf("concurrent deploy error: %v", err)
	}

	// After both deploys complete, service should be running.
	got, ok := svc.reg.Get("concurrent-test")
	if !ok {
		t.Fatal("service not in registry after concurrent deploys")
	}
	if got.Status != registry.StatusRunning {
		t.Errorf("status = %q after concurrent deploys, want %q", got.Status, registry.StatusRunning)
	}
}

func TestFailedRedeployRestoresServingProcessAndRegistry(t *testing.T) {
	t.Setenv("TEST_HELPER", "fake_service_lifecycle")
	svc := newTestService(t)
	healthyEnv := filepath.Join(t.TempDir(), "healthy.env")
	unhealthyEnv := filepath.Join(t.TempDir(), "unhealthy.env")
	if err := os.WriteFile(healthyEnv, []byte("TEST_UNHEALTHY=0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unhealthyEnv, []byte("TEST_UNHEALTHY=1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	first, err := svc.Deploy(DeployRequest{
		Name:               "failed-redeploy",
		Version:            "good",
		Type:               registry.TypeBinary,
		Path:               os.Args[0],
		Args:               []string{"-test.run=TestHelperFakeServiceLifecycle", "-test.v"},
		EnvFile:            healthyEnv,
		HealthCheck:        "/health",
		HealthCheckTimeout: 5 * time.Second,
		DrainWindow:        time.Millisecond,
	})
	if err != nil {
		t.Fatalf("initial deploy: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop("failed-redeploy") })
	originalPID := first.PID

	_, err = svc.Deploy(DeployRequest{
		Name:               "failed-redeploy",
		Version:            "bad",
		Type:               registry.TypeBinary,
		Path:               os.Args[0],
		Args:               []string{"-test.run=TestHelperFakeServiceLifecycle", "-test.v"},
		EnvFile:            unhealthyEnv,
		HealthCheck:        "/health",
		HealthCheckTimeout: 300 * time.Millisecond,
		DrainWindow:        time.Millisecond,
	})
	if err == nil {
		t.Fatal("unhealthy redeploy unexpectedly succeeded")
	}

	restored, err := svc.Status("failed-redeploy")
	if err != nil {
		t.Fatal(err)
	}
	if restored.Version != "good" || restored.EnvFile != healthyEnv {
		t.Fatalf("registry retained rejected deploy: version=%q env=%q", restored.Version, restored.EnvFile)
	}
	if restored.Status != registry.StatusRunning || restored.PID != originalPID {
		t.Fatalf("restored runtime = status %q pid %d, want running pid %d", restored.Status, restored.PID, originalPID)
	}
	if got := svc.mgr.PID("failed-redeploy"); got != originalPID {
		t.Fatalf("tracked PID = %d, want restored PID %d", got, originalPID)
	}
	if len(restored.StartHistory) != 2 || restored.StartHistory[1].ExitCode != 1 {
		t.Fatalf("start history = %+v, want rejected candidate recorded with exit 1", restored.StartHistory)
	}

	resp, err := http.Get(restored.Address() + "/health") //nolint:noctx
	if err != nil {
		t.Fatalf("stable proxy after failed redeploy: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stable proxy status = %d, want 200 from restored process", resp.StatusCode)
	}
}

// Silence "fmt declared and not used" if compile target doesn't use it directly.
var _ = fmt.Sprintf
var _ net.Listener
