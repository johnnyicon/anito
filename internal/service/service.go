// Package service is the shared business logic layer for Anito.
// Both the HTTP API (internal/server) and the MCP server (internal/mcp)
// are thin wrappers around this package. No business logic lives in
// command handlers.
package service

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/johnnyicon/anito/internal/process"
	"github.com/johnnyicon/anito/internal/proxy"
	"github.com/johnnyicon/anito/internal/registry"
	"github.com/johnnyicon/anito/internal/watcher"
)

// sseHealthCheckPaths is the set of health check paths that require an SSE
// readiness probe instead of a plain HTTP 200 check.  The MCP SSE transport
// binds its HTTP listener before the MCP session is ready to serve tool calls,
// so a plain 200 on /sse is not sufficient — we must read the first SSE event
// ("event: endpoint") to confirm the MCP server is fully initialised.
var sseHealthCheckPaths = map[string]bool{
	"/sse": true,
}

const (
	portRangeStart      = 8100
	portRangeEnd        = 8200
	healthCheckInterval = 200 * time.Millisecond
	healthCheckTimeout  = 15 * time.Second

	// DaemonLogName is the reserved name for streaming Anito's own daemon log.
	// Use it with Logs() / LogStream() — no service registry entry is required.
	DaemonLogName = "~daemon"
)

// reservedPorts are Anito's own ports — never allocate these to user services.
var reservedPorts = map[int]bool{
	7700: true, // management API
	7701: true, // MCP server
}

// Service owns the core Anito operations. Create one per daemon via New.
type Service struct {
	reg    *registry.Registry
	mgr    *process.Manager
	prx    *proxy.Manager
	logDir string
	wtch   *watcher.Manager
}

func New(reg *registry.Registry, mgr *process.Manager, prx *proxy.Manager, logDir string, wtch *watcher.Manager) *Service {
	svc := &Service{reg: reg, mgr: mgr, prx: prx, logDir: logDir, wtch: wtch}
	mgr.OnCrash = svc.handleCrash
	return svc
}

// DeployRequest describes a service to deploy.
type DeployRequest struct {
	Name        string
	Version     string // optional semver tag, e.g. "v1.2.3"
	Type        registry.ServiceType
	Path        string   // binary path or static dir
	Args        []string // optional arguments passed to the binary at startup
	StablePort  int      // 0 = auto-allocate from [portRangeStart, portRangeEnd]
	EnvFile     string
	HealthCheck string
	WatchPaths  []string // directories to watch for file changes (triggers restart)
}

// Deploy registers, starts, and health-checks a service.
//
//   - If StablePort == 0, a port is auto-allocated from the configured range.
//   - If StablePort is non-zero but unavailable, auto-allocation is used as fallback.
//   - Re-deploying an existing service always preserves its stable port.
func (s *Service) Deploy(req DeployRequest) (*registry.Service, error) {
	if req.HealthCheck == "" {
		req.HealthCheck = "/health"
	}
	if req.Type == "" {
		req.Type = registry.TypeBinary
	}

	stablePort, err := s.allocatePort(req.Name, req.StablePort)
	if err != nil {
		return nil, err
	}

	version := req.Version
	if version == "" {
		version = hashPath(req.Path)
	}

	svc := &registry.Service{
		Name:        req.Name,
		Version:     version,
		Type:        req.Type,
		BinaryPath:  req.Path,
		Args:        req.Args,
		StablePort:  stablePort,
		EnvFile:     req.EnvFile,
		HealthCheck: req.HealthCheck,
		WatchPaths:  req.WatchPaths,
	}

	if err := s.reg.Register(svc); err != nil {
		return nil, err
	}
	svc, _ = s.reg.Get(req.Name)

	if req.Type == registry.TypeStatic {
		if err := s.prx.SwapStatic(req.Name, req.Path); err != nil {
			return nil, err
		}
		_ = s.reg.UpdateStatus(req.Name, registry.StatusRunning, 0)
		svc, _ = s.reg.Get(req.Name)
		log.Printf("[DEPLOY] name=%s port=%d type=static path=%s", svc.Name, svc.StablePort, req.Path)
		return svc, nil
	}

	// Binary: start new process, health-check it, swap proxy, drain old process.
	// If the manager is already tracking this service (e.g. after a daemon
	// restart + restore), deregister it without killing it so the name slot is
	// free. The old PID will be drained after the new process is healthy.
	oldPID := s.mgr.Deregister(svc.Name)
	if oldPID == 0 {
		oldPID = svc.PID // fall back to registry PID if not tracked in-memory
	}

	internalPort, err := s.mgr.Start(svc)
	if err != nil {
		return nil, err
	}

	if err := waitHealthy(internalPort, req.HealthCheck); err != nil {
		_ = s.mgr.Stop(req.Name)
		return nil, fmt.Errorf("health check failed: %w", err)
	}

	if err := s.prx.Swap(req.Name, internalPort); err != nil {
		return nil, err
	}

	if oldPID > 0 {
		s.mgr.MarkDraining(oldPID)
		go process.StopPID(oldPID)
	}

	svc, _ = s.reg.Get(req.Name)
	log.Printf("[DEPLOY] name=%s port=%d internal=%d pid=%d", svc.Name, svc.StablePort, internalPort, svc.PID)
	s.startWatcher(svc)
	return svc, nil
}

