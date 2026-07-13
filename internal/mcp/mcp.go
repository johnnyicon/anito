// Package mcp exposes Anito's service layer as an MCP server.
// It runs as part of the daemon process and is reachable via
// StreamableHTTP at http://localhost:<mcp-port>.
//
// CLI and MCP are both thin wrappers around internal/service — no
// business logic lives here.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/johnnyicon/anito/internal/doctor"
	"github.com/johnnyicon/anito/internal/issues"
	"github.com/johnnyicon/anito/internal/registry"
	"github.com/johnnyicon/anito/internal/service"
	"github.com/johnnyicon/anito/internal/sessions"
	"github.com/johnnyicon/anito/internal/setup"
)

// Server wraps the MCP SDK server and registers Anito tools.
type Server struct {
	svc  *service.Service
	iss  *issues.Store
	sess *sessions.Store
	port int
}

func New(svc *service.Service, iss *issues.Store, sess *sessions.Store, port int) *Server {
	return &Server{svc: svc, iss: iss, sess: sess, port: port}
}

// logErr auto-logs a tool error to the issue store. Called in each tool handler
// when err != nil. input is the typed input struct — marshalled to JSON inline.
func (s *Server) logErr(tool string, input any, err error) {
	if err == nil || s.iss == nil {
		return
	}
	inputJSON := ""
	if b, merr := json.Marshal(input); merr == nil {
		inputJSON = string(b)
	}
	_ = s.iss.Append(issues.Issue{
		Source:   "mcp:" + tool,
		Tool:     tool,
		Input:    inputJSON,
		Error:    err.Error(),
		Severity: "error",
	})
}

// Start begins serving the MCP StreamableHTTP endpoint. Blocks until error.
func (s *Server) Start() error {
	srv := sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    "anito",
		Version: "v1.0.0",
	}, nil)

	s.registerTools(srv)

	// Stateless mode: the SDK does not validate session IDs, so daemon
	// restarts are seamless — no "session not found" errors. The SDK still
	// assigns Mcp-Session-Id in initialize responses and clients echo it
	// back per the MCP spec; our sessionMiddleware captures those IDs for
	// observability without taking on the session-lifecycle risk of stateful
	// mode.
	mcpHandler := sdkmcp.NewStreamableHTTPHandler(func(r *http.Request) *sdkmcp.Server {
		return srv
	}, &sdkmcp.StreamableHTTPOptions{Stateless: true})

	handler := sessionMiddleware(s.sess, mcpHandler)

	addr := fmt.Sprintf("localhost:%d", s.port)
	log.Printf("[STARTUP] MCP server listening on http://%s", addr)
	return http.ListenAndServe(addr, handler)
}

// sessionMiddleware records session activity in the persistent store.
//   - Requests with Mcp-Session-Id: touch the session (update LastSeenAt,
//     increment call count, capture tool name from JSON-RPC body if present).
//   - Requests without: capture the new Mcp-Session-Id from the response
//     header after the SDK assigns it during initialize.
func sessionMiddleware(sess *sessions.Store, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := r.Header.Get("Mcp-Session-Id"); id != "" {
			tool := extractToolName(r)
			_ = sess.Touch(id, tool)
			next.ServeHTTP(w, r)
			return
		}
		// No session ID yet — let the SDK handle the request, then read the
		// new session ID it assigned from the response header.
		next.ServeHTTP(w, r)
		if id := w.Header().Get("Mcp-Session-Id"); id != "" {
			_ = sess.Create(id)
		}
	})
}

// extractToolName reads the JSON-RPC body to find the tools/call method's
// tool name. Returns "" if the body is not a tool call or cannot be parsed.
// The body is restored so the downstream handler can read it normally.
func extractToolName(r *http.Request) string {
	if r.Body == nil {
		return ""
	}
	body, err := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		return ""
	}
	var rpc struct {
		Method string `json:"method"`
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	if json.Unmarshal(body, &rpc) != nil || rpc.Method != "tools/call" {
		return ""
	}
	return rpc.Params.Name
}

// --- input/output types ---

