// Package issues provides a persistent issue store for Anito runtime issues.
// Legacy JSONL logs are migrated in-memory into aggregated issue records,
// then rewritten as versioned JSON on the next mutation.
package issues

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/johnnyicon/anito/internal/domain"
)

const currentStoreVersion = 1

var (
	generatedIDSeq uint64

	nonAlphaNumRE      = regexp.MustCompile(`[^a-z0-9]+`)
	isoTimestampRE     = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}[tT ][0-9:.+\-zZ]+\b`)
	timeOfDayRE        = regexp.MustCompile(`\b\d{2}:\d{2}:\d{2}(?:\.\d+)?\b`)
	pidRE              = regexp.MustCompile(`\bpid(?:\s*|[=:])\d+\b`)
	portWordRE         = regexp.MustCompile(`\bport(?:\s*|[=:])\d{2,5}\b`)
	hostPortRE         = regexp.MustCompile(`\b(?:localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1\])(?::\d{2,5})\b`)
	listenPortRE       = regexp.MustCompile(`\blisten tcp ([^: ]+):\d{2,5}\b`)
	tempPathRE         = regexp.MustCompile(`(?:/private)?/tmp/[^\s"']+|/var/folders/[^\s"']+`)
	crashAttemptsRE    = regexp.MustCompile(`\bgave up after \d+ crash attempts\b`)
	serviceFromErrorRE = regexp.MustCompile(`\bservice ([A-Za-z0-9._-]+) gave up after \d+ crash attempts\b`)
	serviceNameRE      = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	whitespaceRE       = regexp.MustCompile(`\s+`)
)

// Occurrence preserves one raw issue event inside an aggregate issue record.
type Occurrence struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"`
	Tool      string    `json:"tool,omitempty"`
	Input     string    `json:"input,omitempty"`
	Error     string    `json:"error"`
	Context   string    `json:"context,omitempty"`
	RepoPath  string    `json:"repo_path,omitempty"`
	Severity  string    `json:"severity"`
}

// Issue is one aggregated runtime issue. The legacy top-level fields mirror the
// most recent occurrence for backward compatibility with existing callers.
type Issue struct {
	ID              string           `json:"id"`
	Timestamp       time.Time        `json:"timestamp"`           // most recent occurrence timestamp
	Source          string           `json:"source"`              // most recent occurrence source
	Tool            string           `json:"tool,omitempty"`      // most recent occurrence tool
	Input           string           `json:"input,omitempty"`     // most recent occurrence input
	Error           string           `json:"error"`               // most recent occurrence error
	Context         string           `json:"context,omitempty"`   // most recent occurrence context
	RepoPath        string           `json:"repo_path,omitempty"` // most recent occurrence repo path
	Severity        string           `json:"severity"`            // most recent occurrence severity
	Fingerprint     string           `json:"fingerprint,omitempty"`
	Service         string           `json:"service,omitempty"`
	Kind            string           `json:"kind,omitempty"`
	FirstSeen       time.Time        `json:"first_seen,omitempty"`
	LastSeen        time.Time        `json:"last_seen,omitempty"`
	OccurrenceCount int              `json:"occurrence_count,omitempty"`
	Occurrences     []Occurrence     `json:"occurrences,omitempty"`
	State           string           `json:"state"`
	AcknowledgedAt  *time.Time       `json:"acknowledged_at,omitempty"`
	AcknowledgedBy  string           `json:"acknowledged_by,omitempty"`
	ResolvedAt      *time.Time       `json:"resolved_at,omitempty"`
	ResolvedBy      string           `json:"resolved_by,omitempty"`
	TrackerURL      string           `json:"tracker_url,omitempty"`
	History         []LifecycleEvent `json:"history,omitempty"`
}

const (
	StateActive       = "active"
	StateAcknowledged = "acknowledged"
	StateResolved     = "resolved"
)

type LifecycleEvent struct {
	State     string    `json:"state"`
	Timestamp time.Time `json:"timestamp"`
	Actor     string    `json:"actor,omitempty"`
	Note      string    `json:"note,omitempty"`
}

