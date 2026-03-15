package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ServiceType defines how Anito handles the service
type ServiceType string

const (
	TypeBinary ServiceType = "binary" // self-contained Go binary
	TypeStatic ServiceType = "static" // SPA / static files, wrapped by Anito
)

// ServiceStatus reflects the current runtime state
type ServiceStatus string

const (
	StatusRunning ServiceStatus = "running"
	StatusStopped ServiceStatus = "stopped"
	StatusFailed  ServiceStatus = "failed"
)

// Service is a registered service in the Anito registry
type Service struct {
	Name       string        `json:"name"`
	Type       ServiceType   `json:"type"`
	BinaryPath string        `json:"binary_path"`         // path to binary (TypeBinary) or static dir (TypeStatic)
	Port       int           `json:"port"`                // assigned by Anito
	EnvFile    string        `json:"env_file,omitempty"`  // optional .env file
	Status     ServiceStatus `json:"status"`
	PID        int           `json:"pid,omitempty"`
	DeployedAt time.Time     `json:"deployed_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
}

// Registry manages the on-disk service registry
type Registry struct {
	mu       sync.RWMutex
	path     string
	services map[string]*Service
	portMin  int
	portMax  int
}

type registryFile struct {
	Services map[string]*Service `json:"services"`
}

func New(dataDir string, portMin, portMax int) (*Registry, error) {
	r := &Registry{
		path:     filepath.Join(dataDir, "registry.json"),
		services: make(map[string]*Service),
		portMin:  portMin,
		portMax:  portMax,
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
		return nil // fresh start
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

// AllocatePort finds the next free port in the configured range
func (r *Registry) AllocatePort() (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	used := make(map[int]bool)
	for _, s := range r.services {
		used[s.Port] = true
	}
	for p := r.portMin; p <= r.portMax; p++ {
		if !used[p] {
			return p, nil
		}
	}
	return 0, fmt.Errorf("no available ports in range %d-%d", r.portMin, r.portMax)
}

// Register adds or updates a service entry
func (r *Registry) Register(s *Service) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing, exists := r.services[s.Name]
	if exists {
		s.Port = existing.Port // keep the same port on re-deploy
		s.DeployedAt = existing.DeployedAt
	} else {
		s.DeployedAt = time.Now()
	}
	s.UpdatedAt = time.Now()
	r.services[s.Name] = s
	return r.save()
}

// Get returns a service by name
func (r *Registry) Get(name string) (*Service, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.services[name]
	return s, ok
}

// All returns all registered services
func (r *Registry) All() []*Service {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Service, 0, len(r.services))
	for _, s := range r.services {
		out = append(out, s)
	}
	return out
}

// UpdateStatus updates the runtime status of a service
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

// Remove deletes a service from the registry
func (r *Registry) Remove(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.services, name)
	return r.save()
}
