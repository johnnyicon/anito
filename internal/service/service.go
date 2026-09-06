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
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/johnnyicon/anito/internal/config"
	"github.com/johnnyicon/anito/internal/diagnosis"
	"github.com/johnnyicon/anito/internal/domain"
	"github.com/johnnyicon/anito/internal/issues"
	"github.com/johnnyicon/anito/internal/notify"
	"github.com/johnnyicon/anito/internal/process"
	"github.com/johnnyicon/anito/internal/proxy"
	"github.com/johnnyicon/anito/internal/receipt"
	"github.com/johnnyicon/anito/internal/registry"
	"github.com/johnnyicon/anito/internal/shellcmd"
	"github.com/johnnyicon/anito/internal/watcher"
)

const (
	portRangeStart            = 8100
	portRangeEnd              = 8200
	healthCheckInterval       = 200 * time.Millisecond
	defaultHealthCheckTimeout = 15 * time.Second
	defaultDrainWindow        = 2 * time.Second

	// DaemonLogName is the reserved name for streaming Anito's own daemon log.
	// Use it with Logs() / LogStream() — no service registry entry is required.
	DaemonLogName = "~daemon"
)

// crashBackoffDurations is the wait before each successive crash-restart attempt.
// After all attempts are exhausted the service is left failed and [CRASH_GIVE_UP] is logged.
var crashBackoffDurations = []time.Duration{
	1 * time.Second,
	2 * time.Second,
	4 * time.Second,
	8 * time.Second,
	30 * time.Second,
}

// reservedPorts are Anito's own ports — never allocate these to user services.
var reservedPorts = map[int]bool{
	7700: true, // management API
	7701: true, // MCP server
}

// ValidateServiceName rejects names that would be unsafe as registry/proxy keys
// or log-file path segments.
func ValidateServiceName(name string) error {
	if name == "" {
		return fmt.Errorf("invalid service name: name is required")
	}
	if strings.HasPrefix(name, DaemonLogName) {
		return fmt.Errorf("invalid service name %q: %s is reserved", name, DaemonLogName)
	}
	if len(name) > 64 {
		return fmt.Errorf("invalid service name %q: must be 64 characters or fewer", name)
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("invalid service name %q: must not contain '..'", name)
	}
	if !isServiceNameAlphaNum(name[0]) {
		return fmt.Errorf("invalid service name %q: must start with a letter or digit", name)
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		if isServiceNameAlphaNum(c) || c == '.' || c == '_' || c == '-' {
			continue
		}
		return fmt.Errorf("invalid service name %q: only letters, digits, '.', '_' and '-' are allowed", name)
	}
	return nil
}

func isServiceNameAlphaNum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// Service owns the core Anito operations. Create one per daemon via New.
type Service struct {
	reg    *registry.Registry
	mgr    *process.Manager
	prx    *proxy.Manager
	logDir string
	wtch   *watcher.Manager
	iss    *issues.Store // may be nil in tests

	crashMu       sync.Mutex
	crashAttempts map[string]int // per-service crash attempt counter; reset on successful start

	deployLocks sync.Map // map[string]*sync.Mutex — per-service deploy serialization

	startup startupTracker

	deploysTotal atomic.Int64
	crashesTotal atomic.Int64
}

// Metrics holds aggregate health counters for the daemon.
type Metrics struct {
	ServicesRunning  int   `json:"services_running"`
	ServicesStopped  int   `json:"services_stopped"`
	ServicesFailed   int   `json:"services_failed"`
	ServicesOrphaned int   `json:"services_orphaned"`
	ServicesTotal    int   `json:"services_total"`
	DeploysTotal     int64 `json:"deploys_total"`
	CrashesTotal     int64 `json:"crashes_total"`
}

// Metrics returns a snapshot of aggregate daemon health.
func (s *Service) Metrics() Metrics {
	m := Metrics{
		DeploysTotal: s.deploysTotal.Load(),
		CrashesTotal: s.crashesTotal.Load(),
	}
	for _, svc := range s.Services() {
		m.ServicesTotal++
		switch svc.Status {
		case registry.StatusRunning:
			m.ServicesRunning++
		case registry.StatusStopped:
			m.ServicesStopped++
		case registry.StatusFailed:
			m.ServicesFailed++
		case registry.StatusOrphaned:
			m.ServicesOrphaned++
		}
	}
	return m
}

func New(reg *registry.Registry, mgr *process.Manager, prx *proxy.Manager, logDir string, wtch *watcher.Manager, iss *issues.Store) *Service {
	svc := &Service{
		reg:           reg,
		mgr:           mgr,
		prx:           prx,
		logDir:        logDir,
		wtch:          wtch,
		iss:           iss,
		crashAttempts: make(map[string]int),
		startup:       newStartupTracker(),
	}
	mgr.OnCrash = svc.handleCrash
	return svc
}

// DeployRequest describes a service to deploy.
type DeployRequest struct {
	Name               string
	Version            string // optional semver tag, e.g. "v1.2.3"
	VersionPath        string // optional file or directory to hash when Version is empty
	Type               registry.ServiceType
	Path               string         // binary path or static dir
	Args               []string       // optional arguments passed to the binary at startup
	StablePort         int            // 0 = auto-allocate (backward compat, single port)
	StablePorts        map[string]int // named ports (takes precedence over StablePort if non-nil)
	ProxyBindAddress   string         // stable proxy listener address (default: localhost)
	HealthCheckPort    string         // which named port to health-check
	EnvFile            string
	HealthCheck        string
	WatchPaths         []string      // directories to watch for file changes (triggers restart)
	DrainWindow        time.Duration // grace period between proxy swap and SIGTERM to old process (0 = use defaultDrainWindow)
	HealthCheckTimeout time.Duration // how long to poll /health (0 = use defaultHealthCheckTimeout)
	RestartPolicy      string        // "always" | "on-watch" | "never" (default: "on-watch")
	ConfigPath         string        // absolute path to the .anito/config.yaml that produced this deploy
}

