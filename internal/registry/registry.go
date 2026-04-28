package registry

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ServiceType defines how Anito handles the service.
type ServiceType string

const (
	TypeBinary ServiceType = "binary" // self-contained binary; Anito proxies to it
	TypeStatic ServiceType = "static" // SPA / static files; Anito serves them directly
)

const DefaultProxyBindAddress = "localhost"

// ServiceStatus reflects the current runtime state.
type ServiceStatus string

const (
	StatusRunning  ServiceStatus = "running"
	StatusStopped  ServiceStatus = "stopped"
	StatusFailed   ServiceStatus = "failed"
	StatusOrphaned ServiceStatus = "orphaned" // binary path no longer exists on disk
)

// StartEvent records a single start attempt for a service.
type StartEvent struct {
	StartedAt time.Time     `json:"started_at"`
	ExitCode  int           `json:"exit_code"` // -1 if still running
	Duration  time.Duration `json:"duration"`  // 0 if still running
}

// Service is a registered service entry.
type Service struct {
	Name             string      `json:"name"`
	Type             ServiceType `json:"type"`
	Version          string      `json:"version,omitempty"`            // optional semantic version tag, e.g. "v1.2.3"
	ConfigPath       string      `json:"config_path,omitempty"`        // absolute path to the .anito/config.yaml that produced this deploy
	BinaryPath       string      `json:"binary_path"`                  // binary path (TypeBinary) or static dir (TypeStatic)
	Args             []string    `json:"args,omitempty"`               // optional arguments passed to the binary
	StablePort       int         `json:"stable_port"`                  // primary stable port (backward compat); see StablePorts for multi-port
	InternalPort     int         `json:"internal_port,omitempty"`      // primary ephemeral port (backward compat); see InternalPorts for multi-port
	ProxyBindAddress string      `json:"proxy_bind_address,omitempty"` // stable proxy listener address (default: localhost)
	EnvFile          string      `json:"env_file,omitempty"`
	HealthCheck      string      `json:"health_check"` // path, e.g. "/health"

	// Multi-port support: named ports for services that bind multiple ports.
	// Single-port services have one entry with key "default".
	// The singular StablePort/InternalPort fields are kept in sync for backward compat.
	StablePorts     map[string]int `json:"stable_ports,omitempty"`      // name → stable port
	InternalPorts   map[string]int `json:"internal_ports,omitempty"`    // name → ephemeral port
	HealthCheckPort string         `json:"health_check_port,omitempty"` // which named port to health-check (default: "default" or first key)

	WatchPaths         []string      `json:"watch_paths,omitempty"`          // directories to watch for file changes
	DrainWindow        time.Duration `json:"drain_window,omitempty"`         // grace period between proxy swap and SIGTERM to old process
	HealthCheckTimeout time.Duration `json:"health_check_timeout,omitempty"` // how long to wait for /health → 200 (0 = use default 15s)
	RestartPolicy      string        `json:"restart_policy,omitempty"`       // "always" | "on-watch" | "never" (default: "on-watch")
	Status             ServiceStatus `json:"status"`
	PID                int           `json:"pid,omitempty"`
	DeployedAt         time.Time     `json:"deployed_at"`
	UpdatedAt          time.Time     `json:"updated_at"`
	LastDeployedAt     time.Time     `json:"last_deployed_at,omitzero"`

	// Runtime observability fields (set by service layer, persisted to registry).
	LastStartedAt time.Time    `json:"last_started_at,omitzero"` // when the current (or last) process started
	CrashAttempts int          `json:"crash_attempts,omitempty"` // number of restart attempts in current crash loop
	GaveUp        bool         `json:"gave_up,omitempty"`        // true if crash backoff hit max attempts
	StartHistory  []StartEvent `json:"start_history,omitempty"`  // ring buffer, last 10 starts
}

