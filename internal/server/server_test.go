package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/johnnyicon/anito/internal/domain"
	"github.com/johnnyicon/anito/internal/issues"
	"github.com/johnnyicon/anito/internal/process"
	"github.com/johnnyicon/anito/internal/proxy"
	"github.com/johnnyicon/anito/internal/registry"
	"github.com/johnnyicon/anito/internal/service"
	"github.com/johnnyicon/anito/internal/watcher"
)

// flushRecorder wraps httptest.ResponseRecorder and implements http.Flusher,
// which is required by the SSE streaming handlers.
type flushRecorder struct {
	*httptest.ResponseRecorder
}

func (f *flushRecorder) Flush() { f.ResponseRecorder.Flush() }

// newFlushContext creates an Echo context backed by a flush-capable recorder.
// Use this for tests that exercise streamBuildLogs / streamLogs.
func (h *testHarness) newFlushContext(method, path string) (echo.Context, *flushRecorder) {
	req := httptest.NewRequest(method, path, nil)
	fr := &flushRecorder{httptest.NewRecorder()}
	c := h.echo.NewContext(req, fr)
	return c, fr
}

// nonFlusherRW wraps an http.ResponseWriter without promoting http.Flusher.
// This lets us hit the "streaming not supported" 500 branch in SSE handlers
// when the underlying writer does not support flushing.
type nonFlusherRW struct {
	rec *httptest.ResponseRecorder
}

func (n *nonFlusherRW) Header() http.Header         { return n.rec.Header() }
func (n *nonFlusherRW) Write(b []byte) (int, error) { return n.rec.Write(b) }
func (n *nonFlusherRW) WriteHeader(code int)        { n.rec.WriteHeader(code) }

// newNonFlushContext creates an Echo context backed by a ResponseWriter that
// does NOT implement http.Flusher, triggering the "streaming not supported" path.
func (h *testHarness) newNonFlushContext(method, path string) (echo.Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	nf := &nonFlusherRW{rec: rec}
	c := h.echo.NewContext(req, nf)
	return c, rec
}

// testHarness bundles the temp dirs and components needed by every test.
type testHarness struct {
	tmpDir string
	srv    *Server
	echo   *echo.Echo
}

// newTestHarness creates a fully wired Server backed by real packages
// writing to temp dirs. Caller should defer h.cleanup().
func newTestHarness(t *testing.T) *testHarness {
	t.Helper()

	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "data")
	logDir := filepath.Join(tmp, "logs")
	issDir := filepath.Join(tmp, "issues")

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(issDir, 0755); err != nil {
		t.Fatal(err)
	}

	reg, err := registry.New(dataDir)
	if err != nil {
		t.Fatal(err)
	}

	mgr, err := process.New(logDir, reg)
	if err != nil {
		t.Fatal(err)
	}

	prx := proxy.NewManager()
	wtch := watcher.New()

	svc := service.New(reg, mgr, prx, logDir, wtch, nil)
	iss := issues.New(issDir)

	s := New(svc, iss, nil, 0, "test-v0.0.1")

	e := echo.New()
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
	e.GET("/doctor", s.handleDoctor)
	e.GET("/diagnose", s.handleDiagnose)
	e.GET("/metrics", s.handleMetrics)
	e.POST("/teardown", s.handleTeardown)

	return &testHarness{tmpDir: tmp, srv: s, echo: e}
}

// request builds an Echo context from an httptest recorder and returns both.
func (h *testHarness) request(method, path string, body string) (echo.Context, *httptest.ResponseRecorder) {
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	rec := httptest.NewRecorder()
	c := h.echo.NewContext(req, rec)
	return c, rec
}

// --- handleHealth ---

func TestHandleHealth(t *testing.T) {
	h := newTestHarness(t)
	c, rec := h.request(http.MethodGet, "/health", "")

	if err := h.srv.handleHealth(c); err != nil {
		t.Fatalf("handleHealth returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", resp["status"])
	}
	if resp["version"] != "test-v0.0.1" {
		t.Errorf("expected version=test-v0.0.1, got %v", resp["version"])
	}
	if _, ok := resp["startup"].(map[string]any); !ok {
		t.Fatalf("expected startup object, got %T", resp["startup"])
	}
}

func TestConfigureManagementHTTPServerSetsTimeouts(t *testing.T) {
	srv := &http.Server{}
	configureManagementHTTPServer(srv)

	if srv.ReadHeaderTimeout != managementReadHeaderTimeout {
		t.Fatalf("ReadHeaderTimeout = %s, want %s", srv.ReadHeaderTimeout, managementReadHeaderTimeout)
	}
	if srv.ReadTimeout != managementReadTimeout {
		t.Fatalf("ReadTimeout = %s, want %s", srv.ReadTimeout, managementReadTimeout)
	}
	if srv.IdleTimeout != managementIdleTimeout {
		t.Fatalf("IdleTimeout = %s, want %s", srv.IdleTimeout, managementIdleTimeout)
	}
	if srv.WriteTimeout != 0 {
		t.Fatalf("WriteTimeout = %s, want unset for SSE compatibility", srv.WriteTimeout)
	}
}

// --- handleServices ---

func TestHandleServicesEmpty(t *testing.T) {
	h := newTestHarness(t)
	c, rec := h.request(http.MethodGet, "/services", "")

	if err := h.srv.handleServices(c); err != nil {
		t.Fatalf("handleServices returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp []*registry.Service
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp) != 0 {
		t.Errorf("expected 0 services, got %d", len(resp))
	}
}

// --- handleDeploy ---

func TestHandleDeployMissingNameAndPath(t *testing.T) {
	h := newTestHarness(t)
	c, _ := h.request(http.MethodPost, "/deploy", `{}`)

	err := h.srv.handleDeploy(c)
	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected echo.HTTPError, got %T (%v)", err, err)
	}
	if he.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", he.Code)
	}
}

func TestHandleDeployMissingPath(t *testing.T) {
	h := newTestHarness(t)
	c, _ := h.request(http.MethodPost, "/deploy", `{"name":"svc"}`)

	err := h.srv.handleDeploy(c)
	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected echo.HTTPError, got %T (%v)", err, err)
	}
	if he.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", he.Code)
	}
}