// lockDeploy acquires a per-service deploy lock and returns an unlock function.
// This serializes concurrent Deploy/Restart calls for the same service name.
func (s *Service) lockDeploy(name string) func() {
	v, _ := s.deployLocks.LoadOrStore(name, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// Deploy registers, starts, and health-checks a service.
//
//   - If StablePorts is nil and StablePort == 0, a port is auto-allocated.
//   - Re-deploying an existing service always preserves its stable port(s).
func (s *Service) Deploy(req DeployRequest) (*registry.Service, error) {
	if err := s.ensureMutable(); err != nil {
		return nil, err
	}
	if err := ValidateServiceName(req.Name); err != nil {
		return nil, err
	}
	defer s.lockDeploy(req.Name)()

	if err := validateDeployRequest(req); err != nil {
		return nil, err
	}
	if req.HealthCheck == "" {
		req.HealthCheck = "/health"
	}
	if req.Type == "" {
		req.Type = registry.TypeBinary
	}
	if req.EnvFile == "null" {
		req.EnvFile = ""
	}
	if err := proxy.ValidateProxyBindAddress(req.ProxyBindAddress); err != nil {
		return nil, err
	}
	previous, hadPrevious := s.reg.Get(req.Name)
	previous = registry.Clone(previous)

	// Normalize: singular StablePort → StablePorts map.
	if len(req.StablePorts) == 0 {
		port := req.StablePort // may be 0 (auto-allocate)
		req.StablePorts = map[string]int{"default": port}
	}

	stablePorts, err := s.allocatePorts(req.Name, req.StablePorts, req.ProxyBindAddress)
	if err != nil {
		return nil, err
	}

	version := req.Version
	if version == "" {
		versionPath := req.VersionPath
		if versionPath == "" {
			versionPath = req.Path
		}
		version = hashPath(versionPath)
	}

	drainWindow := req.DrainWindow
	if drainWindow == 0 {
		drainWindow = defaultDrainWindow
	}
	hcTimeout := req.HealthCheckTimeout
	if hcTimeout == 0 {
		hcTimeout = defaultHealthCheckTimeout
	}
	restartPolicy := req.RestartPolicy
	if restartPolicy == "" {
		restartPolicy = "on-watch"
	}

	svc := &registry.Service{
		Name:               req.Name,
		Version:            version,
		Type:               req.Type,
		BinaryPath:         req.Path,
		Args:               req.Args,
		StablePorts:        stablePorts,
		ProxyBindAddress:   req.ProxyBindAddress,
		HealthCheckPort:    req.HealthCheckPort,
		EnvFile:            req.EnvFile,
		HealthCheck:        req.HealthCheck,
		WatchPaths:         req.WatchPaths,
		DrainWindow:        drainWindow,
		HealthCheckTimeout: hcTimeout,
		RestartPolicy:      restartPolicy,
		ConfigPath:         req.ConfigPath,
		Status:             registry.StatusStopped,
	}
	svc.NormalizePorts()

	if err := s.reg.Register(svc); err != nil {
		return nil, err
	}
	svc, _ = s.reg.Get(req.Name)
	if req.Type == registry.TypeStatic {
		if err := s.prx.RegisterPortsWithBind(svc.Name, svc.StablePorts, svc.ProxyBindAddress); err != nil {
			s.restorePrevious(req.Name, previous, nil)
			return nil, err
		}
		if err := s.prx.SwapStatic(req.Name, req.Path); err != nil {
			s.restoreFailedDeploy(req.Name, previous, hadPrevious, nil)
			return nil, err
		}
		_ = s.reg.UpdateStatus(req.Name, registry.StatusRunning, 0)
		_ = s.reg.UpdateLastDeployed(req.Name, time.Now())
		svc, _ = s.reg.Get(req.Name)
		log.Printf("[DEPLOY] name=%s port=%d type=static path=%s", svc.Name, svc.StablePort, req.Path)
		s.deploysTotal.Add(1)
		writeReceipt(svc)
		return svc, nil
	}

	// Binary: start new process, health-check it, swap proxy, drain old process.
	oldProc := s.mgr.Detach(svc.Name)

	internalPorts, err := s.mgr.StartCandidate(svc)
	if err != nil {
		s.restoreFailedDeploy(req.Name, previous, hadPrevious, oldProc)
		return nil, err
	}
	// Health-check on the designated port.
	hcInternalPort := registry.PickPort(internalPorts, req.HealthCheckPort)

	if err := waitHealthy(hcInternalPort, req.HealthCheck, hcTimeout); err != nil {
		_ = s.mgr.StopFailed(req.Name)
		s.restoreFailedDeploy(req.Name, previous, hadPrevious, oldProc)
		return nil, err
	}
	if err := process.VerifyPortsOwnedByProcessTree(internalPorts, s.mgr.PID(req.Name)); err != nil {
		_ = s.mgr.Stop(req.Name)
		s.restoreFailedDeploy(req.Name, previous, hadPrevious, oldProc)
		return nil, err
	}

	// Keep the old stable listener (including its public bind address) until
	// the candidate passes readiness. Registry rollback alone cannot resurrect
	// a public listener that was replaced with a loopback listener too early.
	if err := s.prx.RegisterPortsWithBind(svc.Name, svc.StablePorts, svc.ProxyBindAddress); err != nil {
		_ = s.mgr.StopFailed(req.Name)
		s.restoreFailedDeploy(req.Name, previous, hadPrevious, oldProc)
		return nil, err
	}
	if err := s.prx.SwapPorts(req.Name, internalPorts); err != nil {
		_ = s.mgr.StopFailed(req.Name)
		s.restoreFailedDeploy(req.Name, previous, hadPrevious, oldProc)
		return nil, err
	}
	newPID := s.mgr.PID(req.Name)
	if newPID == 0 {
		s.restoreFailedDeploy(req.Name, previous, hadPrevious, oldProc)
		return nil, fmt.Errorf("service %q candidate exited before activation", req.Name)
	}
	if err := s.mgr.Activate(req.Name); err != nil {
		s.restoreFailedDeploy(req.Name, previous, hadPrevious, oldProc)
		return nil, err
	}
	_ = s.reg.MarkRunning(req.Name, newPID, time.Now())
	_ = s.reg.UpdateCrashState(req.Name, 0, false)

	s.crashMu.Lock()
	delete(s.crashAttempts, req.Name)
	s.crashMu.Unlock()

	if oldProc != nil && oldProc.Cmd() != nil {
		pid := s.mgr.MarkDrainingProcess(oldProc.Cmd())
		if pid == 0 {
			pid = oldProc.PID()
		}
		go func(cmd *exec.Cmd, done <-chan struct{}, p int, window time.Duration) {
			log.Printf("[DRAIN] name=%s pid=%d waiting %s for in-flight requests", req.Name, p, window)
			time.Sleep(window)
			process.DrainProc(cmd, done)
		}(oldProc.Cmd(), oldProc.Done(), pid, drainWindow)
	}

	svc, _ = s.reg.Get(req.Name)
	log.Printf("[DEPLOY] name=%s port=%d internal=%d pid=%d", svc.Name, svc.StablePort, svc.InternalPort, svc.PID)
	notify.Send("Anito", fmt.Sprintf("✓ %s deployed on :%d", svc.Name, svc.StablePort))
	s.deploysTotal.Add(1)
	s.startWatcher(svc)
	writeReceipt(svc)
	return svc, nil
}

func (s *Service) restoreFailedDeploy(name string, previous *registry.Service, hadPrevious bool, oldProc *process.DetachedProcess) {
	if oldProc != nil {
		if restored, err := s.mgr.Restore(name, oldProc); err != nil {
			log.Printf("[ERROR] name=%s restore old process failed: %v", name, err)
		} else if restored {
			log.Printf("[RESTORE] name=%s old process restored after failed replacement pid=%d", name, oldProc.PID())
		}
	}
	if hadPrevious {
		if current, ok := s.reg.Get(name); ok {
			previous.LastStartedAt = current.LastStartedAt
			previous.StartHistory = append([]registry.StartEvent(nil), current.StartHistory...)
		}
		if err := s.reg.Restore(previous); err != nil {
			log.Printf("[ERROR] name=%s restore registry failed: %v", name, err)
		}
		if err := s.prx.RegisterPortsWithBind(name, previous.StablePorts, previous.ProxyBindAddress); err != nil {
			log.Printf("[ERROR] name=%s restore proxy listener failed: %v", name, err)
		} else if previous.Type == registry.TypeStatic {
			if err := s.prx.SwapStatic(name, previous.BinaryPath); err != nil {
				log.Printf("[ERROR] name=%s restore static upstream failed: %v", name, err)
			}
		} else if len(previous.InternalPorts) > 0 {
			if err := s.prx.SwapPorts(name, previous.InternalPorts); err != nil {
				log.Printf("[ERROR] name=%s restore upstream failed: %v", name, err)
			}
		}
		return
	}
	if err := s.reg.Remove(name); err != nil {
		log.Printf("[ERROR] name=%s cleanup registry after failed deploy: %v", name, err)
	}
	s.prx.RemovePorts(name)
}

// isOrphaned reports whether svc's binary no longer exists on disk.
// Only applies to TypeBinary services that are not currently running.
// Running services are never orphaned — the process is live regardless of the binary on disk.
func isOrphaned(svc *registry.Service) bool {
	if svc.Type != registry.TypeBinary || svc.BinaryPath == "" {
		return false
	}
	if svc.Status == registry.StatusRunning {
		return false
	}
	_, err := os.Stat(svc.BinaryPath)
	return os.IsNotExist(err)
}

func (s *Service) Services() []*registry.Service {
	all := s.reg.All()
	result := make([]*registry.Service, len(all))
	for i, svc := range all {
		if isOrphaned(svc) {
			projected := *svc
			projected.Status = registry.StatusOrphaned
			result[i] = &projected
		} else {
			result[i] = svc
		}
	}
	return result
}

// UsedPorts returns the set of stable ports currently claimed in the registry.
// Used by anito_coordinate to avoid port conflicts when allocating new ports.
func (s *Service) UsedPorts() map[int]bool {
	return s.reg.UsedPorts()
}

func (s *Service) Status(name string) (*registry.Service, error) {
	svc, ok := s.reg.Get(name)
	if !ok {
		return nil, domain.MissingServicef("service %q not found", name)
	}
	if isOrphaned(svc) {
		projected := *svc
		projected.Status = registry.StatusOrphaned
		return &projected, nil
	}
	return svc, nil
}

func (s *Service) Stop(name string) error {
	if err := s.ensureMutable(); err != nil {
		return err
	}
	if _, ok := s.reg.Get(name); !ok {
		return domain.MissingServicef("service %q not found", name)
	}
	s.wtch.Stop(name)
	err := s.mgr.Stop(name)
	if err != nil {
		log.Printf("[STOP] name=%s error=%q", name, err)
	} else {
		_ = s.reg.UpdateStatus(name, registry.StatusStopped, 0)
		log.Printf("[STOP] name=%s", name)
	}
	// Always unswap so all stable ports return 503 instead of 502 from the dead backend.
	s.prx.UnswapPorts(name)
	return err
}

func (s *Service) Restart(name string) error {
	if err := s.ensureMutable(); err != nil {
		return err
	}
	if err := ValidateServiceName(name); err != nil {
		return err
	}
	defer s.lockDeploy(name)()
	return s.restartLocked(name)
}

func (s *Service) restartLocked(name string) error {
	svc, ok := s.reg.Get(name)
	if !ok {
		return domain.MissingServicef("service %q not found", name)
	}

	if svc.Type != registry.TypeBinary {
		return nil
	}

	previous := registry.Clone(svc)
	oldProc := s.mgr.Detach(name)

	internalPorts, err := s.mgr.StartCandidate(svc)
	if err != nil {
		log.Printf("[RESTART] name=%s error=%q", name, err)
		s.restoreFailedDeploy(name, previous, true, oldProc)
		return err
	}
	hcTimeout := svc.HealthCheckTimeout
	if hcTimeout == 0 {
		hcTimeout = defaultHealthCheckTimeout
	}

	// Health-check on the designated port.
	hcInternalPort := registry.PickPort(internalPorts, svc.HealthCheckPort)

	if err := waitHealthy(hcInternalPort, svc.HealthCheck, hcTimeout); err != nil {
		log.Printf("[RESTART] name=%s error=%q", name, err)
		_ = s.mgr.StopFailed(name)
		s.restoreFailedDeploy(name, previous, true, oldProc)
		return err
	}
	if err := process.VerifyPortsOwnedByProcessTree(internalPorts, s.mgr.PID(name)); err != nil {
		log.Printf("[RESTART] name=%s ownership=%q", name, err)
		_ = s.mgr.StopFailed(name)
		s.restoreFailedDeploy(name, previous, true, oldProc)
		return err
	}

	if err := s.prx.SwapPorts(name, internalPorts); err != nil {
		_ = s.mgr.StopFailed(name)
		s.restoreFailedDeploy(name, previous, true, oldProc)
		return err
	}
	newPID := s.mgr.PID(name)
	if newPID == 0 {
		s.restoreFailedDeploy(name, previous, true, oldProc)
		return fmt.Errorf("service %q candidate exited before activation", name)
	}
	if err := s.mgr.Activate(name); err != nil {
		s.restoreFailedDeploy(name, previous, true, oldProc)
		return err
	}
	_ = s.reg.MarkRunning(name, newPID, time.Time{})
	_ = s.reg.UpdateCrashState(name, 0, false)

	s.crashMu.Lock()
	delete(s.crashAttempts, name)
	s.crashMu.Unlock()

	drainWindow := svc.DrainWindow
	if drainWindow == 0 {
		drainWindow = defaultDrainWindow
	}
	if oldProc != nil && oldProc.Cmd() != nil {
		pid := s.mgr.MarkDrainingProcess(oldProc.Cmd())
		if pid == 0 {
			pid = oldProc.PID()
		}
		go func(cmd *exec.Cmd, done <-chan struct{}, p int, window time.Duration) {
			log.Printf("[DRAIN] name=%s pid=%d waiting %s for in-flight requests", name, p, window)
			time.Sleep(window)
			process.DrainProc(cmd, done)
		}(oldProc.Cmd(), oldProc.Done(), pid, drainWindow)
	}

	log.Printf("[RESTART] name=%s port=%d internal=%d", name, svc.StablePort, svc.InternalPort)
	notify.Send("Anito", fmt.Sprintf("↺ %s restarted on :%d", name, svc.StablePort))
	return nil
}

func (s *Service) restorePrevious(name string, previous *registry.Service, old *process.DetachedProcess) {
	if previous == nil {
		s.prx.UnswapPorts(name)
		_ = s.reg.UpdateStatus(name, registry.StatusFailed, 0)
		return
	}

	// Keep the failed candidate in lifecycle history while restoring the last
	// known-good deploy configuration and runtime ports.
	if current, ok := s.reg.Get(name); ok {
		previous.LastStartedAt = current.LastStartedAt
		previous.StartHistory = current.StartHistory
	}
	if err := s.reg.Restore(previous); err != nil {
		log.Printf("[ERROR] name=%s registry restore failed: %v", name, err)
		return
	}
	if old != nil {
		if restored, err := s.mgr.Restore(name, old); err != nil {
			log.Printf("[ERROR] name=%s process restore failed: %v", name, err)
		} else if !restored {
			log.Printf("[ERROR] name=%s process restore skipped: previous process exited", name)
		} else {
			if len(previous.InternalPorts) > 0 {
				if err := s.prx.SwapPorts(name, previous.InternalPorts); err != nil {
					log.Printf("[ERROR] name=%s proxy restore failed: %v", name, err)
				}
			}
			_ = s.reg.UpdateStatus(name, registry.StatusRunning, old.PID())
			return
		}
	}
	if previous.Status == registry.StatusRunning && !s.mgr.IsRunning(name) && previous.Type == registry.TypeBinary {
		s.prx.UnswapPorts(name)
		_ = s.reg.UpdateStatus(name, registry.StatusFailed, 0)
	}
}

func validateDeployRequest(req DeployRequest) error {
	if err := validateServiceName(req.Name); err != nil {
		return err
	}
	if req.Path == "" {
		return domain.InvalidConfigf("service path is required")
	}
	typeName := req.Type
	if typeName == "" {
		typeName = registry.TypeBinary
	}
	if typeName != registry.TypeBinary && typeName != registry.TypeStatic {
		return domain.InvalidConfigf("service %q has unsupported type %q", req.Name, typeName)
	}
	info, err := os.Stat(req.Path)
	if err != nil {
		return domain.InvalidConfigf("service %q path %s: %v", req.Name, req.Path, err)
	}
	if typeName == registry.TypeBinary && info.IsDir() {
		return domain.InvalidConfigf("service %q binary path is a directory: %s", req.Name, req.Path)
	}
	if typeName == registry.TypeStatic && !info.IsDir() {
		return domain.InvalidConfigf("service %q static path is not a directory: %s", req.Name, req.Path)
	}
	if req.HealthCheck != "" && !strings.HasPrefix(req.HealthCheck, "/") {
		return domain.InvalidConfigf("service %q health_check must start with /", req.Name)
	}
	if req.RestartPolicy != "" {
		switch req.RestartPolicy {
		case "always", "on-watch", "never":
		default:
			return domain.InvalidConfigf("service %q restart_policy must be always, on-watch, or never", req.Name)
		}
	}
	return nil
}

func validateServiceName(name string) error {
	if name == "" {
		return domain.InvalidConfigf("service name is required")
	}
	if len(name) > 128 {
		return domain.InvalidConfigf("service name must be 128 characters or fewer")
	}
	if name == DaemonLogName || name == "." || name == ".." {
		return domain.InvalidConfigf("service name %q is reserved", name)
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return domain.InvalidConfigf("service name %q contains invalid character %q", name, r)
	}
	return nil
}

func (s *Service) Rollback(name string) (*registry.Service, error) {
	if err := s.ensureMutable(); err != nil {
		return nil, err
	}
	defer s.lockDeploy(name)()

	current, ok := s.reg.Get(name)
	if !ok {
		return nil, domain.MissingServicef("service %q not found", name)
	}
	prev := current.PreviousDeployment
	if prev == nil {
		return nil, domain.InvalidConfigf("service %q has no previous deployment", name)
	}

	restored := &registry.Service{
		Name:               name,
		Version:            prev.Version,
		Type:               prev.Type,
		ConfigPath:         prev.ConfigPath,
		BinaryPath:         prev.BinaryPath,
		Args:               append([]string(nil), prev.Args...),
		StablePorts:        copyPorts(current.StablePorts),
		ProxyBindAddress:   fallbackString(prev.ProxyBindAddress, current.ProxyBindAddress),
		HealthCheckPort:    prev.HealthCheckPort,
		EnvFile:            prev.EnvFile,
		HealthCheck:        prev.HealthCheck,
		WatchPaths:         append([]string(nil), prev.WatchPaths...),
		DrainWindow:        prev.DrainWindow,
		HealthCheckTimeout: prev.HealthCheckTimeout,
		RestartPolicy:      prev.RestartPolicy,
		Status:             registry.StatusStopped,
		DeployedAt:         current.DeployedAt,
		PreviousDeployment: registry.Snapshot(current),
	}
	restored.NormalizePorts()

	if err := s.reg.Register(restored); err != nil {
		return nil, err
	}
	svc, _ := s.reg.Get(name)
	if err := s.prx.RegisterPortsWithBind(svc.Name, svc.StablePorts, svc.ProxyBindAddress); err != nil {
		return nil, err
	}
	if svc.Type == registry.TypeStatic {
		if err := s.prx.SwapStatic(svc.Name, svc.BinaryPath); err != nil {
			return nil, err
		}
		_ = s.reg.UpdateStatus(name, registry.StatusRunning, 0)
		_ = s.reg.UpdateLastDeployed(name, time.Now())
		svc, _ = s.reg.Get(name)
		writeReceipt(svc)
		log.Printf("[ROLLBACK] name=%s version=%s", name, svc.Version)
		return svc, nil
	}
	if err := s.restartLocked(name); err != nil {
		return nil, err
	}
	_ = s.reg.UpdateLastDeployed(name, time.Now())
	svc, _ = s.reg.Get(name)
	writeReceipt(svc)
	log.Printf("[ROLLBACK] name=%s version=%s", name, svc.Version)
	return svc, nil
}

func copyPorts(in map[string]int) map[string]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int, len(in))
	for name, port := range in {
		out[name] = port
	}
	return out
}

