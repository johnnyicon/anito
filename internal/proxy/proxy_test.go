package proxy

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func assertBody(t *testing.T, addr, want string) {
	t.Helper()
	body, status, err := get(addr)
	if err != nil {
		t.Fatalf("GET %s: %v", addr, err)
	}
	if status != http.StatusOK || body != want {
		t.Fatalf("GET %s: status=%d body=%q, want 200 %q", addr, status, body, want)
	}
}

func generationUpstreams(t *testing.T, generation string) (map[string]int, func()) {
	t.Helper()
	ws := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s-ws", generation)
	}))
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s-http", generation)
	}))
	return map[string]int{
			"ws":   ws.Listener.Addr().(*net.TCPAddr).Port,
			"http": httpServer.Listener.Addr().(*net.TCPAddr).Port,
		}, func() {
			ws.Close()
			httpServer.Close()
		}
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

func TestRegisterRebindsExistingServiceToNewPort(t *testing.T) {
	m := NewManager()
	oldPort := freePort(t)
	newPort := freePort(t)
	if err := m.Register("rebind", oldPort); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Remove("rebind") })
	if err := m.Register("rebind", newPort); err != nil {
		t.Fatal(err)
	}
	if got := m.StablePort("rebind"); got != newPort {
		t.Fatalf("StablePort = %d, want rebound port %d", got, newPort)
	}
	time.Sleep(10 * time.Millisecond)
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", newPort)) //nolint:noctx
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("rebound listener status = %d, want 503", resp.StatusCode)
	}
}

func TestRegisterFailedRebindKeepsExistingListener(t *testing.T) {
	m := NewManager()
	oldPort := freePort(t)
	if err := m.Register("failed-rebind", oldPort); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Remove("failed-rebind") })
	occupied, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	badPort := occupied.Addr().(*net.TCPAddr).Port
	if err := m.Register("failed-rebind", badPort); err == nil {
		t.Fatal("expected rebind to occupied port to fail")
	}
	if got := m.StablePort("failed-rebind"); got != oldPort {
		t.Fatalf("StablePort = %d after failed rebind, want %d", got, oldPort)
	}
}

func TestRegisterWithBindUsesExplicitAddress(t *testing.T) {
	m := NewManager()
	port := freePort(t)

	if err := m.RegisterWithBind("svc", port, "127.0.0.1"); err != nil {
		t.Fatalf("RegisterWithBind: %v", err)
	}
	t.Cleanup(func() { m.Remove("svc") })

	time.Sleep(10 * time.Millisecond)

	_, status, err := get(fmt.Sprintf("http://127.0.0.1:%d/", port))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	if status != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", status, http.StatusServiceUnavailable)
	}
}

func TestServerForConfiguresHTTPTimeouts(t *testing.T) {
	e := &entry{portName: "default", route: &serviceRoute{}}
	e.route.generation.Store(&routingGeneration{handlers: map[string]http.Handler{"default": serviceUnavailableHandler()}})

	srv := serverFor(e)
	if srv.ReadHeaderTimeout != proxyReadHeaderTimeout {
		t.Fatalf("ReadHeaderTimeout = %s, want %s", srv.ReadHeaderTimeout, proxyReadHeaderTimeout)
	}
	if srv.ReadTimeout != proxyReadTimeout {
		t.Fatalf("ReadTimeout = %s, want %s", srv.ReadTimeout, proxyReadTimeout)
	}
	if srv.IdleTimeout != proxyIdleTimeout {
		t.Fatalf("IdleTimeout = %s, want %s", srv.IdleTimeout, proxyIdleTimeout)
	}
	if srv.WriteTimeout != 0 {
		t.Fatalf("WriteTimeout = %s, want unset for streaming compatibility", srv.WriteTimeout)
	}
}

