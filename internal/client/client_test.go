package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/johnnyicon/anito/internal/auth"
	"github.com/johnnyicon/anito/internal/issues"
	"github.com/johnnyicon/anito/internal/registry"
)

// newTestClient creates a Client whose base URL points at the given httptest server.
func newTestClient(ts *httptest.Server) *Client {
	return &Client{base: ts.URL}
}

// ---------- New ----------

func TestNew_BaseURL(t *testing.T) {
	c := New(7700)
	want := "http://localhost:7700"
	if c.base != want {
		t.Fatalf("New(7700).base = %q; want %q", c.base, want)
	}
}

func TestNew_DifferentPort(t *testing.T) {
	c := New(9999)
	want := "http://localhost:9999"
	if c.base != want {
		t.Fatalf("New(9999).base = %q; want %q", c.base, want)
	}
}

func TestClientAttachesCapabilityToken(t *testing.T) {
	t.Setenv(auth.EnvToken, "secret")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(auth.HeaderName); got != "secret" {
			t.Fatalf("%s = %q, want secret", auth.HeaderName, got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(registry.Service{Name: "my-svc"})
	}))
	defer ts.Close()

	c := newTestClient(ts)
	if _, err := c.Deploy(DeployRequest{Name: "my-svc", Path: "/tmp/my-svc"}); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
}

// ---------- Deploy ----------

func TestDeploy_Success(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	wantSvc := registry.Service{
		Name:       "my-svc",
		Type:       registry.TypeBinary,
		BinaryPath: "/usr/local/bin/my-svc",
		StablePort: 3000,
		Status:     registry.StatusRunning,
		PID:        1234,
		DeployedAt: now,
		UpdatedAt:  now,
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Deploy: method = %s; want POST", r.Method)
		}
		if r.URL.Path != "/deploy" {
			t.Errorf("Deploy: path = %s; want /deploy", r.URL.Path)
		}
		ct := r.Header.Get("Content-Type")
		if ct != "application/json" {
			t.Errorf("Deploy: Content-Type = %q; want application/json", ct)
		}

		var req DeployRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Deploy: decode request body: %v", err)
		}
		if req.Name != "my-svc" {
			t.Errorf("Deploy request name = %q; want %q", req.Name, "my-svc")
		}
		if req.Path != "/usr/local/bin/my-svc" {
			t.Errorf("Deploy request path = %q; want %q", req.Path, "/usr/local/bin/my-svc")
		}
		if req.VersionPath != "/usr/local/share/my-svc/dist" {
			t.Errorf("Deploy request version_path = %q; want %q", req.VersionPath, "/usr/local/share/my-svc/dist")
		}
		if req.StablePort != 3000 {
			t.Errorf("Deploy request stable_port = %d; want 3000", req.StablePort)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(wantSvc)
	}))
	defer ts.Close()

	c := newTestClient(ts)
	svc, err := c.Deploy(DeployRequest{
		Name:        "my-svc",
		Path:        "/usr/local/bin/my-svc",
		VersionPath: "/usr/local/share/my-svc/dist",
		StablePort:  3000,
		Type:        registry.TypeBinary,
	})
	if err != nil {
		t.Fatalf("Deploy: unexpected error: %v", err)
	}
	if svc.Name != wantSvc.Name {
		t.Errorf("Deploy response name = %q; want %q", svc.Name, wantSvc.Name)
	}
	if svc.StablePort != wantSvc.StablePort {
		t.Errorf("Deploy response stable_port = %d; want %d", svc.StablePort, wantSvc.StablePort)
	}
	if svc.PID != wantSvc.PID {
		t.Errorf("Deploy response PID = %d; want %d", svc.PID, wantSvc.PID)
	}
	if svc.Status != registry.StatusRunning {
		t.Errorf("Deploy response status = %q; want %q", svc.Status, registry.StatusRunning)
	}
}