type deployInput struct {
	Name               string         `json:"name"                  jsonschema:"service name, must be unique"`
	Version            string         `json:"version"               jsonschema:"optional semver tag for this build, e.g. v1.2.3"`
	VersionPath        string         `json:"version_path"          jsonschema:"optional absolute path to hash for the generated version when version is omitted; useful when path is a stable wrapper script"`
	Path               string         `json:"path"                  jsonschema:"absolute path to the binary or static directory"`
	Args               []string       `json:"args"                  jsonschema:"optional arguments passed to the binary at startup"`
	StablePort         int            `json:"stable_port"           jsonschema:"preferred stable port for single-port services (0 = auto-allocate). For multi-port services, use stable_ports instead."`
	StablePorts        map[string]int `json:"stable_ports"          jsonschema:"named ports for multi-port services, e.g. {ws: 7172, http: 7173}. Each port gets its own reverse proxy. The service receives PORT_<NAME> env vars. Mutually exclusive with stable_port for new services."`
	ProxyBindAddress   string         `json:"proxy_bind_address"    jsonschema:"stable proxy listener address. Default: localhost. Use a Tailscale IP for tailnet-only access."`
	HealthCheckPort    string         `json:"health_check_port"     jsonschema:"which named port to health-check (default: first port). Only relevant for multi-port services."`
	Type               string         `json:"type"                  jsonschema:"service type: binary (default) or static"`
	EnvFile            string         `json:"env_file"              jsonschema:"optional path to a KEY=VALUE env file"`
	HealthCheck        string         `json:"health_check"          jsonschema:"health check path polled after start (default: /health)"`
	WatchPaths         []string       `json:"watch_paths"           jsonschema:"directories to watch for file changes; any change triggers an automatic restart"`
	DrainWindow        string         `json:"drain_window"          jsonschema:"grace period between proxy swap and SIGTERM to the old process (e.g. '3s', '500ms'). Use this for SSE services to let in-flight connections finish."`
	HealthCheckTimeout string         `json:"health_check_timeout"  jsonschema:"how long to wait for /health to return 200 (e.g. '30s', '60s'). Default: '15s'. Increase for slow-starting services."`
	RestartPolicy      string         `json:"restart_policy"        jsonschema:"crash restart behavior: 'on-watch' (default, restart only if watch paths set), 'always' (always restart on crash), 'never' (never auto-restart)"`
	ConfigPath         string         `json:"config_path,omitempty" jsonschema:"absolute path to the .anito/config.yaml that defines this service. Doctor will flag services without a recorded config path."`
	ReplaceConfig      bool           `json:"replace_config"        jsonschema:"for redeploys only: replace optional configuration instead of preserving omitted fields from the registered service. Default false."`
}

type serviceView struct {
	Name               string                `json:"name"`
	Version            string                `json:"version,omitempty"`
	Type               string                `json:"type"`
	StablePort         int                   `json:"stable_port"`                // primary port (backward compat)
	PinnedAddress      string                `json:"pinned_address"`             // primary address (backward compat)
	StablePorts        map[string]int        `json:"stable_ports,omitempty"`     // all named ports
	PinnedAddresses    map[string]string     `json:"pinned_addresses,omitempty"` // all named addresses
	ProxyBindAddress   string                `json:"proxy_bind_address,omitempty"`
	InternalPort       int                   `json:"internal_port,omitempty"`     // primary internal port (backward compat)
	InternalPorts      map[string]int        `json:"internal_ports,omitempty"`    // all named internal ports
	HealthCheckPort    string                `json:"health_check_port,omitempty"` // which named port is health-checked
	HealthCheck        string                `json:"health_check,omitempty"`
	HealthCheckTimeout string                `json:"health_check_timeout,omitempty"`
	DrainWindow        string                `json:"drain_window,omitempty"`
	WatchPaths         []string              `json:"watch_paths,omitempty"`
	RestartPolicy      string                `json:"restart_policy,omitempty"`
	Status             string                `json:"status"`
	PID                int                   `json:"pid,omitempty"`
	CrashAttempts      int                   `json:"crash_attempts,omitempty"`
	GaveUp             bool                  `json:"gave_up,omitempty"`
	BinaryPath         string                `json:"binary_path"`
	Args               []string              `json:"args,omitempty"`
	EnvFile            string                `json:"env_file,omitempty"`
	ConfigPath         string                `json:"config_path,omitempty"`
	DeployedAt         time.Time             `json:"deployed_at,omitempty"`
	UpdatedAt          time.Time             `json:"updated_at,omitempty"`
	LastDeployedAt     time.Time             `json:"last_deployed_at,omitempty"`
	LastStartedAt      time.Time             `json:"last_started_at,omitempty"`
	StartHistory       []registry.StartEvent `json:"start_history,omitempty"`
}

// setupInput is the unified input for anito_setup.
// For a single-service repo: provide only Path.
// For a composite app: also provide Services and Relationships.
type setupInput struct {
	Path          string          `json:"path"          jsonschema:"absolute path to the repo root to inspect or coordinate"`
	Apply         bool            `json:"apply"         jsonschema:"if true, Anito writes generated files, safely applies existing managed source blocks, and reserves ports. Default false returns a dry-run plan."`
	Services      []coordinateSvc `json:"services"      jsonschema:"for composite apps only: list each service with its name, path, and optional preferred_port. Omit for single-service repos — anito_setup will inspect the repo at Path automatically."`
	Relationships []coordinateRel `json:"relationships" jsonschema:"for composite apps only: which services communicate with each other. Each entry drives proxy config generation (e.g. Vite proxy) and env var injection."`
}