func TestValidateProxyBindAddressAllowsLoopback(t *testing.T) {
	for _, bindAddress := range []string{"", "localhost", "127.0.0.1", "::1", "[::1]"} {
		t.Run(bindAddress, func(t *testing.T) {
			if err := ValidateProxyBindAddress(bindAddress); err != nil {
				t.Fatalf("ValidateProxyBindAddress(%q): %v", bindAddress, err)
			}
		})
	}
}

func TestValidateProxyBindAddressRejectsWildcardAndLAN(t *testing.T) {
	for _, bindAddress := range []string{"0.0.0.0", "::", "192.168.1.10", "10.0.0.5", "example.test"} {
		t.Run(bindAddress, func(t *testing.T) {
			if err := ValidateProxyBindAddress(bindAddress); err == nil {
				t.Fatalf("ValidateProxyBindAddress(%q) succeeded, want rejection", bindAddress)
			}
		})
	}
}

func TestValidateProxyBindAddressAllowsLocalTailscaleIP(t *testing.T) {
	restoreInterfaceAddrs(t, []net.Addr{mustCIDRAddr(t, "100.94.58.29/32")})
	if err := ValidateProxyBindAddress("100.94.58.29"); err != nil {
		t.Fatalf("ValidateProxyBindAddress(local tailscale): %v", err)
	}
}

func TestValidateProxyBindAddressRejectsUnassignedTailscaleIP(t *testing.T) {
	restoreInterfaceAddrs(t, []net.Addr{mustCIDRAddr(t, "100.94.58.29/32")})
	err := ValidateProxyBindAddress("100.94.58.30")
	if err == nil {
		t.Fatal("expected unassigned Tailscale IP to be rejected")
	}
	if !strings.Contains(err.Error(), "not assigned") {
		t.Fatalf("error = %q, want not assigned", err.Error())
	}
}

func TestRegisterWithBindRejectsUnsafeAddress(t *testing.T) {
	m := NewManager()
	port := freePort(t)

	err := m.RegisterWithBind("svc", port, "0.0.0.0")
	if err == nil {
		t.Fatal("RegisterWithBind with wildcard address succeeded")
	}
	if !strings.Contains(err.Error(), "proxy_bind_address") {
		t.Fatalf("error = %q, want proxy_bind_address", err.Error())
	}
}

func restoreInterfaceAddrs(t *testing.T, addrs []net.Addr) {
	t.Helper()
	orig := interfaceAddrs
	interfaceAddrs = func() ([]net.Addr, error) {
		return addrs, nil
	}
	t.Cleanup(func() {
		interfaceAddrs = orig
	})
}

func mustCIDRAddr(t *testing.T, cidr string) net.Addr {
	t.Helper()
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		t.Fatalf("ParseCIDR(%q): %v", cidr, err)
	}
	return ipNet
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

// ---------------------------------------------------------------------------
// New tests — SwapStatic, Unswap/UnswapPorts, StablePort, RegisterPorts,
// SwapPorts multi-port, rollback, flushProxy, X-Anito-Proxy header
// ---------------------------------------------------------------------------

// TestSwapStaticServesFiles verifies that SwapStatic serves files from a
// directory via http.FileServer.
func TestSwapStaticServesFiles(t *testing.T) {
	m := NewManager()
	port := freePort(t)

	if err := m.Register("static-svc", port); err != nil {
		t.Fatalf("Register: %v", err)
	}
	t.Cleanup(func() { m.Remove("static-svc") })

	// Create a temp directory with a file to serve.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("static-content"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	if err := m.SwapStatic("static-svc", dir); err != nil {
		t.Fatalf("SwapStatic: %v", err)
	}

	addr := fmt.Sprintf("http://localhost:%d/hello.txt", port)
	body, status, err := get(addr)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
	if body != "static-content" {
		t.Errorf("body = %q, want %q", body, "static-content")
	}
}

