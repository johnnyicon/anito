// Package watcher provides file-system change detection for Anito services.
// When watched paths change, a debounced callback triggers a service restart.
package watcher

import (
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const debounceDelay = 500 * time.Millisecond

// Manager tracks one fsnotify watcher per service.
type Manager struct {
	mu       sync.Mutex
	watchers map[string]*entry
}

type entry struct {
	done chan struct{}
}

// New creates a new watcher Manager.
func New() *Manager {
	return &Manager{
		watchers: make(map[string]*entry),
	}
}

// Start begins watching paths for the named service.
// onTrigger is called (debounced) when any relevant file in paths changes.
// If a watcher already exists for this service it is stopped first.
func (m *Manager) Start(name string, paths []string, onTrigger func(trigger string)) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if e, ok := m.watchers[name]; ok {
		close(e.done)
		delete(m.watchers, name)
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	for _, root := range paths {
		if err := addDirsRecursive(w, root); err != nil {
			_ = w.Close()
			return err
		}
	}

	done := make(chan struct{})
	m.watchers[name] = &entry{done: done}
	go runWatcher(name, w, done, onTrigger)
	return nil
}

// Stop stops the watcher for the named service.
func (m *Manager) Stop(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.watchers[name]; ok {
		close(e.done)
		delete(m.watchers, name)
	}
}

// StopAll stops all active watchers.
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, e := range m.watchers {
		close(e.done)
		delete(m.watchers, name)
	}
}

// Active returns the names of all services with an active watcher.
func (m *Manager) Active() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.watchers))
	for name := range m.watchers {
		names = append(names, name)
	}
	return names
}

func runWatcher(name string, w *fsnotify.Watcher, done chan struct{}, onTrigger func(string)) {
	defer w.Close()

	var (
		debounce    *time.Timer
		debounceMu  sync.Mutex
		generation  uint64
		lastTrigger string
		eventCount  int
	)

	resetDebounce := func(path string) {
		debounceMu.Lock()
		defer debounceMu.Unlock()
		lastTrigger = path
		eventCount++
		generation++
		currentGeneration := generation
		if debounce != nil {
			debounce.Stop()
		}
		debounce = time.AfterFunc(debounceDelay, func() {
			select {
			case <-done:
				return
			default:
			}
			debounceMu.Lock()
			if generation != currentGeneration {
				debounceMu.Unlock()
				return
			}
			count := eventCount
			trigger := lastTrigger
			eventCount = 0
			debounce = nil
			debounceMu.Unlock()
			log.Printf("[WATCH] name=%s coalesced=%d trigger=%s", name, count, trigger)
			onTrigger(trigger)
		})
	}

	for {
		select {
		case <-done:
			debounceMu.Lock()
			if debounce != nil {
				debounce.Stop()
			}
			debounceMu.Unlock()
			return
		case event, ok := <-w.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) {
				base := filepath.Base(event.Name)
				// Skip hidden files and compiler temp files.
				if len(base) > 0 && (base[0] == '.' || base[0] == '#') {
					continue
				}
				resetDebounce(event.Name)
			}
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			log.Printf("[WATCH] name=%s error=%v", name, err)
		}
	}
}

// addDirsRecursive walks root and adds every directory to the watcher.
// Hidden directories (starting with '.') are skipped.
func addDirsRecursive(w *fsnotify.Watcher, root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible paths
		}
		if !info.IsDir() {
			return nil
		}
		// Skip hidden directories (vendor, .git, etc.) except the root itself.
		if path != root && len(info.Name()) > 0 && info.Name()[0] == '.' {
			return filepath.SkipDir
		}
		return w.Add(path)
	})
}