func (s *Service) Services() []*registry.Service {
	return s.reg.All()
}

func (s *Service) Status(name string) (*registry.Service, error) {
	svc, ok := s.reg.Get(name)
	if !ok {
		return nil, fmt.Errorf("service %q not found", name)
	}
	return svc, nil
}

func (s *Service) Stop(name string) error {
	s.wtch.Stop(name)
	err := s.mgr.Stop(name)
	if err != nil {
		log.Printf("[STOP] name=%s error=%q", name, err)
	} else {
		log.Printf("[STOP] name=%s", name)
	}
	return err
}

func (s *Service) Restart(name string) error {
	svc, ok := s.reg.Get(name)
	if !ok {
		return fmt.Errorf("service %q not found", name)
	}
	internalPort, err := s.mgr.Restart(svc)
	if err != nil {
		log.Printf("[RESTART] name=%s error=%q", name, err)
		return err
	}
	if svc.Type == registry.TypeBinary {
		if err := waitHealthy(internalPort, svc.HealthCheck); err != nil {
			err = fmt.Errorf("health check failed after restart: %w", err)
			log.Printf("[RESTART] name=%s error=%q", name, err)
			return err
		}
		if err := s.prx.Swap(name, internalPort); err != nil {
			return err
		}
	}
	log.Printf("[RESTART] name=%s port=%d internal=%d", name, svc.StablePort, internalPort)
	return nil
}

func (s *Service) Remove(name string) error {
	s.wtch.Stop(name)
	_ = s.mgr.Stop(name)
	s.prx.Remove(name)
	err := s.reg.Remove(name)
	if err != nil {
		log.Printf("[REMOVE] name=%s error=%q", name, err)
	} else {
		log.Printf("[REMOVE] name=%s", name)
	}
	return err
}

// startWatcher launches a file watcher for svc if it has WatchPaths configured.
func (s *Service) startWatcher(svc *registry.Service) {
	if len(svc.WatchPaths) == 0 {
		return
	}
	err := s.wtch.Start(svc.Name, svc.WatchPaths, func(trigger string) {
		log.Printf("[WATCH] name=%s restarting due to change in %s", svc.Name, trigger)
		if err := s.Restart(svc.Name); err != nil {
			log.Printf("[ERROR] name=%s watch restart failed: %v", svc.Name, err)
		}
	})
	if err != nil {
		log.Printf("[ERROR] name=%s could not start watcher: %v", svc.Name, err)
	}
}

// StartWatchers starts file watchers for all registered services that have WatchPaths.
// Called once at daemon startup after services are restored.
func (s *Service) StartWatchers() {
	for _, svc := range s.reg.All() {
		if len(svc.WatchPaths) > 0 {
			s.startWatcher(svc)
		}
	}
}

// handleCrash is called by the process manager when a service exits unexpectedly.
// Services with WatchPaths are automatically restarted (with a brief cooldown).
// Services that were intentionally stopped are not restarted.
func (s *Service) handleCrash(name string) {
	svc, ok := s.reg.Get(name)
	if !ok || len(svc.WatchPaths) == 0 {
		return
	}
	if svc.Status == registry.StatusStopped {
		return // intentionally stopped — do not restart
	}
	time.Sleep(2 * time.Second) // brief cooldown to avoid tight restart loops
	log.Printf("[RESTART] name=%s reason=crash", name)
	if err := s.Restart(name); err != nil {
		log.Printf("[ERROR] name=%s crash restart failed: %v", name, err)
	}
}

// logFilePath resolves the log file path for name.
// The special name DaemonLogName maps to the Anito daemon log; all other names
// require a registered service entry.
func (s *Service) logFilePath(name string) (string, error) {
	if name == DaemonLogName {
		return filepath.Join(s.logDir, "anito.log"), nil
	}
	if _, ok := s.reg.Get(name); !ok {
		return "", fmt.Errorf("service %q not found", name)
	}
	return filepath.Join(s.logDir, name+".log"), nil
}

