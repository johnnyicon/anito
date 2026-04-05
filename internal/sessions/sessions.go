// Package sessions provides a persistent store for MCP session tracking.
// Sessions are written to <dataDir>/sessions.json and survive daemon restarts.
// Stale sessions (no activity for maxAge) are pruned on startup.
package sessions

import (
	"encoding/json"
	"os"
	"sort"
	"sync"
	"time"
)

// Session is one tracked MCP client session.
type Session struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	CallCount  int64     `json:"call_count"`
	LastTool   string    `json:"last_tool,omitempty"`
}

// Store is a thread-safe persistent session registry.
type Store struct {
	mu   sync.Mutex
	path string
}

// New returns a Store backed by <dataDir>/sessions.json.
func New(dataDir string) *Store {
	return &Store{path: dataDir + "/sessions.json"}
}

// Create records a newly initialised session.
func (s *Store) Create(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, _ := s.load()
	if m == nil {
		m = make(map[string]Session)
	}
	now := time.Now()
	m[id] = Session{ID: id, CreatedAt: now, LastSeenAt: now}
	return s.save(m)
}

// Touch updates the last-seen timestamp and increments the call counter.
// tool is the MCP tool name ("anito_deploy" etc.); pass "" if unknown.
func (s *Store) Touch(id, tool string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, _ := s.load()
	if m == nil {
		m = make(map[string]Session)
	}
	sess := m[id]
	if sess.ID == "" {
		// First touch for an unknown session — create it now.
		sess = Session{ID: id, CreatedAt: time.Now()}
	}
	sess.LastSeenAt = time.Now()
	sess.CallCount++
	if tool != "" {
		sess.LastTool = tool
	}
	m[id] = sess
	return s.save(m)
}

// List returns all sessions sorted by LastSeenAt descending (most recent first).
func (s *Store) List() ([]Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return nil, err
	}
	out := make([]Session, 0, len(m))
	for _, sess := range m {
		out = append(out, sess)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].LastSeenAt.After(out[j].LastSeenAt)
	})
	return out, nil
}

// Cleanup removes sessions that have been inactive for longer than maxAge.
// Returns the number of sessions removed.
func (s *Store) Cleanup(maxAge time.Duration) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().Add(-maxAge)
	removed := 0
	for id, sess := range m {
		if sess.LastSeenAt.Before(cutoff) {
			delete(m, id)
			removed++
		}
	}
	if removed > 0 {
		if err := s.save(m); err != nil {
			return 0, err
		}
	}
	return removed, nil
}

type storeFile struct {
	Sessions map[string]Session `json:"sessions"`
}

func (s *Store) load() (map[string]Session, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return make(map[string]Session), nil
	}
	if err != nil {
		return nil, err
	}
	var f storeFile
	if err := json.Unmarshal(data, &f); err != nil {
		return make(map[string]Session), nil
	}
	if f.Sessions == nil {
		f.Sessions = make(map[string]Session)
	}
	return f.Sessions, nil
}

func (s *Store) save(m map[string]Session) error {
	data, err := json.MarshalIndent(storeFile{Sessions: m}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0644)
}
