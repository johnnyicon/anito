package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"

	"github.com/johnnyicon/anito/internal/auth"
	"github.com/johnnyicon/anito/internal/doctor"
	"github.com/johnnyicon/anito/internal/issues"
	"github.com/johnnyicon/anito/internal/registry"
	"github.com/johnnyicon/anito/internal/service"
	"github.com/johnnyicon/anito/internal/sessions"
)

const (
	managementReadHeaderTimeout = 5 * time.Second
	managementReadTimeout       = 30 * time.Second
	managementIdleTimeout       = 60 * time.Second
)

//go:embed ui/dist
var distFiles embed.FS

// Server is the Anito HTTP management API (default port 7700).
type Server struct {
	svc     *service.Service
	iss     *issues.Store
	sess    *sessions.Store
	port    int
	version string

	capabilityToken string
}

func New(svc *service.Service, iss *issues.Store, sess *sessions.Store, port int, version string) *Server {
	return &Server{svc: svc, iss: iss, sess: sess, port: port, version: version}
}

func (s *Server) SetCapabilityToken(token string) {
	s.capabilityToken = token
}

func (s *Server) Start() error {
	if s.capabilityToken == "" {
		token, source, err := auth.LoadOrCreateToken()
		if err != nil {
			return fmt.Errorf("capability auth: %w", err)
		}
		s.capabilityToken = token
		log.Printf("[STARTUP] management API capability auth source=%s", source)
	}

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	configureManagementHTTPServer(e.Server)

	// Request logging middleware
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			err := next(c)
			log.Printf("[API] %s %s → %d (%s)",
				c.Request().Method,
				c.Request().URL.Path,
				c.Response().Status,
				time.Since(start).Round(time.Millisecond),
			)
			return err
		}
	})

	// Recover from panics
	e.Use(echomiddleware.Recover())
	e.Use(s.capabilityMiddleware())

	// API routes
	e.GET("/health", s.handleHealth)
	e.GET("/services", s.handleServices)
	e.POST("/deploy", s.handleDeploy)
	e.POST("/stop/:name", s.handleStop)
	e.POST("/restart/:name", s.handleRestart)
	e.POST("/rollback/:name", s.handleRollback)
	e.GET("/status/:name", s.handleStatus)
	e.POST("/remove/:name", s.handleRemove)
	e.GET("/logs/:name", s.handleLogs)
	e.POST("/issues", s.handlePostIssue)
	e.GET("/issues", s.handleGetIssues)
	e.DELETE("/issues", s.handleClearIssues)
	e.GET("/doctor", s.handleDoctor)
	e.GET("/metrics", s.handleMetrics)
	e.GET("/sessions", s.handleSessions)
	e.POST("/teardown", s.handleTeardown)

	// Serve embedded SPA — must be registered last (catch-all)
	sub, err := fs.Sub(distFiles, "ui/dist")
	if err != nil {
		return fmt.Errorf("embed sub: %w", err)
	}
	staticHandler := http.FileServer(http.FS(sub))
	e.GET("/*", func(c echo.Context) error {
		p := strings.TrimPrefix(c.Request().URL.Path, "/")
		f, err := sub.Open(p)
		if err == nil {
			f.Close()
			staticHandler.ServeHTTP(c.Response(), c.Request())
			return nil
		}
		// SPA fallback — serve index.html for client-side routing
		index, rerr := fs.ReadFile(sub, "index.html")
		if rerr != nil {
			return echo.NewHTTPError(http.StatusNotFound, "index.html not found")
		}
		return c.HTMLBlob(http.StatusOK, index)
	})

	addr := fmt.Sprintf("localhost:%d", s.port)
	log.Printf("[STARTUP] management API listening on %s", addr)
	return e.Start(addr)
}

func configureManagementHTTPServer(srv *http.Server) {
	srv.ReadHeaderTimeout = managementReadHeaderTimeout
	srv.ReadTimeout = managementReadTimeout
	srv.IdleTimeout = managementIdleTimeout
}

func (s *Server) capabilityMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if !requiresCapability(c.Request().Method, c.Request().URL.Path) {
				return next(c)
			}
			if auth.Authorized(c.Request(), s.capabilityToken) {
				return next(c)
			}
			c.Response().Header().Set("WWW-Authenticate", `Bearer realm="anito"`)
			return echo.NewHTTPError(http.StatusUnauthorized, "anito capability token required")
		}
	}
}

