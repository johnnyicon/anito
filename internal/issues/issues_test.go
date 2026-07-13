package issues

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestAppendCreatesVersionedStoreFile(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	if err := s.Append(Issue{Error: "boom"}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	path := filepath.Join(dir, "issues.jsonl")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}

	file := readStoreFile(t, path)
	if file.Version != currentStoreVersion {
		t.Fatalf("store version = %d, want %d", file.Version, currentStoreVersion)
	}
	if len(file.Issues) != 1 {
		t.Fatalf("stored issues = %d, want 1", len(file.Issues))
	}
}

func TestAppendDefaultsCompatibilityFields(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	before := time.Now()
	if err := s.Append(Issue{Error: "no defaults"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	after := time.Now()

	got := recentAll(t, s)
	if len(got) != 1 {
		t.Fatalf("got %d issues, want 1", len(got))
	}

	iss := got[0]
	if iss.ID == "" {
		t.Fatal("expected auto-generated issue ID")
	}
	if iss.Timestamp.Before(before) || iss.Timestamp.After(after) {
		t.Fatalf("Timestamp %v not in [%v, %v]", iss.Timestamp, before, after)
	}
	if iss.Severity != "error" {
		t.Fatalf("Severity = %q, want error", iss.Severity)
	}
	if iss.FirstSeen.IsZero() || iss.LastSeen.IsZero() {
		t.Fatalf("expected first/last seen timestamps to be set: %+v", iss)
	}
	if iss.OccurrenceCount != 1 {
		t.Fatalf("OccurrenceCount = %d, want 1", iss.OccurrenceCount)
	}
	if len(iss.Occurrences) != 1 {
		t.Fatalf("Occurrences = %d, want 1", len(iss.Occurrences))
	}
	if iss.Occurrences[0].Error != "no defaults" {
		t.Fatalf("occurrence error = %q, want %q", iss.Occurrences[0].Error, "no defaults")
	}
}

func TestAppendPreservesCallerValues(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	ts := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	iss := Issue{
		ID:        "custom-id",
		Timestamp: ts,
		Source:    "consumer:test-service",
		Tool:      "report",
		Input:     `{"name":"test-service"}`,
		Error:     "caller provided",
		Context:   "custom context",
		RepoPath:  "/Users/test/my-repo",
		Severity:  "warning",
	}

	if err := s.Append(iss); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got := recentAll(t, s)
	if len(got) != 1 {
		t.Fatalf("got %d issues, want 1", len(got))
	}

	stored := got[0]
	if stored.ID != iss.ID {
		t.Errorf("ID = %q, want %q", stored.ID, iss.ID)
	}
	if !stored.Timestamp.Equal(ts) {
		t.Errorf("Timestamp = %v, want %v", stored.Timestamp, ts)
	}
	if stored.Source != iss.Source {
		t.Errorf("Source = %q, want %q", stored.Source, iss.Source)
	}
	if stored.Tool != iss.Tool {
		t.Errorf("Tool = %q, want %q", stored.Tool, iss.Tool)
	}
	if stored.Input != iss.Input {
		t.Errorf("Input = %q, want %q", stored.Input, iss.Input)
	}
	if stored.Error != iss.Error {
		t.Errorf("Error = %q, want %q", stored.Error, iss.Error)
	}
	if stored.Context != iss.Context {
		t.Errorf("Context = %q, want %q", stored.Context, iss.Context)
	}
	if stored.RepoPath != iss.RepoPath {
		t.Errorf("RepoPath = %q, want %q", stored.RepoPath, iss.RepoPath)
	}
	if stored.Severity != iss.Severity {
		t.Errorf("Severity = %q, want %q", stored.Severity, iss.Severity)
	}
	if stored.OccurrenceCount != 1 {
		t.Errorf("OccurrenceCount = %d, want 1", stored.OccurrenceCount)
	}
}

func TestAppendAggregatesEquivalentOccurrencesIgnoringVolatileData(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	ts1 := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	ts2 := ts1.Add(5 * time.Minute)
	err1 := `listen tcp 127.0.0.1:7011: bind: address already in use pid=111 at 2026-07-13T10:00:00Z temp=/var/folders/a1/T/go-build123/b001/exe/app`
	err2 := `listen tcp 127.0.0.1:7099: bind: address already in use pid=222 at 2026-07-13T10:05:00Z temp=/var/folders/z9/T/go-build999/b001/exe/app`

	first := Issue{
		ID:        "first-occurrence",
		Timestamp: ts1,
		Source:    "mcp:anito_deploy",
		Tool:      "anito_deploy",
		Input:     `{"name":"api"}`,
		Error:     err1,
		Context:   "first attempt",
		Severity:  "error",
	}
	second := Issue{
		ID:        "second-occurrence",
		Timestamp: ts2,
		Source:    "mcp:anito_deploy",
		Tool:      "anito_deploy",
		Input:     `{"name":"api"}`,
		Error:     err2,
		Context:   "second attempt",
		Severity:  "error",
	}

	if err := s.Append(first); err != nil {
		t.Fatalf("Append(first): %v", err)
	}
	if err := s.Append(second); err != nil {
		t.Fatalf("Append(second): %v", err)
	}

	got := recentAll(t, s)
	if len(got) != 1 {
		t.Fatalf("got %d issues, want 1", len(got))
	}

	iss := got[0]
	if iss.ID != first.ID {
		t.Fatalf("aggregate ID = %q, want first occurrence ID %q", iss.ID, first.ID)
	}
	if iss.Service != "api" {
		t.Fatalf("Service = %q, want api", iss.Service)
	}
	if iss.Kind != "anito_deploy" {
		t.Fatalf("Kind = %q, want anito_deploy", iss.Kind)
	}
	if iss.OccurrenceCount != 2 {
		t.Fatalf("OccurrenceCount = %d, want 2", iss.OccurrenceCount)
	}
	if !iss.FirstSeen.Equal(ts1) {
		t.Fatalf("FirstSeen = %v, want %v", iss.FirstSeen, ts1)
	}
	if !iss.LastSeen.Equal(ts2) {
		t.Fatalf("LastSeen = %v, want %v", iss.LastSeen, ts2)
	}
	if !iss.Timestamp.Equal(ts2) {
		t.Fatalf("Timestamp = %v, want most recent %v", iss.Timestamp, ts2)
	}
	if iss.Error != err2 {
		t.Fatalf("Error = %q, want latest raw error %q", iss.Error, err2)
	}
	if len(iss.Occurrences) != 2 {
		t.Fatalf("Occurrences = %d, want 2", len(iss.Occurrences))
	}
	if iss.Occurrences[0].Error != err1 || iss.Occurrences[1].Error != err2 {
		t.Fatalf("raw occurrence evidence was not preserved: %+v", iss.Occurrences)
	}
	if iss.Fingerprint == "" {
		t.Fatal("expected non-empty fingerprint")
	}
}

func TestAppendDoesNotMergeDifferentServiceOrSource(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	cases := []Issue{
		{
			Timestamp: time.Date(2026, 7, 13, 11, 0, 0, 0, time.UTC),
			Source:    "mcp:anito_deploy",
			Tool:      "anito_deploy",
			Input:     `{"name":"api"}`,
			Error:     "binary not found",
		},
		{
			Timestamp: time.Date(2026, 7, 13, 11, 1, 0, 0, time.UTC),
			Source:    "mcp:anito_deploy",
			Tool:      "anito_deploy",
			Input:     `{"name":"worker"}`,
			Error:     "binary not found",
		},
		{
			Timestamp: time.Date(2026, 7, 13, 11, 2, 0, 0, time.UTC),
			Source:    "cli:deploy",
			Tool:      "deploy",
			Input:     "api",
			Error:     "binary not found",
		},
	}

	for i, iss := range cases {
		if err := s.Append(iss); err != nil {
			t.Fatalf("Append(%d): %v", i, err)
		}
	}

	got := recentAll(t, s)
	if len(got) != 3 {
		t.Fatalf("got %d issues, want 3 distinct aggregates", len(got))
	}
	if got[0].Fingerprint == got[1].Fingerprint || got[1].Fingerprint == got[2].Fingerprint || got[0].Fingerprint == got[2].Fingerprint {
		t.Fatalf("expected distinct fingerprints, got %+v", got)
	}
}

func TestRecentReturnsLastNUniqueIssues(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	for i := 0; i < 5; i++ {
		if err := s.Append(Issue{
			ID:        fmt.Sprintf("id-%d", i),
			Timestamp: time.Date(2026, 7, 13, 12, i, 0, 0, time.UTC),
			Source:    "test:unit",
			Tool:      "unit",
			Error:     fmt.Sprintf("error-%d", i),
		}); err != nil {
			t.Fatalf("Append(%d): %v", i, err)
		}
	}

	got, err := s.Recent(2, "")
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d issues, want 2", len(got))
	}
	if got[0].ID != "id-3" {
		t.Fatalf("got[0].ID = %q, want id-3", got[0].ID)
	}
	if got[1].ID != "id-4" {
		t.Fatalf("got[1].ID = %q, want id-4", got[1].ID)
	}
}

func TestRecentFiltersBySourcePrefix(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	for _, iss := range []Issue{
		{Source: "mcp:anito_deploy", Tool: "anito_deploy", Error: "alpha", Input: `{"name":"svc-a"}`},
		{Source: "cli:deploy", Tool: "deploy", Error: "beta", Input: "svc-b"},
		{Source: "mcp:anito_logs", Tool: "anito_logs", Error: "gamma", Input: `{"name":"svc-c"}`},
	} {
		if err := s.Append(iss); err != nil {
			t.Fatalf("Append(%q): %v", iss.Source, err)
		}
	}

	got, err := s.Recent(0, "mcp:")
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d issues, want 2", len(got))
	}
	for _, iss := range got {
		if !strings.HasPrefix(iss.Source, "mcp:") {
			t.Fatalf("unexpected source %q", iss.Source)
		}
	}
}

