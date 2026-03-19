// Package issues provides an append-only JSONL store for Anito runtime issues.
// Issues are logged automatically when MCP tools or HTTP API handlers fail,
// and can be logged manually by consuming repos via POST /issues or anito_report.
package issues

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Issue is one logged event.
type Issue struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"`              // "mcp:anito_deploy" | "cli:deploy" | "consumer:sogs-api"
	Tool      string    `json:"tool,omitempty"`      // "anito_deploy" | "deploy" | etc.
	Input     string    `json:"input,omitempty"`     // JSON-encoded inputs (no secrets)
	Error     string    `json:"error"`               // error message
	Context   string    `json:"context,omitempty"`   // free-text context from the caller
	RepoPath  string    `json:"repo_path,omitempty"` // optional — consuming repo root path
	Severity  string    `json:"severity"`            // "error" | "warning" | "info"
}

// Store is a thread-safe append-only JSONL issue log.
type Store struct {
	mu   sync.Mutex
	path string
}

// New returns a Store backed by <dataDir>/issues.jsonl.
// The file is created on first write; the directory must already exist.
func New(dataDir string) *Store {
	return &Store{path: filepath.Join(dataDir, "issues.jsonl")}
}

// Append adds an issue to the log. ID and Timestamp are set automatically
// if not provided by the caller.
func (s *Store) Append(iss Issue) error {
	if iss.ID == "" {
		iss.ID = strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	if iss.Timestamp.IsZero() {
		iss.Timestamp = time.Now()
	}
	if iss.Severity == "" {
		iss.Severity = "error"
	}

	b, err := json.Marshal(iss)
	if err != nil {
		return fmt.Errorf("issues: marshal: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("issues: open %s: %w", s.path, err)
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s\n", b)
	return err
}

// Recent returns the last n issues from the log, optionally filtered by a
// source prefix (e.g. "mcp:", "consumer:", "cli:"). Pass source="" to return
// all sources. Pass n<=0 to return all issues.
func (s *Store) Recent(n int, source string) ([]Issue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("issues: open %s: %w", s.path, err)
	}
	defer f.Close()

	var all []Issue
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var iss Issue
		if err := json.Unmarshal(line, &iss); err == nil {
			if source == "" || strings.HasPrefix(iss.Source, source) {
				all = append(all, iss)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("issues: scan: %w", err)
	}

	if n <= 0 || n >= len(all) {
		return all, nil
	}
	return all[len(all)-n:], nil
}
