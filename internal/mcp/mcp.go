// Package mcp exposes Anito's service layer as an MCP server.
// It runs as part of the daemon process and is reachable via
// StreamableHTTP at http://localhost:<mcp-port>.
//
// CLI and MCP are both thin wrappers around internal/service — no
// business logic lives here.
package mcp

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/johnnyicon/anito/internal/registry"
	"github.com/johnnyicon/anito/internal/service"
	"github.com/johnnyicon/anito/internal/setup"
)

// Server wraps the MCP SDK server and registers Anito tools.
type Server struct {
	svc  *service.Service
	port int
}

func New(svc *service.Service, port int) *Server {
	return &Server{svc: svc, port: port}
}

// Start begins serving the MCP StreamableHTTP endpoint. Blocks until error.
func (s *Server) Start() error {
	srv := sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    "anito",
		Version: "v1.0.0",
	}, nil)

	s.registerTools(srv)

	// Stateless mode: no session ID validation. Each request gets a fresh
	// temporary session. This means the server can be reloaded (anito reload)
	// without agents getting "session not found" errors on subsequent calls.
	// Our tools are all request/response — no server-initiated messages — so
	// stateless is correct here.
	handler := sdkmcp.NewStreamableHTTPHandler(func(r *http.Request) *sdkmcp.Server {
		return srv
	}, &sdkmcp.StreamableHTTPOptions{Stateless: true})

	addr := fmt.Sprintf("localhost:%d", s.port)
	log.Printf("[STARTUP] MCP server listening on http://%s", addr)
	return http.ListenAndServe(addr, handler)
}

// --- input/output types ---

type deployInput struct {
	Name        string        `json:"name"         jsonschema:"service name, must be unique"`
	Version     string        `json:"version"      jsonschema:"optional semver tag for this build, e.g. v1.2.3"`
	Path        string        `json:"path"         jsonschema:"absolute path to the binary or static directory"`
	Args        []string      `json:"args"         jsonschema:"optional arguments passed to the binary at startup, e.g. [\"serve\", \"--config\", \"prod.yaml\"]"`
	StablePort  int           `json:"stable_port"  jsonschema:"preferred stable port consumers connect to (0 = auto-allocate); ports 7700 and 7701 are reserved"`
	Type        string        `json:"type"         jsonschema:"service type: binary (default) or static"`
	EnvFile     string        `json:"env_file"     jsonschema:"optional path to a KEY=VALUE env file"`
	HealthCheck string        `json:"health_check" jsonschema:"health check path polled after start (default: /health)"`
	WatchPaths  []string      `json:"watch_paths"  jsonschema:"directories to watch for file changes; any change triggers an automatic restart"`
	DrainWindow time.Duration `json:"drain_window" jsonschema:"grace period between proxy swap and SIGTERM to the old process (e.g. 3000000000 for 3s); use this for SSE services to let in-flight connections finish"`
}

type serviceView struct {
	Name          string `json:"name"`
	Version       string `json:"version,omitempty"`
	Type          string `json:"type"`
	StablePort    int    `json:"stable_port"`
	PinnedAddress string `json:"pinned_address"` // permanent address — never changes on redeploy
	InternalPort  int    `json:"internal_port,omitempty"`
	Status        string `json:"status"`
	PID           int    `json:"pid,omitempty"`
	BinaryPath    string `json:"binary_path"`
}

type setupInput struct {
	Path string `json:"path" jsonschema:"absolute path to the repo root to inspect"`
}

type setupOutput struct {
	RepoPath        string        `json:"repo_path"`
	ServiceName     string        `json:"service_name"`
	Language        string        `json:"language"`
	HasAnitoConfig  bool          `json:"has_anito_config"`
	HasPORT         bool          `json:"has_port"`
	HasHealthRoute  bool          `json:"has_health_route"`
	Issues          []setupIssue  `json:"issues"`
	SuggestedConfig string        `json:"suggested_config"`
	Instructions    []string      `json:"instructions"`
}

type setupIssue struct {
	Severity string `json:"severity"`
	What     string `json:"what"`
	Fix      string `json:"fix"`
}

// --- coordinate types ---