// Logs returns the last n lines from the service's log file.
// If n <= 0, all lines are returned.
func (s *Service) Logs(name string, n int) ([]string, error) {
	path, err := s.logFilePath(name)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

// LogStream tails the service's log file and sends new lines to the returned channel.
// The channel is closed when ctx is done.
func (s *Service) LogStream(ctx context.Context, name string) (<-chan string, error) {
	path, err := s.logFilePath(name)
	if err != nil {
		return nil, err
	}
	ch := make(chan string, 64)
	go func() {
		defer close(ch)

		var (
			f       *os.File
			scanner *bufio.Scanner
		)
		tryOpen := func() {
			f2, err := os.Open(path)
			if err != nil {
				return
			}
			_, _ = f2.Seek(0, io.SeekEnd)
			f = f2
			scanner = bufio.NewScanner(f)
		}
		tryOpen()
		defer func() {
			if f != nil {
				_ = f.Close()
			}
		}()

		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if scanner == nil {
					tryOpen()
					continue
				}
				for scanner.Scan() {
					line := scanner.Text()
					select {
					case ch <- line:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()
	return ch, nil
}

// allocatePort returns the stable port to use for the named service.
//
//   - Existing services keep their current port (redeploy preserves stability).
//   - New services try preferred first; if unavailable, auto-allocate from range.
func (s *Service) allocatePort(name string, preferred int) (int, error) {
	// Preserve port for existing services.
	if existing, ok := s.reg.Get(name); ok && existing.StablePort != 0 {
		_ = s.prx.Register(name, existing.StablePort) // idempotent
		return existing.StablePort, nil
	}

	if preferred != 0 {
		if reservedPorts[preferred] {
			return 0, fmt.Errorf("port %d is reserved by Anito and cannot be assigned to a service", preferred)
		}
		if err := s.prx.Register(name, preferred); err == nil {
			return preferred, nil
		}
		// Preferred port unavailable — fall through to auto-allocate.
	}

	used := s.reg.UsedPorts()
	for port := portRangeStart; port <= portRangeEnd; port++ {
		if used[port] {
			continue
		}
		if err := s.prx.Register(name, port); err == nil {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available ports in range %d–%d", portRangeStart, portRangeEnd)
}

// hashPath returns a short content hash for a file or directory.
// Used as a fallback version identifier when no explicit version is provided.
func hashPath(path string) string {
	h := sha256.New()
	info, err := os.Stat(path)
	if err != nil {
		return "unknown"
	}
	if info.IsDir() {
		_ = filepath.Walk(path, func(p string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() {
				return nil
			}
			f, err := os.Open(p)
			if err != nil {
				return nil
			}
			defer f.Close()
			_, _ = io.Copy(h, f)
			return nil
		})
	} else {
		f, err := os.Open(path)
		if err != nil {
			return "unknown"
		}
		defer f.Close()
		_, _ = io.Copy(h, f)
	}
	return "sha:" + hex.EncodeToString(h.Sum(nil))[:8]
}

// waitHealthy polls the health check endpoint until it passes or the deadline
// is reached.  For SSE endpoints (e.g. /sse), a plain HTTP 200 is not
// sufficient — we read the first event line to confirm the MCP transport is
// fully ready to serve tool calls.
func waitHealthy(internalPort int, path string) error {
	if sseHealthCheckPaths[path] {
		return waitSSEReady(internalPort, path)
	}
	return waitHTTPReady(internalPort, path)
}

// waitHTTPReady polls path until it returns HTTP 200.
func waitHTTPReady(internalPort int, path string) error {
	url := fmt.Sprintf("http://localhost:%d%s", internalPort, path)
	deadline := time.Now().Add(healthCheckTimeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url) //nolint:noctx
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(healthCheckInterval)
	}
	return fmt.Errorf("timed out after %s waiting for %s", healthCheckTimeout, url)
}

// waitSSEReady connects to an SSE endpoint and waits for the first event line
// to be emitted.  For the MCP SSE transport this is the "event: endpoint"
// advertisement, which is only sent once the server is ready to accept
// initialize handshakes.  We treat any non-empty "event:" or "data:" line as
// proof of readiness — the exact event type is not important.
func waitSSEReady(internalPort int, path string) error {
	rawURL := fmt.Sprintf("http://localhost:%d%s", internalPort, path)
	deadline := time.Now().Add(healthCheckTimeout)

	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		ready, err := probeSSE(ctx, rawURL)
		cancel()
		if err == nil && ready {
			return nil
		}
		time.Sleep(healthCheckInterval)
	}
	return fmt.Errorf("timed out after %s waiting for SSE readiness on %s", healthCheckTimeout, rawURL)
}

// probeSSE opens an SSE connection to url and returns (true, nil) as soon as
// it receives a non-empty event or data line.  It returns (false, err) if the
// connection fails or the context expires before an event is received.
func probeSSE(ctx context.Context, url string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("SSE probe: unexpected status %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event:") || strings.HasPrefix(line, "data:") {
			return true, nil
		}
	}
	// scanner.Err() returns nil on clean EOF; either way we got no event.
	return false, fmt.Errorf("SSE connection closed before first event")
}