func TestDeploy_MultiPort(t *testing.T) {
	wantSvc := registry.Service{
		Name:       "my-daemon",
		Type:       registry.TypeBinary,
		BinaryPath: "/usr/local/bin/my-daemon",
		StablePort: 7172,
		StablePorts: map[string]int{
			"ws":   7172,
			"http": 7173,
		},
		HealthCheckPort: "ws",
		Status:          registry.StatusRunning,
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req DeployRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.StablePorts["ws"] != 7172 || req.StablePorts["http"] != 7173 {
			t.Errorf("Deploy request stable_ports mismatch: got %v", req.StablePorts)
		}
		if req.HealthCheckPort != "ws" {
			t.Errorf("Deploy request health_check_port = %q; want %q", req.HealthCheckPort, "ws")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(wantSvc)
	}))
	defer ts.Close()

	c := newTestClient(ts)
	svc, err := c.Deploy(DeployRequest{
		Name:            "my-daemon",
		Path:            "/usr/local/bin/my-daemon",
		Type:            registry.TypeBinary,
		StablePorts:     map[string]int{"ws": 7172, "http": 7173},
		HealthCheckPort: "ws",
	})
	if err != nil {
		t.Fatalf("Deploy: unexpected error: %v", err)
	}
	if svc.StablePorts["ws"] != 7172 {
		t.Errorf("Deploy response stable_ports[ws] = %d; want 7172", svc.StablePorts["ws"])
	}
	if svc.StablePorts["http"] != 7173 {
		t.Errorf("Deploy response stable_ports[http] = %d; want 7173", svc.StablePorts["http"])
	}
}

// ---------- Services ----------

func TestServices_Success(t *testing.T) {
	svcs := []*registry.Service{
		{Name: "svc-a", StablePort: 3000, Status: registry.StatusRunning},
		{Name: "svc-b", StablePort: 3001, Status: registry.StatusStopped},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Services: method = %s; want GET", r.Method)
		}
		if r.URL.Path != "/services" {
			t.Errorf("Services: path = %s; want /services", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(svcs)
	}))
	defer ts.Close()

	c := newTestClient(ts)
	got, err := c.Services()
	if err != nil {
		t.Fatalf("Services: unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Services: len = %d; want 2", len(got))
	}
	if got[0].Name != "svc-a" {
		t.Errorf("Services[0].Name = %q; want %q", got[0].Name, "svc-a")
	}
	if got[1].Status != registry.StatusStopped {
		t.Errorf("Services[1].Status = %q; want %q", got[1].Status, registry.StatusStopped)
	}
}

func TestServices_Empty(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
	}))
	defer ts.Close()

	c := newTestClient(ts)
	got, err := c.Services()
	if err != nil {
		t.Fatalf("Services: unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Services: len = %d; want 0", len(got))
	}
}

// ---------- Status ----------

func TestStatus_Success(t *testing.T) {
	svc := registry.Service{
		Name:       "hello",
		StablePort: 8100,
		Status:     registry.StatusRunning,
		PID:        5678,
		BinaryPath: "/tmp/hello",
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Status: method = %s; want GET", r.Method)
		}
		if r.URL.Path != "/status/hello" {
			t.Errorf("Status: path = %s; want /status/hello", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(svc)
	}))
	defer ts.Close()

	c := newTestClient(ts)
	got, err := c.Status("hello")
	if err != nil {
		t.Fatalf("Status: unexpected error: %v", err)
	}
	if got.Name != "hello" {
		t.Errorf("Status response name = %q; want %q", got.Name, "hello")
	}
	if got.PID != 5678 {
		t.Errorf("Status response PID = %d; want 5678", got.PID)
	}
	if got.BinaryPath != "/tmp/hello" {
		t.Errorf("Status response binary_path = %q; want %q", got.BinaryPath, "/tmp/hello")
	}
}

// ---------- Stop ----------