type coordinateInput struct {
	RepoPath      string           `json:"repo_path"      jsonschema:"absolute path to the repository root"`
	Services      []coordinateSvc  `json:"services"       jsonschema:"services to coordinate — each gets a stable port assignment"`
	Relationships []coordinateRel  `json:"relationships"  jsonschema:"which services talk to each other; drives proxy config in Vite/Next"`
}

type coordinateSvc struct {
	Name          string `json:"name"           jsonschema:"service name — must be unique, becomes the Anito service identifier"`
	Path          string `json:"path"           jsonschema:"absolute path to this service's root directory"`
	PreferredPort int    `json:"preferred_port" jsonschema:"preferred stable port (0 or omit = auto-allocate from 8100-8200)"`
}

type coordinateRel struct {
	From      string `json:"from"       jsonschema:"service name that needs to know the other's address"`
	To        string `json:"to"         jsonschema:"service name being depended on"`
	ProxyPath string `json:"proxy_path" jsonschema:"HTTP path to proxy in Vite/Next dev server, e.g. /api (optional)"`
}

type coordinateFile struct {
	RelPath string `json:"rel_path"` // relative to repo root
	Content string `json:"content"`
}

type coordinatePatch struct {
	RelPath     string `json:"rel_path"`    // relative to repo root
	Marker      string `json:"marker"`      // block marker name
	Block       string `json:"block"`       // the [anito:managed] block to write
	Instruction string `json:"instruction"` // plain-language instruction for the LLM
}

type coordinateOutput struct {
	Allocations    map[string]int    `json:"allocations"`     // service name → stable port
	PortsEnvPath   string            `json:"ports_env_path"`  // always ".anito/ports.env"
	GeneratedFiles []coordinateFile  `json:"generated_files"` // files to write (ports.env, config.yaml, wrapper scripts)
	SourcePatches  []coordinatePatch `json:"source_patches"`  // [anito:managed] blocks to apply
	Instructions   []string          `json:"instructions"`    // ordered action list for the LLM
}

type reserveInput struct {
	Name          string `json:"name"           jsonschema:"service name"`
	PreferredPort int    `json:"preferred_port" jsonschema:"preferred stable port (0 = auto-allocate)"`
}

type reserveOutput struct {
	Name       string `json:"name"`
	StablePort int    `json:"stable_port"`
	Address    string `json:"address"`
}

// ---

type nameInput struct {
	Name string `json:"name" jsonschema:"service name"`
}

type logsInput struct {
	Name  string `json:"name"  jsonschema:"service name"`
	Lines int    `json:"lines" jsonschema:"number of recent lines to return (default: 100)"`
}

type servicesOutput struct {
	Services []serviceView `json:"services"`
}

type logsOutput struct {
	Name  string   `json:"name"`
	Lines []string `json:"lines"`
}

type opResult struct {
	Status string `json:"status"`
	Name   string `json:"name"`
}

// --- tool registration ---

