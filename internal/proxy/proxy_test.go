package proxy

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// freePort asks the OS for an available TCP port, releases it, and returns it.
// There is an inherent TOCTOU race, but it is acceptable for test use.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

// get makes a plain HTTP GET to addr and returns the response body string and
// status code. It does not fail the test on a connection error — callers check
// the return values.
func get(addr string) (body string, status int, err error) {
	resp, err := http.Get(addr) //nolint:noctx
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return strings.TrimSpace(string(b)), resp.StatusCode, nil
}

// TestBeforeSwapReturns503 verifies that a registered proxy returns 503 before
// any upstream has been swapped in.
func TestBeforeSwapReturns503(t *testing.T) {
	m := NewManager()
	port := freePort(t)

	if err := m.Register("svc", port); err != nil {
		t.Fatalf("Register: %v", err)
	}
	t.Cleanup(func() { m.Remove("svc") })

	addr := fmt.Sprintf("http://localhost:%d/", port)
	// Brief pause to let the server goroutine start accepting.
	time.Sleep(10 * time.Millisecond)

	_, status, err := get(addr)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	if status != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d (ServiceUnavailable)", status, http.StatusServiceUnavailable)
	}
}

// TestSwapChangesUpstream verifies that Swap atomically points the proxy at the
// new upstream, and that a second Swap redirects to yet another upstream.
func TestSwapChangesUpstream(t *testing.T) {
	m := NewManager()
	stablePort := freePort(t)

	if err := m.Register("svc", stablePort); err != nil {
		t.Fatalf("Register: %v", err)
	}
	t.Cleanup(func() { m.Remove("svc") })

	// Upstream A
	upstreamA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "upstream-A")
	}))
	defer upstreamA.Close()
	portA := upstreamA.Listener.Addr().(*net.TCPAddr).Port

	// Upstream B
	upstreamB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "upstream-B")
	}))
	defer upstreamB.Close()
	portB := upstreamB.Listener.Addr().(*net.TCPAddr).Port

	addr := fmt.Sprintf("http://localhost:%d/", stablePort)
	time.Sleep(10 * time.Millisecond)

	if err := m.Swap("svc", portA); err != nil {
		t.Fatalf("Swap to A: %v", err)
	}
	body, status, err := get(addr)
	if err != nil {
		t.Fatalf("GET after swap A: %v", err)
	}
	if status != http.StatusOK || body != "upstream-A" {
		t.Errorf("after Swap(A): status=%d body=%q, want 200 upstream-A", status, body)
	}

	if err := m.Swap("svc", portB); err != nil {
		t.Fatalf("Swap to B: %v", err)
	}
	body, status, err = get(addr)
	if err != nil {
		t.Fatalf("GET after swap B: %v", err)
	}
	if status != http.StatusOK || body != "upstream-B" {
		t.Errorf("after Swap(B): status=%d body=%q, want 200 upstream-B", status, body)
	}
}

// TestStablePortNeverDropsDuringSwap sends concurrent requests while a swap is
// in progress and verifies that none receive "connection refused". Responses
// may be from either upstream or a 502 (if upstream is gone mid-request), but
// the proxy itself must remain reachable.
func TestStablePortNeverDropsDuringSwap(t *testing.T) {
	m := NewManager()
	stablePort := freePort(t)

	if err := m.Register("svc", stablePort); err != nil {
		t.Fatalf("Register: %v", err)
	}
	t.Cleanup(func() { m.Remove("svc") })

	upstreamA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "A")
	}))
	defer upstreamA.Close()

	upstreamB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "B")
	}))
	defer upstreamB.Close()

	portA := upstreamA.Listener.Addr().(*net.TCPAddr).Port
	portB := upstreamB.Listener.Addr().(*net.TCPAddr).Port

	if err := m.Swap("svc", portA); err != nil {
		t.Fatalf("initial Swap: %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	addr := fmt.Sprintf("http://localhost:%d/", stablePort)

	const numRequests = 30
	var wg sync.WaitGroup
	errors := make([]error, numRequests)

	// Start the swap concurrently with the requests.
	go func() {
		time.Sleep(5 * time.Millisecond)
		_ = m.Swap("svc", portB)
	}()

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, _, err := get(addr)
			errors[idx] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errors {
		if err != nil {
			// connection refused means the proxy dropped — that is the failure.
			if strings.Contains(err.Error(), "connection refused") {
				t.Errorf("request %d: connection refused (stable port dropped): %v", i, err)
			}
		}
	}
}

// TestRegisterIdempotent verifies that calling Register twice for the same name
// is a no-op and does not return an error.
func TestRegisterIdempotent(t *testing.T) {
	m := NewManager()
	port := freePort(t)

	if err := m.Register("svc", port); err != nil {
		t.Fatalf("Register (first): %v", err)
	}
	t.Cleanup(func() { m.Remove("svc") })

	// Second call with the same name — must not error.
	if err := m.Register("svc", port); err != nil {
		t.Errorf("Register (second, same name): unexpected error: %v", err)
	}

	// Only one entry should exist.
	if m.StablePort("svc") != port {
		t.Errorf("StablePort = %d, want %d", m.StablePort("svc"), port)
	}
}

// TestRemoveReleasesPort verifies that after Remove(), the port is no longer
// held by the proxy and can be rebound by a new listener.
func TestRemoveReleasesPort(t *testing.T) {
	m := NewManager()
	port := freePort(t)

	if err := m.Register("svc", port); err != nil {
		t.Fatalf("Register: %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	m.Remove("svc")

	// Give the server a moment to fully close.
	time.Sleep(20 * time.Millisecond)

	// The port should now be rebindable.
	l, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		t.Fatalf("port %d not released after Remove: %v", port, err)
	}
	l.Close()
}