func TestStop_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Stop: method = %s; want POST", r.Method)
		}
		if r.URL.Path != "/stop/my-svc" {
			t.Errorf("Stop: path = %s; want /stop/my-svc", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := newTestClient(ts)
	if err := c.Stop("my-svc"); err != nil {
		t.Fatalf("Stop: unexpected error: %v", err)
	}
}

// ---------- Restart ----------

func TestRestart_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Restart: method = %s; want POST", r.Method)
		}
		if r.URL.Path != "/restart/my-svc" {
			t.Errorf("Restart: path = %s; want /restart/my-svc", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := newTestClient(ts)
	if err := c.Restart("my-svc"); err != nil {
		t.Fatalf("Restart: unexpected error: %v", err)
	}
}

// ---------- Rollback ----------

func TestRollback_Success(t *testing.T) {
	wantSvc := registry.Service{
		Name:       "my-svc",
		Version:    "v1",
		Type:       registry.TypeStatic,
		BinaryPath: "/tmp/static-v1",
		StablePort: 8100,
		Status:     registry.StatusRunning,
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Rollback: method = %s; want POST", r.Method)
		}
		if r.URL.Path != "/rollback/my-svc" {
			t.Errorf("Rollback: path = %s; want /rollback/my-svc", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(wantSvc)
	}))
	defer ts.Close()

	c := newTestClient(ts)
	svc, err := c.Rollback("my-svc")
	if err != nil {
		t.Fatalf("Rollback: unexpected error: %v", err)
	}
	if svc.Name != "my-svc" {
		t.Errorf("Rollback response name = %q; want my-svc", svc.Name)
	}
	if svc.Version != "v1" {
		t.Errorf("Rollback response version = %q; want v1", svc.Version)
	}
	if svc.StablePort != 8100 {
		t.Errorf("Rollback response stable_port = %d; want 8100", svc.StablePort)
	}
}

// ---------- Remove ----------

func TestRemove_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Remove: method = %s; want POST", r.Method)
		}
		if r.URL.Path != "/remove/old-svc" {
			t.Errorf("Remove: path = %s; want /remove/old-svc", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := newTestClient(ts)
	if err := c.Remove("old-svc"); err != nil {
		t.Fatalf("Remove: unexpected error: %v", err)
	}
}

// ---------- DaemonVersion ----------

func TestDaemonVersion_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("DaemonVersion: method = %s; want GET", r.Method)
		}
		if r.URL.Path != "/health" {
			t.Errorf("DaemonVersion: path = %s; want /health", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"version": "v0.5.0",
		})
	}))
	defer ts.Close()

	c := newTestClient(ts)
	ver, err := c.DaemonVersion()
	if err != nil {
		t.Fatalf("DaemonVersion: unexpected error: %v", err)
	}
	if ver != "v0.5.0" {
		t.Errorf("DaemonVersion = %q; want %q", ver, "v0.5.0")
	}
}

func TestDaemonVersion_NoVersionKey(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer ts.Close()

	c := newTestClient(ts)
	ver, err := c.DaemonVersion()
	if err != nil {
		t.Fatalf("DaemonVersion: unexpected error: %v", err)
	}
	if ver != "" {
		t.Errorf("DaemonVersion = %q; want empty string", ver)
	}
}

// ---------- Logs ----------

func TestLogs_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Logs: method = %s; want GET", r.Method)
		}
		if r.URL.Path != "/logs/my-svc" {
			t.Errorf("Logs: path = %s; want /logs/my-svc", r.URL.Path)
		}
		if r.URL.Query().Get("lines") != "50" {
			t.Errorf("Logs: lines param = %s; want 50", r.URL.Query().Get("lines"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]string{
			"2026/03/16 11:51:34 [STARTUP] booting",
			"2026/03/16 11:51:35 [DEPLOY] name=my-svc port=3000",
		})
	}))
	defer ts.Close()

	c := newTestClient(ts)
	lines, err := c.Logs("my-svc", 50)
	if err != nil {
		t.Fatalf("Logs: unexpected error: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("Logs: len = %d; want 2", len(lines))
	}
	if !strings.Contains(lines[1], "[DEPLOY]") {
		t.Errorf("Logs[1] = %q; expected to contain [DEPLOY]", lines[1])
	}
}