// TestSwapStaticErrorUnregistered verifies that SwapStatic returns an error
// for a service that has not been registered.
func TestSwapStaticErrorUnregistered(t *testing.T) {
	m := NewManager()
	err := m.SwapStatic("no-such-svc", t.TempDir())
	if err == nil {
		t.Fatal("SwapStatic should return error for unregistered service")
	}
	if !strings.Contains(err.Error(), "not registered") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "not registered")
	}
}

// TestUnswapRestores503 verifies that Unswap restores the 503 placeholder
// handler after a successful Swap.
func TestUnswapRestores503(t *testing.T) {
	m := NewManager()
	port := freePort(t)

	if err := m.Register("svc", port); err != nil {
		t.Fatalf("Register: %v", err)
	}
	t.Cleanup(func() { m.Remove("svc") })

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "upstream")
	}))
	defer upstream.Close()
	upPort := upstream.Listener.Addr().(*net.TCPAddr).Port

	time.Sleep(10 * time.Millisecond)

	// Swap to upstream — should return 200.
	if err := m.Swap("svc", upPort); err != nil {
		t.Fatalf("Swap: %v", err)
	}
	_, status, err := get(fmt.Sprintf("http://localhost:%d/", port))
	if err != nil {
		t.Fatalf("GET after Swap: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("after Swap: status = %d, want 200", status)
	}

	// Unswap — should return 503.
	m.Unswap("svc")
	_, status, err = get(fmt.Sprintf("http://localhost:%d/", port))
	if err != nil {
		t.Fatalf("GET after Unswap: %v", err)
	}
	if status != http.StatusServiceUnavailable {
		t.Errorf("after Unswap: status = %d, want 503", status)
	}
}

// TestUnswapPortsRestores503OnAllPorts verifies that UnswapPorts restores the
// 503 placeholder on every named port of a multi-port service.
func TestUnswapPortsRestores503OnAllPorts(t *testing.T) {
	m := NewManager()
	portWS := freePort(t)
	portHTTP := freePort(t)

	ports := map[string]int{"ws": portWS, "http": portHTTP}
	if err := m.RegisterPorts("multi", ports); err != nil {
		t.Fatalf("RegisterPorts: %v", err)
	}
	t.Cleanup(func() { m.RemovePorts("multi") })

	upWS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ws-up")
	}))
	defer upWS.Close()
	upHTTP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "http-up")
	}))
	defer upHTTP.Close()

	time.Sleep(10 * time.Millisecond)

	internalPorts := map[string]int{
		"ws":   upWS.Listener.Addr().(*net.TCPAddr).Port,
		"http": upHTTP.Listener.Addr().(*net.TCPAddr).Port,
	}
	if err := m.SwapPorts("multi", internalPorts); err != nil {
		t.Fatalf("SwapPorts: %v", err)
	}

	// Verify both ports serve upstream content.
	for portName, stablePort := range ports {
		body, status, err := get(fmt.Sprintf("http://localhost:%d/", stablePort))
		if err != nil {
			t.Fatalf("GET %s: %v", portName, err)
		}
		if status != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", portName, status)
		}
		if body == "" {
			t.Errorf("%s: body is empty", portName)
		}
	}

	// UnswapPorts — both should return 503.
	m.UnswapPorts("multi")
	for portName, stablePort := range ports {
		_, status, err := get(fmt.Sprintf("http://localhost:%d/", stablePort))
		if err != nil {
			t.Fatalf("GET %s after UnswapPorts: %v", portName, err)
		}
		if status != http.StatusServiceUnavailable {
			t.Errorf("%s after UnswapPorts: status = %d, want 503", portName, status)
		}
	}
}

// TestStablePortReturnsPortForRegistered verifies that StablePort returns the
// correct port for a registered service.
func TestStablePortReturnsPortForRegistered(t *testing.T) {
	m := NewManager()
	port := freePort(t)

	if err := m.Register("known", port); err != nil {
		t.Fatalf("Register: %v", err)
	}
	t.Cleanup(func() { m.Remove("known") })

	got := m.StablePort("known")
	if got != port {
		t.Errorf("StablePort = %d, want %d", got, port)
	}
}