func requiresCapability(method, path string) bool {
	switch {
	case method == http.MethodPost && path == "/deploy":
		return true
	case method == http.MethodPost && strings.HasPrefix(path, "/stop/"):
		return true
	case method == http.MethodPost && strings.HasPrefix(path, "/restart/"):
		return true
	case method == http.MethodPost && strings.HasPrefix(path, "/rollback/"):
		return true
	case method == http.MethodPost && strings.HasPrefix(path, "/remove/"):
		return true
	case method == http.MethodPost && path == "/issues":
		return true
	case method == http.MethodDelete && path == "/issues":
		return true
	case method == http.MethodPost && path == "/teardown":
		return true
	default:
		return false
	}
}

// DeployRequest is the payload for POST /deploy.
type DeployRequest struct {
	Name               string               `json:"name"`
	Version            string               `json:"version,omitempty"`
	VersionPath        string               `json:"version_path,omitempty"`
	Type               registry.ServiceType `json:"type"`
	Path               string               `json:"path"`
	Args               []string             `json:"args,omitempty"`
	StablePort         int                  `json:"stable_port,omitempty"`
	StablePorts        map[string]int       `json:"stable_ports,omitempty"`
	ProxyBindAddress   string               `json:"proxy_bind_address,omitempty"`
	HealthCheckPort    string               `json:"health_check_port,omitempty"`
	EnvFile            string               `json:"env_file,omitempty"`
	HealthCheck        string               `json:"health_check,omitempty"`
	WatchPaths         []string             `json:"watch_paths,omitempty"`
	DrainWindow        string               `json:"drain_window,omitempty"`
	HealthCheckTimeout string               `json:"health_check_timeout,omitempty"`
	RestartPolicy      string               `json:"restart_policy,omitempty"`
	ConfigPath         string               `json:"config_path,omitempty"`
}

func (s *Server) handleHealth(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok", "version": s.version})
}

func (s *Server) handleServices(c echo.Context) error {
	return c.JSON(http.StatusOK, s.svc.Services())
}

func (s *Server) handleDeploy(c echo.Context) error {
	var req DeployRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if req.Name == "" || req.Path == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name and path are required")
	}

	var drainWindow time.Duration
	if req.DrainWindow != "" {
		d, err := time.ParseDuration(req.DrainWindow)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid drain_window %q: use a duration string like '3s' or '500ms'", req.DrainWindow))
		}
		drainWindow = d
	}
	var hcTimeout time.Duration
	if req.HealthCheckTimeout != "" {
		d, err := time.ParseDuration(req.HealthCheckTimeout)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid health_check_timeout %q: use a duration string like '30s'", req.HealthCheckTimeout))
		}
		hcTimeout = d
	}

	svc, err := s.svc.Deploy(service.DeployRequest{
		Name:               req.Name,
		Version:            req.Version,
		VersionPath:        req.VersionPath,
		Type:               req.Type,
		Path:               req.Path,
		Args:               req.Args,
		StablePort:         req.StablePort,
		StablePorts:        req.StablePorts,
		ProxyBindAddress:   req.ProxyBindAddress,
		HealthCheckPort:    req.HealthCheckPort,
		EnvFile:            req.EnvFile,
		HealthCheck:        req.HealthCheck,
		WatchPaths:         req.WatchPaths,
		DrainWindow:        drainWindow,
		HealthCheckTimeout: hcTimeout,
		RestartPolicy:      req.RestartPolicy,
		ConfigPath:         req.ConfigPath,
	})
	if err != nil {
		log.Printf("[ERROR] deploy name=%s error=%q", req.Name, err)
		if s.iss != nil {
			_ = s.iss.Append(issues.Issue{
				Source:   "cli:deploy",
				Tool:     "deploy",
				Input:    req.Name,
				Error:    err.Error(),
				Severity: "error",
			})
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, svc)
}

func (s *Server) handleStop(c echo.Context) error {
	name := c.Param("name")
	if err := s.svc.Stop(name); err != nil {
		log.Printf("[ERROR] stop name=%s error=%q", name, err)
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "stopped", "name": name})
}

func (s *Server) handleRestart(c echo.Context) error {
	name := c.Param("name")
	if err := s.svc.Restart(name); err != nil {
		log.Printf("[ERROR] restart name=%s error=%q", name, err)
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	svc, err := s.svc.Status(name)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, svc)
}

func (s *Server) handleRollback(c echo.Context) error {
	name := c.Param("name")
	svc, err := s.svc.Rollback(name)
	if err != nil {
		log.Printf("[ERROR] rollback name=%s error=%q", name, err)
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, svc)
}

func (s *Server) handleStatus(c echo.Context) error {
	name := c.Param("name")
	svc, err := s.svc.Status(name)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	}
	return c.JSON(http.StatusOK, svc)
}