func TestLogs_DaemonLog(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/logs/~daemon" {
			t.Errorf("Logs daemon: path = %s; want /logs/~daemon", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]string{"daemon log line"})
	}))
	defer ts.Close()

	c := newTestClient(ts)
	lines, err := c.Logs("~daemon", 100)
	if err != nil {
		t.Fatalf("Logs daemon: unexpected error: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("Logs daemon: len = %d; want 1", len(lines))
	}
}

// ---------- LogsFollow ----------

func TestLogsFollow_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("LogsFollow: method = %s; want GET", r.Method)
		}
		if r.URL.Path != "/logs/my-svc" {
			t.Errorf("LogsFollow: path = %s; want /logs/my-svc", r.URL.Path)
		}
		if r.URL.Query().Get("follow") != "true" {
			t.Errorf("LogsFollow: follow param = %s; want true", r.URL.Query().Get("follow"))
		}
		if r.URL.Query().Get("lines") != "10" {
			t.Errorf("LogsFollow: lines param = %s; want 10", r.URL.Query().Get("lines"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, "data: first line")
		fmt.Fprintln(w, "data: second line")
		fmt.Fprintln(w, ": heartbeat")
		fmt.Fprintln(w, "data: third line")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer ts.Close()

	c := newTestClient(ts)
	var buf bytes.Buffer
	err := c.LogsFollow("my-svc", 10, &buf)
	if err != nil {
		t.Fatalf("LogsFollow: unexpected error: %v", err)
	}

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 3 {
		t.Fatalf("LogsFollow: got %d lines; want 3\noutput: %q", len(lines), output)
	}
	if lines[0] != "first line" {
		t.Errorf("LogsFollow line 0 = %q; want %q", lines[0], "first line")
	}
	if lines[2] != "third line" {
		t.Errorf("LogsFollow line 2 = %q; want %q", lines[2], "third line")
	}
}

func TestLogsFollow_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("service not found"))
	}))
	defer ts.Close()

	c := newTestClient(ts)
	var buf bytes.Buffer
	err := c.LogsFollow("no-such-svc", 10, &buf)
	if err == nil {
		t.Fatal("LogsFollow: expected error for 404, got nil")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("LogsFollow error = %q; want to contain '404'", err.Error())
	}
}

// ---------- Issues ----------

func TestIssues_Success(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	payload := struct {
		Issues []issues.Issue `json:"issues"`
	}{
		Issues: []issues.Issue{
			{ID: "abc123", Timestamp: now, Source: "mcp:anito_deploy", Error: "binary not found", Severity: "error"},
			{ID: "def456", Timestamp: now, Source: "cli:deploy", Error: "port conflict", Severity: "warning"},
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Issues: method = %s; want GET", r.Method)
		}
		if r.URL.Path != "/issues" {
			t.Errorf("Issues: path = %s; want /issues", r.URL.Path)
		}
		if r.URL.Query().Get("lines") != "20" {
			t.Errorf("Issues: lines param = %s; want 20", r.URL.Query().Get("lines"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(payload)
	}))
	defer ts.Close()

	c := newTestClient(ts)
	got, err := c.Issues(20, "")
	if err != nil {
		t.Fatalf("Issues: unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Issues: len = %d; want 2", len(got))
	}
	if got[0].ID != "abc123" {
		t.Errorf("Issues[0].ID = %q; want %q", got[0].ID, "abc123")
	}
	if got[1].Severity != "warning" {
		t.Errorf("Issues[1].Severity = %q; want %q", got[1].Severity, "warning")
	}
}

func TestIssues_WithSourceFilter(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("source") != "mcp:" {
			t.Errorf("Issues: source param = %q; want %q", r.URL.Query().Get("source"), "mcp:")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Issues []issues.Issue `json:"issues"`
		}{Issues: []issues.Issue{}})
	}))
	defer ts.Close()

	c := newTestClient(ts)
	_, err := c.Issues(10, "mcp:")
	if err != nil {
		t.Fatalf("Issues with source filter: unexpected error: %v", err)
	}
}