// NormalizePorts ensures StablePorts/InternalPorts maps are populated from
// the singular fields (backward compat with old registry.json files).
// Also keeps the singular fields in sync with the maps.
func (s *Service) NormalizePorts() {
	// Upgrade: singular → map (loading old registry data)
	if len(s.StablePorts) == 0 && s.StablePort != 0 {
		s.StablePorts = map[string]int{"default": s.StablePort}
	}
	if len(s.InternalPorts) == 0 && s.InternalPort != 0 {
		s.InternalPorts = map[string]int{"default": s.InternalPort}
	}
	// Downgrade: map → singular (keep backward compat fields in sync)
	s.syncSingularFromMap()
	if s.ProxyBindAddress == "" {
		s.ProxyBindAddress = DefaultProxyBindAddress
	}
}

// syncSingularFromMap keeps StablePort/InternalPort in sync with the maps.
// Uses the primary port (HealthCheckPort, or "default", or first alphabetically).
func (s *Service) syncSingularFromMap() {
	s.StablePort = s.PrimaryStablePort()
	s.InternalPort = s.PrimaryInternalPort()
}

// PrimaryStablePort returns the stable port that should be used for health checks
// and backward-compat display. Prefers HealthCheckPort, then "default", then first key.
func (s *Service) PrimaryStablePort() int {
	return primaryPort(s.StablePorts, s.HealthCheckPort)
}

// PrimaryInternalPort returns the internal port for the health check port.
func (s *Service) PrimaryInternalPort() int {
	return primaryPort(s.InternalPorts, s.HealthCheckPort)
}

// IsMultiPort returns true if the service has more than one named port.
func (s *Service) IsMultiPort() bool {
	return len(s.StablePorts) > 1
}

func (s *Service) Address() string {
	return AddressFor(s.ProxyBindAddress, s.StablePort)
}

func (s *Service) Addresses() map[string]string {
	if len(s.StablePorts) == 0 {
		return nil
	}
	out := make(map[string]string, len(s.StablePorts))
	for name, port := range s.StablePorts {
		out[name] = AddressFor(s.ProxyBindAddress, port)
	}
	return out
}

func AddressFor(bindAddress string, port int) string {
	if bindAddress == "" {
		bindAddress = DefaultProxyBindAddress
	}
	return "http://" + net.JoinHostPort(bindAddress, fmt.Sprint(port))
}

func primaryPort(ports map[string]int, healthCheckPort string) int {
	if len(ports) == 0 {
		return 0
	}
	// Prefer the health check port
	if healthCheckPort != "" {
		if p, ok := ports[healthCheckPort]; ok {
			return p
		}
	}
	// Then "default"
	if p, ok := ports["default"]; ok {
		return p
	}
	// Then first key alphabetically (deterministic)
	var first string
	for k := range ports {
		if first == "" || k < first {
			first = k
		}
	}
	return ports[first]
}

// Registry manages the on-disk service registry.
type Registry struct {
	mu       sync.RWMutex
	path     string
	services map[string]*Service
}

type registryFile struct {
	Services map[string]*Service `json:"services"`
}

func New(dataDir string) (*Registry, error) {
	r := &Registry{
		path:     filepath.Join(dataDir, "registry.json"),
		services: make(map[string]*Service),
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}
	if err := r.load(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Registry) load() error {
	data, err := os.ReadFile(r.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var f registryFile
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	r.services = f.Services
	// Normalize all services so StablePorts/InternalPorts maps are populated
	// from old registry files that only had the singular fields.
	for _, svc := range r.services {
		svc.NormalizePorts()
	}
	return nil
}

func (r *Registry) save() error {
	// Keep singular fields in sync with maps before persisting.
	for _, svc := range r.services {
		svc.syncSingularFromMap()
	}
	f := registryFile{Services: r.services}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}

// Register adds or updates a service entry.
// On re-deploy, StablePorts (and the singular StablePort) are preserved from the existing entry.
func (r *Registry) Register(s *Service) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, exists := r.services[s.Name]; exists {
		// Preserve stable port assignments — they never change on redeploy.
		if len(existing.StablePorts) > 0 {
			s.StablePorts = existing.StablePorts
		}
		s.StablePort = existing.StablePort
		if s.ProxyBindAddress == "" {
			s.ProxyBindAddress = existing.ProxyBindAddress
		}
		s.DeployedAt = existing.DeployedAt
	} else {
		s.DeployedAt = time.Now()
	}
	s.NormalizePorts()
	s.UpdatedAt = time.Now()
	r.services[s.Name] = s
	return r.save()
}

// Get returns a service by name.
func (r *Registry) Get(name string) (*Service, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.services[name]
	return s, ok
}

// All returns all registered services.
func (r *Registry) All() []*Service {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Service, 0, len(r.services))
	for _, s := range r.services {
		out = append(out, s)
	}
	return out
}

