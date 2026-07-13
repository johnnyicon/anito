package issues

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	want := filepath.Join(dir, "issues.jsonl")
	if s.path != want {
		t.Fatalf("New(%q).path = %q, want %q", dir, s.path, want)
	}
}

func TestAppendCreatesFile(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	if err := s.Append(Issue{Error: "boom"}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	path := filepath.Join(dir, "issues.jsonl")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}

func TestAppendAutoGeneratesID(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	if err := s.Append(Issue{Error: "no id"}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	issues, err := s.Recent(0, "")
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1", len(issues))
	}
	if issues[0].ID == "" {
		t.Fatal("expected auto-generated ID, got empty string")
	}
}

func TestAppendAutoSetsTimestamp(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	before := time.Now()
	if err := s.Append(Issue{Error: "no ts"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	after := time.Now()

	issues, err := s.Recent(0, "")
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1", len(issues))
	}

	ts := issues[0].Timestamp
	if ts.Before(before) || ts.After(after) {
		t.Fatalf("auto-set Timestamp %v not in [%v, %v]", ts, before, after)
	}
}

func TestAppendDefaultsSeverityToError(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	if err := s.Append(Issue{Error: "no sev"}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	issues, err := s.Recent(0, "")
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1", len(issues))
	}
	if issues[0].Severity != "error" {
		t.Fatalf("Severity = %q, want %q", issues[0].Severity, "error")
	}
}

func TestAppendPreservesCallerValues(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	ts := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	iss := Issue{
		ID:        "custom-id",
		Timestamp: ts,
		Severity:  "warning",
		Error:     "caller provided",
	}

	if err := s.Append(iss); err != nil {
		t.Fatalf("Append: %v", err)
	}

	issues, err := s.Recent(0, "")
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1", len(issues))
	}

	got := issues[0]
	if got.ID != "custom-id" {
		t.Errorf("ID = %q, want %q", got.ID, "custom-id")
	}
	if !got.Timestamp.Equal(ts) {
		t.Errorf("Timestamp = %v, want %v", got.Timestamp, ts)
	}
	if got.Severity != "warning" {
		t.Errorf("Severity = %q, want %q", got.Severity, "warning")
	}
}

func TestAppendMultipleIssuesAsSeparateLines(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	for i := 0; i < 3; i++ {
		if err := s.Append(Issue{Error: "err"}); err != nil {
			t.Fatalf("Append #%d: %v", i, err)
		}
	}

	data, err := os.ReadFile(filepath.Join(dir, "issues.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d JSONL lines, want 3", len(lines))
	}

	for i, line := range lines {
		var iss Issue
		if err := json.Unmarshal([]byte(line), &iss); err != nil {
			t.Errorf("line %d: invalid JSON: %v", i, err)
		}
	}
}

func TestRecentAllWhenNLeZero(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	for i := 0; i < 5; i++ {
		if err := s.Append(Issue{Error: "err"}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	issues, err := s.Recent(0, "")
	if err != nil {
		t.Fatalf("Recent(0): %v", err)
	}
	if len(issues) != 5 {
		t.Fatalf("Recent(0) returned %d issues, want 5", len(issues))
	}

	issues, err = s.Recent(-1, "")
	if err != nil {
		t.Fatalf("Recent(-1): %v", err)
	}
	if len(issues) != 5 {
		t.Fatalf("Recent(-1) returned %d issues, want 5", len(issues))
	}
}

func TestRecentReturnsLastN(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	for i := 0; i < 5; i++ {
		if err := s.Append(Issue{ID: strings.Repeat("x", i+1), Error: "err"}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	issues, err := s.Recent(2, "")
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("got %d issues, want 2", len(issues))
	}

	// Should be the last two appended (IDs "xxxx" and "xxxxx").
	if issues[0].ID != "xxxx" {
		t.Errorf("issues[0].ID = %q, want %q", issues[0].ID, "xxxx")
	}
	if issues[1].ID != "xxxxx" {
		t.Errorf("issues[1].ID = %q, want %q", issues[1].ID, "xxxxx")
	}
}

func TestRecentFiltersBySourcePrefix(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	sources := []string{"mcp:anito_deploy", "cli:deploy", "mcp:anito_logs", "consumer:sogs-api"}
	for _, src := range sources {
		if err := s.Append(Issue{Source: src, Error: "err"}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	issues, err := s.Recent(0, "mcp:")
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("got %d issues for source prefix 'mcp:', want 2", len(issues))
	}
	for _, iss := range issues {
		if !strings.HasPrefix(iss.Source, "mcp:") {
			t.Errorf("unexpected source %q, want prefix 'mcp:'", iss.Source)
		}
	}
}

func TestRecentEmptySourceReturnsAll(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	sources := []string{"mcp:x", "cli:y", "consumer:z"}
	for _, src := range sources {
		if err := s.Append(Issue{Source: src, Error: "err"}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	issues, err := s.Recent(0, "")
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(issues) != 3 {
		t.Fatalf("got %d issues, want 3", len(issues))
	}
}

func TestRecentNonExistentFileReturnsNil(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	issues, err := s.Recent(10, "")
	if err != nil {
		t.Fatalf("Recent on missing file: unexpected error %v", err)
	}
	if issues != nil {
		t.Fatalf("expected nil, got %v", issues)
	}
}

func TestRecentSkipsEmptyLines(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	if err := s.Append(Issue{Error: "first"}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Inject empty lines into the file.
	path := filepath.Join(dir, "issues.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open for append: %v", err)
	}
	f.WriteString("\n\n")
	f.Close()

	if err := s.Append(Issue{Error: "second"}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	issues, err := s.Recent(0, "")
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("got %d issues, want 2 (empty lines should be skipped)", len(issues))
	}
}

func TestRoundTripPreservesAllFields(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	ts := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	original := Issue{
		ID:        "round-trip-id",
		Timestamp: ts,
		Source:    "consumer:test-service",
		Tool:      "anito_deploy",
		Input:     `{"name":"test"}`,
		Error:     "something broke",
		Context:   "deploying from CI",
		RepoPath:  "/Users/test/my-repo",
		Severity:  "info",
	}

	if err := s.Append(original); err != nil {
		t.Fatalf("Append: %v", err)
	}

	issues, err := s.Recent(0, "")
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1", len(issues))
	}

	got := issues[0]
	if got.ID != original.ID {
		t.Errorf("ID = %q, want %q", got.ID, original.ID)
	}
	if !got.Timestamp.Equal(original.Timestamp) {
		t.Errorf("Timestamp = %v, want %v", got.Timestamp, original.Timestamp)
	}
	if got.Source != original.Source {
		t.Errorf("Source = %q, want %q", got.Source, original.Source)
	}
	if got.Tool != original.Tool {
		t.Errorf("Tool = %q, want %q", got.Tool, original.Tool)
	}
	if got.Input != original.Input {
		t.Errorf("Input = %q, want %q", got.Input, original.Input)
	}
	if got.Error != original.Error {
		t.Errorf("Error = %q, want %q", got.Error, original.Error)
	}
	if got.Context != original.Context {
		t.Errorf("Context = %q, want %q", got.Context, original.Context)
	}
	if got.RepoPath != original.RepoPath {
		t.Errorf("RepoPath = %q, want %q", got.RepoPath, original.RepoPath)
	}
	if got.Severity != original.Severity {
		t.Errorf("Severity = %q, want %q", got.Severity, original.Severity)
	}
}

func TestClearRemovesAllIssues(t *testing.T) {
	s := New(t.TempDir())
	if err := s.Append(Issue{Error: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(Issue{Error: "second"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	got, err := s.Recent(0, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("issues after Clear = %d, want 0", len(got))
	}
}
