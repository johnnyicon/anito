package sessions

import (
	"os"
	"testing"
	"time"
)

func newTempStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	return New(dir)
}

func TestCreate(t *testing.T) {
	s := newTempStore(t)
	if err := s.Create("abc"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	list, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 session, got %d", len(list))
	}
	if list[0].ID != "abc" {
		t.Errorf("want ID abc, got %s", list[0].ID)
	}
	if list[0].CallCount != 0 {
		t.Errorf("want CallCount 0, got %d", list[0].CallCount)
	}
}

func TestCreate_Idempotent(t *testing.T) {
	s := newTempStore(t)
	_ = s.Create("abc")
	// Creating again should overwrite (not duplicate).
	_ = s.Create("abc")
	list, _ := s.List()
	if len(list) != 1 {
		t.Errorf("want 1 session, got %d", len(list))
	}
}

func TestTouch_ExistingSession(t *testing.T) {
	s := newTempStore(t)
	_ = s.Create("abc")
	before := time.Now()
	time.Sleep(time.Millisecond)
	if err := s.Touch("abc", "anito_deploy"); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	list, _ := s.List()
	sess := list[0]
	if sess.CallCount != 1 {
		t.Errorf("want CallCount 1, got %d", sess.CallCount)
	}
	if sess.LastTool != "anito_deploy" {
		t.Errorf("want LastTool anito_deploy, got %s", sess.LastTool)
	}
	if !sess.LastSeenAt.After(before) {
		t.Errorf("LastSeenAt not updated: %v", sess.LastSeenAt)
	}
}

func TestTouch_CreatesUnknownSession(t *testing.T) {
	s := newTempStore(t)
	if err := s.Touch("unknown", "anito_status"); err != nil {
		t.Fatalf("Touch on unknown: %v", err)
	}
	list, _ := s.List()
	if len(list) != 1 {
		t.Fatalf("want 1 session, got %d", len(list))
	}
	if list[0].ID != "unknown" {
		t.Errorf("want ID unknown, got %s", list[0].ID)
	}
	if list[0].CallCount != 1 {
		t.Errorf("want CallCount 1, got %d", list[0].CallCount)
	}
}

func TestTouch_EmptyToolDoesNotClearLastTool(t *testing.T) {
	s := newTempStore(t)
	_ = s.Touch("abc", "anito_deploy")
	_ = s.Touch("abc", "")
	list, _ := s.List()
	if list[0].LastTool != "anito_deploy" {
		t.Errorf("want LastTool anito_deploy, got %s", list[0].LastTool)
	}
}

func TestList_SortedByLastSeenAt(t *testing.T) {
	s := newTempStore(t)
	_ = s.Create("first")
	time.Sleep(2 * time.Millisecond)
	_ = s.Create("second")
	time.Sleep(2 * time.Millisecond)
	_ = s.Touch("first", "")
	list, _ := s.List()
	if list[0].ID != "first" {
		t.Errorf("want first to be most recent, got %s", list[0].ID)
	}
}

func TestCleanup_RemovesStale(t *testing.T) {
	s := newTempStore(t)
	_ = s.Create("stale")
	// Manually backdate the session in the file.
	m, _ := s.load()
	sess := m["stale"]
	sess.LastSeenAt = time.Now().Add(-48 * time.Hour)
	m["stale"] = sess
	_ = s.save(m)

	removed, err := s.Cleanup(24 * time.Hour)
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if removed != 1 {
		t.Errorf("want 1 removed, got %d", removed)
	}
	list, _ := s.List()
	if len(list) != 0 {
		t.Errorf("want 0 sessions after cleanup, got %d", len(list))
	}
}

func TestCleanup_KeepsRecent(t *testing.T) {
	s := newTempStore(t)
	_ = s.Create("fresh")
	removed, err := s.Cleanup(24 * time.Hour)
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if removed != 0 {
		t.Errorf("want 0 removed, got %d", removed)
	}
	list, _ := s.List()
	if len(list) != 1 {
		t.Errorf("want 1 session, got %d", len(list))
	}
}

func TestPersistence(t *testing.T) {
	dir := t.TempDir()
	s1 := New(dir)
	_ = s1.Create("persist-me")

	// New store pointing at same dir.
	s2 := New(dir)
	list, err := s2.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID != "persist-me" {
		t.Errorf("session not persisted; got %v", list)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	dir := t.TempDir()
	// Remove any file that might exist.
	_ = os.Remove(dir + "/sessions.json")
	s := New(dir)
	list, err := s.List()
	if err != nil {
		t.Fatalf("List on missing file: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("want empty list, got %d", len(list))
	}
}