func TestHandleDeployInvalidDrainWindow(t *testing.T) {
	h := newTestHarness(t)
	c, _ := h.request(http.MethodPost, "/deploy", `{"name":"svc","path":"/bin/true","drain_window":"nope"}`)

	err := h.srv.handleDeploy(c)
	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected echo.HTTPError, got %T (%v)", err, err)
	}
	if he.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", he.Code)
	}
	wire, ok := he.Message.(domain.WireError)
	if !ok {
		t.Fatalf("expected domain wire error, got %T", he.Message)
	}
	if wire.Code != domain.CodeInvalidConfig || !strings.Contains(wire.Error, "drain_window") {
		t.Errorf("error should be invalid_config drain_window, got %+v", wire)
	}
}

func TestHandleDeployInvalidHealthCheckTimeout(t *testing.T) {
	h := newTestHarness(t)
	c, _ := h.request(http.MethodPost, "/deploy", `{"name":"svc","path":"/bin/true","health_check_timeout":"bad"}`)

	err := h.srv.handleDeploy(c)
	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected echo.HTTPError, got %T (%v)", err, err)
	}
	if he.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", he.Code)
	}
	wire, ok := he.Message.(domain.WireError)
	if !ok {
		t.Fatalf("expected domain wire error, got %T", he.Message)
	}
	if wire.Code != domain.CodeInvalidConfig || !strings.Contains(wire.Error, "health_check_timeout") {
		t.Errorf("error should be invalid_config health_check_timeout, got %+v", wire)
	}
}

func TestHandleDeployInvalidBody(t *testing.T) {
	h := newTestHarness(t)
	c, _ := h.request(http.MethodPost, "/deploy", `not json`)

	err := h.srv.handleDeploy(c)
	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected echo.HTTPError, got %T (%v)", err, err)
	}
	if he.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", he.Code)
	}
}

func TestHandleDeployIssueRedactsSecrets(t *testing.T) {
	h := newTestHarness(t)
	c, _ := h.request(http.MethodPost, "/deploy", `{"name":"secret-svc","path":"/tmp/API_KEY=supersecret"}`)

	err := h.srv.handleDeploy(c)
	if err == nil {
		t.Fatal("expected deploy error")
	}
	list, listErr := h.srv.iss.Recent(1, "cli:deploy")
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(list) != 1 {
		t.Fatalf("issues = %d, want 1", len(list))
	}
	if strings.Contains(list[0].Error, "supersecret") {
		t.Fatalf("issue leaked secret: %q", list[0].Error)
	}
	if !strings.Contains(list[0].Error, "[redacted]") {
		t.Fatalf("issue was not redacted: %q", list[0].Error)
	}
}

// --- handleStop ---

func TestHandleStopNotFound(t *testing.T) {
	h := newTestHarness(t)
	c, _ := h.request(http.MethodPost, "/stop/nonexistent", "")
	c.SetParamNames("name")
	c.SetParamValues("nonexistent")

	err := h.srv.handleStop(c)
	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected echo.HTTPError, got %T (%v)", err, err)
	}
	if he.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", he.Code)
	}
	wire, ok := he.Message.(domain.WireError)
	if !ok {
		t.Fatalf("expected domain wire error, got %T", he.Message)
	}
	if wire.Code != domain.CodeMissingService {
		t.Errorf("expected missing_service, got %q", wire.Code)
	}
}

// --- handleRestart ---

func TestHandleRestartNotFound(t *testing.T) {
	h := newTestHarness(t)
	c, _ := h.request(http.MethodPost, "/restart/nonexistent", "")
	c.SetParamNames("name")
	c.SetParamValues("nonexistent")

	err := h.srv.handleRestart(c)
	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected echo.HTTPError, got %T (%v)", err, err)
	}
	if he.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", he.Code)
	}
	wire, ok := he.Message.(domain.WireError)
	if !ok {
		t.Fatalf("expected domain wire error, got %T", he.Message)
	}
	if wire.Code != domain.CodeMissingService {
		t.Errorf("expected missing_service, got %q", wire.Code)
	}
}

// --- handleRollback ---

func TestHandleRollbackNotFound(t *testing.T) {
	h := newTestHarness(t)
	c, _ := h.request(http.MethodPost, "/rollback/nonexistent", "")
	c.SetParamNames("name")
	c.SetParamValues("nonexistent")

	err := h.srv.handleRollback(c)
	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected echo.HTTPError, got %T (%v)", err, err)
	}
	if he.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", he.Code)
	}
	wire, ok := he.Message.(domain.WireError)
	if !ok {
		t.Fatalf("expected domain wire error, got %T", he.Message)
	}
	if wire.Code != domain.CodeMissingService {
		t.Errorf("expected missing_service, got %q", wire.Code)
	}
}

