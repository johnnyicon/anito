package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/johnnyicon/anito/internal/process"
	"github.com/johnnyicon/anito/internal/proxy"
	"github.com/johnnyicon/anito/internal/registry"
)

const (
	healthCheckInterval = 200 * time.Millisecond
	healthCheckTimeout  = 15 * time.Second
)

// Server is the Anito management API (runs on port 7700).
type Server struct {
	reg   *registry.Registry
	mgr   *process.Manager
	prx   *proxy.Manager
	port  int
}

func New(reg *registry.Registry, mgr *process.Manager, prx *proxy.Manager, port int) *Server {
	return &Server{reg: reg, mgr: mgr, prx: prx, port: port}
}

func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/services", s.handleServices)
	mux.HandleFunc("/deploy", s.handleDeploy)
	mux.HandleFunc("/stop/", s.handleStop)
	mux.HandleFunc("/restart/", s.handleRestart)
	mux.HandleFunc("/status/", s.handleStatus)
	mux.HandleFunc("/remove/", s.handleRemove)

	addr := fmt.Sprintf("localhost:%d", s.port)
	fmt.Printf("anito daemon listening on %s\n", addr)
	return http.ListenAndServe(addr, mux)
}

// DeployRequest is the payload for POST /deploy.
type DeployRequest struct {
	Name        string               `json:"name"`
	Type        registry.ServiceType `json:"type"`
	Path        string               `json:"path"`         // binary path or static dir
	StablePort  int                  `json:"stable_port"`  // the port consumers connect to
	EnvFile     string               `json:"env_file,omitempty"`
	HealthCheck string               `json:"health_check"` // e.g. "/health"
}

func (s *Server) handleDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req DeployRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.Path == "" {
		http.Error(w, "name and path are required", http.StatusBadRequest)
		return
	}
	if req.StablePort == 0 {
		http.Error(w, "stable_port is required", http.StatusBadRequest)
		return
	}
	if req.Type == "" {
		req.Type = registry.TypeBinary
	}
	if req.HealthCheck == "" {
		req.HealthCheck = "/health"
	}

	// Ensure proxy listener is registered on the stable port (idempotent).
	if err := s.prx.Register(req.Name, req.StablePort); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	svc := &registry.Service{
		Name:        req.Name,
		Type:        req.Type,
		BinaryPath:  req.Path,
		StablePort:  req.StablePort,
		EnvFile:     req.EnvFile,
		HealthCheck: req.HealthCheck,
	}

	if err := s.reg.Register(svc); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Re-fetch so we have the persisted values (DeployedAt etc).
	svc, _ = s.reg.Get(req.Name)

	if req.Type == registry.TypeStatic {
		// Static services: just swap the file server — no process needed.
		if err := s.prx.SwapStatic(req.Name, req.Path); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = s.reg.UpdateStatus(req.Name, registry.StatusRunning, 0)
		svc, _ = s.reg.Get(req.Name)
		writeJSON(w, svc)
		return
	}

	// Binary service: start new process, health-check it, then swap proxy.
	oldPID := svc.PID

	internalPort, err := s.mgr.Start(svc)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := waitHealthy(internalPort, req.HealthCheck); err != nil {
		_ = s.mgr.Stop(req.Name)
		http.Error(w, fmt.Sprintf("health check failed: %v", err), http.StatusBadGateway)
		return
	}

	// Proxy now points at new process — old process can be drained.
	if err := s.prx.Swap(req.Name, internalPort); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if oldPID > 0 {
		go process.StopPID(oldPID)
	}

	svc, _ = s.reg.Get(req.Name)
	writeJSON(w, svc)
}

func (s *Server) handleServices(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.reg.All())
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/stop/")
	if err := s.mgr.Stop(name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "stopped", "name": name})
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/restart/")
	svc, ok := s.reg.Get(name)
	if !ok {
		http.Error(w, "service not found", http.StatusNotFound)
		return
	}

	internalPort, err := s.mgr.Restart(svc)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if svc.Type == registry.TypeBinary {
		if err := waitHealthy(internalPort, svc.HealthCheck); err != nil {
			http.Error(w, fmt.Sprintf("health check failed after restart: %v", err), http.StatusBadGateway)
			return
		}
		if err := s.prx.Swap(name, internalPort); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	writeJSON(w, map[string]string{"status": "restarted", "name": name})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/status/")
	svc, ok := s.reg.Get(name)
	if !ok {
		http.Error(w, "service not found", http.StatusNotFound)
		return
	}
	writeJSON(w, svc)
}

func (s *Server) handleRemove(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/remove/")
	_ = s.mgr.Stop(name)
	s.prx.Remove(name)
	if err := s.reg.Remove(name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "removed", "name": name})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"status": "ok"})
}

// waitHealthy polls the process's health endpoint until it responds 200 or times out.
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

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
