package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/johnnyicon/anito/internal/process"
	"github.com/johnnyicon/anito/internal/registry"
)

type Server struct {
	reg  *registry.Registry
	mgr  *process.Manager
	port int
}

func New(reg *registry.Registry, mgr *process.Manager, port int) *Server {
	return &Server{reg: reg, mgr: mgr, port: port}
}

func (s *Server) Start() error {
	mux := http.NewServeMux()

	mux.HandleFunc("/services", s.handleServices)
	mux.HandleFunc("/deploy", s.handleDeploy)
	mux.HandleFunc("/stop/", s.handleStop)
	mux.HandleFunc("/restart/", s.handleRestart)
	mux.HandleFunc("/status/", s.handleStatus)
	mux.HandleFunc("/remove/", s.handleRemove)
	mux.HandleFunc("/health", s.handleHealth)

	addr := fmt.Sprintf("localhost:%d", s.port)
	fmt.Printf("anito listening on %s\n", addr)
	return http.ListenAndServe(addr, mux)
}

// DeployRequest is the payload for POST /deploy
type DeployRequest struct {
	Name       string              `json:"name"`
	Type       registry.ServiceType `json:"type"`        // "binary" | "static"
	Path       string              `json:"path"`         // binary path or static dir
	EnvFile    string              `json:"env_file,omitempty"`
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
	if req.Type == "" {
		req.Type = registry.TypeBinary
	}

	// Check if already registered (re-deploy keeps same port)
	svc, exists := s.reg.Get(req.Name)
	if !exists {
		port, err := s.reg.AllocatePort()
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		svc = &registry.Service{
			Name: req.Name,
			Port: port,
		}
	}

	svc.Type = req.Type
	svc.BinaryPath = req.Path
	svc.EnvFile = req.EnvFile

	if err := s.reg.Register(svc); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := s.mgr.Restart(svc); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

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
	if err := s.mgr.Restart(svc); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
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
	if err := s.reg.Remove(name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "removed", "name": name})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
