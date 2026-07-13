// Package sessions provides a persistent store for MCP session tracking.
// Sessions are written to <dataDir>/sessions.json and survive daemon restarts.
// Stale sessions (no activity for maxAge) are pruned on startup.
package sessions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const maxTrackedSessions = 500

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
	mu       sync.Mutex
	path     string
	sessions map[string]Session
	loaded   bool
	lastSave time.Time
	now      func() time.Time
}

var touchSaveInterval = 2 * time.Second

// New returns a Store backed by <dataDir>/sessions.json.
func New(dataDir string) *Store {
	return &Store{path: filepath.Join(dataDir, "sessions.json"), now: time.Now}
}

// Create records a newly initialised session.
func (s *Store) Create(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadCached(); err != nil {
		return err
	}
	now := s.now()
	s.sessions[id] = Session{ID: id, CreatedAt: now, LastSeenAt: now}
	return s.save(s.sessions)
}

// Touch updates the last-seen timestamp and increments the call counter.
// tool is the MCP tool name ("anito_deploy" etc.); pass "" if unknown.
func (s *Store) Touch(id, tool string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadCached(); err != nil {
		return err
	}
	now := s.now()
	sess := s.sessions[id]
	isNew := sess.ID == ""
	if isNew {
		// First touch for an unknown session — create it now.
		sess = Session{ID: id, CreatedAt: now}
	}
	sess.LastSeenAt = now
	sess.CallCount++
	if tool != "" {
		sess.LastTool = tool
	}
	s.sessions[id] = sess
	if isNew || now.Sub(s.lastSave) >= touchSaveInterval {
		return s.save(s.sessions)
	}
	return nil
}

// List returns all sessions sorted by LastSeenAt descending (most recent first).
func (s *Store) List() ([]Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadCached(); err != nil {
		return nil, err
	}
	out := make([]Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
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
	if err := s.loadCached(); err != nil {
		return 0, err
	}
	cutoff := s.now().Add(-maxAge)
	removed := 0
	for id, sess := range s.sessions {
		if sess.LastSeenAt.Before(cutoff) {
			delete(s.sessions, id)
			removed++
		}
	}
	removed += pruneOldest(s.sessions, maxTrackedSessions)
	if removed > 0 {
		if err := s.save(s.sessions); err != nil {
			return 0, err
		}
	}
	return removed, nil
}

type storeFile struct {
	Sessions map[string]Session `json:"sessions"`
}

func (s *Store) loadCached() error {
	if s.loaded {
		return nil
	}
	m, err := s.load()
	if err != nil {
		return err
	}
	s.sessions = m
	s.loaded = true
	if s.now == nil {
		s.now = time.Now
	}
	s.lastSave = s.now()
	return nil
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
	pruneOldest(m, maxTrackedSessions)
	data, err := json.MarshalIndent(storeFile{Sessions: m}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.path, data, 0644); err != nil {
		return err
	}
	s.sessions = m
	s.loaded = true
	if s.now == nil {
		s.now = time.Now
	}
	s.lastSave = s.now()
	return nil
}

func pruneOldest(m map[string]Session, limit int) int {
	if limit <= 0 || len(m) <= limit {
		return 0
	}
	all := make([]Session, 0, len(m))
	for _, sess := range m {
		all = append(all, sess)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].LastSeenAt.Equal(all[j].LastSeenAt) {
			return all[i].ID < all[j].ID
		}
		return all[i].LastSeenAt.After(all[j].LastSeenAt)
	})
	for _, sess := range all[limit:] {
		delete(m, sess.ID)
	}
	return len(all) - limit
}
