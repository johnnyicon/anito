package registry

import (
	"testing"
	"time"
)

// TestStablePortPreservedOnRedeploy verifies the core invariant: re-registering
// a service with a different StablePort value must not change the port that was
// stored on the first registration.
func TestStablePortPreservedOnRedeploy(t *testing.T) {
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	original := &Service{
		Name:       "svc",
		Type:       TypeBinary,
		BinaryPath: "/bin/svc",
		StablePort: 3000,
		Status:     StatusRunning,
	}
	if err := r.Register(original); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Redeploy with a different StablePort value — must be ignored.
	redeploy := &Service{
		Name:       "svc",
		Type:       TypeBinary,
		BinaryPath: "/bin/svc-v2",
		StablePort: 9999, // should be overwritten with 3000
		Status:     StatusRunning,
	}
	if err := r.Register(redeploy); err != nil {
		t.Fatalf("Register (redeploy): %v", err)
	}

	got, ok := r.Get("svc")
	if !ok {
		t.Fatal("service not found after redeploy")
	}
	if got.StablePort != 3000 {
		t.Errorf("StablePort = %d, want 3000", got.StablePort)
	}
}

// TestDeployedAtNotOverwrittenOnRedeploy verifies that DeployedAt is set on
// first registration and not changed on subsequent registrations, while
// UpdatedAt is refreshed.
func TestDeployedAtNotOverwrittenOnRedeploy(t *testing.T) {
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := r.Register(&Service{Name: "svc", StablePort: 4000, Status: StatusRunning}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	first, _ := r.Get("svc")
	deployedAt := first.DeployedAt
	updatedAt := first.UpdatedAt

	// Brief pause to ensure time advances.
	time.Sleep(2 * time.Millisecond)

	if err := r.Register(&Service{Name: "svc", StablePort: 4000, Status: StatusRunning}); err != nil {
		t.Fatalf("Register (redeploy): %v", err)
	}

	second, _ := r.Get("svc")
	if !second.DeployedAt.Equal(deployedAt) {
		t.Errorf("DeployedAt changed: was %v, now %v", deployedAt, second.DeployedAt)
	}
	if !second.UpdatedAt.After(updatedAt) {
		t.Errorf("UpdatedAt not bumped: was %v, still %v", updatedAt, second.UpdatedAt)
	}
}

// TestPersistenceRoundTrip verifies that services survive a save/reload cycle
// with all fields intact.
func TestPersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()

	r1, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := r1.Register(&Service{
		Name:       "alpha",
		Type:       TypeBinary,
		BinaryPath: "/bin/alpha",
		StablePort: 5000,
		Status:     StatusRunning,
	}); err != nil {
		t.Fatalf("Register alpha: %v", err)
	}
	if err := r1.Register(&Service{
		Name:       "beta",
		Type:       TypeStatic,
		BinaryPath: "/var/www/beta",
		StablePort: 5001,
		Status:     StatusStopped,
	}); err != nil {
		t.Fatalf("Register beta: %v", err)
	}

	// Open a new Registry from the same directory — simulates a daemon restart.
	r2, err := New(dir)
	if err != nil {
		t.Fatalf("New (reload): %v", err)
	}

	alpha, ok := r2.Get("alpha")
	if !ok {
		t.Fatal("alpha not found after reload")
	}
	if alpha.StablePort != 5000 {
		t.Errorf("alpha.StablePort = %d, want 5000", alpha.StablePort)
	}
	if alpha.BinaryPath != "/bin/alpha" {
		t.Errorf("alpha.BinaryPath = %q, want /bin/alpha", alpha.BinaryPath)
	}

	beta, ok := r2.Get("beta")
	if !ok {
		t.Fatal("beta not found after reload")
	}
	if beta.StablePort != 5001 {
		t.Errorf("beta.StablePort = %d, want 5001", beta.StablePort)
	}
	if beta.Type != TypeStatic {
		t.Errorf("beta.Type = %q, want %q", beta.Type, TypeStatic)
	}

	all := r2.All()
	if len(all) != 2 {
		t.Errorf("All() returned %d services, want 2", len(all))
	}
}

// TestUsedPortsTracking verifies UsedPorts returns the set of stable ports for
// all registered services.
func TestUsedPortsTracking(t *testing.T) {
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := r.Register(&Service{Name: "a", StablePort: 6000, Status: StatusRunning}); err != nil {
		t.Fatalf("Register a: %v", err)
	}
	if err := r.Register(&Service{Name: "b", StablePort: 6001, Status: StatusRunning}); err != nil {
		t.Fatalf("Register b: %v", err)
	}

	used := r.UsedPorts()
	if !used[6000] {
		t.Error("UsedPorts missing 6000")
	}
	if !used[6001] {
		t.Error("UsedPorts missing 6001")
	}
	if len(used) != 2 {
		t.Errorf("UsedPorts len = %d, want 2", len(used))
	}
}

// TestRemoveClearsFromRegistryAndPorts verifies that Remove deletes the service
// from Get and from UsedPorts.
func TestRemoveClearsFromRegistryAndPorts(t *testing.T) {
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := r.Register(&Service{Name: "svc", StablePort: 7000, Status: StatusRunning}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := r.Remove("svc"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if _, ok := r.Get("svc"); ok {
		t.Error("Get returned a service after Remove")
	}

	used := r.UsedPorts()
	if used[7000] {
		t.Error("UsedPorts still contains 7000 after Remove")
	}
}
