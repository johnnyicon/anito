package service

import (
	"fmt"
	"sync"
	"time"
)

// StartupPhase describes daemon startup reconciliation progress.
type StartupPhase string

const (
	StartPhaseIdle             StartupPhase = "idle"
	StartPhaseBindingListeners StartupPhase = "binding_listeners"
	StartPhaseReconciling      StartupPhase = "reconciling"
	StartPhaseReady            StartupPhase = "ready"
)

// StartupState is a read-only snapshot of startup reconciliation.
type StartupState struct {
	Phase            StartupPhase
	StartedAt        time.Time
	CompletedAt      time.Time
	Total            int
	Completed        int
	MaxParallel      int
	MutationsBlocked bool
	LastResult       *RestoreAllResult
}

// StartupGateError is returned by service mutators while startup reconciliation
// is still active.
type StartupGateError struct {
	Phase     StartupPhase
	Completed int
	Total     int
}

func (e *StartupGateError) Error() string {
	return fmt.Sprintf("startup reconciliation in progress (%d/%d complete, phase=%s)", e.Completed, e.Total, e.Phase)
}

type startupTracker struct {
	mu          sync.RWMutex
	phase       StartupPhase
	startedAt   time.Time
	completedAt time.Time
	total       int
	completed   int
	maxParallel int
	lastResult  *RestoreAllResult
}

func newStartupTracker() startupTracker {
	return startupTracker{phase: StartPhaseReady}
}

// BeginStartup marks the service as reconciling before management listeners are
// exposed. The daemon uses this to close the handoff window between creating a
// Service and starting RestoreAll in a goroutine.
func (s *Service) BeginStartup(maxParallel int) {
	if maxParallel <= 0 {
		maxParallel = defaultRestoreMaxParallel
	}
	s.startup.begin(time.Now(), len(s.reg.All()), maxParallel)
}

func (s *Service) StartupState() StartupState {
	s.startup.mu.RLock()
	defer s.startup.mu.RUnlock()
	return StartupState{
		Phase:            s.startup.phase,
		StartedAt:        s.startup.startedAt,
		CompletedAt:      s.startup.completedAt,
		Total:            s.startup.total,
		Completed:        s.startup.completed,
		MaxParallel:      s.startup.maxParallel,
		MutationsBlocked: s.startup.mutationsBlockedLocked(),
		LastResult:       cloneRestoreAllResult(s.startup.lastResult),
	}
}

func (s *Service) ensureMutable() error {
	state := s.StartupState()
	if !state.MutationsBlocked {
		return nil
	}
	return &StartupGateError{
		Phase:     state.Phase,
		Completed: state.Completed,
		Total:     state.Total,
	}
}

func (t *startupTracker) mutationsBlockedLocked() bool {
	return t.phase == StartPhaseBindingListeners || t.phase == StartPhaseReconciling
}

func (t *startupTracker) begin(startedAt time.Time, total, maxParallel int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.phase = StartPhaseBindingListeners
	t.startedAt = startedAt
	t.completedAt = time.Time{}
	t.total = total
	t.completed = 0
	t.maxParallel = maxParallel
	t.lastResult = nil
}

func (t *startupTracker) setPhase(phase StartupPhase) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.phase = phase
}

func (t *startupTracker) incrementCompleted() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.completed++
}

func (t *startupTracker) finish(completedAt time.Time, result *RestoreAllResult) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.phase = StartPhaseReady
	t.completedAt = completedAt
	t.completed = t.total
	t.lastResult = cloneRestoreAllResult(result)
}