func TestIssues_NoSourceParam(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// When source is empty, it should not appear in the query string.
		if r.URL.Query().Has("source") {
			t.Errorf("Issues: source param present but should be absent when empty")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Issues []issues.Issue `json:"issues"`
		}{Issues: []issues.Issue{}})
	}))
	defer ts.Close()

	c := newTestClient(ts)
	_, err := c.Issues(10, "")
	if err != nil {
		t.Fatalf("Issues no source: unexpected error: %v", err)
	}
}

// ---------- Report ----------

func TestReport_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Report: method = %s; want POST", r.Method)
		}
		if r.URL.Path != "/issues" {
			t.Errorf("Report: path = %s; want /issues", r.URL.Path)
		}

		var iss issues.Issue
		if err := json.NewDecoder(r.Body).Decode(&iss); err != nil {
			t.Fatalf("Report: decode body: %v", err)
		}
		if iss.Error != "deploy failed" {
			t.Errorf("Report issue error = %q; want %q", iss.Error, "deploy failed")
		}
		if iss.Source != "consumer:my-app" {
			t.Errorf("Report issue source = %q; want %q", iss.Source, "consumer:my-app")
		}
		if iss.Tool != "anito_deploy" {
			t.Errorf("Report issue tool = %q; want %q", iss.Tool, "anito_deploy")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := newTestClient(ts)
	err := c.Report(issues.Issue{
		Error:    "deploy failed",
		Source:   "consumer:my-app",
		Tool:     "anito_deploy",
		Severity: "error",
		Context:  "service crashed on startup",
	})
	if err != nil {
		t.Fatalf("Report: unexpected error: %v", err)
	}
}

// ---------- Teardown ----------

func TestTeardown_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Teardown: method = %s; want POST", r.Method)
		}
		if r.URL.Path != "/teardown" {
			t.Errorf("Teardown: path = %s; want /teardown", r.URL.Path)
		}

		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Teardown: decode body: %v", err)
		}
		if body["repo_path"] != "/Users/me/my-repo" {
			t.Errorf("Teardown repo_path = %q; want %q", body["repo_path"], "/Users/me/my-repo")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"removed": []string{"svc-a", "svc-b"},
			"count":   2,
		})
	}))
	defer ts.Close()

	c := newTestClient(ts)
	removed, err := c.Teardown("/Users/me/my-repo")
	if err != nil {
		t.Fatalf("Teardown: unexpected error: %v", err)
	}
	if len(removed) != 2 {
		t.Fatalf("Teardown: len(removed) = %d; want 2", len(removed))
	}
	if removed[0] != "svc-a" || removed[1] != "svc-b" {
		t.Errorf("Teardown removed = %v; want [svc-a svc-b]", removed)
	}
}

func TestTeardown_NoneRemoved(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"removed": []string{},
			"count":   0,
		})
	}))
	defer ts.Close()

	c := newTestClient(ts)
	removed, err := c.Teardown("/Users/me/empty-repo")
	if err != nil {
		t.Fatalf("Teardown: unexpected error: %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("Teardown: len(removed) = %d; want 0", len(removed))
	}
}

// ---------- Error handling ----------

func TestError_400(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, "invalid service name")
	}))
	defer ts.Close()

	c := newTestClient(ts)
	_, err := c.Status("bad!name")
	if err == nil {
		t.Fatal("expected error for 400 response, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error = %q; want to contain '400'", err.Error())
	}
	if !strings.Contains(err.Error(), "invalid service name") {
		t.Errorf("error = %q; want to contain 'invalid service name'", err.Error())
	}
}

