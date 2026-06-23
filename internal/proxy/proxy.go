package proxy

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/johnnyicon/anito/internal/registry"
)

var (
	tailscaleIPv4Prefix = netip.MustParsePrefix("100.64.0.0/10")
	tailscaleIPv6Prefix = netip.MustParsePrefix("fd7a:115c:a1e0::/48")
	interfaceAddrs      = net.InterfaceAddrs
)

// handlerWrapper is stored in an atomic.Value so it can be swapped safely.
type handlerWrapper struct {
	h http.Handler
}

// entry holds the permanent listener(s) and the currently active handler.
// We bind on both IPv4 (127.0.0.1) and IPv6 ([::1]) so that clients
// connecting via either loopback family always hit Anito's proxy and not a
// rogue process that grabbed the IPv6 side first.
type entry struct {
	stablePort  int
	bindAddress string
	listener    net.Listener // 127.0.0.1:port
	listener6   net.Listener // [::1]:port  — nil if IPv6 unavailable
	server      *http.Server
	handler     atomic.Value // stores handlerWrapper
}

// Manager owns one persistent listener per service port and swaps the upstream atomically.
// Entries are keyed by "service:portName" for multi-port support.
type Manager struct {
	mu      sync.Mutex
	entries map[string]*entry
}

func NewManager() *Manager {
	return &Manager{entries: make(map[string]*entry)}
}

// entryKey returns the composite key for a service's named port.
func entryKey(service, portName string) string {
	return service + ":" + portName
}

// servicePrefix returns the prefix used to find all entries for a service.
func servicePrefix(service string) string {
	return service + ":"
}

// Register creates a permanent listener on stablePort for the named service.
// For backward compat, this registers a single "default" port.
// It is idempotent — calling it again for the same service name is a no-op.
func (m *Manager) Register(name string, stablePort int) error {
	return m.RegisterPorts(name, map[string]int{"default": stablePort})
}

func (m *Manager) RegisterWithBind(name string, stablePort int, bindAddress string) error {
	return m.RegisterPortsWithBind(name, map[string]int{"default": stablePort}, bindAddress)
}

// RegisterPorts creates permanent listeners for all named ports of a service.
// On partial failure, all already-bound listeners are rolled back.
// Idempotent for ports already registered.
func (m *Manager) RegisterPorts(name string, ports map[string]int) error {
	return m.RegisterPortsWithBind(name, ports, registry.DefaultProxyBindAddress)
}

func (m *Manager) RegisterPortsWithBind(name string, ports map[string]int, bindAddress string) error {
	bindAddress = strings.TrimSpace(bindAddress)
	if bindAddress == "" {
		bindAddress = registry.DefaultProxyBindAddress
	}
	if err := ValidateProxyBindAddress(bindAddress); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Track newly created entries so we can roll back on failure.
	var created []string

	for portName, stablePort := range ports {
		key := entryKey(name, portName)
		if e, ok := m.entries[key]; ok {
			if e.stablePort == stablePort && e.bindAddress == bindAddress {
				continue // already registered with the requested listener
			}
			l, l6, err := listen(bindAddress, stablePort)
			if err != nil {
				return fmt.Errorf("proxy: cannot bind port %d for %q (port %s): %w", stablePort, name, portName, err)
			}
			_ = e.server.Close()
			if e.listener6 != nil {
				_ = e.listener6.Close()
			}
			e.stablePort = stablePort
			e.bindAddress = bindAddress
			e.listener = l
			e.listener6 = l6
			e.server = serverFor(e)
			logNonLoopbackBind(name, portName, stablePort, bindAddress)
			go e.server.Serve(l) //nolint:errcheck
			if l6 != nil {
				go e.server.Serve(l6) //nolint:errcheck
			}
			continue
		}

		l, l6, err := listen(bindAddress, stablePort)
		if err != nil {
			// Roll back all entries created in this call.
			for _, k := range created {
				if e, ok := m.entries[k]; ok {
					_ = e.server.Close()
					if e.listener6 != nil {
						_ = e.listener6.Close()
					}
					delete(m.entries, k)
				}
			}
			return fmt.Errorf("proxy: cannot bind port %d for %q (port %s): %w", stablePort, name, portName, err)
		}

		e := &entry{stablePort: stablePort, bindAddress: bindAddress, listener: l, listener6: l6}
		logNonLoopbackBind(name, portName, stablePort, bindAddress)

		// Placeholder handler until Swap is called after a successful health check.
		e.handler.Store(handlerWrapper{h: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "service starting", http.StatusServiceUnavailable)
		})})

		e.server = serverFor(e)

		m.entries[key] = e
		created = append(created, key)
		go e.server.Serve(l) //nolint:errcheck
		if l6 != nil {
			go e.server.Serve(l6) //nolint:errcheck
		}
	}
	return nil
}