func fallbackString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func (s *Service) Remove(name string) error {
	if err := s.ensureMutable(); err != nil {
		return err
	}
	// Read config path before removing from registry.
	svc, _ := s.reg.Get(name)

	s.wtch.Stop(name)
	_ = s.mgr.Stop(name)
	s.prx.RemovePorts(name)
	err := s.reg.Remove(name)
	if err != nil {
		log.Printf("[REMOVE] name=%s error=%q", name, err)
	} else {
		log.Printf("[REMOVE] name=%s", name)
		if svc != nil {
			_ = receipt.Clear(name, svc.ConfigPath)
		}
	}
	return err
}

// Teardown reads deployed.json from repoPath/.anito/ and removes every listed
// service from Anito, then deletes the receipt file. Safe to call even if the
// receipt file does not exist (no-op). Errors for individual removals are
// collected and returned together.
func (s *Service) Teardown(repoPath string) ([]string, error) {
	if err := s.ensureMutable(); err != nil {
		return nil, err
	}
	f, err := receipt.Load(repoPath)
	if err != nil {
		return nil, fmt.Errorf("teardown: read receipt: %w", err)
	}
	if len(f.Services) == 0 {
		return nil, nil
	}

	var removed []string
	var errs []string
	repoRoot, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, fmt.Errorf("teardown: resolve repo path: %w", err)
	}

	for name, entry := range f.Services {
		if !s.teardownEntryBelongsToRepo(repoRoot, name, entry) {
			log.Printf("[TEARDOWN] skip name=%s repo=%s reason=receipt ownership mismatch", name, repoRoot)
			continue
		}
		if rmErr := s.Remove(name); rmErr != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", name, rmErr))
		} else {
			removed = append(removed, name)
		}
	}

	// Remove receipt file even on partial success.
	_ = receipt.DeleteAll(repoPath)

	if len(errs) > 0 {
		return removed, fmt.Errorf("teardown partial failure: %s", strings.Join(errs, "; "))
	}
	return removed, nil
}