// TestStablePortReturnsZeroForUnknown verifies that StablePort returns 0 for a
// service that has never been registered.
func TestStablePortReturnsZeroForUnknown(t *testing.T) {
	m := NewManager()
	got := m.StablePort("nonexistent")
	if got != 0 {
		t.Errorf("StablePort = %d, want 0", got)
	}
}

// TestStablePortFallsBackToNonDefault verifies that StablePort falls back to
// any port for the service when "default" is not registered (multi-port
// services that don't use a "default" port name).
func TestStablePortFallsBackToNonDefault(t *testing.T) {
	m := NewManager()
	portWS := freePort(t)

	// Register only a "ws" port — no "default".
	if err := m.RegisterPorts("daemon", map[string]int{"ws": portWS}); err != nil {
		t.Fatalf("RegisterPorts: %v", err)
	}
	t.Cleanup(func() { m.RemovePorts("daemon") })

	got := m.StablePort("daemon")
	if got != portWS {
		t.Errorf("StablePort = %d, want %d (fallback to ws port)", got, portWS)
	}
}

// TestRegisterPortsMultiPort verifies that RegisterPorts binds two named ports
// and both serve requests after SwapPorts.
func TestRegisterPortsMultiPort(t *testing.T) {
	m := NewManager()
	portWS := freePort(t)
	portHTTP := freePort(t)

	ports := map[string]int{"ws": portWS, "http": portHTTP}
	if err := m.RegisterPorts("multi-svc", ports); err != nil {
		t.Fatalf("RegisterPorts: %v", err)
	}
	t.Cleanup(func() { m.RemovePorts("multi-svc") })

	time.Sleep(10 * time.Millisecond)

	// Before swap — both should return 503.
	for portName, sp := range ports {
		_, status, err := get(fmt.Sprintf("http://localhost:%d/", sp))
		if err != nil {
			t.Fatalf("GET %s before swap: %v", portName, err)
		}
		if status != http.StatusServiceUnavailable {
			t.Errorf("%s before swap: status = %d, want 503", portName, status)
		}
	}

	// Create upstreams.
	upWS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ws-response")
	}))
	defer upWS.Close()
	upHTTP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "http-response")
	}))
	defer upHTTP.Close()

	internalPorts := map[string]int{
		"ws":   upWS.Listener.Addr().(*net.TCPAddr).Port,
		"http": upHTTP.Listener.Addr().(*net.TCPAddr).Port,
	}
	if err := m.SwapPorts("multi-svc", internalPorts); err != nil {
		t.Fatalf("SwapPorts: %v", err)
	}

	// After swap — verify each port routes to its correct upstream.
	body, status, err := get(fmt.Sprintf("http://localhost:%d/", portWS))
	if err != nil {
		t.Fatalf("GET ws: %v", err)
	}
	if status != http.StatusOK || body != "ws-response" {
		t.Errorf("ws: status=%d body=%q, want 200 ws-response", status, body)
	}

	body, status, err = get(fmt.Sprintf("http://localhost:%d/", portHTTP))
	if err != nil {
		t.Fatalf("GET http: %v", err)
	}
	if status != http.StatusOK || body != "http-response" {
		t.Errorf("http: status=%d body=%q, want 200 http-response", status, body)
	}
}