func serverFor(e *entry) *http.Server {
	return &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Anito-Proxy", "1")
			e.handler.Load().(handlerWrapper).h.ServeHTTP(w, r)
		}),
	}
}

func listen(bindAddress string, stablePort int) (net.Listener, net.Listener, error) {
	if bindAddress == "" || bindAddress == registry.DefaultProxyBindAddress {
		l, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", stablePort))
		if err != nil {
			return nil, nil, err
		}
		// Best-effort IPv6 bind — don't fail if the system has no IPv6 loopback.
		l6, _ := net.Listen("tcp6", fmt.Sprintf("[::1]:%d", stablePort))
		return l, l6, nil
	}
	addr := net.JoinHostPort(bindAddress, strconv.Itoa(stablePort))
	l, err := net.Listen("tcp", addr)
	return l, nil, err
}

func ValidateProxyBindAddress(bindAddress string) error {
	bindAddress = strings.TrimSpace(bindAddress)
	if bindAddress == "" || bindAddress == registry.DefaultProxyBindAddress {
		return nil
	}
	addrText := strings.TrimPrefix(strings.TrimSuffix(bindAddress, "]"), "[")
	addr, err := netip.ParseAddr(addrText)
	if err != nil {
		return fmt.Errorf("proxy_bind_address %q must be localhost, a loopback IP, or a local Tailscale IP", bindAddress)
	}
	addr = addr.Unmap()
	if addr.IsLoopback() {
		return nil
	}
	if addr.IsUnspecified() {
		return fmt.Errorf("proxy_bind_address %q is a wildcard bind; use localhost or this host's actual Tailscale IP", bindAddress)
	}
	if !isTailscaleAddress(addr) {
		return fmt.Errorf("proxy_bind_address %q is not a Tailscale address; use localhost or this host's actual Tailscale IP", bindAddress)
	}
	ok, err := localInterfaceHasAddress(addr)
	if err != nil {
		return fmt.Errorf("validating proxy_bind_address %q: %w", bindAddress, err)
	}
	if !ok {
		return fmt.Errorf("proxy_bind_address %q is not assigned to a local interface", bindAddress)
	}
	return nil
}

func isTailscaleAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	return tailscaleIPv4Prefix.Contains(addr) || tailscaleIPv6Prefix.Contains(addr)
}

func localInterfaceHasAddress(want netip.Addr) (bool, error) {
	addrs, err := interfaceAddrs()
	if err != nil {
		return false, err
	}
	want = want.Unmap()
	for _, raw := range addrs {
		if addr, ok := netAddrToNetIP(raw); ok && addr.Unmap() == want {
			return true, nil
		}
	}
	return false, nil
}

func netAddrToNetIP(raw net.Addr) (netip.Addr, bool) {
	switch addr := raw.(type) {
	case *net.IPNet:
		return netIPToNetIPAddr(addr.IP)
	case *net.IPAddr:
		return netIPToNetIPAddr(addr.IP)
	default:
		return netip.Addr{}, false
	}
}

