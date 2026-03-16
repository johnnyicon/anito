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
)

const (
	portRangeStart      = 8100
	portRangeEnd        = 8200
	healthCheckInterval = 200 * time.Millisecond
	healthCheckTimeout  = 15 * time.Second
)

// Service owns the core Anito operations. Create one per daemon via New.
type Service struct {
	reg    *registry.Registry
	mgr    *process.Manager
	prx    *proxy.Manager
	logDir string
}

func New(reg *registry.Registry, mgr *process.Manager, prx *proxy.Manager, logDir string) *Service {
	return &Service{reg: reg, mgr: mgr, prx: prx, logDir: logDir}
}

// DeployRequest describes a service to deploy.
type DeployRequest struct {
	Name        string
	Version     string // optional semver tag, e.g. "v1.2.3"
	Type        registry.ServiceType
	Path        string // binary path or static dir
	StablePort  int    // 0 = auto-allocate from [portRangeStart, portRangeEnd]
	EnvFile     string
	HealthCheck string
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
		StablePort:  stablePort,
		EnvFile:     req.EnvFile,
		HealthCheck: req.HealthCheck,
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
		go process.StopPID(oldPID)
	}

	svc, _ = s.reg.Get(req.Name)
	log.Printf("[DEPLOY] name=%s port=%d internal=%d pid=%d", svc.Name, svc.StablePort, internalPort, svc.PID)
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

// Logs returns the last n lines from the service's log file.
// If n <= 0, all lines are returned.
func (s *Service) Logs(name string, n int) ([]string, error) {
	if _, ok := s.reg.Get(name); !ok {
		return nil, fmt.Errorf("service %q not found", name)
	}
	path := filepath.Join(s.logDir, name+".log")
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
	if _, ok := s.reg.Get(name); !ok {
		return nil, fmt.Errorf("service %q not found", name)
	}
	path := filepath.Join(s.logDir, name+".log")
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

func waitHealthy(internalPort int, path string) error {
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