func TestError_500(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "internal error: registry corrupted")
	}))
	defer ts.Close()

	c := newTestClient(ts)
	err := c.Stop("any-svc")
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %q; want to contain '500'", err.Error())
	}
	if !strings.Contains(err.Error(), "internal error: registry corrupted") {
		t.Errorf("error = %q; want to contain body text", err.Error())
	}
}

func TestError_404_Deploy(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, "service not found")
	}))
	defer ts.Close()

	c := newTestClient(ts)
	_, err := c.Deploy(DeployRequest{Name: "ghost"})
	if err == nil {
		t.Fatal("expected error for 404 response, got nil")
	}
	if !strings.Contains(err.Error(), "daemon error 404") {
		t.Errorf("error = %q; want to contain 'daemon error 404'", err.Error())
	}
}

func TestError_EmptyBody_FallsBackToStatusText(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		// No body written.
	}))
	defer ts.Close()

	c := newTestClient(ts)
	_, err := c.Services()
	if err == nil {
		t.Fatal("expected error for 503 response, got nil")
	}
	// parseError falls back to resp.Status when body is empty.
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error = %q; want to contain '503'", err.Error())
	}
	if !strings.Contains(err.Error(), "Service Unavailable") {
		t.Errorf("error = %q; want to contain 'Service Unavailable'", err.Error())
	}
}

func TestError_ConnectionRefused(t *testing.T) {
	// Point at a port where nothing is listening.
	c := New(19999)
	_, err := c.Services()
	if err == nil {
		t.Fatal("expected error for connection refused, got nil")
	}
	if !strings.Contains(err.Error(), "daemon unreachable") {
		t.Errorf("error = %q; want to contain 'daemon unreachable'", err.Error())
	}
}

func TestError_ConnectionRefused_Post(t *testing.T) {
	c := New(19999)
	err := c.Stop("any")
	if err == nil {
		t.Fatal("expected error for connection refused on POST, got nil")
	}
	if !strings.Contains(err.Error(), "daemon unreachable") {
		t.Errorf("error = %q; want to contain 'daemon unreachable'", err.Error())
	}
}

func TestError_ConnectionRefused_LogsFollow(t *testing.T) {
	c := New(19999)
	var buf bytes.Buffer
	err := c.LogsFollow("any", 10, &buf)
	if err == nil {
		t.Fatal("expected error for connection refused on LogsFollow, got nil")
	}
	if !strings.Contains(err.Error(), "daemon unreachable") {
		t.Errorf("error = %q; want to contain 'daemon unreachable'", err.Error())
	}
}

// ---------- parseError ----------

func TestParseError_WithBody(t *testing.T) {
	resp := &http.Response{
		StatusCode: 422,
		Status:     "422 Unprocessable Entity",
		Body:       io.NopCloser(strings.NewReader("port already in use")),
	}
	err := parseError(resp)
	if err == nil {
		t.Fatal("parseError: expected error, got nil")
	}
	want := "daemon error 422: port already in use"
	if err.Error() != want {
		t.Errorf("parseError = %q; want %q", err.Error(), want)
	}
}

func TestParseError_EmptyBody(t *testing.T) {
	resp := &http.Response{
		StatusCode: 502,
		Status:     "502 Bad Gateway",
		Body:       io.NopCloser(strings.NewReader("")),
	}
	err := parseError(resp)
	if err == nil {
		t.Fatal("parseError: expected error, got nil")
	}
	want := "daemon error 502: 502 Bad Gateway"
	if err.Error() != want {
		t.Errorf("parseError = %q; want %q", err.Error(), want)
	}
}

// ---------- Error propagation for all methods ----------

func TestRestart_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		io.WriteString(w, "service is already restarting")
	}))
	defer ts.Close()

	c := newTestClient(ts)
	err := c.Restart("busy-svc")
	if err == nil {
		t.Fatal("Restart: expected error for 409, got nil")
	}
	if !strings.Contains(err.Error(), "409") {
		t.Errorf("Restart error = %q; want to contain '409'", err.Error())
	}
}

