package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/johnnyicon/anito/internal/registry"
	"github.com/johnnyicon/anito/internal/service"
)

// Server is the Anito HTTP management API (default port 7700).
// It is a thin wrapper around the shared service layer.
type Server struct {
	svc     *service.Service
	port    int
	version string // daemon version, embedded at build time
}

func New(svc *service.Service, port int, version string) *Server {
	return &Server{svc: svc, port: port, version: version}
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
	mux.HandleFunc("/logs/", s.handleLogs)

	addr := fmt.Sprintf("localhost:%d", s.port)
	log.Printf("[STARTUP] management API listening on %s", addr)
	return http.ListenAndServe(addr, requestLogger(mux))
}

// DeployRequest is the payload for POST /deploy.
// stable_port is optional — omit or set to 0 for auto-allocation.
type DeployRequest struct {
	Name        string               `json:"name"`
	Version     string               `json:"version,omitempty"`
	Type        registry.ServiceType `json:"type"`
	Path        string               `json:"path"`
	StablePort  int                  `json:"stable_port,omitempty"`
	EnvFile     string               `json:"env_file,omitempty"`
	HealthCheck string               `json:"health_check,omitempty"`
}

// requestLogger wraps a handler and logs every request with method, path,
// status code, and duration. Output goes to the daemon log file via stdout.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("[API] %s %s → %d (%s)", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
	})
}

// statusRecorder captures the HTTP status code written by a handler.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
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
	svc, err := s.svc.Deploy(service.DeployRequest{
		Name:        req.Name,
		Version:     req.Version,
		Type:        req.Type,
		Path:        req.Path,
		StablePort:  req.StablePort,
		EnvFile:     req.EnvFile,
		HealthCheck: req.HealthCheck,
	})
	if err != nil {
		log.Printf("[ERROR] deploy name=%s error=%q", req.Name, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, svc)
}

func (s *Server) handleServices(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.svc.Services())
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/stop/")
	if err := s.svc.Stop(name); err != nil {
		log.Printf("[ERROR] stop name=%s error=%q", name, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "stopped", "name": name})
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/restart/")
	if err := s.svc.Restart(name); err != nil {
		log.Printf("[ERROR] restart name=%s error=%q", name, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "restarted", "name": name})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/status/")
	svc, err := s.svc.Status(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, svc)
}

func (s *Server) handleRemove(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/remove/")
	if err := s.svc.Remove(name); err != nil {
		log.Printf("[ERROR] remove name=%s error=%q", name, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "removed", "name": name})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"status": "ok", "version": s.version})
}

// handleLogs serves GET /logs/:name
//
//   - ?lines=N    — return last N lines as JSON array (default 100)
//   - ?follow=true — stream new lines as SSE (also sends backlog first)
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/logs/")
	if name == "" {
		http.Error(w, "service name required", http.StatusBadRequest)
		return
	}

	lines := 100
	if v := r.URL.Query().Get("lines"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			lines = n
		}
	}

	if r.URL.Query().Get("follow") == "true" {
		s.streamLogs(w, r, name, lines)
		return
	}

	logLines, err := s.svc.Logs(name, lines)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, logLines)
}

// streamLogs sends the backlog then tails new lines as SSE.
func (s *Server) streamLogs(w http.ResponseWriter, r *http.Request, name string, backlogLines int) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	backlog, err := s.svc.Logs(name, backlogLines)
	if err != nil {
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
		flusher.Flush()
		return
	}
	for _, line := range backlog {
		fmt.Fprintf(w, "data: %s\n\n", line)
	}
	flusher.Flush()

	ch, err := s.svc.LogStream(r.Context(), name)
	if err != nil {
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
		flusher.Flush()
		return
	}
	for line := range ch {
		fmt.Fprintf(w, "data: %s\n\n", line)
		flusher.Flush()
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