func TestHandleRollbackStaticService(t *testing.T) {
	h := newTestHarness(t)
	v1 := filepath.Join(h.tmpDir, "static-v1")
	v2 := filepath.Join(h.tmpDir, "static-v2")
	if err := os.MkdirAll(v1, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(v2, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(v1, "index.html"), []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(v2, "index.html"), []byte("v2"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := h.srv.svc.Deploy(service.DeployRequest{
		Name:    "static-svc",
		Version: "v1",
		Type:    registry.TypeStatic,
		Path:    v1,
	}); err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	if _, err := h.srv.svc.Deploy(service.DeployRequest{
		Name:    "static-svc",
		Version: "v2",
		Type:    registry.TypeStatic,
		Path:    v2,
	}); err != nil {
		t.Fatalf("deploy v2: %v", err)
	}

	c, rec := h.request(http.MethodPost, "/rollback/static-svc", "")
	c.SetParamNames("name")
	c.SetParamValues("static-svc")

	if err := h.srv.handleRollback(c); err != nil {
		t.Fatalf("handleRollback returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp registry.Service
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Version != "v1" {
		t.Errorf("rollback response version = %q, want v1", resp.Version)
	}
	if resp.BinaryPath != v1 {
		t.Errorf("rollback response binary_path = %q, want %q", resp.BinaryPath, v1)
	}
}

// --- handleStatus ---

func TestHandleStatusNotFound(t *testing.T) {
	h := newTestHarness(t)
	c, _ := h.request(http.MethodGet, "/status/nonexistent", "")
	c.SetParamNames("name")
	c.SetParamValues("nonexistent")

	err := h.srv.handleStatus(c)
	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected echo.HTTPError, got %T (%v)", err, err)
	}
	if he.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", he.Code)
	}
	wire, ok := he.Message.(domain.WireError)
	if !ok {
		t.Fatalf("expected domain wire error, got %T", he.Message)
	}
	if wire.Code != domain.CodeMissingService {
		t.Errorf("expected missing_service, got %q", wire.Code)
	}
}

func TestHandleDiagnoseMissingService(t *testing.T) {
	h := newTestHarness(t)
	c, rec := h.request(http.MethodGet, "/diagnose?service_name=ghost", "")

	if err := h.srv.handleDiagnose(c); err != nil {
		t.Fatalf("handleDiagnose returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp struct {
		Healthy  bool `json:"healthy"`
		Errors   int  `json:"errors"`
		Findings []struct {
			Code domain.Code `json:"code"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Healthy || resp.Errors != 1 {
		t.Fatalf("healthy/errors = %v/%d, want false/1", resp.Healthy, resp.Errors)
	}
	if len(resp.Findings) != 1 || resp.Findings[0].Code != domain.CodeMissingService {
		t.Fatalf("findings = %+v, want missing_service", resp.Findings)
	}
}

func TestHandleIssueLifecycleTransitions(t *testing.T) {
	h := newTestHarness(t)
	c, _ := h.request(http.MethodPost, "/issues", `{"source":"test","error":"boom"}`)
	if err := h.srv.handlePostIssue(c); err != nil {
		t.Fatal(err)
	}
	list, err := h.srv.iss.Recent(1, "")
	if err != nil || len(list) != 1 {
		t.Fatalf("recent = %v, %v", list, err)
	}
	id := list[0].ID
	ctx, _ := h.request(http.MethodPost, "/issues/"+id+"/acknowledge", `{"actor":"test-agent"}`)
	ctx.SetParamNames("id")
	ctx.SetParamValues(id)
	if err := h.srv.handleAcknowledgeIssue(ctx); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	ctx, _ = h.request(http.MethodPost, "/issues/"+id+"/resolve", `{"actor":"test-agent","tracker_url":"https://tracker.invalid/1"}`)
	ctx.SetParamNames("id")
	ctx.SetParamValues(id)
	if err := h.srv.handleResolveIssue(ctx); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	ctx, _ = h.request(http.MethodPost, "/issues/"+id+"/reopen", `{}`)
	ctx.SetParamNames("id")
	ctx.SetParamValues(id)
	if err := h.srv.handleReopenIssue(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestHandlePruneRequiresConfirmation(t *testing.T) {
	h := newTestHarness(t)
	c, _ := h.request(http.MethodPost, "/prune/ghost", "")
	c.SetParamNames("name")
	c.SetParamValues("ghost")
	err := h.srv.handlePrune(c)
	he, ok := err.(*echo.HTTPError)
	if !ok || he.Code != http.StatusConflict {
		t.Fatalf("prune error = %#v, want 409", err)
	}
}

func TestHandleIssueTransitionRejectsInvalidBody(t *testing.T) {
	h := newTestHarness(t)
	c, _ := h.request(http.MethodPost, "/issues/ghost/acknowledge", "{")
	c.SetParamNames("id")
	c.SetParamValues("ghost")
	err := h.srv.handleAcknowledgeIssue(c)
	he, ok := err.(*echo.HTTPError)
	if !ok || he.Code != http.StatusBadRequest {
		t.Fatalf("invalid transition error = %#v, want 400", err)
	}
}

func TestHandleStatusRegistered(t *testing.T) {
	h := newTestHarness(t)

	// Manually register a service via the registry to avoid needing a real deploy.
	dataDir := filepath.Join(h.tmpDir, "data")
	reg, _ := registry.New(dataDir)
	svc := &registry.Service{
		Name:        "test-svc",
		Type:        registry.TypeBinary,
		BinaryPath:  "/bin/true",
		StablePort:  9999,
		StablePorts: map[string]int{"default": 9999},
		HealthCheck: "/health",
		Status:      registry.StatusStopped,
	}
	if err := reg.Register(svc); err != nil {
		t.Fatal(err)
	}

	// Recreate the service layer with the same data dir so it sees the
	// registered service.
	logDir := filepath.Join(h.tmpDir, "logs")
	mgr, _ := process.New(logDir, reg)
	prx := proxy.NewManager()
	wtch := watcher.New()
	svcLayer := service.New(reg, mgr, prx, logDir, wtch, nil)
	issDir := filepath.Join(h.tmpDir, "issues")
	iss := issues.New(issDir)
	h.srv = New(svcLayer, iss, nil, 0, "test-v0.0.1")

	c, rec := h.request(http.MethodGet, "/status/test-svc", "")
	c.SetParamNames("name")
	c.SetParamValues("test-svc")

	if err := h.srv.handleStatus(c); err != nil {
		t.Fatalf("handleStatus returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp registry.Service
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Name != "test-svc" {
		t.Errorf("expected name=test-svc, got %q", resp.Name)
	}
}

// --- handleRemove ---

func TestHandleRemoveNotFound(t *testing.T) {
	h := newTestHarness(t)
	c, _ := h.request(http.MethodPost, "/remove/nonexistent", "")
	c.SetParamNames("name")
	c.SetParamValues("nonexistent")

	// Remove on an unknown service succeeds — registry.Remove just deletes
	// the key (noop if absent) and save() writes the file. Check that no
	// HTTP error is returned.
	err := h.srv.handleRemove(c)
	if err != nil {
		t.Fatalf("handleRemove returned unexpected error: %v", err)
	}
}

func TestHandleRemoveRegistered(t *testing.T) {
	h := newTestHarness(t)

	// Register a service directly.
	dataDir := filepath.Join(h.tmpDir, "data")
	reg, _ := registry.New(dataDir)
	svc := &registry.Service{
		Name:        "rm-svc",
		Type:        registry.TypeBinary,
		BinaryPath:  "/bin/true",
		StablePort:  9998,
		StablePorts: map[string]int{"default": 9998},
		HealthCheck: "/health",
		Status:      registry.StatusStopped,
	}
	if err := reg.Register(svc); err != nil {
		t.Fatal(err)
	}

	logDir := filepath.Join(h.tmpDir, "logs")
	mgr, _ := process.New(logDir, reg)
	prx := proxy.NewManager()
	wtch := watcher.New()
	svcLayer := service.New(reg, mgr, prx, logDir, wtch, nil)
	issDir := filepath.Join(h.tmpDir, "issues")
	iss := issues.New(issDir)
	h.srv = New(svcLayer, iss, nil, 0, "test-v0.0.1")

	c, rec := h.request(http.MethodPost, "/remove/rm-svc", "")
	c.SetParamNames("name")
	c.SetParamValues("rm-svc")

	if err := h.srv.handleRemove(c); err != nil {
		t.Fatalf("handleRemove returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["status"] != "removed" {
		t.Errorf("expected status=removed, got %q", resp["status"])
	}
	if resp["name"] != "rm-svc" {
		t.Errorf("expected name=rm-svc, got %q", resp["name"])
	}
}

// --- handleLogs ---

func TestHandleLogsNotFound(t *testing.T) {
	h := newTestHarness(t)
	c, _ := h.request(http.MethodGet, "/logs/nonexistent", "")
	c.SetParamNames("name")
	c.SetParamValues("nonexistent")

	err := h.srv.handleLogs(c)
	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected echo.HTTPError, got %T (%v)", err, err)
	}
	if he.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", he.Code)
	}
	wire, ok := he.Message.(domain.WireError)
	if !ok {
		t.Fatalf("expected domain wire error, got %T", he.Message)
	}
	if wire.Code != domain.CodeMissingService {
		t.Errorf("expected missing_service, got %q", wire.Code)
	}
}

func TestHandleLogsDaemonNoFile(t *testing.T) {
	h := newTestHarness(t)
	c, rec := h.request(http.MethodGet, "/logs/~daemon", "")
	c.SetParamNames("name")
	c.SetParamValues("~daemon")

	if err := h.srv.handleLogs(c); err != nil {
		t.Fatalf("handleLogs returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var lines []string
	if err := json.Unmarshal(rec.Body.Bytes(), &lines); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(lines) != 0 {
		t.Errorf("expected 0 log lines, got %d", len(lines))
	}
}

func TestHandleLogsDaemonWithContent(t *testing.T) {
	h := newTestHarness(t)

	// Write a fake daemon log.
	logPath := filepath.Join(h.tmpDir, "logs", "anito.log")
	content := "line1\nline2\nline3\n"
	if err := os.WriteFile(logPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	c, rec := h.request(http.MethodGet, "/logs/~daemon?lines=2", "")
	c.SetParamNames("name")
	c.SetParamValues("~daemon")
	c.QueryParams().Set("lines", "2")

	if err := h.srv.handleLogs(c); err != nil {
		t.Fatalf("handleLogs returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var lines []string
	if err := json.Unmarshal(rec.Body.Bytes(), &lines); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(lines) != 2 {
		t.Errorf("expected 2 log lines, got %d: %v", len(lines), lines)
	}
}

// --- handleDoctor ---

func TestHandleDoctorMissingPath(t *testing.T) {
	h := newTestHarness(t)
	c, _ := h.request(http.MethodGet, "/doctor", "")

	err := h.srv.handleDoctor(c)
	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected echo.HTTPError, got %T (%v)", err, err)
	}
	if he.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", he.Code)
	}
}

func TestHandleDoctorNoAnitoDir(t *testing.T) {
	h := newTestHarness(t)
	// Point to a directory with no .anito/
	c, _ := h.request(http.MethodGet, "/doctor?path="+h.tmpDir, "")
	c.QueryParams().Set("path", h.tmpDir)

	err := h.srv.handleDoctor(c)
	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected echo.HTTPError, got %T (%v)", err, err)
	}
	if he.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", he.Code)
	}
}

// --- handleTeardown ---

func TestHandleTeardownEmptyRepoPath(t *testing.T) {
	h := newTestHarness(t)
	c, _ := h.request(http.MethodPost, "/teardown", `{"repo_path":""}`)

	err := h.srv.handleTeardown(c)
	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected echo.HTTPError, got %T (%v)", err, err)
	}
	if he.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", he.Code)
	}
}

func TestHandleTeardownInvalidBody(t *testing.T) {
	h := newTestHarness(t)
	c, _ := h.request(http.MethodPost, "/teardown", `not json`)

	err := h.srv.handleTeardown(c)
	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected echo.HTTPError, got %T (%v)", err, err)
	}
	if he.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", he.Code)
	}
}

// --- handlePostIssue ---

func TestHandlePostIssueMissingError(t *testing.T) {
	h := newTestHarness(t)
	c, _ := h.request(http.MethodPost, "/issues", `{"source":"test"}`)

	err := h.srv.handlePostIssue(c)
	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected echo.HTTPError, got %T (%v)", err, err)
	}
	if he.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", he.Code)
	}
}

func TestHandlePostIssueInvalidBody(t *testing.T) {
	h := newTestHarness(t)
	c, _ := h.request(http.MethodPost, "/issues", `not json`)

	err := h.srv.handlePostIssue(c)
	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected echo.HTTPError, got %T (%v)", err, err)
	}
	if he.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", he.Code)
	}
}

func TestHandlePostIssueValid(t *testing.T) {
	h := newTestHarness(t)
	body := `{"error":"something broke","source":"test:unit","context":"testing"}`
	c, rec := h.request(http.MethodPost, "/issues", body)

	if err := h.srv.handlePostIssue(c); err != nil {
		t.Fatalf("handlePostIssue returned error: %v", err)
	}

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rec.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["status"] != "logged" {
		t.Errorf("expected status=logged, got %q", resp["status"])
	}
}

func TestHandlePostIssueDefaultSource(t *testing.T) {
	h := newTestHarness(t)
	// Omit source — should default to "external".
	body := `{"error":"whoops"}`
	c, rec := h.request(http.MethodPost, "/issues", body)

	if err := h.srv.handlePostIssue(c); err != nil {
		t.Fatalf("handlePostIssue returned error: %v", err)
	}

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rec.Code)
	}
}

// --- handleGetIssues ---

func TestHandleGetIssuesEmpty(t *testing.T) {
	h := newTestHarness(t)
	c, rec := h.request(http.MethodGet, "/issues", "")

	if err := h.srv.handleGetIssues(c); err != nil {
		t.Fatalf("handleGetIssues returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	var issuesList []issues.Issue
	if err := json.Unmarshal(resp["issues"], &issuesList); err != nil {
		t.Fatalf("unmarshal issues: %v", err)
	}
	if len(issuesList) != 0 {
		t.Errorf("expected 0 issues, got %d", len(issuesList))
	}
}

func TestHandleGetIssuesAfterPost(t *testing.T) {
	h := newTestHarness(t)

	// Post an issue first.
	postBody := `{"error":"test error","source":"test:unit"}`
	c1, _ := h.request(http.MethodPost, "/issues", postBody)
	if err := h.srv.handlePostIssue(c1); err != nil {
		t.Fatalf("handlePostIssue returned error: %v", err)
	}

	// Now retrieve issues.
	c2, rec2 := h.request(http.MethodGet, "/issues", "")

	if err := h.srv.handleGetIssues(c2); err != nil {
		t.Fatalf("handleGetIssues returned error: %v", err)
	}

	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec2.Code)
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(rec2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	var issuesList []issues.Issue
	if err := json.Unmarshal(resp["issues"], &issuesList); err != nil {
		t.Fatalf("unmarshal issues: %v", err)
	}
	if len(issuesList) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issuesList))
	}
	if issuesList[0].Error != "test error" {
		t.Errorf("expected error=%q, got %q", "test error", issuesList[0].Error)
	}
	if issuesList[0].Source != "test:unit" {
		t.Errorf("expected source=%q, got %q", "test:unit", issuesList[0].Source)
	}
}

func TestHandleGetIssuesWithSourceFilter(t *testing.T) {
	h := newTestHarness(t)

	// Post two issues with different sources.
	c1, _ := h.request(http.MethodPost, "/issues", `{"error":"alpha","source":"mcp:deploy"}`)
	if err := h.srv.handlePostIssue(c1); err != nil {
		t.Fatal(err)
	}
	c2, _ := h.request(http.MethodPost, "/issues", `{"error":"beta","source":"cli:stop"}`)
	if err := h.srv.handlePostIssue(c2); err != nil {
		t.Fatal(err)
	}

	// Filter by "mcp:" source.
	c3, rec3 := h.request(http.MethodGet, "/issues?source=mcp:", "")
	c3.QueryParams().Set("source", "mcp:")

	if err := h.srv.handleGetIssues(c3); err != nil {
		t.Fatal(err)
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(rec3.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	var issuesList []issues.Issue
	if err := json.Unmarshal(resp["issues"], &issuesList); err != nil {
		t.Fatal(err)
	}
	if len(issuesList) != 1 {
		t.Fatalf("expected 1 issue with source=mcp:, got %d", len(issuesList))
	}
	if issuesList[0].Error != "alpha" {
		t.Errorf("expected error=%q, got %q", "alpha", issuesList[0].Error)
	}
}

// --- handleDeploy static success ---

// TestHandleDeploy_StaticSuccess verifies that deploying a static service
// (type=static) with a valid directory returns 200 with the service record.
// Static deploys skip the health-check and process-start, so this works
// without a real binary.
func TestHandleDeploy_StaticSuccess(t *testing.T) {
	h := newTestHarness(t)

	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("<html/>"), 0644); err != nil {
		t.Fatal(err)
	}

	body := `{"name":"static-svc","type":"static","path":"` + staticDir + `"}`
	c, rec := h.request(http.MethodPost, "/deploy", body)

	if err := h.srv.handleDeploy(c); err != nil {
		t.Fatalf("handleDeploy returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var svc registry.Service
	if err := json.Unmarshal(rec.Body.Bytes(), &svc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if svc.Name != "static-svc" {
		t.Errorf("expected name=static-svc, got %q", svc.Name)
	}
	if svc.Status != registry.StatusRunning {
		t.Errorf("expected status=running, got %q", svc.Status)
	}

	// Release the proxy listener so it doesn't leak across tests.
	t.Cleanup(func() { _ = h.srv.svc.Remove("static-svc") })
}

// --- handleLogs streaming paths ---

// --- handleRestart static success ---

// TestHandleRestart_Static verifies that restarting a TypeStatic service
// returns 200 with the service record. Static services have no process to
// start, so service.Restart returns nil immediately.
func TestHandleRestart_Static(t *testing.T) {
	h := newTestHarness(t)

	// Deploy a static service first so the registry has an entry.
	staticDir := t.TempDir()
	body := `{"name":"restart-static","type":"static","path":"` + staticDir + `"}`
	c1, _ := h.request(http.MethodPost, "/deploy", body)
	if err := h.srv.handleDeploy(c1); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	t.Cleanup(func() { _ = h.srv.svc.Remove("restart-static") })

	// Now restart it.
	c2, rec := h.request(http.MethodPost, "/restart/restart-static", "")
	c2.SetParamNames("name")
	c2.SetParamValues("restart-static")

	if err := h.srv.handleRestart(c2); err != nil {
		t.Fatalf("handleRestart returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var svc registry.Service
	if err := json.Unmarshal(rec.Body.Bytes(), &svc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if svc.Name != "restart-static" {
		t.Errorf("expected name=restart-static, got %q", svc.Name)
	}
}

// TestHandleLogs_EmptyName verifies that an empty :name param returns 400.
func TestHandleLogs_EmptyName(t *testing.T) {
	h := newTestHarness(t)
	// Do not set param names — c.Param("name") returns "".
	c, _ := h.request(http.MethodGet, "/logs/", "")

	err := h.srv.handleLogs(c)
	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected echo.HTTPError, got %T (%v)", err, err)
	}
	if he.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", he.Code)
	}
}

// TestHandleLogs_StreamBuild_NoFlusher verifies that requesting stream=build
// with a ResponseWriter that does not implement http.Flusher returns 500
// "streaming not supported".
func TestHandleLogs_StreamBuild_NoFlusher(t *testing.T) {
	h := newTestHarness(t)
	c, _ := h.newNonFlushContext(http.MethodGet, "/logs/svc?stream=build")
	c.SetParamNames("name")
	c.SetParamValues("svc")
	c.QueryParams().Set("stream", "build")

	err := h.srv.handleLogs(c)
	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected echo.HTTPError, got %T (%v)", err, err)
	}
	if he.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", he.Code)
	}
}

// TestHandleLogs_Follow_NoFlusher verifies that requesting follow=true
// with a ResponseWriter that does not implement http.Flusher returns 500
// "streaming not supported".
func TestHandleLogs_Follow_NoFlusher(t *testing.T) {
	h := newTestHarness(t)
	c, _ := h.newNonFlushContext(http.MethodGet, "/logs/svc?follow=true")
	c.SetParamNames("name")
	c.SetParamValues("svc")
	c.QueryParams().Set("follow", "true")

	err := h.srv.handleLogs(c)
	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected echo.HTTPError, got %T (%v)", err, err)
	}
	if he.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", he.Code)
	}
}

// TestHandleLogs_StreamBuild_WithFlusher verifies that streamBuildLogs writes
// SSE headers and an error event when the service is not registered. Uses a
// flusher-capable recorder to pass the flusher check.
func TestHandleLogs_StreamBuild_WithFlusher(t *testing.T) {
	h := newTestHarness(t)
	c, fr := h.newFlushContext(http.MethodGet, "/logs/nosvc?stream=build")
	c.SetParamNames("name")
	c.SetParamValues("nosvc")
	c.QueryParams().Set("stream", "build")

	if err := h.srv.handleLogs(c); err != nil {
		t.Fatalf("handleLogs returned unexpected error: %v", err)
	}

	body := fr.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Errorf("expected SSE error event in response body, got: %q", body)
	}
	ct := fr.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("expected Content-Type=text/event-stream, got %q", ct)
	}
}

// TestHandleLogs_Follow_WithFlusher verifies that streamLogs writes SSE headers
// and an error event when the service is not registered.
func TestHandleLogs_Follow_WithFlusher(t *testing.T) {
	h := newTestHarness(t)
	c, fr := h.newFlushContext(http.MethodGet, "/logs/nosvc?follow=true")
	c.SetParamNames("name")
	c.SetParamValues("nosvc")
	c.QueryParams().Set("follow", "true")

	if err := h.srv.handleLogs(c); err != nil {
		t.Fatalf("handleLogs returned unexpected error: %v", err)
	}

	body := fr.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Errorf("expected SSE error event in response body, got: %q", body)
	}
	ct := fr.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("expected Content-Type=text/event-stream, got %q", ct)
	}
}

// --- handleTeardown success ---

// TestHandleTeardown_ValidPathNoReceipt verifies that teardown with a valid
// repo_path that has no deployed.json returns 200 with an empty removed list.
func TestHandleTeardown_ValidPathNoReceipt(t *testing.T) {
	h := newTestHarness(t)
	body := `{"repo_path":"` + h.tmpDir + `"}`
	c, rec := h.request(http.MethodPost, "/teardown", body)

	if err := h.srv.handleTeardown(c); err != nil {
		t.Fatalf("handleTeardown returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	count, _ := resp["count"].(float64)
	if count != 0 {
		t.Errorf("expected count=0, got %v", count)
	}
}

// --- handleDoctor success ---

// TestHandleDoctor_ValidDir verifies that doctor returns 200 when the repo has
// a .anito/ directory with at least one .yaml config file.
func TestHandleDoctor_ValidDir(t *testing.T) {
	h := newTestHarness(t)

	anitoDir := filepath.Join(h.tmpDir, ".anito")
	if err := os.MkdirAll(anitoDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := "name: test-svc\noutput: ./bin/test-svc\n"
	if err := os.WriteFile(filepath.Join(anitoDir, "config.yaml"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}

	c, rec := h.request(http.MethodGet, "/doctor?path="+h.tmpDir, "")
	c.QueryParams().Set("path", h.tmpDir)

	if err := h.srv.handleDoctor(c); err != nil {
		t.Fatalf("handleDoctor returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Result struct fields have no JSON tags, so keys are PascalCase.
	if _, ok := result["Healthy"]; !ok {
		t.Error("response should contain 'Healthy' field")
	}
}

// --- handleGetIssues with lines param ---

// TestHandleGetIssues_WithLinesParam verifies that the ?lines= query parameter
// is parsed and limits the number of issues returned.
func TestHandleGetIssues_WithLinesParam(t *testing.T) {
	h := newTestHarness(t)

	// Post 3 issues.
	for _, msg := range []string{"first", "second", "third"} {
		c, _ := h.request(http.MethodPost, "/issues", `{"error":"`+msg+`","source":"test"}`)
		if err := h.srv.handlePostIssue(c); err != nil {
			t.Fatal(err)
		}
	}

	// Retrieve with lines=2.
	c, rec := h.request(http.MethodGet, "/issues?lines=2", "")
	c.QueryParams().Set("lines", "2")

	if err := h.srv.handleGetIssues(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	var issuesList []issues.Issue
	if err := json.Unmarshal(resp["issues"], &issuesList); err != nil {
		t.Fatal(err)
	}
	if len(issuesList) != 2 {
		t.Errorf("expected 2 issues with lines=2, got %d", len(issuesList))
	}
}

// --- handleDeploy with valid duration strings ---

// TestHandleDeploy_BinaryFails verifies that handleDeploy returns 503 when
// the service deploy fails (binary exits before health check passes).
// Uses /usr/bin/true which exits immediately without ever serving HTTP.
func TestHandleDeploy_BinaryFails(t *testing.T) {
	h := newTestHarness(t)
	body := `{"name":"fail-svc","type":"binary","path":"/usr/bin/true","health_check_timeout":"100ms"}`
	c, _ := h.request(http.MethodPost, "/deploy", body)

	err := h.srv.handleDeploy(c)
	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected echo.HTTPError, got %T (%v)", err, err)
	}
	if he.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", he.Code)
	}
	wire, ok := he.Message.(domain.WireError)
	if !ok {
		t.Fatalf("expected domain wire error, got %T", he.Message)
	}
	if wire.Code != domain.CodeReadinessFailure {
		t.Errorf("expected readiness_failure, got %q", wire.Code)
	}
}

// TestHandleDeploy_StaticWithDurations covers the "valid drain_window and
// health_check_timeout" branches inside handleDeploy that are skipped by the
// invalid-duration error tests.
func TestHandleDeploy_StaticWithDurations(t *testing.T) {
	h := newTestHarness(t)
	staticDir := t.TempDir()

	body := `{"name":"static-dur","type":"static","path":"` + staticDir + `",` +
		`"drain_window":"1s","health_check_timeout":"5s"}`
	c, rec := h.request(http.MethodPost, "/deploy", body)

	if err := h.srv.handleDeploy(c); err != nil {
		t.Fatalf("handleDeploy returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	t.Cleanup(func() { _ = h.srv.svc.Remove("static-dur") })
}

// --- streaming success paths (backlog coverage) ---

// registerServiceForStream sets up a service in the test harness's registry
// (without starting a process) so that BuildLogs/Logs can find it.
// Returns the service name and a teardown function.
func registerServiceForStream(t *testing.T, h *testHarness, name string) {
	t.Helper()
	dataDir := filepath.Join(h.tmpDir, "data")
	reg, _ := registry.New(dataDir)
	svc := &registry.Service{
		Name:        name,
		Type:        registry.TypeBinary,
		BinaryPath:  "/bin/true",
		StablePort:  0,
		StablePorts: map[string]int{"default": 0},
		HealthCheck: "/health",
		Status:      registry.StatusStopped,
	}
	if err := reg.Register(svc); err != nil {
		t.Fatalf("register: %v", err)
	}

	logDir := filepath.Join(h.tmpDir, "logs")
	mgr, _ := process.New(logDir, reg)
	prx := proxy.NewManager()
	wtch := watcher.New()
	svcLayer := service.New(reg, mgr, prx, logDir, wtch, nil)
	issDir := filepath.Join(h.tmpDir, "issues")
	iss := issues.New(issDir)
	h.srv = New(svcLayer, iss, nil, 0, "test-v0.0.1")
}

// TestHandleLogs_StreamBuild_WithBacklog verifies that streamBuildLogs writes
// previously-logged build log lines (the backlog) as SSE data events before
// starting to tail. Uses a pre-cancelled context to exit the tail goroutine
// immediately after it starts.
func TestHandleLogs_StreamBuild_WithBacklog(t *testing.T) {
	h := newTestHarness(t)
	registerServiceForStream(t, h, "build-svc")

	// Write fake build log content.
	logDir := filepath.Join(h.tmpDir, "logs")
	buildLog := filepath.Join(logDir, "build-svc-build.log")
	if err := os.WriteFile(buildLog, []byte("buildline1\nbuildline2\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Pre-cancel the context so the stream goroutine exits immediately.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest(http.MethodGet, "/logs/build-svc?stream=build", nil).WithContext(ctx)
	fr := &flushRecorder{httptest.NewRecorder()}
	c := h.echo.NewContext(req, fr)
	c.SetParamNames("name")
	c.SetParamValues("build-svc")
	c.QueryParams().Set("stream", "build")

	if err := h.srv.handleLogs(c); err != nil {
		t.Fatalf("handleLogs returned error: %v", err)
	}

	body := fr.Body.String()
	if !strings.Contains(body, "buildline1") {
		t.Errorf("SSE response should contain backlog line, got: %q", body)
	}
	if !strings.Contains(body, "text/event-stream") {
		ct := fr.Header().Get("Content-Type")
		if !strings.Contains(ct, "text/event-stream") {
			t.Errorf("expected Content-Type=text/event-stream, got %q", ct)
		}
	}
}

// TestHandleLogs_Follow_WithBacklog verifies that streamLogs writes existing
// log lines (the backlog) as SSE data events. Uses a pre-cancelled context to
// exit the tail goroutine immediately.
func TestHandleLogs_Follow_WithBacklog(t *testing.T) {
	h := newTestHarness(t)
	registerServiceForStream(t, h, "follow-svc")

	// Write fake service log content.
	logDir := filepath.Join(h.tmpDir, "logs")
	svcLog := filepath.Join(logDir, "follow-svc.log")
	if err := os.WriteFile(svcLog, []byte("logline1\nlogline2\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Pre-cancel the context so the stream goroutine exits immediately.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest(http.MethodGet, "/logs/follow-svc?follow=true", nil).WithContext(ctx)
	fr := &flushRecorder{httptest.NewRecorder()}
	c := h.echo.NewContext(req, fr)
	c.SetParamNames("name")
	c.SetParamValues("follow-svc")
	c.QueryParams().Set("follow", "true")

	if err := h.srv.handleLogs(c); err != nil {
		t.Fatalf("handleLogs returned error: %v", err)
	}

	body := fr.Body.String()
	if !strings.Contains(body, "logline1") {
		t.Errorf("SSE response should contain backlog line, got: %q", body)
	}
}

// TestHandleLogs_StreamBuild_UnknownService verifies that streamBuildLogs writes
// an SSE error event when the service is not registered (BuildLogs returns error).
func TestHandleLogs_StreamBuild_UnknownService(t *testing.T) {
	h := newTestHarness(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest(http.MethodGet, "/logs/ghost-svc?stream=build", nil).WithContext(ctx)
	fr := &flushRecorder{httptest.NewRecorder()}
	c := h.echo.NewContext(req, fr)
	c.SetParamNames("name")
	c.SetParamValues("ghost-svc")
	c.QueryParams().Set("stream", "build")

	if err := h.srv.handleLogs(c); err != nil {
		t.Fatalf("handleLogs returned error: %v", err)
	}
	body := fr.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Errorf("expected SSE error event for unknown service; got: %q", body)
	}
}

// TestHandleLogs_Follow_UnknownService verifies that streamLogs writes an SSE
// error event when the service is not registered (Logs returns error).
func TestHandleLogs_Follow_UnknownService(t *testing.T) {
	h := newTestHarness(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest(http.MethodGet, "/logs/ghost-svc?follow=true", nil).WithContext(ctx)
	fr := &flushRecorder{httptest.NewRecorder()}
	c := h.echo.NewContext(req, fr)
	c.SetParamNames("name")
	c.SetParamValues("ghost-svc")
	c.QueryParams().Set("follow", "true")

	if err := h.srv.handleLogs(c); err != nil {
		t.Fatalf("handleLogs returned error: %v", err)
	}
	body := fr.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Errorf("expected SSE error event for unknown service; got: %q", body)
	}
}

// TestHandleStop_Static verifies handleStop succeeds when the service is stopped
// (Stop on a static service errors in the process manager but the handler checks
// the service layer error). This test exercises the 200 OK path via Stop on a
// registered but not-running service (which returns an error from mgr but the
// service layer bubbles it up). We verify the "stopped" response path by using
// a service that's actually tracked by the process manager via the fake binary.
// Since we can't easily start a real process in a server unit test, we instead
// confirm handleStop returns an HTTPError for an unknown service (already tested)
// and that handleStop returns 200 for a registered-but-stopped service when
// service.Stop succeeds. We use the fact that mgr.Stop errors but the Stop
// path flows all the way through.
//
// NOTE: The success path of handleStop (200 OK) requires a process that mgr can
// actually Stop. We test the 200 path by verifying handleStop itself produces
// 200 when Stop succeeds — this needs the process manager to track the service.
// Since starting a real process is complex in server tests, we verify only the
// error path here and note the 200 path is covered by integration tests.

// TestHandleLogs_StreamBuild_LiveLine verifies that streamBuildLogs sends lines
// written to the build log file AFTER the stream is opened (covers the
// "for line := range ch" loop body).
func TestHandleLogs_StreamBuild_LiveLine(t *testing.T) {
	h := newTestHarness(t)

	logDir := filepath.Join(h.tmpDir, "logs")
	registerServiceForStream(t, h, "live-build-stream-svc")

	// Create empty build log so BuildLogStream can open it.
	buildLog := filepath.Join(logDir, "live-build-stream-svc-build.log")
	if err := os.WriteFile(buildLog, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/logs/live-build-stream-svc?stream=build", nil).WithContext(ctx)
	fr := &flushRecorder{httptest.NewRecorder()}
	c := h.echo.NewContext(req, fr)
	c.SetParamNames("name")
	c.SetParamValues("live-build-stream-svc")
	c.QueryParams().Set("stream", "build")

	done := make(chan error, 1)
	go func() {
		done <- h.srv.handleLogs(c)
	}()

	// Give the goroutine time to open the file and seek to end.
	time.Sleep(100 * time.Millisecond)

	// Append a line — the BuildLogStream ticker (200ms) will pick it up.
	f, err := os.OpenFile(buildLog, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintln(f, "live-build-line"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	// Wait for the line to be relayed then cancel the context.
	time.Sleep(500 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("handleLogs returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handleLogs did not complete after context cancel")
	}

	body := fr.Body.String()
	if !strings.Contains(body, "live-build-line") {
		t.Errorf("expected live-build-line in SSE body, got: %q", body)
	}
}

// TestHandleLogs_Follow_LiveLine verifies that streamLogs sends lines written
// to the log file AFTER the stream is opened (covers the for-loop body).
func TestHandleLogs_Follow_LiveLine(t *testing.T) {
	h := newTestHarness(t)

	logDir := filepath.Join(h.tmpDir, "logs")
	registerServiceForStream(t, h, "live-follow-svc")

	// Create empty service log.
	svcLog := filepath.Join(logDir, "live-follow-svc.log")
	if err := os.WriteFile(svcLog, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/logs/live-follow-svc?follow=true", nil).WithContext(ctx)
	fr := &flushRecorder{httptest.NewRecorder()}
	c := h.echo.NewContext(req, fr)
	c.SetParamNames("name")
	c.SetParamValues("live-follow-svc")
	c.QueryParams().Set("follow", "true")

	done := make(chan error, 1)
	go func() {
		done <- h.srv.handleLogs(c)
	}()

	time.Sleep(100 * time.Millisecond)

	f, err := os.OpenFile(svcLog, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintln(f, "live-follow-line"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	time.Sleep(500 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("handleLogs returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handleLogs did not complete after context cancel")
	}

	body := fr.Body.String()
	if !strings.Contains(body, "live-follow-line") {
		t.Errorf("expected live-follow-line in SSE body, got: %q", body)
	}
}

// TestHandleGetIssues_NilListCoercion verifies that handleGetIssues returns an
// empty array (not null) when no issues exist.
func TestHandleGetIssues_NilListCoercion(t *testing.T) {
	h := newTestHarness(t)
	c, rec := h.request(http.MethodGet, "/issues", "")

	if err := h.srv.handleGetIssues(c); err != nil {
		t.Fatalf("handleGetIssues returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	issuesList, ok := result["issues"]
	if !ok {
		t.Fatal("response missing 'issues' field")
	}
	// Must be an array, not null.
	arr, ok := issuesList.([]interface{})
	if !ok {
		t.Errorf("expected issues to be an array, got %T", issuesList)
	}
	if len(arr) != 0 {
		t.Errorf("expected empty array, got len=%d", len(arr))
	}
}

// TestHandleMetrics_Shape verifies GET /metrics returns the expected JSON fields
// with numeric values.
func TestHandleMetrics_Shape(t *testing.T) {
	h := newTestHarness(t)
	c, rec := h.request(http.MethodGet, "/metrics", "")
	if err := h.srv.handleMetrics(c); err != nil {
		t.Fatalf("handleMetrics: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, field := range []string{
		"services_running", "services_stopped", "services_failed",
		"services_orphaned", "services_total",
		"deploys_total", "crashes_total",
	} {
		if _, ok := m[field]; !ok {
			t.Errorf("metrics response missing field %q", field)
		}
	}
}
