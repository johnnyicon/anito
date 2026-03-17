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

	"github.com/johnnyicon/anito/internal/registry"
	"github.com/johnnyicon/anito/internal/service"
)

//go:embed ui/dist
var distFiles embed.FS

// Server is the Anito HTTP management API (default port 7700).
type Server struct {
	svc     *service.Service
	port    int
	version string
}

func New(svc *service.Service, port int, version string) *Server {
	return &Server{svc: svc, port: port, version: version}
}

func (s *Server) Start() error {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

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

	// API routes
	e.GET("/health", s.handleHealth)
	e.GET("/services", s.handleServices)
	e.POST("/deploy", s.handleDeploy)
	e.POST("/stop/:name", s.handleStop)
	e.POST("/restart/:name", s.handleRestart)
	e.GET("/status/:name", s.handleStatus)
	e.POST("/remove/:name", s.handleRemove)
	e.GET("/logs/:name", s.handleLogs)

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

// DeployRequest is the payload for POST /deploy.
type DeployRequest struct {
	Name        string               `json:"name"`
	Version     string               `json:"version,omitempty"`
	Type        registry.ServiceType `json:"type"`
	Path        string               `json:"path"`
	Args        []string             `json:"args,omitempty"`
	StablePort  int                  `json:"stable_port,omitempty"`
	EnvFile     string               `json:"env_file,omitempty"`
	HealthCheck string               `json:"health_check,omitempty"`
	WatchPaths  []string             `json:"watch_paths,omitempty"`
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
	svc, err := s.svc.Deploy(service.DeployRequest{
		Name:        req.Name,
		Version:     req.Version,
		Type:        req.Type,
		Path:        req.Path,
		Args:        req.Args,
		StablePort:  req.StablePort,
		EnvFile:     req.EnvFile,
		HealthCheck: req.HealthCheck,
		WatchPaths:  req.WatchPaths,
	})
	if err != nil {
		log.Printf("[ERROR] deploy name=%s error=%q", req.Name, err)
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
	return c.JSON(http.StatusOK, map[string]string{"status": "restarted", "name": name})
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

	if c.QueryParam("follow") == "true" {
		return s.streamLogs(c, name, lines)
	}

	logLines, err := s.svc.Logs(name, lines)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	}
	return c.JSON(http.StatusOK, logLines)
}

func (s *Server) streamLogs(c echo.Context, name string, backlogLines int) error {
	w := c.Response()
	flusher, ok := w.Writer.(http.Flusher)
	if !ok {
		return echo.NewHTTPError(http.StatusInternalServerError, "streaming not supported")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

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