// Store is a thread-safe persistent issue registry.
type Store struct {
	mu   sync.Mutex
	path string
}

type storeFile struct {
	Version int     `json:"version"`
	Issues  []Issue `json:"issues"`
}

type fingerprintParts struct {
	Source  string
	Service string
	Kind    string
	Error   string
}

// New returns a Store backed by <dataDir>/issues.jsonl. The on-disk path stays
// unchanged for compatibility, but the file contents are versioned JSON.
func New(dataDir string) *Store {
	return &Store{path: filepath.Join(dataDir, "issues.jsonl")}
}

// Append records an occurrence and aggregates it into a matching issue
// fingerprint. ID, Timestamp, and Severity are defaulted when absent.
func (s *Store) Append(iss Issue) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	store, err := s.loadLocked()
	if err != nil {
		return err
	}

	occ := normalizeOccurrence(issueToOccurrence(iss))
	store.mergeOccurrence(occ)
	return s.saveLocked(store)
}

// Clear removes all issues from the store while keeping the versioned file
// shape intact.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(newStoreFile())
}

// Get returns one issue by its stable aggregate ID.
func (s *Store) Get(id string) (*Issue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	store, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	for _, iss := range store.Issues {
		if iss.ID == id {
			copy := cloneIssue(iss)
			return &copy, nil
		}
	}
	return nil, domain.MissingServicef("issue %q not found", id)
}

func (s *Store) Acknowledge(id, actor string) (*Issue, error) {
	return s.transition(id, StateAcknowledged, actor, "acknowledged")
}

func (s *Store) Resolve(id, actor, trackerURL string) (*Issue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	store, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	for i := range store.Issues {
		if store.Issues[i].ID != id {
			continue
		}
		if store.Issues[i].State == StateResolved {
			return nil, domain.Conflictf("issue %q is already resolved", id)
		}
		now := time.Now()
		store.Issues[i].State = StateResolved
		store.Issues[i].ResolvedAt = &now
		store.Issues[i].ResolvedBy = strings.TrimSpace(actor)
		if strings.TrimSpace(trackerURL) != "" {
			store.Issues[i].TrackerURL = strings.TrimSpace(trackerURL)
		}
		store.Issues[i].History = append(store.Issues[i].History, LifecycleEvent{State: StateResolved, Timestamp: now, Actor: actor})
		if err := s.saveLocked(store); err != nil {
			return nil, err
		}
		copy := cloneIssue(store.Issues[i])
		return &copy, nil
	}
	return nil, domain.MissingServicef("issue %q not found", id)
}

func (s *Store) Reopen(id, actor string) (*Issue, error) {
	return s.transition(id, StateActive, actor, "reopened")
}

func (s *Store) transition(id, state, actor, note string) (*Issue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	store, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	for i := range store.Issues {
		if store.Issues[i].ID != id {
			continue
		}
		current := store.Issues[i].State
		if current == "" {
			current = StateActive
		}
		if state == StateAcknowledged && current != StateActive {
			return nil, domain.Conflictf("issue %q cannot be acknowledged from state %s", id, current)
		}
		if state == StateActive && current != StateResolved {
			return nil, domain.Conflictf("issue %q cannot be reopened from state %s", id, current)
		}
		now := time.Now()
		store.Issues[i].State = state
		if state == StateAcknowledged {
			store.Issues[i].AcknowledgedAt = &now
			store.Issues[i].AcknowledgedBy = strings.TrimSpace(actor)
		}
		if state == StateActive {
			store.Issues[i].ResolvedAt = nil
			store.Issues[i].ResolvedBy = ""
		}
		store.Issues[i].History = append(store.Issues[i].History, LifecycleEvent{State: state, Timestamp: now, Actor: actor, Note: note})
		if err := s.saveLocked(store); err != nil {
			return nil, err
		}
		copy := cloneIssue(store.Issues[i])
		return &copy, nil
	}
	return nil, domain.MissingServicef("issue %q not found", id)
}