// UpdateStatus updates the runtime status and PID of a service.
func (r *Registry) UpdateStatus(name string, status ServiceStatus, pid int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.services[name]
	if !ok {
		return fmt.Errorf("service %q not found", name)
	}
	s.Status = status
	s.PID = pid
	s.UpdatedAt = time.Now()
	return r.save()
}

// UpdateLastDeployed records the time of the most recent successful deploy.
func (r *Registry) UpdateLastDeployed(name string, t time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.services[name]
	if !ok {
		return fmt.Errorf("service %q not found", name)
	}
	s.LastDeployedAt = t
	s.UpdatedAt = time.Now()
	return r.save()
}

// UpdateInternalPort records the ephemeral port the process is running on.
//
// Deprecated: This method sets s.InternalPort directly, but save() calls
// syncSingularFromMap() which overwrites it from the InternalPorts map.
// The singular field is effectively derived — use UpdateInternalPorts instead.
// Kept for backward compatibility; will be removed before v1.0.
func (r *Registry) UpdateInternalPort(name string, port int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.services[name]
	if !ok {
		return fmt.Errorf("service %q not found", name)
	}
	s.InternalPort = port
	s.UpdatedAt = time.Now()
	return r.save()
}

// UpdateStartHistory records a new start event in the ring buffer (last 10 entries).
// Call with exitCode=-1, duration=0 when the process is starting.
// Call again with the actual exit code and duration when it exits.
func (r *Registry) UpdateStartHistory(name string, event StartEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.services[name]
	if !ok {
		return fmt.Errorf("service %q not found", name)
	}
	s.StartHistory = append(s.StartHistory, event)
	if len(s.StartHistory) > 10 {
		s.StartHistory = s.StartHistory[len(s.StartHistory)-10:]
	}
	s.UpdatedAt = time.Now()
	return r.save()
}

// UpdateLastStarted records when the current process started.
func (r *Registry) UpdateLastStarted(name string, t time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.services[name]
	if !ok {
		return fmt.Errorf("service %q not found", name)
	}
	s.LastStartedAt = t
	s.UpdatedAt = time.Now()
	return r.save()
}

// UpdateCrashState records the crash attempt counter and gave-up state.
func (r *Registry) UpdateCrashState(name string, attempts int, gaveUp bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.services[name]
	if !ok {
		return fmt.Errorf("service %q not found", name)
	}
	s.CrashAttempts = attempts
	s.GaveUp = gaveUp
	s.UpdatedAt = time.Now()
	return r.save()
}

// UsedPorts returns a set of all stable ports currently in use (across all named ports).
func (r *Registry) UsedPorts() map[int]bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ports := make(map[int]bool, len(r.services)*2)
	for _, svc := range r.services {
		for _, p := range svc.StablePorts {
			if p != 0 {
				ports[p] = true
			}
		}
		// Backward compat: also include the singular field in case StablePorts is empty.
		if svc.StablePort != 0 {
			ports[svc.StablePort] = true
		}
	}
	return ports
}

// UpdateInternalPorts records the ephemeral ports the process is running on.
func (r *Registry) UpdateInternalPorts(name string, ports map[string]int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.services[name]
	if !ok {
		return fmt.Errorf("service %q not found", name)
	}
	s.InternalPorts = ports
	s.syncSingularFromMap()
	s.UpdatedAt = time.Now()
	return r.save()
}

// Remove deletes a service from the registry.
func (r *Registry) Remove(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.services, name)
	return r.save()
}
