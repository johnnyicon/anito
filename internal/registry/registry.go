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

// Service is a registered service entry.
type Service struct {
	Name         string        `json:"name"`
	Type         ServiceType   `json:"type"`
	Version      string        `json:"version,omitempty"`       // optional semantic version tag, e.g. "v1.2.3"
	BinaryPath   string        `json:"binary_path"`             // binary path (TypeBinary) or static dir (TypeStatic)
	StablePort   int           `json:"stable_port"`             // permanent port exposed to consumers via proxy
	InternalPort int           `json:"internal_port,omitempty"` // ephemeral port the process is actually on
	EnvFile      string        `json:"env_file,omitempty"`
	HealthCheck  string        `json:"health_check"` // path, e.g. "/health"
	Status       ServiceStatus `json:"status"`
	PID          int           `json:"pid,omitempty"`
	DeployedAt   time.Time     `json:"deployed_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
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
	return os.WriteFile(r.path, data, 0644)
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
