package diagnosis

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/johnnyicon/anito/internal/domain"
	"github.com/johnnyicon/anito/internal/registry"
)

type fakeStatus struct {
	services map[string]*registry.Service
	err      error
}

func (f fakeStatus) Status(name string) (*registry.Service, error) {
	if f.err != nil {
		return nil, f.err
	}
	svc, ok := f.services[name]
	if !ok {
		return nil, domain.MissingServicef("service %q not found", name)
	}
	return svc, nil
}

func TestRunClassifiesMissingService(t *testing.T) {
	result, err := Run(Request{ServiceName: "ghost"}, fakeStatus{services: map[string]*registry.Service{}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Healthy || result.Errors != 1 {
		t.Fatalf("healthy/errors = %v/%d, want false/1", result.Healthy, result.Errors)
	}
	if got := result.Findings[0].Code; got != domain.CodeMissingService {
		t.Fatalf("code = %q, want missing_service", got)
	}
}

func TestRunClassifiesReadinessFailure(t *testing.T) {
	result, err := Run(Request{ServiceName: "bad"}, fakeStatus{services: map[string]*registry.Service{
		"bad": {Name: "bad", Status: registry.StatusFailed, HealthCheck: "/health", GaveUp: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Findings[0].Code; got != domain.CodeReadinessFailure {
		t.Fatalf("code = %q, want readiness_failure", got)
	}
}

func TestRunClassifiesRepoConflict(t *testing.T) {
	dir := t.TempDir()
	anitoDir := filepath.Join(dir, ".anito")
	if err := os.MkdirAll(anitoDir, 0755); err != nil {
		t.Fatal(err)
	}
	port := startForeignHTTP(t)
	cfg := fmt.Sprintf("name: svc\nport: %d\ntype: binary\noutput: /bin/true\nhealth_check: /health\n", port)
	if err := os.WriteFile(filepath.Join(anitoDir, "config.yaml"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := Run(Request{RepoPath: dir}, fakeStatus{services: map[string]*registry.Service{}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Healthy {
		t.Fatal("expected unhealthy result")
	}
	found := false
	for _, finding := range result.Findings {
		if finding.Code == domain.CodeConflict {
			found = true
		}
	}
	if !found {
		t.Fatalf("findings = %+v, want conflict", result.Findings)
	}
}

func TestRunRedactsFindingMessages(t *testing.T) {
	result, err := Run(Request{ServiceName: "ghost"}, fakeStatus{
		err: domain.InvalidConfigf("bad env API_KEY=secret-value"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(result.Findings))
	}
	if contains(result.Findings[0].Message, "secret-value") {
		t.Fatalf("finding leaked secret: %q", result.Findings[0].Message)
	}
}

func startForeignHTTP(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})}
	go srv.Serve(l) //nolint:errcheck
	t.Cleanup(func() { _ = srv.Close() })
	return port
}

func contains(s, sub string) bool {
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