// setupResult is the unified output from anito_setup.
// mode is either 'single' (one service inspected) or 'composite' (multi-service coordinated).
// In both modes, generated_files contains everything to write and instructions is the action list.
type setupResult struct {
	Mode     string `json:"mode"` // "single" | "composite"
	RepoPath string `json:"repo_path"`

	// Single-service inspection results (mode: single)
	ServiceName    string       `json:"service_name,omitempty"`
	Language       string       `json:"language,omitempty"`
	HasAnitoConfig bool         `json:"has_anito_config,omitempty"`
	HasPORT        bool         `json:"has_port,omitempty"`
	HasHealthRoute bool         `json:"has_health_route,omitempty"`
	Issues         []setupIssue `json:"issues,omitempty"`

	// Composite coordination results (mode: composite)
	Allocations map[string]int `json:"allocations,omitempty"` // service name → stable port

	// Shared in both modes: files to write + source patches + action list
	GeneratedFiles   []coordinateFile  `json:"generated_files,omitempty"`
	SourcePatches    []coordinatePatch `json:"source_patches,omitempty"`
	Instructions     []string          `json:"instructions"`
	Applied          bool              `json:"applied,omitempty"`
	AppliedFiles     []string          `json:"applied_files,omitempty"`
	AppliedPatches   []string          `json:"applied_patches,omitempty"`
	UnappliedPatches []string          `json:"unapplied_patches,omitempty"`
}

type setupIssue struct {
	Severity string `json:"severity"`
	What     string `json:"what"`
	Fix      string `json:"fix"`
}

// --- coordinate types ---

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
	Name           string         `json:"name"            jsonschema:"service name"`
	PreferredPort  int            `json:"preferred_port"  jsonschema:"preferred stable port for single-port services (0 = auto-allocate)"`
	PreferredPorts map[string]int `json:"preferred_ports" jsonschema:"named ports to reserve for multi-port services, e.g. {ws: 7172, http: 7173}. Use this instead of preferred_port for multi-port services."`
}

type reserveOutput struct {
	Name        string            `json:"name"`
	StablePort  int               `json:"stable_port"`            // primary port (backward compat)
	Address     string            `json:"address"`                // primary address (backward compat)
	StablePorts map[string]int    `json:"stable_ports,omitempty"` // all named ports
	Addresses   map[string]string `json:"addresses,omitempty"`    // all named addresses
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

type doctorInput struct {
	Path string `json:"path" jsonschema:"absolute path to the repo root containing .anito/"`
}

type doctorIssueView struct {
	Severity string `json:"severity"`
	Field    string `json:"field"`
	Message  string `json:"message"`
	Action   string `json:"action,omitempty"`
}

type doctorConfigResult struct {
	ConfigFile string            `json:"config_file"`
	Name       string            `json:"name,omitempty"`
	ParseError string            `json:"parse_error,omitempty"`
	Issues     []doctorIssueView `json:"issues,omitempty"`
	Errors     int               `json:"errors"`
	Warnings   int               `json:"warnings"`
}

type doctorResult struct {
	Configs  []doctorConfigResult `json:"configs"`
	Errors   int                  `json:"errors"`
	Warnings int                  `json:"warnings"`
	Healthy  bool                 `json:"healthy"`
}

type issuesQueryInput struct {
	Lines  int    `json:"lines"  jsonschema:"number of recent issues to return (default: 20)"`
	Source string `json:"source" jsonschema:"filter by source prefix, e.g. 'mcp:', 'cli:', 'consumer:'. Omit for all sources."`
}

type issueView struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"`
	Tool      string    `json:"tool,omitempty"`
	Input     string    `json:"input,omitempty"`
	Error     string    `json:"error"`
	Context   string    `json:"context,omitempty"`
	RepoPath  string    `json:"repo_path,omitempty"`
	Severity  string    `json:"severity"`
}

type issuesOutput struct {
	Issues []issueView `json:"issues"`
}

type reportInput struct {
	Error    string `json:"error"              jsonschema:"required — what went wrong"`
	Source   string `json:"source"             jsonschema:"who is reporting: use 'consumer:<your-service-name>' to identify the calling repo"`
	Tool     string `json:"tool,omitempty"     jsonschema:"which Anito tool or CLI command was being used when the error occurred"`
	Context  string `json:"context,omitempty"  jsonschema:"free-text context: what you were doing, what you observed, any relevant state"`
	RepoPath string `json:"repo_path,omitempty" jsonschema:"absolute path to the consuming repo root"`
	Severity string `json:"severity,omitempty" jsonschema:"'error' (default), 'warning', or 'info'"`
}