func (s *Server) registerTools(srv *sdkmcp.Server) {
	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name: "anito_deploy",
		Description: "Deploy a service to Anito. Starts the binary on an ephemeral port, " +
			"polls /health until 200, then atomically swaps the reverse proxy. " +
			"Re-deploying an existing service is zero-downtime. " +
			"If stable_port is 0 or omitted, a port is auto-allocated from the range 8100-8200. " +
			"IMPORTANT: the stable_port returned is permanent and pinned to this service name. " +
			"It will never change on subsequent deploys. Record it — other services and agents " +
			"should connect to this service at localhost:<stable_port> going forward. " +
			"Ports 7700 (management API) and 7701 (MCP) are reserved and cannot be used.",
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest, in deployInput) (*sdkmcp.CallToolResult, serviceView, error) {
		log.Printf("[MCP] tool=anito_deploy name=%s path=%s port=%d", in.Name, in.Path, in.StablePort)
		svc, err := s.svc.Deploy(service.DeployRequest{
			Name:        in.Name,
			Type:        registry.ServiceType(in.Type),
			WatchPaths:  in.WatchPaths,
			Path:        in.Path,
			Args:        in.Args,
			StablePort:  in.StablePort,
			EnvFile:     in.EnvFile,
			HealthCheck: in.HealthCheck,
			DrainWindow: in.DrainWindow,
		})
		if err != nil {
			log.Printf("[MCP] tool=anito_deploy name=%s error=%q", in.Name, err)
			return nil, serviceView{}, err
		}
		return nil, toView(svc), nil
	})

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        "anito_services",
		Description: "List all services registered with Anito, including their stable ports and current status.",
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest, in struct{}) (*sdkmcp.CallToolResult, servicesOutput, error) {
		log.Printf("[MCP] tool=anito_services")
		svcs := s.svc.Services()
		out := servicesOutput{Services: make([]serviceView, len(svcs))}
		for i, svc := range svcs {
			out.Services[i] = toView(svc)
		}
		return nil, out, nil
	})

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        "anito_status",
		Description: "Get the current status, stable port, internal port, and PID of a specific service.",
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest, in nameInput) (*sdkmcp.CallToolResult, serviceView, error) {
		log.Printf("[MCP] tool=anito_status name=%s", in.Name)
		svc, err := s.svc.Status(in.Name)
		if err != nil {
			return nil, serviceView{}, err
		}
		return nil, toView(svc), nil
	})

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        "anito_logs",
		Description: "Return the last N lines from a service's log file. Use this to inspect recent output or diagnose failures.",
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest, in logsInput) (*sdkmcp.CallToolResult, logsOutput, error) {
		lines := in.Lines
		if lines <= 0 {
			lines = 100
		}
		log.Printf("[MCP] tool=anito_logs name=%s lines=%d", in.Name, lines)
		logLines, err := s.svc.Logs(in.Name, lines)
		if err != nil {
			return nil, logsOutput{}, err
		}
		return nil, logsOutput{Name: in.Name, Lines: logLines}, nil
	})

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        "anito_restart",
		Description: "Restart a service. Starts a new process, waits for the health check to pass, then swaps the proxy. The stable port stays live throughout.",
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest, in nameInput) (*sdkmcp.CallToolResult, opResult, error) {
		log.Printf("[MCP] tool=anito_restart name=%s", in.Name)
		if err := s.svc.Restart(in.Name); err != nil {
			log.Printf("[MCP] tool=anito_restart name=%s error=%q", in.Name, err)
			return nil, opResult{}, err
		}
		return nil, opResult{Status: "restarted", Name: in.Name}, nil
	})

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        "anito_stop",
		Description: "Stop a running service. The service stays registered; use anito_deploy or anito_restart to bring it back.",
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest, in nameInput) (*sdkmcp.CallToolResult, opResult, error) {
		log.Printf("[MCP] tool=anito_stop name=%s", in.Name)
		if err := s.svc.Stop(in.Name); err != nil {
			return nil, opResult{}, err
		}
		return nil, opResult{Status: "stopped", Name: in.Name}, nil
	})

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        "anito_remove",
		Description: "Stop a service and remove it from the Anito registry. The stable port is released and can be reused.",
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest, in nameInput) (*sdkmcp.CallToolResult, opResult, error) {
		log.Printf("[MCP] tool=anito_remove name=%s", in.Name)
		if err := s.svc.Remove(in.Name); err != nil {
			return nil, opResult{}, err
		}
		return nil, opResult{Status: "removed", Name: in.Name}, nil
	})

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name: "anito_reserve",
		Description: "Reserve a stable port for a service before its binary exists. " +
			"Use this when you know a service's name and want to guarantee its port " +
			"before building or deploying. The reserved port will never be auto-allocated " +
			"to another service. A subsequent anito_deploy for the same name will use the reserved port. " +
			"Returns the assigned stable port and permanent address.",
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest, in reserveInput) (*sdkmcp.CallToolResult, reserveOutput, error) {
		log.Printf("[MCP] tool=anito_reserve name=%s preferred=%d", in.Name, in.PreferredPort)
		port, err := s.svc.Reserve(in.Name, in.PreferredPort)
		if err != nil {
			log.Printf("[MCP] tool=anito_reserve name=%s error=%q", in.Name, err)
			return nil, reserveOutput{}, err
		}
		return nil, reserveOutput{
			Name:       in.Name,
			StablePort: port,
			Address:    fmt.Sprintf("http://localhost:%d", port),
		}, nil
	})

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name: "anito_coordinate",
		Description: "Set up port coordination for a composite app (multiple services that talk to each other). " +
			"Assigns stable ports to all services, generates .anito/ports.env (the shared address map), " +
			"per-service .anito/*.yaml config files, and [anito:managed] source patches for frameworks " +
			"that need them (Vite proxy config, Next.js rewrites, etc.). " +
			"The generated blocks are marked with [anito:managed] — tell developers not to edit them manually. " +
			"Call anito_reserve for each service after getting the allocations to lock the ports in the registry. " +
			"Then deploy with anito_deploy using the generated config files.",
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest, in coordinateInput) (*sdkmcp.CallToolResult, coordinateOutput, error) {
		log.Printf("[MCP] tool=anito_coordinate repo=%s services=%d", in.RepoPath, len(in.Services))

		// Convert input to setup.ServiceSpec
		specs := make([]setup.ServiceSpec, len(in.Services))
		for i, svc := range in.Services {
			specs[i] = setup.ServiceSpec{
				Name:          svc.Name,
				Path:          svc.Path,
				PreferredPort: svc.PreferredPort,
			}
		}
		rels := make([]setup.Relationship, len(in.Relationships))
		for i, rel := range in.Relationships {
			rels[i] = setup.Relationship{
				From:      rel.From,
				To:        rel.To,
				ProxyPath: rel.ProxyPath,
			}
		}

		result, err := setup.CoordinateApp(in.RepoPath, specs, rels, s.svc.UsedPorts())
		if err != nil {
			log.Printf("[MCP] tool=anito_coordinate repo=%s error=%q", in.RepoPath, err)
			return nil, coordinateOutput{}, err
		}

		out := coordinateOutput{
			Allocations:  map[string]int(result.Allocations),
			PortsEnvPath: result.PortsEnvPath,
		}
		for _, f := range result.GeneratedFiles {
			out.GeneratedFiles = append(out.GeneratedFiles, coordinateFile{RelPath: f.RelPath, Content: f.Content})
		}
		for _, p := range result.SourcePatches {
			out.SourcePatches = append(out.SourcePatches, coordinatePatch{
				RelPath:     p.RelPath,
				Marker:      p.Marker,
				Block:       p.Block,
				Instruction: p.Instruction,
			})
		}
		out.Instructions = result.Instructions
		return nil, out, nil
	})

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name: "anito_setup",
		Description: "Inspect a repo and return everything needed to make it Anito-compatible. " +
			"Detects the language, checks for PORT env var usage and a /health endpoint, " +
			"generates a suggested .anito/config.yaml, and returns step-by-step instructions. " +
			"Use this when onboarding a new repo.",
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest, in setupInput) (*sdkmcp.CallToolResult, setupOutput, error) {
		log.Printf("[MCP] tool=anito_setup path=%s", in.Path)
		result, err := setup.Inspect(in.Path)
		if err != nil {
			log.Printf("[MCP] tool=anito_setup path=%s error=%q", in.Path, err)
			return nil, setupOutput{}, err
		}
		issues := make([]setupIssue, len(result.Issues))
		for i, iss := range result.Issues {
			issues[i] = setupIssue{Severity: iss.Severity, What: iss.What, Fix: iss.Fix}
		}
		return nil, setupOutput{
			RepoPath:        result.RepoPath,
			ServiceName:     result.ServiceName,
			Language:        string(result.Language),
			HasAnitoConfig:  result.HasAnitoConfig,
			HasPORT:         result.HasPORT,
			HasHealthRoute:  result.HasHealthRoute,
			Issues:          issues,
			SuggestedConfig: result.SuggestedConfig,
			Instructions:    result.Instructions,
		}, nil
	})
}

func toView(svc *registry.Service) serviceView {
	return serviceView{
		Name:          svc.Name,
		Version:       svc.Version,
		Type:          string(svc.Type),
		StablePort:    svc.StablePort,
		PinnedAddress: fmt.Sprintf("http://localhost:%d", svc.StablePort),
		InternalPort:  svc.InternalPort,
		Status:        string(svc.Status),
		PID:           svc.PID,
		BinaryPath:    svc.BinaryPath,
	}
}