func netIPToNetIPAddr(ip net.IP) (netip.Addr, bool) {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func logNonLoopbackBind(name, portName string, stablePort int, bindAddress string) {
	addr, err := netip.ParseAddr(strings.TrimPrefix(strings.TrimSuffix(bindAddress, "]"), "["))
	if err != nil || addr.Unmap().IsLoopback() {
		return
	}
	log.Printf("[PROXY] name=%s port=%s stable_port=%d bind_address=%s exposure=tailnet auth=upstream", name, portName, stablePort, bindAddress)
}

// Swap atomically points the proxy for name at internalPort (default port).
// The old process can be killed after this returns — all new requests go to the new one.
func (m *Manager) Swap(name string, internalPort int) error {
	return m.SwapPorts(name, map[string]int{"default": internalPort})
}

// SwapPorts atomically points all named proxies for a service at their new internal ports.
func (m *Manager) SwapPorts(name string, internalPorts map[string]int) error {
	m.mu.Lock()
	// Collect entries first, then release lock before creating proxies.
	entries := make(map[string]*entry, len(internalPorts))
	for portName := range internalPorts {
		key := entryKey(name, portName)
		e, ok := m.entries[key]
		if !ok {
			m.mu.Unlock()
			return fmt.Errorf("proxy: service %q port %q not registered", name, portName)
		}
		entries[portName] = e
	}
	m.mu.Unlock()

	// Build and swap all handlers.
	for portName, e := range entries {
		port := internalPorts[portName]
		target, err := url.Parse(fmt.Sprintf("http://localhost:%d", port))
		if err != nil {
			return err
		}
		rp := httputil.NewSingleHostReverseProxy(target)
		rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, fmt.Sprintf("upstream error: %v", err), http.StatusBadGateway)
		}
		e.handler.Store(handlerWrapper{h: &flushProxy{rp: rp}})
	}
	return nil
}

// SwapStatic points the proxy at a directory of static files (for SPA deployments).
func (m *Manager) SwapStatic(name string, dir string) error {
	m.mu.Lock()
	key := entryKey(name, "default")
	e, ok := m.entries[key]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("proxy: service %q not registered", name)
	}
	e.handler.Store(handlerWrapper{h: http.FileServer(http.Dir(dir))})
	return nil
}

// Unswap restores the 503 placeholder handler for name (default port).
func (m *Manager) Unswap(name string) {
	m.UnswapPorts(name)
}

// UnswapPorts restores the 503 placeholder handler for all named ports of a service.
func (m *Manager) UnswapPorts(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix := servicePrefix(name)
	for key, e := range m.entries {
		if strings.HasPrefix(key, prefix) {
			e.handler.Store(handlerWrapper{h: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			})})
		}
	}
}

// Remove shuts down the listener for name (default port) and removes it from the manager.
func (m *Manager) Remove(name string) {
	m.RemovePorts(name)
}

// RemovePorts shuts down all listeners for a service and removes them from the manager.
func (m *Manager) RemovePorts(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix := servicePrefix(name)
	for key, e := range m.entries {
		if strings.HasPrefix(key, prefix) {
			_ = e.server.Close()
			if e.listener6 != nil {
				_ = e.listener6.Close()
			}
			delete(m.entries, key)
		}
	}
}

// StablePort returns the stable port for a registered service's default port, or 0.
func (m *Manager) StablePort(name string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := entryKey(name, "default")
	if e, ok := m.entries[key]; ok {
		return e.stablePort
	}
	// Fall back to finding any port for this service.
	prefix := servicePrefix(name)
	for key, e := range m.entries {
		if strings.HasPrefix(key, prefix) {
			return e.stablePort
		}
	}
	return 0
}

// flushProxy wraps httputil.ReverseProxy and flushes after every write,
// which is required for SSE streams (e.g. MCP servers using SSE transport).
// WebSocket upgrade requests are passed through without flush wrapping.
type flushProxy struct {
	rp *httputil.ReverseProxy
}

func (f *flushProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// WebSocket upgrade: pass through directly — the reverse proxy handles
	// the protocol switch and bidirectional forwarding natively.
	if strings.EqualFold(r.Header.Get("Connection"), "upgrade") {
		f.rp.ServeHTTP(w, r)
		return
	}
	// For SSE or any streaming response, wrap the writer with auto-flush.
	if r.Header.Get("Accept") == "text/event-stream" {
		f.rp.ServeHTTP(&flushWriter{w: w}, r)
		return
	}
	f.rp.ServeHTTP(w, r)
}

type flushWriter struct{ w http.ResponseWriter }

func (fw *flushWriter) Header() http.Header  { return fw.w.Header() }
func (fw *flushWriter) WriteHeader(code int) { fw.w.WriteHeader(code) }
func (fw *flushWriter) Write(b []byte) (int, error) {
	n, err := fw.w.Write(b)
	if f, ok := fw.w.(http.Flusher); ok {
		f.Flush()
	}
	return n, err
}