type reportOutput struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type caseStudyInput struct {
	PainPoint    string   `json:"pain_point"     jsonschema:"required — describe the problem you had before Anito, in terms of workflow friction or reliability issues. Do not name specific internal products or proprietary systems."`
	Workflow     string   `json:"workflow"       jsonschema:"required — describe how you use Anito day-to-day: deploy cycle, watch mode, MCP integration, etc. Be specific about the mechanics, vague about what the services are."`
	Outcome      string   `json:"outcome"        jsonschema:"required — what improved? Describe the observable or measurable result: faster iteration, fewer interruptions, more reliable local stack, etc."`
	StackContext string   `json:"stack_context"  jsonschema:"optional — brief vague technical context that conveys complexity without revealing specifics, e.g. 'Go monorepo with 5 cooperating daemons' or 'Node API + React SPA'. Do not name the actual services or products."`
	Quote        string   `json:"quote"          jsonschema:"optional — a short pull quote (1-2 sentences) suitable for marketing. First person. Should convey genuine value without naming internal systems."`
	CreditAs     string   `json:"credit_as"      jsonschema:"optional — how to attribute this case study publicly, e.g. 'a fintech team', 'a solo indie developer', or leave blank for anonymous."`
	FeaturesUsed []string `json:"features_used"  jsonschema:"optional — Anito features that were central to your workflow, e.g. ['hot-swap', 'watch-mode', 'mcp-integration', 'multi-port']."`
}