func (s *Service) teardownEntryBelongsToRepo(repoRoot, name string, entry receipt.Entry) bool {
	svc, ok := s.reg.Get(name)
	if !ok {
		return pathWithinRepo(repoRoot, entry.ConfigPath)
	}
	if !pathWithinRepo(repoRoot, svc.ConfigPath) {
		return false
	}
	if entry.ConfigPath == "" {
		return true
	}
	return sameCleanPath(entry.ConfigPath, svc.ConfigPath)
}

func pathWithinRepo(repoRoot, candidate string) bool {
	if strings.TrimSpace(candidate) == "" {
		return false
	}
	candidateAbs, err := filepath.Abs(candidate)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(repoRoot), filepath.Clean(candidateAbs))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

func sameCleanPath(a, b string) bool {
	aAbs, err := filepath.Abs(a)
	if err != nil {
		return false
	}
	bAbs, err := filepath.Abs(b)
	if err != nil {
		return false
	}
	return filepath.Clean(aAbs) == filepath.Clean(bAbs)
}

// writeReceipt writes a deployment receipt into the consuming repo's
// .anito/deployed.json alongside config.yaml. Silently skips if ConfigPath
// is not set (e.g. services deployed via MCP without a config file).
func writeReceipt(svc *registry.Service) {
	if svc == nil || svc.ConfigPath == "" {
		return
	}
	e := receipt.Entry{
		Name:       svc.Name,
		StablePort: svc.StablePort,
		Address:    svc.Address(),
		BinaryPath: svc.BinaryPath,
		ConfigPath: svc.ConfigPath,
		Version:    svc.Version,
		DeployedAt: svc.LastDeployedAt,
	}
	// Multi-port: include all named ports and addresses.
	if len(svc.StablePorts) > 0 {
		e.StablePorts = svc.StablePorts
		e.Addresses = svc.Addresses()
	}
	_ = receipt.Write(e)
}