// Recent returns the last n aggregated issues, optionally filtered by a source
// prefix. Pass source="" to return all sources. Pass n<=0 to return all issues.
func (s *Store) Recent(n int, source string) ([]Issue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	store, err := s.loadLocked()
	if err != nil {
		return nil, err
	}

	filtered := make([]Issue, 0, len(store.Issues))
	for _, iss := range store.Issues {
		if source == "" || strings.HasPrefix(iss.Source, source) {
			filtered = append(filtered, cloneIssue(iss))
		}
	}
	if len(filtered) == 0 {
		return nil, nil
	}
	if n <= 0 || n >= len(filtered) {
		return filtered, nil
	}
	return filtered[len(filtered)-n:], nil
}

func newStoreFile() storeFile {
	return storeFile{Version: currentStoreVersion, Issues: []Issue{}}
}

func (s *Store) loadLocked() (storeFile, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return newStoreFile(), nil
	}
	if err != nil {
		return storeFile{}, fmt.Errorf("issues: open %s: %w", s.path, err)
	}
	return decodeStoreFile(data)
}

func decodeStoreFile(data []byte) (storeFile, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return newStoreFile(), nil
	}
	if trimmed[0] == '{' {
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &probe); err != nil {
			return decodeLegacyJSONL(trimmed), nil
		}
		if _, hasVersion := probe["version"]; !hasVersion {
			if _, hasIssues := probe["issues"]; !hasIssues {
				return decodeLegacyJSONL(trimmed), nil
			}
		}
		var file storeFile
		if err := json.Unmarshal(trimmed, &file); err != nil {
			return storeFile{}, fmt.Errorf("issues: unmarshal store: %w", err)
		}
		if file.Version == 0 {
			file.Version = currentStoreVersion
		}
		if file.Version > currentStoreVersion {
			return storeFile{}, fmt.Errorf("issues: unsupported store version %d", file.Version)
		}
		return normalizeStoreFile(file), nil
	}
	return decodeLegacyJSONL(trimmed), nil
}

func decodeLegacyJSONL(data []byte) storeFile {
	store := newStoreFile()
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var legacy Issue
		if err := json.Unmarshal(line, &legacy); err != nil {
			continue
		}
		store.mergeOccurrence(normalizeOccurrence(issueToOccurrence(legacy)))
	}
	return store
}

func normalizeStoreFile(file storeFile) storeFile {
	out := newStoreFile()
	out.Version = file.Version
	if len(file.Issues) == 0 {
		return out
	}
	for _, stored := range file.Issues {
		occs := cloneOccurrences(stored.Occurrences)
		if len(occs) == 0 {
			occs = []Occurrence{normalizeOccurrence(issueToOccurrence(stored))}
		} else {
			for i := range occs {
				occs[i] = normalizeOccurrence(occs[i])
			}
		}
		rebuilt := rebuildIssue(stored.ID, occs)
		rebuilt.State = stored.State
		if rebuilt.State == "" {
			rebuilt.State = StateActive
		}
		rebuilt.AcknowledgedAt = stored.AcknowledgedAt
		rebuilt.AcknowledgedBy = stored.AcknowledgedBy
		rebuilt.ResolvedAt = stored.ResolvedAt
		rebuilt.ResolvedBy = stored.ResolvedBy
		rebuilt.TrackerURL = stored.TrackerURL
		rebuilt.History = append([]LifecycleEvent(nil), stored.History...)
		out.Issues = append(out.Issues, rebuilt)
	}
	sortIssues(out.Issues)
	return out
}

func (s *Store) saveLocked(store storeFile) error {
	sortIssues(store.Issues)
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("issues: marshal: %w", err)
	}
	data = append(data, '\n')
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("issues: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("issues: rename %s: %w", s.path, err)
	}
	return nil
}