type caseStudyOutput struct {
	Status  string `json:"status"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

// --- tool registration ---

func (s *Server) registerTools(srv *sdkmcp.Server) {
	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name: "anito_deploy",
		Description: "Use for both the first deploy and every subsequent redeploy. " +
			"Deploy a service to Anito. Starts the binary on an ephemeral port, " +
			"polls /health until 200, then atomically swaps the reverse proxy. " +
			"Re-deploying an existing service is zero-downtime. " +
			"If stable_port is 0 or omitted, a port is auto-allocated from the range 8100-8200. " +
			"IMPORTANT: the stable_port returned is permanent and pinned to this service name. " +
			"On redeploy, omitted optional fields preserve their registered values; set replace_config=true only for a complete configuration replacement. " +
			"It will never change on subsequent deploys. Record it — other services and agents " +
			"should connect to this service at localhost:<stable_port> going forward. " +
			"Ports 7700 (management API) and 7701 (MCP) are reserved and cannot be used.",
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest, in deployInput) (*sdkmcp.CallToolResult, serviceView, error) {
		log.Printf("[MCP] tool=anito_deploy name=%s path=%s port=%d", in.Name, in.Path, in.StablePort)
		if existing, err := s.svc.Status(in.Name); err == nil {
			in = mergeDeployInput(in, existing)
		}

		var drainWindow time.Duration
		if in.DrainWindow != "" {
			d, err := time.ParseDuration(in.DrainWindow)
			if err != nil {
				return nil, serviceView{}, fmt.Errorf("invalid drain_window %q: use a duration string like '3s' or '500ms'", in.DrainWindow)
			}
			drainWindow = d
		}
		var hcTimeout time.Duration
		if in.HealthCheckTimeout != "" {
			d, err := time.ParseDuration(in.HealthCheckTimeout)
			if err != nil {
				return nil, serviceView{}, fmt.Errorf("invalid health_check_timeout %q: use a duration string like '30s'", in.HealthCheckTimeout)
			}
			hcTimeout = d
		}

		svc, err := s.svc.Deploy(service.DeployRequest{
			Name:               in.Name,
			Version:            in.Version,
			VersionPath:        in.VersionPath,
			Type:               registry.ServiceType(in.Type),
			WatchPaths:         in.WatchPaths,
			Path:               in.Path,
			Args:               in.Args,
			StablePort:         in.StablePort,
			StablePorts:        in.StablePorts,
			ProxyBindAddress:   in.ProxyBindAddress,
			HealthCheckPort:    in.HealthCheckPort,
			EnvFile:            in.EnvFile,
			HealthCheck:        in.HealthCheck,
			DrainWindow:        drainWindow,
			HealthCheckTimeout: hcTimeout,
			RestartPolicy:      in.RestartPolicy,
			ConfigPath:         in.ConfigPath,
		})
		if err != nil {
			log.Printf("[MCP] tool=anito_deploy name=%s error=%q", in.Name, err)
			s.logErr("anito_deploy", in, err)
			return nil, serviceView{}, err
		}
		return nil, toView(svc), nil
	})

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        "anito_services",
		Description: "List all services registered with Anito, including their stable ports and current status.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
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
		Description: "Return the last N lines from a service's log file. Use this to inspect recent output or diagnose failures. Pass name=\"~daemon\" to read Anito's own daemon log — useful for diagnosing crashes, deploys, and watch events across all services.",
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
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest, in nameInput) (*sdkmcp.CallToolResult, serviceView, error) {
		log.Printf("[MCP] tool=anito_restart name=%s", in.Name)
		if err := s.svc.Restart(in.Name); err != nil {
			log.Printf("[MCP] tool=anito_restart name=%s error=%q", in.Name, err)
			s.logErr("anito_restart", in, err)
			return nil, serviceView{}, err
		}
		svc, err := s.svc.Status(in.Name)
		if err != nil {
			return nil, serviceView{}, err
		}
		return nil, toView(svc), nil
	})

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        "anito_rollback",
		Description: "Restore the previous deployment for a service and restart it behind the same stable port. Use after a bad deploy when the last known good deployment should become live again.",
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest, in nameInput) (*sdkmcp.CallToolResult, serviceView, error) {
		log.Printf("[MCP] tool=anito_rollback name=%s", in.Name)
		svc, err := s.svc.Rollback(in.Name)
		if err != nil {
			log.Printf("[MCP] tool=anito_rollback name=%s error=%q", in.Name, err)
			s.logErr("anito_rollback", in, err)
			return nil, serviceView{}, err
		}
		return nil, toView(svc), nil
	})

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        "anito_stop",
		Description: "Stop a running service. The service stays registered; use anito_deploy or anito_restart to bring it back. The port assignment is preserved — the same port is reused on restart. Use this for temporary pauses.",
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest, in nameInput) (*sdkmcp.CallToolResult, opResult, error) {
		log.Printf("[MCP] tool=anito_stop name=%s", in.Name)
		if err := s.svc.Stop(in.Name); err != nil {
			return nil, opResult{}, err
		}
		return nil, opResult{Status: "stopped", Name: in.Name}, nil
	})

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        "anito_remove",
		Description: "Stop a service and remove it from the Anito registry. The stable port is released and can be reused. The stable port is released and may be reassigned. Use this to retire a service permanently.",
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
		// Multi-port reserve if PreferredPorts is provided.
		if len(in.PreferredPorts) > 0 {
			log.Printf("[MCP] tool=anito_reserve name=%s ports=%v", in.Name, in.PreferredPorts)
			ports, err := s.svc.ReservePorts(in.Name, in.PreferredPorts)
			if err != nil {
				log.Printf("[MCP] tool=anito_reserve name=%s error=%q", in.Name, err)
				s.logErr("anito_reserve", in, err)
				return nil, reserveOutput{}, err
			}
			out := reserveOutput{
				Name:        in.Name,
				StablePorts: ports,
				Addresses:   make(map[string]string, len(ports)),
			}
			for name, port := range ports {
				out.Addresses[name] = registry.AddressFor(registry.DefaultProxyBindAddress, port)
			}
			// Set primary port for backward compat.
			for _, p := range ports {
				out.StablePort = p
				out.Address = registry.AddressFor(registry.DefaultProxyBindAddress, p)
				break
			}
			return nil, out, nil
		}

		// Single-port reserve (backward compat).
		log.Printf("[MCP] tool=anito_reserve name=%s preferred=%d", in.Name, in.PreferredPort)
		port, err := s.svc.Reserve(in.Name, in.PreferredPort)
		if err != nil {
			log.Printf("[MCP] tool=anito_reserve name=%s error=%q", in.Name, err)
			s.logErr("anito_reserve", in, err)
			return nil, reserveOutput{}, err
		}
		return nil, reserveOutput{
			Name:       in.Name,
			StablePort: port,
			Address:    registry.AddressFor(registry.DefaultProxyBindAddress, port),
		}, nil
	})

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name: "anito_setup",
		Description: "Set up a repo for Anito — works for both single-service repos and composite apps. " +
			"SINGLE SERVICE: call with only path. Inspects the repo, checks the Anito service contract " +
			"(PORT env var, /health route), generates .anito/config.yaml, returns issues and instructions. " +
			"COMPOSITE APP (multiple services that talk to each other): also provide services[] and relationships[]. " +
			"Assigns stable ports to all services, generates .anito/ports.env (shared address map), " +
			"per-service config files, dev wrapper scripts, and [anito:managed] source patches for " +
			"frameworks that need them (Vite proxy config, Next.js rewrites, etc.). " +
			"Default is dry-run: generated_files contains every file to write and instructions is the action list. " +
			"If apply=true, Anito writes generated files, reserves ports in the registry, and safely replaces existing managed source blocks. " +
			"After setup, call anito_deploy to start services. " +
			"One-time scaffolding only. If .anito/config.yaml already exists, call anito_deploy instead.",
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest, in setupInput) (*sdkmcp.CallToolResult, setupResult, error) {
		mode := "single"
		if len(in.Services) > 0 {
			mode = "composite"
		}
		log.Printf("[MCP] tool=anito_setup mode=%s path=%s services=%d", mode, in.Path, len(in.Services))
		plan, err := setup.DryRun(toSetupPlanRequest(in), setupPorts{s: s})
		if err != nil {
			log.Printf("[MCP] tool=anito_setup mode=%s path=%s error=%q", mode, in.Path, err)
			return nil, setupResult{}, err
		}
		out := toSetupResult(plan, nil)
		if in.Apply {
			applied, err := s.applySetup(plan)
			if err != nil {
				log.Printf("[MCP] tool=anito_setup mode=%s path=%s apply error=%q", mode, in.Path, err)
				s.logErr("anito_setup", in, err)
				return nil, setupResult{}, err
			}
			out = toSetupResult(applied.Plan, applied)
		}
		return nil, out, nil
	})

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name: "anito_doctor",
		Description: "Validate a repo's .anito/config.yaml and check registry alignment. " +
			"Reports errors (blocking issues), warnings (hygiene problems), and info (advisory). " +
			"Checks: required fields, output file existence, watch path hygiene (asset files that " +
			"cause spurious restarts), drain_window sanity, and registry alignment (port mismatch, " +
			"failed status). Call this before deploying a new repo or after a config change.",
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest, in doctorInput) (*sdkmcp.CallToolResult, doctorResult, error) {
		log.Printf("[MCP] tool=anito_doctor path=%s", in.Path)
		result, err := doctor.Check(in.Path, s.svc)
		if err != nil {
			log.Printf("[MCP] tool=anito_doctor path=%s error=%q", in.Path, err)
			s.logErr("anito_doctor", in, err)
			return nil, doctorResult{}, err
		}
		out := doctorResult{
			Errors:   result.Errors,
			Warnings: result.Warnings,
			Healthy:  result.Healthy,
		}
		for _, cr := range result.Configs {
			dcr := doctorConfigResult{
				ConfigFile: cr.ConfigFile,
				Name:       cr.Name,
				ParseError: cr.ParseError,
				Errors:     cr.Errors,
				Warnings:   cr.Warnings,
			}
			for _, iss := range cr.Issues {
				dcr.Issues = append(dcr.Issues, doctorIssueView{
					Severity: iss.Severity,
					Field:    iss.Field,
					Message:  iss.Message,
					Action:   iss.Action,
				})
			}
			out.Configs = append(out.Configs, dcr)
		}
		return nil, out, nil
	})

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name: "anito_issues",
		Description: "Retrieve recent issues logged by Anito — tool errors, deploy failures, and manual reports from consuming repos. " +
			"Use this to investigate what went wrong after a failed deploy or restart. " +
			"Filter by source prefix: 'mcp:' for MCP tool errors, 'cli:' for CLI errors, 'consumer:' for reports from consuming repos.",
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest, in issuesQueryInput) (*sdkmcp.CallToolResult, issuesOutput, error) {
		log.Printf("[MCP] tool=anito_issues lines=%d source=%q", in.Lines, in.Source)
		n := in.Lines
		if n <= 0 {
			n = 20
		}
		list, err := s.iss.Recent(n, in.Source)
		if err != nil {
			return nil, issuesOutput{}, err
		}
		out := issuesOutput{Issues: make([]issueView, len(list))}
		for i, iss := range list {
			out.Issues[i] = issueView{
				ID:        iss.ID,
				Timestamp: iss.Timestamp,
				Source:    iss.Source,
				Tool:      iss.Tool,
				Input:     iss.Input,
				Error:     iss.Error,
				Context:   iss.Context,
				RepoPath:  iss.RepoPath,
				Severity:  iss.Severity,
			}
		}
		return nil, out, nil
	})

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name: "anito_teardown",
		Description: "Remove all services a repo has registered with Anito, then clear its deployed.json receipt. " +
			"Call this before deleting a worktree or when decommissioning a repo's services entirely. " +
			"Reads .anito/deployed.json in the repo to discover service names — safe to call even if the receipt is missing (no-op). " +
			"Returns the list of services that were removed.",
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest, in struct {
		RepoPath string `json:"repo_path" jsonschema:"required — absolute path to the repo root whose .anito/deployed.json should be used for cleanup"`
	}) (*sdkmcp.CallToolResult, map[string]any, error) {
		log.Printf("[MCP] tool=anito_teardown repo=%q", in.RepoPath)
		if in.RepoPath == "" {
			return nil, nil, fmt.Errorf("repo_path is required")
		}
		removed, err := s.svc.Teardown(in.RepoPath)
		if err != nil {
			s.logErr("anito_teardown", in, err)
			return nil, nil, err
		}
		if removed == nil {
			removed = []string{}
		}
		return nil, map[string]any{"removed": removed, "count": len(removed)}, nil
	})

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name: "anito_report",
		Description: "Report an issue to Anito from a consuming repo. Use this when you observe a problem related to an Anito tool or service — " +
			"failed deploys, unexpected restarts, port conflicts, or anything the consuming repo's agent has context about that Anito itself cannot see. " +
			"Set source to 'consumer:<your-service-name>' so the report is attributable. " +
			"Include context: what you were doing, what you observed, what the service was doing at the time.",
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest, in reportInput) (*sdkmcp.CallToolResult, reportOutput, error) {
		log.Printf("[MCP] tool=anito_report source=%q error=%q", in.Source, in.Error)
		if in.Error == "" {
			return nil, reportOutput{}, fmt.Errorf("error field is required")
		}
		source := in.Source
		if source == "" {
			source = "consumer:unknown"
		}
		sev := in.Severity
		if sev == "" {
			sev = "error"
		}
		iss := issues.Issue{
			Source:   source,
			Tool:     in.Tool,
			Error:    in.Error,
			Context:  in.Context,
			RepoPath: in.RepoPath,
			Severity: sev,
		}
		if err := s.iss.Append(iss); err != nil {
			return nil, reportOutput{}, err
		}
		// Re-read to get the generated ID
		recent, _ := s.iss.Recent(1, source)
		id := ""
		if len(recent) > 0 {
			id = recent[len(recent)-1].ID
		}
		return nil, reportOutput{ID: id, Status: "logged"}, nil
	})

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name: "anito_submit_case_study",
		Description: "Submit a case study or testimonial about using Anito. " +
			"Describe the workflow problem, how Anito solved it, and the outcome. " +
			"IMPORTANT: do not include product names, internal service names, company names, " +
			"or proprietary implementation details — describe workflows and outcomes in generic terms. " +
			"The maintainer reviews all submissions before publishing. " +
			"Use stack_context to convey technical complexity (e.g. 'Go monorepo, 5 services') without naming specific products.",
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest, in caseStudyInput) (*sdkmcp.CallToolResult, caseStudyOutput, error) {
		log.Printf("[MCP] tool=anito_submit_case_study credit_as=%q", in.CreditAs)
		path, err := s.svc.SubmitCaseStudy(service.CaseStudyRequest{
			PainPoint:    in.PainPoint,
			Workflow:     in.Workflow,
			Outcome:      in.Outcome,
			StackContext: in.StackContext,
			Quote:        in.Quote,
			CreditAs:     in.CreditAs,
			FeaturesUsed: in.FeaturesUsed,
		})
		if err != nil {
			s.logErr("anito_submit_case_study", in, err)
			return nil, caseStudyOutput{}, err
		}
		return nil, caseStudyOutput{
			Status:  "received",
			Path:    path,
			Message: "Draft saved. The maintainer will review and publish when ready.",
		}, nil
	})
}

func toSetupPlanRequest(in setupInput) setup.PlanRequest {
	req := setup.PlanRequest{
		Path:          in.Path,
		Services:      make([]setup.ServiceSpec, len(in.Services)),
		Relationships: make([]setup.Relationship, len(in.Relationships)),
	}
	for i, svc := range in.Services {
		req.Services[i] = setup.ServiceSpec{
			Name:          svc.Name,
			Path:          svc.Path,
			PreferredPort: svc.PreferredPort,
		}
	}
	for i, rel := range in.Relationships {
		req.Relationships[i] = setup.Relationship{
			From:      rel.From,
			To:        rel.To,
			ProxyPath: rel.ProxyPath,
		}
	}
	return req
}

func toSetupResult(plan *setup.Plan, applied *setup.ApplyResult) setupResult {
	if plan == nil {
		return setupResult{}
	}
	out := setupResult{
		Mode:           string(plan.Mode),
		RepoPath:       plan.RepoPath,
		ServiceName:    plan.ServiceName,
		Language:       string(plan.Language),
		HasAnitoConfig: plan.HasAnitoConfig,
		HasPORT:        plan.HasPORT,
		HasHealthRoute: plan.HasHealthRoute,
		Instructions:   append([]string(nil), plan.Instructions...),
	}
	if len(plan.Issues) > 0 {
		out.Issues = make([]setupIssue, len(plan.Issues))
		for i, iss := range plan.Issues {
			out.Issues[i] = setupIssue{Severity: iss.Severity, What: iss.What, Fix: iss.Fix}
		}
	}
	if len(plan.Allocations) > 0 {
		out.Allocations = make(map[string]int, len(plan.Allocations))
		for name, port := range plan.Allocations {
			out.Allocations[name] = port
		}
	}
	for _, f := range plan.GeneratedFiles {
		out.GeneratedFiles = append(out.GeneratedFiles, coordinateFile{RelPath: f.RelPath, Content: f.Content})
	}
	for _, p := range plan.SourcePatches {
		out.SourcePatches = append(out.SourcePatches, coordinatePatch{
			RelPath: p.RelPath, Marker: p.Marker, Block: p.Block, Instruction: p.Instruction,
		})
	}
	if applied != nil {
		out.Applied = applied.Applied
		out.AppliedFiles = append([]string(nil), applied.AppliedFiles...)
		out.AppliedPatches = append([]string(nil), applied.AppliedPatches...)
		out.UnappliedPatches = append([]string(nil), applied.UnappliedPatches...)
	}
	return out
}

func toView(svc *registry.Service) serviceView {
	v := serviceView{
		Name:               svc.Name,
		Version:            svc.Version,
		Type:               string(svc.Type),
		StablePort:         svc.StablePort,
		PinnedAddress:      svc.Address(),
		ProxyBindAddress:   svc.ProxyBindAddress,
		InternalPort:       svc.InternalPort,
		HealthCheckPort:    svc.HealthCheckPort,
		HealthCheck:        svc.HealthCheck,
		HealthCheckTimeout: durationString(svc.HealthCheckTimeout),
		DrainWindow:        durationString(svc.DrainWindow),
		WatchPaths:         svc.WatchPaths,
		RestartPolicy:      svc.RestartPolicy,
		Status:             string(svc.Status),
		PID:                svc.PID,
		CrashAttempts:      svc.CrashAttempts,
		GaveUp:             svc.GaveUp,
		BinaryPath:         svc.BinaryPath,
		Args:               svc.Args,
		EnvFile:            svc.EnvFile,
		ConfigPath:         svc.ConfigPath,
		DeployedAt:         svc.DeployedAt,
		UpdatedAt:          svc.UpdatedAt,
		LastDeployedAt:     svc.LastDeployedAt,
		LastStartedAt:      svc.LastStartedAt,
		StartHistory:       svc.StartHistory,
	}
	// Multi-port: include all named ports and addresses.
	if len(svc.StablePorts) > 0 {
		v.StablePorts = svc.StablePorts
		v.PinnedAddresses = make(map[string]string, len(svc.StablePorts))
		for name, port := range svc.StablePorts {
			v.PinnedAddresses[name] = registry.AddressFor(svc.ProxyBindAddress, port)
		}
	}
	if len(svc.InternalPorts) > 0 {
		v.InternalPorts = svc.InternalPorts
	}
	return v
}

func durationString(value time.Duration) string {
	if value == 0 {
		return ""
	}
	return value.String()
}

func mergeDeployInput(in deployInput, existing *registry.Service) deployInput {
	if existing == nil || in.ReplaceConfig {
		return in
	}
	if in.Type == "" {
		in.Type = string(existing.Type)
	}
	if in.Args == nil {
		in.Args = append([]string(nil), existing.Args...)
	}
	if in.ProxyBindAddress == "" {
		in.ProxyBindAddress = existing.ProxyBindAddress
	}
	if in.HealthCheckPort == "" {
		in.HealthCheckPort = existing.HealthCheckPort
	}
	if in.EnvFile == "" {
		in.EnvFile = existing.EnvFile
	}
	if in.HealthCheck == "" {
		in.HealthCheck = existing.HealthCheck
	}
	if in.WatchPaths == nil {
		in.WatchPaths = append([]string(nil), existing.WatchPaths...)
	}
	if in.DrainWindow == "" {
		in.DrainWindow = durationString(existing.DrainWindow)
	}
	if in.HealthCheckTimeout == "" {
		in.HealthCheckTimeout = durationString(existing.HealthCheckTimeout)
	}
	if in.RestartPolicy == "" {
		in.RestartPolicy = existing.RestartPolicy
	}
	if in.ConfigPath == "" {
		in.ConfigPath = existing.ConfigPath
	}
	return in
}