func (s *Server) handleRemove(c echo.Context) error {
	name := c.Param("name")
	if err := s.svc.Remove(name); err != nil {
		log.Printf("[ERROR] remove name=%s error=%q", name, err)
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "removed", "name": name})
}

func (s *Server) handleLogs(c echo.Context) error {
	name := c.Param("name")
	if name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "service name required")
	}

	lines := 100
	if v := c.QueryParam("lines"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			lines = n
		}
	}

	streamType := c.QueryParam("stream") // "build" or ""
	follow := c.QueryParam("follow") == "true"

	if streamType == "build" {
		return s.streamBuildLogs(c, name)
	}

	if follow {
		return s.streamLogs(c, name, lines)
	}

	logLines, err := s.svc.Logs(name, lines)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	}
	return c.JSON(http.StatusOK, logLines)
}

func (s *Server) handleDoctor(c echo.Context) error {
	path := c.QueryParam("path")
	if path == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "path query parameter is required")
	}
	result, err := doctor.Check(path, s.svc)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return c.JSON(http.StatusOK, result)
}

func (s *Server) handleMetrics(c echo.Context) error {
	return c.JSON(http.StatusOK, s.svc.Metrics())
}

func (s *Server) handleSessions(c echo.Context) error {
	if s.sess == nil {
		return c.JSON(http.StatusOK, map[string]any{"sessions": []any{}, "count": 0})
	}
	list, err := s.sess.List()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"sessions": list, "count": len(list)})
}

func (s *Server) handleTeardown(c echo.Context) error {
	var req struct {
		RepoPath string `json:"repo_path"`
	}
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if req.RepoPath == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "repo_path is required")
	}
	removed, err := s.svc.Teardown(req.RepoPath)
	if err != nil {
		log.Printf("[ERROR] teardown repo=%s error=%q", req.RepoPath, err)
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	log.Printf("[TEARDOWN] repo=%s removed=%v", req.RepoPath, removed)
	return c.JSON(http.StatusOK, map[string]any{"removed": removed, "count": len(removed)})
}

func (s *Server) handlePostIssue(c echo.Context) error {
	var iss issues.Issue
	if err := json.NewDecoder(c.Request().Body).Decode(&iss); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if iss.Error == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "error field is required")
	}
	if iss.Source == "" {
		iss.Source = "external"
	}
	if err := s.iss.Append(iss); err != nil {
		log.Printf("[ERROR] issues append: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusCreated, map[string]string{"status": "logged"})
}

func (s *Server) handleGetIssues(c echo.Context) error {
	n := 50
	if v := c.QueryParam("lines"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			n = parsed
		}
	}
	src := c.QueryParam("source")
	list, err := s.iss.Recent(n, src)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	if list == nil {
		list = []issues.Issue{}
	}
	return c.JSON(http.StatusOK, map[string]any{"issues": list})
}

func (s *Server) handleClearIssues(c echo.Context) error {
	if err := s.iss.Clear(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"status": "cleared"})
}

// setSSEHeaders configures the response for Server-Sent Events streaming.
func setSSEHeaders(w *echo.Response) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) streamBuildLogs(c echo.Context, name string) error {
	w := c.Response()
	flusher, ok := w.Writer.(http.Flusher)
	if !ok {
		return echo.NewHTTPError(http.StatusInternalServerError, "streaming not supported")
	}

	setSSEHeaders(w)

	backlog, err := s.svc.BuildLogs(name, 500)
	if err != nil {
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
		flusher.Flush()
		return nil
	}
	for _, line := range backlog {
		fmt.Fprintf(w, "data: %s\n\n", line)
	}
	flusher.Flush()

	ch, err := s.svc.BuildLogStream(c.Request().Context(), name)
	if err != nil {
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
		flusher.Flush()
		return nil
	}
	for line := range ch {
		fmt.Fprintf(w, "data: %s\n\n", line)
		flusher.Flush()
	}
	return nil
}

func (s *Server) streamLogs(c echo.Context, name string, backlogLines int) error {
	w := c.Response()
	flusher, ok := w.Writer.(http.Flusher)
	if !ok {
		return echo.NewHTTPError(http.StatusInternalServerError, "streaming not supported")
	}

	setSSEHeaders(w)

	backlog, err := s.svc.Logs(name, backlogLines)
	if err != nil {
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
		flusher.Flush()
		return nil
	}
	for _, line := range backlog {
		fmt.Fprintf(w, "data: %s\n\n", line)
	}
	flusher.Flush()

	ch, err := s.svc.LogStream(c.Request().Context(), name)
	if err != nil {
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
		flusher.Flush()
		return nil
	}
	for line := range ch {
		fmt.Fprintf(w, "data: %s\n\n", line)
		flusher.Flush()
	}
	return nil
}
