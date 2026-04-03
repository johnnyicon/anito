package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/johnnyicon/anito/internal/issues"
	"github.com/johnnyicon/anito/internal/process"
	"github.com/johnnyicon/anito/internal/proxy"
	"github.com/johnnyicon/anito/internal/registry"
	"github.com/johnnyicon/anito/internal/service"
	"github.com/johnnyicon/anito/internal/watcher"
)

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

	svc := service.New(reg, mgr, prx, logDir, wtch)
	iss := issues.New(issDir)

	s := New(svc, iss, 0, "test-v0.0.1")

	e := echo.New()
	e.GET("/health", s.handleHealth)
	e.GET("/services", s.handleServices)
	e.POST("/deploy", s.handleDeploy)
	e.POST("/stop/:name", s.handleStop)
	e.POST("/restart/:name", s.handleRestart)
	e.GET("/status/:name", s.handleStatus)
	e.POST("/remove/:name", s.handleRemove)
	e.GET("/logs/:name", s.handleLogs)
	e.POST("/issues", s.handlePostIssue)
	e.GET("/issues", s.handleGetIssues)
	e.GET("/doctor", s.handleDoctor)
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

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status=ok, got %q", resp["status"])
	}
	if resp["version"] != "test-v0.0.1" {
		t.Errorf("expected version=test-v0.0.1, got %q", resp["version"])
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
	msg, _ := he.Message.(string)
	if !strings.Contains(msg, "drain_window") {
		t.Errorf("error message should mention drain_window, got %q", msg)
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
	msg, _ := he.Message.(string)
	if !strings.Contains(msg, "health_check_timeout") {
		t.Errorf("error message should mention health_check_timeout, got %q", msg)
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
	if he.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", he.Code)
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
	if he.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", he.Code)
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
	svcLayer := service.New(reg, mgr, prx, logDir, wtch)
	issDir := filepath.Join(h.tmpDir, "issues")
	iss := issues.New(issDir)
	h.srv = New(svcLayer, iss, 0, "test-v0.0.1")

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
	svcLayer := service.New(reg, mgr, prx, logDir, wtch)
	issDir := filepath.Join(h.tmpDir, "issues")
	iss := issues.New(issDir)
	h.srv = New(svcLayer, iss, 0, "test-v0.0.1")

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