func TestSwapPortsPublishesOneImmutableGenerationForNamedPorts(t *testing.T) {
	m := NewManager()
	stablePorts := map[string]int{"ws": freePort(t), "http": freePort(t)}
	if err := m.RegisterPorts("atomic-svc", stablePorts); err != nil {
		t.Fatalf("RegisterPorts: %v", err)
	}
	t.Cleanup(func() { m.RemovePorts("atomic-svc") })

	oldWS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "old-ws")
	}))
	defer oldWS.Close()
	oldHTTP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "old-http")
	}))
	defer oldHTTP.Close()
	newWS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "new-ws")
	}))
	defer newWS.Close()
	newHTTP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "new-http")
	}))
	defer newHTTP.Close()

	if err := m.SwapPorts("atomic-svc", map[string]int{
		"ws":   oldWS.Listener.Addr().(*net.TCPAddr).Port,
		"http": oldHTTP.Listener.Addr().(*net.TCPAddr).Port,
	}); err != nil {
		t.Fatalf("SwapPorts old: %v", err)
	}

	route := m.routes["atomic-svc"]
	if route == nil {
		t.Fatal("route was not registered")
	}
	if m.entries[entryKey("atomic-svc", "ws")].route != route || m.entries[entryKey("atomic-svc", "http")].route != route {
		t.Fatal("named ports do not share one service route")
	}
	oldGeneration := route.generation.Load().(*routingGeneration)

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	routingGenerationPublishHook.Store(routeGenerationPublishHook{before: func(service string, generation *routingGeneration) {
		if service != "atomic-svc" {
			return
		}
		once.Do(func() {
			close(entered)
			<-release
		})
	}})
	defer routingGenerationPublishHook.Store(routeGenerationPublishHook{})

	done := make(chan error, 1)
	go func() {
		done <- m.SwapPorts("atomic-svc", map[string]int{
			"ws":   newWS.Listener.Addr().(*net.TCPAddr).Port,
			"http": newHTTP.Listener.Addr().(*net.TCPAddr).Port,
		})
	}()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for swap to reach generation publish point")
	}

	if got := route.generation.Load().(*routingGeneration); got != oldGeneration {
		t.Fatal("route generation changed before the synchronized publish point")
	}
	assertBody(t, fmt.Sprintf("http://127.0.0.1:%d/", stablePorts["ws"]), "old-ws")
	assertBody(t, fmt.Sprintf("http://127.0.0.1:%d/", stablePorts["http"]), "old-http")

	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SwapPorts new: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for swap to finish")
	}

	newGeneration := route.generation.Load().(*routingGeneration)
	if newGeneration == oldGeneration {
		t.Fatal("route generation pointer did not change after publish")
	}
	assertBody(t, fmt.Sprintf("http://127.0.0.1:%d/", stablePorts["ws"]), "new-ws")
	assertBody(t, fmt.Sprintf("http://127.0.0.1:%d/", stablePorts["http"]), "new-http")
}

