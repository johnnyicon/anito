package proxy

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
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
	stablePort int
	listener   net.Listener // 127.0.0.1:port
	listener6  net.Listener // [::1]:port  — nil if IPv6 unavailable
	server     *http.Server
	handler    atomic.Value // stores handlerWrapper
}

// Manager owns one persistent listener per service and swaps the upstream atomically.
type Manager struct {
	mu      sync.Mutex
	entries map[string]*entry
}

func NewManager() *Manager {
	return &Manager{entries: make(map[string]*entry)}
}

// Register creates a permanent listener on stablePort for the named service.
// It is idempotent — calling it again for the same service name is a no-op.
func (m *Manager) Register(name string, stablePort int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.entries[name]; ok {
		return nil
	}

	l, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", stablePort))
	if err != nil {
		return fmt.Errorf("proxy: cannot bind port %d for %q: %w", stablePort, name, err)
	}
	// Best-effort IPv6 bind — don't fail if the system has no IPv6 loopback.
	l6, _ := net.Listen("tcp6", fmt.Sprintf("[::1]:%d", stablePort))

	e := &entry{stablePort: stablePort, listener: l, listener6: l6}

	// Placeholder handler until Swap is called after a successful health check.
	e.handler.Store(handlerWrapper{h: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "service starting", http.StatusServiceUnavailable)
	})})

	e.server = &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Anito-Proxy", "1")
			e.handler.Load().(handlerWrapper).h.ServeHTTP(w, r)
		}),
	}

	m.entries[name] = e
	go e.server.Serve(l) //nolint:errcheck
	if l6 != nil {
		go e.server.Serve(l6) //nolint:errcheck
	}
	return nil
}

// Swap atomically points the proxy for name at internalPort.
// The old process can be killed after this returns — all new requests go to the new one.
func (m *Manager) Swap(name string, internalPort int) error {
	m.mu.Lock()
	e, ok := m.entries[name]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("proxy: service %q not registered", name)
	}

	target, err := url.Parse(fmt.Sprintf("http://localhost:%d", internalPort))
	if err != nil {
		return err
	}

	rp := httputil.NewSingleHostReverseProxy(target)
	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, fmt.Sprintf("upstream error: %v", err), http.StatusBadGateway)
	}

	e.handler.Store(handlerWrapper{h: &flushProxy{rp: rp}})
	return nil
}

// SwapStatic points the proxy at a directory of static files (for SPA deployments).
func (m *Manager) SwapStatic(name string, dir string) error {
	m.mu.Lock()
	e, ok := m.entries[name]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("proxy: service %q not registered", name)
	}
	e.handler.Store(handlerWrapper{h: http.FileServer(http.Dir(dir))})
	return nil
}

// Unswap restores the 503 placeholder handler for name.
// Call this when a service stops or crashes so the stable port returns a clean
// "service unavailable" instead of a 502 pointing at a dead backend.
// A subsequent Swap call (after a successful restart or deploy) re-enables proxying.
func (m *Manager) Unswap(name string) {
	m.mu.Lock()
	e, ok := m.entries[name]
	m.mu.Unlock()
	if !ok {
		return
	}
	e.handler.Store(handlerWrapper{h: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	})})
}

// Remove shuts down the listener for name and removes it from the manager.
func (m *Manager) Remove(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[name]
	if !ok {
		return
	}
	_ = e.server.Close()
	if e.listener6 != nil {
		_ = e.listener6.Close()
	}
	delete(m.entries, name)
}

// StablePort returns the stable port for a registered service, or 0.
func (m *Manager) StablePort(name string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.entries[name]; ok {
		return e.stablePort
	}
	return 0
}

// flushProxy wraps httputil.ReverseProxy and flushes after every write,
// which is required for SSE streams (e.g. MCP servers using SSE transport).
type flushProxy struct {
	rp *httputil.ReverseProxy
}

func (f *flushProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// For SSE or any streaming response, wrap the writer with auto-flush.
	if r.Header.Get("Accept") == "text/event-stream" {
		f.rp.ServeHTTP(&flushWriter{w: w}, r)
		return
	}
	f.rp.ServeHTTP(w, r)
}

type flushWriter struct{ w http.ResponseWriter }

func (fw *flushWriter) Header() http.Header        { return fw.w.Header() }
func (fw *flushWriter) WriteHeader(code int)        { fw.w.WriteHeader(code) }
func (fw *flushWriter) Write(b []byte) (int, error) {
	n, err := fw.w.Write(b)
	if f, ok := fw.w.(http.Flusher); ok {
		f.Flush()
	}
	return n, err
}