func (f *storeFile) mergeOccurrence(occ Occurrence) {
	parts := buildFingerprintParts(occ)
	fingerprint := parts.hash()
	for i := range f.Issues {
		if f.Issues[i].Fingerprint != fingerprint {
			continue
		}
		previous := f.Issues[i]
		occs := append(cloneOccurrences(previous.Occurrences), occ)
		f.Issues[i] = rebuildIssue(previous.ID, occs)
		f.Issues[i].State = previous.State
		if f.Issues[i].State == "" {
			f.Issues[i].State = StateActive
		}
		f.Issues[i].AcknowledgedAt = previous.AcknowledgedAt
		f.Issues[i].AcknowledgedBy = previous.AcknowledgedBy
		f.Issues[i].ResolvedAt = previous.ResolvedAt
		f.Issues[i].ResolvedBy = previous.ResolvedBy
		f.Issues[i].TrackerURL = previous.TrackerURL
		f.Issues[i].History = append([]LifecycleEvent(nil), previous.History...)
		if previous.State == StateResolved {
			now := time.Now()
			f.Issues[i].State = StateActive
			f.Issues[i].ResolvedAt = nil
			f.Issues[i].ResolvedBy = ""
			f.Issues[i].History = append(f.Issues[i].History, LifecycleEvent{State: StateActive, Timestamp: now, Actor: "system", Note: "reopened by new occurrence"})
		}
		return
	}
	f.Issues = append(f.Issues, rebuildIssue(occ.ID, []Occurrence{occ}))
}

func rebuildIssue(id string, occs []Occurrence) Issue {
	if len(occs) == 0 {
		return Issue{}
	}
	normalized := cloneOccurrences(occs)
	for i := range normalized {
		normalized[i] = normalizeOccurrence(normalized[i])
	}

	firstSeen := normalized[0].Timestamp
	lastIdx := 0
	for i := 1; i < len(normalized); i++ {
		if normalized[i].Timestamp.Before(firstSeen) {
			firstSeen = normalized[i].Timestamp
		}
		if normalized[i].Timestamp.After(normalized[lastIdx].Timestamp) ||
			normalized[i].Timestamp.Equal(normalized[lastIdx].Timestamp) {
			lastIdx = i
		}
	}
	latest := normalized[lastIdx]
	parts := buildFingerprintParts(latest)
	if id == "" {
		id = normalized[0].ID
	}
	return Issue{
		ID:              id,
		Timestamp:       latest.Timestamp,
		Source:          latest.Source,
		Tool:            latest.Tool,
		Input:           latest.Input,
		Error:           latest.Error,
		Context:         latest.Context,
		RepoPath:        latest.RepoPath,
		Severity:        latest.Severity,
		Fingerprint:     parts.hash(),
		Service:         parts.Service,
		Kind:            parts.Kind,
		FirstSeen:       firstSeen,
		LastSeen:        latest.Timestamp,
		OccurrenceCount: len(normalized),
		Occurrences:     normalized,
		State:           StateActive,
	}
}

func issueToOccurrence(iss Issue) Occurrence {
	return Occurrence{
		ID:        iss.ID,
		Timestamp: iss.Timestamp,
		Source:    iss.Source,
		Tool:      iss.Tool,
		Input:     iss.Input,
		Error:     iss.Error,
		Context:   iss.Context,
		RepoPath:  iss.RepoPath,
		Severity:  iss.Severity,
	}
}

func normalizeOccurrence(occ Occurrence) Occurrence {
	now := time.Now()
	if occ.ID == "" {
		occ.ID = newGeneratedID(now)
	}
	if occ.Timestamp.IsZero() {
		occ.Timestamp = now
	}
	if occ.Severity == "" {
		occ.Severity = "error"
	}
	occ.Source = strings.TrimSpace(occ.Source)
	occ.Tool = strings.TrimSpace(occ.Tool)
	occ.Input = strings.TrimSpace(occ.Input)
	occ.Error = strings.TrimSpace(occ.Error)
	occ.Context = strings.TrimSpace(occ.Context)
	occ.RepoPath = strings.TrimSpace(occ.RepoPath)
	return occ
}

func newGeneratedID(now time.Time) string {
	seq := atomic.AddUint64(&generatedIDSeq, 1)
	return strconv.FormatInt(now.UnixNano(), 36) + "-" + strconv.FormatUint(seq, 36)
}