func TestRemove_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, "service not found")
	}))
	defer ts.Close()

	c := newTestClient(ts)
	err := c.Remove("ghost")
	if err == nil {
		t.Fatal("Remove: expected error for 404, got nil")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("Remove error = %q; want to contain '404'", err.Error())
	}
}

func TestReport_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "disk full")
	}))
	defer ts.Close()

	c := newTestClient(ts)
	err := c.Report(issues.Issue{Error: "test", Source: "test"})
	if err == nil {
		t.Fatal("Report: expected error for 500, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("Report error = %q; want to contain '500'", err.Error())
	}
}

func TestTeardown_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, "missing deployed.json")
	}))
	defer ts.Close()

	c := newTestClient(ts)
	_, err := c.Teardown("/bad/path")
	if err == nil {
		t.Fatal("Teardown: expected error for 400, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("Teardown error = %q; want to contain '400'", err.Error())
	}
}

func TestDaemonVersion_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "daemon panicked")
	}))
	defer ts.Close()

	c := newTestClient(ts)
	_, err := c.DaemonVersion()
	if err == nil {
		t.Fatal("DaemonVersion: expected error for 500, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("DaemonVersion error = %q; want to contain '500'", err.Error())
	}
}

func TestLogs_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, "no such service")
	}))
	defer ts.Close()

	c := newTestClient(ts)
	_, err := c.Logs("ghost", 100)
	if err == nil {
		t.Fatal("Logs: expected error for 404, got nil")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("Logs error = %q; want to contain '404'", err.Error())
	}
}

// ---------- Deploy with optional fields ----------

func TestDeploy_WithWatchPathsAndEnvFile(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req DeployRequest
		json.NewDecoder(r.Body).Decode(&req)
		if len(req.WatchPaths) != 2 {
			t.Errorf("Deploy: WatchPaths len = %d; want 2", len(req.WatchPaths))
		}
		if req.EnvFile != "/tmp/.env" {
			t.Errorf("Deploy: EnvFile = %q; want /tmp/.env", req.EnvFile)
		}
		if req.HealthCheck != "/healthz" {
			t.Errorf("Deploy: HealthCheck = %q; want /healthz", req.HealthCheck)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(registry.Service{
			Name:       req.Name,
			StablePort: 8100,
			Status:     registry.StatusRunning,
		})
	}))
	defer ts.Close()

	c := newTestClient(ts)
	svc, err := c.Deploy(DeployRequest{
		Name:        "watch-svc",
		Path:        "/tmp/watch-svc",
		WatchPaths:  []string{"/src", "/cmd"},
		EnvFile:     "/tmp/.env",
		HealthCheck: "/healthz",
	})
	if err != nil {
		t.Fatalf("Deploy: unexpected error: %v", err)
	}
	if svc.Name != "watch-svc" {
		t.Errorf("Deploy response name = %q; want %q", svc.Name, "watch-svc")
	}
}

func TestDeploy_WithDurationStrings(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw map[string]any
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if raw["drain_window"] != "5s" {
			t.Fatalf("drain_window = %#v, want 5s", raw["drain_window"])
		}
		if raw["health_check_timeout"] != "30s" {
			t.Fatalf("health_check_timeout = %#v, want 30s", raw["health_check_timeout"])
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(registry.Service{
			Name:       "timed-svc",
			StablePort: 8100,
			Status:     registry.StatusRunning,
		})
	}))
	defer ts.Close()

	c := newTestClient(ts)
	if _, err := c.Deploy(DeployRequest{
		Name:               "timed-svc",
		Path:               "/tmp/timed-svc",
		DrainWindow:        "5s",
		HealthCheckTimeout: "30s",
	}); err != nil {
		t.Fatalf("Deploy: unexpected error: %v", err)
	}
}