// startWatcher launches a file watcher for svc if it has WatchPaths configured.
func (s *Service) startWatcher(svc *registry.Service) {
	if len(svc.WatchPaths) == 0 {
		return
	}
	err := s.wtch.Start(svc.Name, svc.WatchPaths, func(trigger string) {
		if err := s.handleWatchTrigger(svc.Name, trigger); err != nil {
			log.Printf("[ERROR] name=%s watch trigger=%q failed: %v", svc.Name, trigger, err)
		}
	})
	if err != nil {
		log.Printf("[ERROR] name=%s could not start watcher: %v", svc.Name, err)
	}
}

func (s *Service) handleWatchTrigger(name string, trigger string) error {
	if err := ValidateServiceName(name); err != nil {
		return err
	}
	defer s.lockDeploy(name)()
	if err := s.runWatchBuild(name, trigger); err != nil {
		return err
	}
	return s.restartLocked(name)
}

func (s *Service) runWatchBuild(name string, trigger string) error {
	svc, ok := s.reg.Get(name)
	if !ok {
		return fmt.Errorf("service %q not found", name)
	}
	if svc.ConfigPath == "" {
		return nil
	}

	configPath, err := filepath.Abs(svc.ConfigPath)
	if err != nil {
		return fmt.Errorf("resolving config path for %q: %w", name, err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config for watch build %q: %w", name, err)
	}
	if cfg.Build == "" {
		return nil
	}

	logPath, err := s.buildLogFilePath(name)
	if err != nil {
		return err
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("opening build log for %q: %w", name, err)
	}
	defer logFile.Close()

	now := time.Now().Format(time.RFC3339)
	_, _ = fmt.Fprintf(logFile, "\n[%s] watch build started trigger=%q\n", now, trigger)
	cmd := shellcmd.Command(cfg.Build, shellcmd.BuildDir(configPath))
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Run(); err != nil {
		_, _ = fmt.Fprintf(logFile, "[%s] watch build failed: %v\n", time.Now().Format(time.RFC3339), err)
		return fmt.Errorf("watch build failed for %q: %w", name, err)
	}
	_, _ = fmt.Fprintf(logFile, "[%s] watch build completed\n", time.Now().Format(time.RFC3339))
	log.Printf("[WATCH_BUILD] name=%s trigger=%q", name, trigger)
	return nil
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
// Restart behaviour is governed by restart_policy:
//   - "never":    never restart automatically
//   - "on-watch": restart only if the service has WatchPaths configured (default)
//   - "always":   always restart regardless of WatchPaths
//
// Restarts use exponential backoff (1s→2s→4s→8s→30s). After all attempts are
// exhausted the service is left in the failed state and [CRASH_GIVE_UP] is logged.
func (s *Service) handleCrash(name string) {
	s.crashesTotal.Add(1)
	notify.SendWithSound("Anito", fmt.Sprintf("⚠ %s crashed", name))
	svc, ok := s.reg.Get(name)
	if !ok {
		return
	}
	if svc.Status == registry.StatusStopped {
		return // intentionally stopped — do not restart
	}

	// Unswap immediately so all stable ports return 503 instead of 502 from the
	// now-dead backend. A successful Restart() below will re-swap the proxy.
	s.prx.UnswapPorts(name)

	policy := svc.RestartPolicy
	if policy == "" {
		policy = "on-watch"
	}
	switch policy {
	case "never":
		return
	case "on-watch":
		if len(svc.WatchPaths) == 0 {
			return
		}
	case "always":
		// fall through — restart unconditionally
	}

	s.crashMu.Lock()
	attempt := s.crashAttempts[name]
	if attempt >= len(crashBackoffDurations) {
		s.crashMu.Unlock()
		log.Printf("[CRASH_GIVE_UP] name=%s attempts=%d", name, attempt)
		notify.SendWithSound("Anito", fmt.Sprintf("✕ %s gave up after %d crashes", name, attempt))
		_ = s.reg.UpdateCrashState(name, attempt, true)
		_ = s.reg.UpdateStatus(name, registry.StatusFailed, 0)
		if s.iss != nil {
			logContext := ""
			if logPath, err := s.logFilePath(name); err == nil {
				logContext = tailLog(logPath, 15)
			}
			_ = s.iss.Append(issues.Issue{
				Source:   "daemon:crash_give_up",
				Tool:     "crash_recovery",
				Error:    fmt.Sprintf("service %s gave up after %d crash attempts", name, attempt),
				Context:  logContext,
				Severity: "error",
			})
		}
		return
	}
	s.crashAttempts[name] = attempt + 1
	s.crashMu.Unlock()
	_ = s.reg.UpdateCrashState(name, attempt+1, false)

	wait := crashBackoffDurations[attempt]
	log.Printf("[RESTART] name=%s reason=crash attempt=%d waiting=%s", name, attempt+1, wait)
	time.Sleep(wait)

	if err := s.Restart(name); err != nil {
		log.Printf("[ERROR] name=%s crash restart failed: %v", name, err)
	}
}

// BuildLog runs the build command from the service's config_path and pipes
// output to ~/.anito/logs/<name>-build.log.
// Returns the build log path (always) and a non-nil error if the build failed.
func (s *Service) BuildLog(name string) (string, error) {
	svc, ok := s.reg.Get(name)
	if !ok {
		return "", domain.MissingServicef("service %q not found", name)
	}
	if svc.ConfigPath == "" {
		return "", domain.InvalidConfigf("service %q has no config_path — cannot run build", name)
	}

	cfg, err := config.Load(svc.ConfigPath)
	if err != nil {
		return "", domain.InvalidConfigf("loading config: %v", err)
	}
	if cfg.Build == "" {
		return "", domain.InvalidConfigf("service %q config has no build command", name)
	}

	buildLogPath := filepath.Join(s.logDir, name+"-build.log")
	logFile, err := os.Create(buildLogPath)
	if err != nil {
		return "", fmt.Errorf("creating build log: %w", err)
	}
	defer logFile.Close()

	cmd := shellcmd.Command(cfg.Build, shellcmd.BuildDir(svc.ConfigPath))
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if runErr := cmd.Run(); runErr != nil {
		return buildLogPath, fmt.Errorf("build failed: %w", runErr)
	}
	return buildLogPath, nil
}

// buildLogFilePath returns the path to the build log for a service.
// Returns an error if the service is not registered.
func (s *Service) buildLogFilePath(name string) (string, error) {
	if err := ValidateServiceName(name); err != nil {
		return "", err
	}
	if _, ok := s.reg.Get(name); !ok {
		return "", domain.MissingServicef("service %q not found", name)
	}
	return filepath.Join(s.logDir, name+"-build.log"), nil
}

// BuildLogs returns the last n lines from the service's build log file.
func (s *Service) BuildLogs(name string, n int) ([]string, error) {
	path, err := s.buildLogFilePath(name)
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

// BuildLogStream tails the service's build log file and sends new lines to the returned channel.
// The channel is closed when ctx is done.
func (s *Service) BuildLogStream(ctx context.Context, name string) (<-chan string, error) {
	path, err := s.buildLogFilePath(name)
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

// logFilePath resolves the log file path for name.
// The special name DaemonLogName maps to the Anito daemon log; all other names
// require a registered service entry.
func (s *Service) logFilePath(name string) (string, error) {
	if name == DaemonLogName {
		return filepath.Join(s.logDir, "anito.log"), nil
	}
	if err := ValidateServiceName(name); err != nil {
		return "", err
	}
	if _, ok := s.reg.Get(name); !ok {
		return "", domain.MissingServicef("service %q not found", name)
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

// Reserve claims stable port(s) for a named service without deploying it.
// It is used by multi-service setup (anito_coordinate) to guarantee port
// assignments before any binary exists. A subsequent Deploy() for the same
// service name will re-use the reserved port(s) — the assignment never changes.
//
// For backward compat, preferredPort allocates a single "default" port.
// For multi-port, use ReservePorts instead.
func (s *Service) Reserve(name string, preferredPort int) (int, error) {
	if err := s.ensureMutable(); err != nil {
		return 0, err
	}
	if err := ValidateServiceName(name); err != nil {
		return 0, err
	}
	ports, err := s.allocatePorts(name, map[string]int{"default": preferredPort}, registry.DefaultProxyBindAddress)
	if err != nil {
		return 0, err
	}
	port := ports["default"]
	if _, exists := s.reg.Get(name); !exists {
		svc := &registry.Service{
			Name:        name,
			StablePorts: ports,
			Type:        registry.TypeBinary,
			HealthCheck: "/health",
			Status:      registry.StatusStopped,
		}
		svc.NormalizePorts()
		_ = s.reg.Register(svc)
	}
	log.Printf("[RESERVE] name=%s port=%d", name, port)
	return port, nil
}

// ReservePorts claims multiple named stable ports for a service without deploying it.
func (s *Service) ReservePorts(name string, preferred map[string]int) (map[string]int, error) {
	if err := s.ensureMutable(); err != nil {
		return nil, err
	}
	if err := ValidateServiceName(name); err != nil {
		return nil, err
	}
	ports, err := s.allocatePorts(name, preferred, registry.DefaultProxyBindAddress)
	if err != nil {
		return nil, err
	}
	if _, exists := s.reg.Get(name); !exists {
		svc := &registry.Service{
			Name:        name,
			StablePorts: ports,
			Type:        registry.TypeBinary,
			HealthCheck: "/health",
			Status:      registry.StatusStopped,
		}
		svc.NormalizePorts()
		_ = s.reg.Register(svc)
	}
	log.Printf("[RESERVE] name=%s ports=%v", name, ports)
	return ports, nil
}

// allocatePorts returns the stable ports to use for the named service.
//
//   - Existing services keep their current ports (redeploy preserves stability).
//   - New services try preferred first; if unavailable, auto-allocate from range.
//   - On partial failure, all already-registered ports are rolled back.
func (s *Service) allocatePorts(name string, preferred map[string]int, bindAddress string) (map[string]int, error) {
	// Preserve ports for existing services.
	if existing, ok := s.reg.Get(name); ok && len(existing.StablePorts) > 0 {
		_ = s.prx.RegisterPortsWithBind(name, existing.StablePorts, existing.ProxyBindAddress) // idempotent
		return existing.StablePorts, nil
	}

	result := make(map[string]int, len(preferred))
	used := s.reg.UsedPorts()

	for portName, pref := range preferred {
		port, err := s.allocateOnePort(name, portName, pref, used, bindAddress)
		if err != nil {
			// Roll back: remove all ports registered in this call.
			s.prx.RemovePorts(name)
			return nil, err
		}
		result[portName] = port
		used[port] = true // prevent double-allocation within this call
	}

	return result, nil
}

// allocateOnePort allocates a single named port for a service.
func (s *Service) allocateOnePort(name, portName string, preferred int, used map[int]bool, bindAddress string) (int, error) {
	if preferred != 0 {
		if reservedPorts[preferred] {
			return 0, domain.Conflictf("port %d is reserved by Anito and cannot be assigned to a service", preferred)
		}
		if !used[preferred] {
			if err := s.prx.RegisterPortsWithBind(name, map[string]int{portName: preferred}, bindAddress); err == nil {
				return preferred, nil
			}
		}
		// Preferred port unavailable — fall through to auto-allocate.
	}

	for port := portRangeStart; port <= portRangeEnd; port++ {
		if used[port] {
			continue
		}
		if err := s.prx.RegisterPortsWithBind(name, map[string]int{portName: port}, bindAddress); err == nil {
			return port, nil
		}
	}
	return 0, domain.Conflictf("no available ports in range %d-%d for %s (port %s)", portRangeStart, portRangeEnd, name, portName)
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

// WaitHealthy polls the health check endpoint until it passes or the deadline is reached.
// Exported for use by main.go's service restore loop.
func WaitHealthy(internalPort int, path string, timeout time.Duration) error {
	return waitHealthy(internalPort, path, timeout)
}

// waitHealthy polls the health check endpoint until it returns 200 OK or the deadline is reached.
func waitHealthy(internalPort int, path string, timeout time.Duration) error {
	return waitHTTPReady(internalPort, path, timeout)
}

// waitHTTPReady polls path until it returns HTTP 200.
func waitHTTPReady(internalPort int, path string, timeout time.Duration) error {
	url := fmt.Sprintf("http://localhost:%d%s", internalPort, path)
	deadline := time.Now().Add(timeout)
	var lastStatus int
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		requestTimeout := time.Second
		if remaining < requestTimeout {
			requestTimeout = remaining
		}
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		var resp *http.Response
		var err error
		if reqErr == nil {
			resp, err = http.DefaultClient.Do(req)
		} else {
			err = reqErr
		}
		if err == nil {
			lastStatus = resp.StatusCode
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				cancel()
				return nil
			}
		}
		cancel()
		remaining = time.Until(deadline)
		if remaining <= 0 {
			break
		}
		wait := healthCheckInterval
		if remaining < wait {
			wait = remaining
		}
		time.Sleep(wait)
	}
	msg := fmt.Sprintf("health check timed out after %s: GET %s did not return 200", timeout, url)
	if lastStatus > 0 {
		msg += fmt.Sprintf(" (last status: %d)", lastStatus)
	}
	msg += "\nAnito requires your service to expose GET " + path + " → 200 OK and read PORT from the environment."
	return domain.ReadinessFailuref("%s", msg)
}

func (s *Service) Diagnose(req diagnosis.Request) (*diagnosis.Result, error) {
	return diagnosis.Run(req, s)
}

// CaseStudyRequest is the structured input for a consumer case study submission.
// Fields are deliberately scoped to exclude product names and proprietary details.
type CaseStudyRequest struct {
	PainPoint    string   // required — the problem before Anito
	Workflow     string   // required — how Anito is used day-to-day
	Outcome      string   // required — measurable or observable result
	StackContext string   // optional — vague technical context, e.g. "Go monorepo, 5 services"
	Quote        string   // optional — pull quote for marketing
	CreditAs     string   // optional — "a fintech team", "solo indie dev", or blank for anonymous
	FeaturesUsed []string // optional — Anito features that were key, e.g. ["hot-swap", "watch-mode"]
}

// SubmitCaseStudy writes a structured draft markdown file to
// <dataDir>/case-studies/ and returns the file path.
// Submissions are always written as drafts (draft: true in frontmatter).
// The developer reviews and publishes via Gomanan when ready.
func (s *Service) SubmitCaseStudy(req CaseStudyRequest) (string, error) {
	if req.PainPoint == "" || req.Workflow == "" || req.Outcome == "" {
		return "", fmt.Errorf("pain_point, workflow, and outcome are required")
	}

	dir := filepath.Join(filepath.Dir(s.logDir), "case-studies")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("case-studies: mkdir: %w", err)
	}

	date := time.Now().Format("2006-01-02")
	slug := caseStudySlug(req.PainPoint)
	filename := fmt.Sprintf("%s-%s.md", date, slug)
	path := filepath.Join(dir, filename)

	content := formatCaseStudy(req, date)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("case-studies: write: %w", err)
	}

	log.Printf("[CASE_STUDY] draft written path=%s", path)
	return path, nil
}

// caseStudySlug turns a string into a short URL-safe slug (max 50 chars).
func caseStudySlug(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			if b.Len() > 0 && b.String()[b.Len()-1] != '-' {
				b.WriteByte('-')
			}
		}
		if b.Len() >= 50 {
			break
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// formatCaseStudy renders a case study as draft frontmatter + markdown body.
func formatCaseStudy(req CaseStudyRequest, date string) string {
	var sb strings.Builder

	credit := req.CreditAs
	if credit == "" {
		credit = "anonymous"
	}

	features := ""
	if len(req.FeaturesUsed) > 0 {
		features = fmt.Sprintf("\nfeatures_used: [%s]", strings.Join(req.FeaturesUsed, ", "))
	}

	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("date: %s\n", date))
	sb.WriteString("draft: true\n")
	sb.WriteString("type: case-study\n")
	sb.WriteString("tags: [case-study]\n")
	sb.WriteString(fmt.Sprintf("credit_as: \"%s\"\n", credit))
	sb.WriteString(features)
	if features != "" {
		sb.WriteString("\n")
	}
	sb.WriteString("---\n\n")

	sb.WriteString("## The Problem\n\n")
	sb.WriteString(req.PainPoint)
	sb.WriteString("\n\n")

	sb.WriteString("## Workflow with Anito\n\n")
	sb.WriteString(req.Workflow)
	sb.WriteString("\n\n")

	sb.WriteString("## The Outcome\n\n")
	sb.WriteString(req.Outcome)
	sb.WriteString("\n")

	if req.StackContext != "" {
		sb.WriteString(fmt.Sprintf("\n*Stack: %s*\n", req.StackContext))
	}

	if req.Quote != "" {
		sb.WriteString(fmt.Sprintf("\n> \"%s\" — %s\n", req.Quote, credit))
	}

	return sb.String()
}

// tailLog returns the last n lines of the file at path as a single string.
// Returns an empty string if the file cannot be opened or is empty.
func tailLog(path string, n int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > n {
			lines = lines[1:]
		}
	}
	return strings.Join(lines, "\n")
}