func TestSwapPortsRepeatedStressNoMixedPublishedGeneration(t *testing.T) {
	m := NewManager()
	stablePorts := map[string]int{"ws": freePort(t), "http": freePort(t)}
	if err := m.RegisterPorts("stress-svc", stablePorts); err != nil {
		t.Fatalf("RegisterPorts: %v", err)
	}
	t.Cleanup(func() { m.RemovePorts("stress-svc") })

	portsA, closeA := generationUpstreams(t, "A")
	defer closeA()
	portsB, closeB := generationUpstreams(t, "B")
	defer closeB()

	for i := 0; i < 80; i++ {
		want := "A"
		ports := portsA
		if i%2 == 1 {
			want = "B"
			ports = portsB
		}
		if err := m.SwapPorts("stress-svc", ports); err != nil {
			t.Fatalf("SwapPorts iteration %d: %v", i, err)
		}

		var wg sync.WaitGroup
		errs := make(chan error, 12)
		for j := 0; j < 6; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				wsBody, wsStatus, err := get(fmt.Sprintf("http://127.0.0.1:%d/", stablePorts["ws"]))
				if err != nil {
					errs <- fmt.Errorf("ws GET: %w", err)
					return
				}
				httpBody, httpStatus, err := get(fmt.Sprintf("http://127.0.0.1:%d/", stablePorts["http"]))
				if err != nil {
					errs <- fmt.Errorf("http GET: %w", err)
					return
				}
				if wsStatus != http.StatusOK || httpStatus != http.StatusOK {
					errs <- fmt.Errorf("statuses ws=%d http=%d, want 200/200", wsStatus, httpStatus)
					return
				}
				if wsBody != want+"-ws" || httpBody != want+"-http" {
					errs <- fmt.Errorf("mixed generation: ws=%q http=%q want %q/%q", wsBody, httpBody, want+"-ws", want+"-http")
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestSwapPortsValidationFailureDoesNotPublishGeneration(t *testing.T) {
	m := NewManager()
	stablePorts := map[string]int{"ws": freePort(t), "http": freePort(t)}
	if err := m.RegisterPorts("validate-svc", stablePorts); err != nil {
		t.Fatalf("RegisterPorts: %v", err)
	}
	t.Cleanup(func() { m.RemovePorts("validate-svc") })

	oldPorts, closeOld := generationUpstreams(t, "old")
	defer closeOld()
	newWS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "new-ws")
	}))
	defer newWS.Close()

	if err := m.SwapPorts("validate-svc", oldPorts); err != nil {
		t.Fatalf("SwapPorts old: %v", err)
	}
	route := m.routes["validate-svc"]
	before := route.generation.Load().(*routingGeneration)

	err := m.SwapPorts("validate-svc", map[string]int{
		"ws":   newWS.Listener.Addr().(*net.TCPAddr).Port,
		"grpc": 65535,
	})
	if err == nil {
		t.Fatal("SwapPorts should fail for an unregistered port name")
	}
	if after := route.generation.Load().(*routingGeneration); after != before {
		t.Fatal("route generation changed after validation failure")
	}
	assertBody(t, fmt.Sprintf("http://127.0.0.1:%d/", stablePorts["ws"]), "old-ws")
	assertBody(t, fmt.Sprintf("http://127.0.0.1:%d/", stablePorts["http"]), "old-http")
}

// TestSwapPortsErrorUnregisteredPortName verifies that SwapPorts returns an
// error when given a port name that was not registered.
func TestSwapPortsErrorUnregisteredPortName(t *testing.T) {
	m := NewManager()
	port := freePort(t)

	if err := m.Register("svc", port); err != nil {
		t.Fatalf("Register: %v", err)
	}
	t.Cleanup(func() { m.Remove("svc") })

	// "grpc" was never registered — only "default" was.
	err := m.SwapPorts("svc", map[string]int{"grpc": 9999})
	if err == nil {
		t.Fatal("SwapPorts should return error for unregistered port name")
	}
	if !strings.Contains(err.Error(), "not registered") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "not registered")
	}
}

// TestRegisterPortsRollbackOnPartialFailure verifies that when RegisterPorts
// fails to bind one port, all previously bound ports in that call are released.
func TestRegisterPortsRollbackOnPartialFailure(t *testing.T) {
	m := NewManager()
	portGood := freePort(t)

	// Occupy a port so that RegisterPorts will fail on it.
	occupied, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer occupied.Close()
	portBad := occupied.Addr().(*net.TCPAddr).Port

	// RegisterPorts with two ports — one will fail because portBad is occupied.
	err = m.RegisterPorts("rollback-svc", map[string]int{"good": portGood, "bad": portBad})
	if err == nil {
		// Cleanup just in case.
		m.RemovePorts("rollback-svc")
		t.Fatal("RegisterPorts should fail when a port is occupied")
	}

	// The "good" port should have been rolled back and be rebindable.
	time.Sleep(20 * time.Millisecond)
	l, lerr := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", portGood))
	if lerr != nil {
		t.Fatalf("port %d not released after rollback: %v", portGood, lerr)
	}
	l.Close()

	// StablePort should return 0 — nothing should be registered.
	if got := m.StablePort("rollback-svc"); got != 0 {
		t.Errorf("StablePort after rollback = %d, want 0", got)
	}
}