func TestRecentNonExistentFileReturnsNil(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	got, err := s.Recent(10, "")
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestRecentReadsLegacyJSONLAndAggregatesOccurrences(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	path := filepath.Join(dir, "issues.jsonl")

	writeLegacyJSONL(t, path, []Issue{
		{
			ID:        "legacy-1",
			Timestamp: time.Date(2026, 7, 13, 13, 0, 0, 0, time.UTC),
			Source:    "consumer:api",
			Tool:      "report",
			Error:     "worker pid=111 exited from /tmp/run-a at 2026-07-13T13:00:00Z",
			Severity:  "error",
		},
		{
			ID:        "legacy-2",
			Timestamp: time.Date(2026, 7, 13, 13, 1, 0, 0, time.UTC),
			Source:    "consumer:api",
			Tool:      "report",
			Error:     "worker pid=222 exited from /tmp/run-b at 2026-07-13T13:01:00Z",
			Severity:  "error",
		},
		{
			ID:        "legacy-3",
			Timestamp: time.Date(2026, 7, 13, 13, 2, 0, 0, time.UTC),
			Source:    "consumer:web",
			Tool:      "report",
			Error:     "worker pid=333 exited from /tmp/run-c at 2026-07-13T13:02:00Z",
			Severity:  "error",
		},
	})

	got := recentAll(t, s)
	if len(got) != 2 {
		t.Fatalf("got %d issues, want 2 aggregates", len(got))
	}
	if got[0].ID != "legacy-1" {
		t.Fatalf("first aggregate ID = %q, want legacy-1", got[0].ID)
	}
	if got[0].OccurrenceCount != 2 {
		t.Fatalf("first aggregate OccurrenceCount = %d, want 2", got[0].OccurrenceCount)
	}
	if got[1].OccurrenceCount != 1 {
		t.Fatalf("second aggregate OccurrenceCount = %d, want 1", got[1].OccurrenceCount)
	}
}

func TestAppendMigratesLegacyJSONLToVersionedStore(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	path := filepath.Join(dir, "issues.jsonl")

	writeLegacyJSONL(t, path, []Issue{
		{
			ID:        "legacy-1",
			Timestamp: time.Date(2026, 7, 13, 14, 0, 0, 0, time.UTC),
			Source:    "mcp:anito_deploy",
			Tool:      "anito_deploy",
			Input:     `{"name":"api"}`,
			Error:     "listen tcp 127.0.0.1:7011: bind: address already in use pid=111",
			Severity:  "error",
		},
	})

	if err := s.Append(Issue{
		ID:        "legacy-2",
		Timestamp: time.Date(2026, 7, 13, 14, 1, 0, 0, time.UTC),
		Source:    "mcp:anito_deploy",
		Tool:      "anito_deploy",
		Input:     `{"name":"api"}`,
		Error:     "listen tcp 127.0.0.1:7099: bind: address already in use pid=222",
		Severity:  "error",
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	file := readStoreFile(t, path)
	if file.Version != currentStoreVersion {
		t.Fatalf("store version = %d, want %d", file.Version, currentStoreVersion)
	}
	if len(file.Issues) != 1 {
		t.Fatalf("stored issues = %d, want 1", len(file.Issues))
	}
	if file.Issues[0].OccurrenceCount != 2 {
		t.Fatalf("OccurrenceCount = %d, want 2", file.Issues[0].OccurrenceCount)
	}
}

func TestRecentLoadsVersionedIssueWithoutOccurrences(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	ts := time.Date(2026, 7, 13, 15, 0, 0, 0, time.UTC)
	data, err := json.MarshalIndent(storeFile{
		Version: currentStoreVersion,
		Issues: []Issue{
			{
				ID:        "compat-id",
				Timestamp: ts,
				Source:    "test:compat",
				Tool:      "compat",
				Error:     "compat error",
				Severity:  "warning",
			},
		},
	}, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "issues.jsonl"), append(data, '\n'), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got := recentAll(t, s)
	if len(got) != 1 {
		t.Fatalf("got %d issues, want 1", len(got))
	}
	if got[0].OccurrenceCount != 1 {
		t.Fatalf("OccurrenceCount = %d, want 1", got[0].OccurrenceCount)
	}
	if len(got[0].Occurrences) != 1 {
		t.Fatalf("Occurrences = %d, want 1", len(got[0].Occurrences))
	}
	if got[0].Occurrences[0].ID != "compat-id" {
		t.Fatalf("occurrence ID = %q, want compat-id", got[0].Occurrences[0].ID)
	}
}

func TestClearRemovesAllIssues(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	if err := s.Append(Issue{Error: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(Issue{Error: "second"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}

	got := recentAll(t, s)
	if got != nil {
		t.Fatalf("issues after Clear = %v, want nil", got)
	}

	file := readStoreFile(t, filepath.Join(dir, "issues.jsonl"))
	if len(file.Issues) != 0 {
		t.Fatalf("stored issues after Clear = %d, want 0", len(file.Issues))
	}
}

func TestAppendConcurrentSameFingerprint(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	const goroutines = 12
	const perGoroutine = 25

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				err := s.Append(Issue{
					Source:   "mcp:anito_deploy",
					Tool:     "anito_deploy",
					Input:    `{"name":"api"}`,
					Error:    fmt.Sprintf("listen tcp 127.0.0.1:%d: bind: address already in use pid=%d", 7000+i, 1000+g*perGoroutine+i),
					Severity: "error",
				})
				if err != nil {
					t.Errorf("Append: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	got := recentAll(t, s)
	if len(got) != 1 {
		t.Fatalf("got %d issues, want 1", len(got))
	}
	wantCount := goroutines * perGoroutine
	if got[0].OccurrenceCount != wantCount {
		t.Fatalf("OccurrenceCount = %d, want %d", got[0].OccurrenceCount, wantCount)
	}
	if len(got[0].Occurrences) != wantCount {
		t.Fatalf("Occurrences = %d, want %d", len(got[0].Occurrences), wantCount)
	}

	file := readStoreFile(t, filepath.Join(dir, "issues.jsonl"))
	if len(file.Issues) != 1 || file.Issues[0].OccurrenceCount != wantCount {
		t.Fatalf("invalid persisted store after concurrent appends: %+v", file)
	}
}

func recentAll(t *testing.T, s *Store) []Issue {
	t.Helper()
	got, err := s.Recent(0, "")
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	return got
}

func readStoreFile(t *testing.T, path string) storeFile {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	var file storeFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("Unmarshal(%s): %v\n%s", path, err, data)
	}
	return file
}

func writeLegacyJSONL(t *testing.T, path string, records []Issue) {
	t.Helper()
	lines := make([]string, 0, len(records))
	for _, record := range records {
		data, err := json.Marshal(record)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		lines = append(lines, string(data))
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}
