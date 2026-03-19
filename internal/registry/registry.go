package registry

import (
	"encoding/json"
	"fmt"
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

// ServiceStatus reflects the current runtime state.
type ServiceStatus string

const (
	StatusRunning ServiceStatus = "running"
	StatusStopped ServiceStatus = "stopped"
	StatusFailed  ServiceStatus = "failed"
)

// StartEvent records a single start attempt for a service.
type StartEvent struct {
	StartedAt time.Time     `json:"started_at"`
	ExitCode  int           `json:"exit_code"`  // -1 if still running
	Duration  time.Duration `json:"duration"`   // 0 if still running
}

// Service is a registered service entry.
type Service struct {
	Name         string        `json:"name"`
	Type         ServiceType   `json:"type"`
	Version      string        `json:"version,omitempty"`       // optional semantic version tag, e.g. "v1.2.3"
	ConfigPath   string        `json:"config_path,omitempty"`   // absolute path to the .anito/config.yaml that produced this deploy
	BinaryPath   string        `json:"binary_path"`             // binary path (TypeBinary) or static dir (TypeStatic)
	Args         []string      `json:"args,omitempty"`          // optional arguments passed to the binary
	StablePort   int           `json:"stable_port"`             // permanent port exposed to consumers via proxy
	InternalPort int           `json:"internal_port,omitempty"` // ephemeral port the process is actually on
	EnvFile      string        `json:"env_file,omitempty"`
	HealthCheck  string        `json:"health_check"` // path, e.g. "/health"
	WatchPaths          []string      `json:"watch_paths,omitempty"`           // directories to watch for file changes
	DrainWindow         time.Duration `json:"drain_window,omitempty"`          // grace period between proxy swap and SIGTERM to old process
	HealthCheckTimeout  time.Duration `json:"health_check_timeout,omitempty"`  // how long to wait for /health → 200 (0 = use default 15s)
	RestartPolicy       string        `json:"restart_policy,omitempty"`        // "always" | "on-watch" | "never" (default: "on-watch")
	Status              ServiceStatus `json:"status"`
	PID          int           `json:"pid,omitempty"`
	DeployedAt   time.Time     `json:"deployed_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
	LastDeployedAt time.Time   `json:"last_deployed_at,omitempty"`

	// Runtime observability fields (set by service layer, persisted to registry).
	LastStartedAt time.Time    `json:"last_started_at,omitempty"` // when the current (or last) process started
	CrashAttempts int          `json:"crash_attempts,omitempty"`  // number of restart attempts in current crash loop
	GaveUp        bool         `json:"gave_up,omitempty"`         // true if crash backoff hit max attempts
	StartHistory  []StartEvent `json:"start_history,omitempty"`   // ring buffer, last 10 starts
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
	return nil
}

func (r *Registry) save() error {
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
// On re-deploy, StablePort is preserved from the existing entry.
func (r *Registry) Register(s *Service) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, exists := r.services[s.Name]; exists {
		s.StablePort = existing.StablePort // never change the stable port
		s.DeployedAt = existing.DeployedAt
	} else {
		s.DeployedAt = time.Now()
	}
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

// UsedPorts returns a set of all stable ports currently in use.
func (r *Registry) UsedPorts() map[int]bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ports := make(map[int]bool, len(r.services))
	for _, svc := range r.services {
		if svc.StablePort != 0 {
			ports[svc.StablePort] = true
		}
	}
	return ports
}

// Remove deletes a service from the registry.
func (r *Registry) Remove(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.services, name)
	return r.save()
}