// TestXAnitoProxyHeaderBeforeSwap verifies that the X-Anito-Proxy header is
// present on responses even before an upstream is swapped in (503 placeholder).
func TestXAnitoProxyHeaderBeforeSwap(t *testing.T) {
	m := NewManager()
	port := freePort(t)

	if err := m.Register("hdr-svc", port); err != nil {
		t.Fatalf("Register: %v", err)
	}
	t.Cleanup(func() { m.Remove("hdr-svc") })

	time.Sleep(10 * time.Millisecond)

	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/", port)) //nolint:noctx
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	val := resp.Header.Get("X-Anito-Proxy")
	if val != "1" {
		t.Errorf("X-Anito-Proxy = %q, want %q (before swap)", val, "1")
	}
}

// TestXAnitoProxyHeaderAfterSwap verifies that the X-Anito-Proxy header is
// present on responses after an upstream has been swapped in.
func TestXAnitoProxyHeaderAfterSwap(t *testing.T) {
	m := NewManager()
	port := freePort(t)

	if err := m.Register("hdr-svc", port); err != nil {
		t.Fatalf("Register: %v", err)
	}
	t.Cleanup(func() { m.Remove("hdr-svc") })

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer upstream.Close()

	time.Sleep(10 * time.Millisecond)

	if err := m.Swap("hdr-svc", upstream.Listener.Addr().(*net.TCPAddr).Port); err != nil {
		t.Fatalf("Swap: %v", err)
	}

	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/", port)) //nolint:noctx
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	val := resp.Header.Get("X-Anito-Proxy")
	if val != "1" {
		t.Errorf("X-Anito-Proxy = %q, want %q (after swap)", val, "1")
	}
}

func TestXAnitoClientIPHeaderAfterSwap(t *testing.T) {
	m := NewManager()
	port := freePort(t)

	if err := m.Register("client-ip-svc", port); err != nil {
		t.Fatalf("Register: %v", err)
	}
	t.Cleanup(func() { m.Remove("client-ip-svc") })

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, r.Header.Get("X-Anito-Client-IP"))
	}))
	defer upstream.Close()

	if err := m.Swap("client-ip-svc", upstream.Listener.Addr().(*net.TCPAddr).Port); err != nil {
		t.Fatalf("Swap: %v", err)
	}

	req, err := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/", port), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("X-Anito-Client-IP", "spoofed")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	body := strings.TrimSpace(string(b))
	status := resp.StatusCode
	if status != http.StatusOK || body != "127.0.0.1" {
		t.Fatalf("status=%d body=%q, want 200 127.0.0.1", status, body)
	}
}

// TestSSERequestIsFlushed verifies that SSE requests (Accept: text/event-stream)
// are auto-flushed so that events arrive without waiting for the buffer to fill.
func TestSSERequestIsFlushed(t *testing.T) {
	m := NewManager()
	port := freePort(t)

	if err := m.Register("sse-svc", port); err != nil {
		t.Fatalf("Register: %v", err)
	}
	t.Cleanup(func() { m.Remove("sse-svc") })

	// SSE upstream that sends two events then closes.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		for _, msg := range []string{"data: event-1\n\n", "data: event-2\n\n"} {
			fmt.Fprint(w, msg)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	defer upstream.Close()

	time.Sleep(10 * time.Millisecond)

	if err := m.Swap("sse-svc", upstream.Listener.Addr().(*net.TCPAddr).Port); err != nil {
		t.Fatalf("Swap: %v", err)
	}

	// Make an SSE request through the proxy.
	req, err := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d/events", port), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// Read SSE events from the proxied response. The flushProxy should ensure
	// each event arrives promptly. We collect "data:" lines.
	scanner := bufio.NewScanner(resp.Body)
	var events []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			events = append(events, line)
		}
	}

	if len(events) != 2 {
		t.Fatalf("got %d events, want 2: %v", len(events), events)
	}
	if events[0] != "data: event-1" {
		t.Errorf("event[0] = %q, want %q", events[0], "data: event-1")
	}
	if events[1] != "data: event-2" {
		t.Errorf("event[1] = %q, want %q", events[1], "data: event-2")
	}
}