func buildFingerprintParts(occ Occurrence) fingerprintParts {
	sourcePrefix, sourceSuffix := splitSource(occ.Source)
	sourceKey := normalizeKey(sourcePrefix)
	if sourceKey == "" {
		sourceKey = normalizeKey(occ.Source)
	}

	service := ""
	switch sourceKey {
	case "consumer":
		service = normalizeKey(sourceSuffix)
	default:
		service = normalizeKey(extractServiceFromInput(occ.Input))
	}
	if service == "" {
		service = normalizeKey(extractServiceFromError(occ.Error))
	}

	kind := ""
	switch sourceKey {
	case "daemon":
		kind = normalizeKey(sourceSuffix)
	case "consumer":
		kind = normalizeKey(occ.Tool)
		if kind == "" {
			kind = "report"
		}
	default:
		kind = normalizeKey(occ.Tool)
		if kind == "" {
			kind = normalizeKey(sourceSuffix)
		}
	}
	if kind == "" {
		kind = sourceKey
	}
	if kind == "" {
		kind = "unknown"
	}

	errorKey := normalizeError(occ.Error)
	if errorKey == "" {
		errorKey = "empty"
	}

	return fingerprintParts{
		Source:  sourceKey,
		Service: service,
		Kind:    kind,
		Error:   errorKey,
	}
}

func (p fingerprintParts) hash() string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		p.Source,
		p.Service,
		p.Kind,
		p.Error,
	}, "\x1f")))
	return hex.EncodeToString(sum[:])
}

func splitSource(source string) (string, string) {
	source = strings.TrimSpace(source)
	prefix, suffix, ok := strings.Cut(source, ":")
	if !ok {
		return source, ""
	}
	return prefix, suffix
}

func extractServiceFromInput(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	var obj map[string]any
	if strings.HasPrefix(input, "{") && json.Unmarshal([]byte(input), &obj) == nil {
		for _, key := range []string{"name", "service", "service_name"} {
			if v, ok := obj[key].(string); ok && serviceNameRE.MatchString(strings.TrimSpace(v)) {
				return v
			}
		}
		return ""
	}
	if serviceNameRE.MatchString(input) {
		return input
	}
	return ""
}

func extractServiceFromError(msg string) string {
	matches := serviceFromErrorRE.FindStringSubmatch(msg)
	if len(matches) != 2 {
		return ""
	}
	return matches[1]
}

func normalizeKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	value = nonAlphaNumRE.ReplaceAllString(value, "_")
	return strings.Trim(value, "_")
}

func normalizeError(msg string) string {
	msg = strings.ToLower(strings.TrimSpace(msg))
	if msg == "" {
		return ""
	}
	msg = tempPathRE.ReplaceAllString(msg, "<tmp>")
	msg = isoTimestampRE.ReplaceAllString(msg, "<time>")
	msg = timeOfDayRE.ReplaceAllString(msg, "<time>")
	msg = hostPortRE.ReplaceAllString(msg, "<addr>")
	msg = listenPortRE.ReplaceAllString(msg, "listen tcp $1:<port>")
	msg = portWordRE.ReplaceAllString(msg, "port=<port>")
	msg = pidRE.ReplaceAllString(msg, "pid=<pid>")
	msg = crashAttemptsRE.ReplaceAllString(msg, "gave up after <n> crash attempts")
	msg = whitespaceRE.ReplaceAllString(msg, " ")
	return strings.TrimSpace(msg)
}

func sortIssues(issues []Issue) {
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].LastSeen.Equal(issues[j].LastSeen) {
			return issues[i].ID < issues[j].ID
		}
		return issues[i].LastSeen.Before(issues[j].LastSeen)
	})
}

func cloneIssue(iss Issue) Issue {
	iss.Occurrences = cloneOccurrences(iss.Occurrences)
	iss.History = append([]LifecycleEvent(nil), iss.History...)
	return iss
}

func cloneOccurrences(occs []Occurrence) []Occurrence {
	if len(occs) == 0 {
		return nil
	}
	out := make([]Occurrence, len(occs))
	copy(out, occs)
	return out
}
