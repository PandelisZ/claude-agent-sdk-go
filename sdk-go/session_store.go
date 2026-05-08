package claudeagentsdk

import (
	"context"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// SessionListSubkeysKey scopes subkey listing to one main transcript.
type SessionListSubkeysKey struct {
	ProjectKey string `json:"project_key"`
	SessionID  string `json:"session_id"`
}

// SessionStoreEntry is one JSON-safe transcript entry. Store implementations
// should treat entries as opaque pass-through data.
type SessionStoreEntry map[string]any

// SessionStoreListEntry describes one main transcript returned by a store.
type SessionStoreListEntry struct {
	SessionID string `json:"session_id"`
	MTime     int64  `json:"mtime"`
}

// SessionSummaryEntry is an opaque SDK-owned summary sidecar for one session.
type SessionSummaryEntry struct {
	SessionID string         `json:"session_id"`
	MTime     int64          `json:"mtime"`
	Data      map[string]any `json:"data"`
}

// SessionStore is the required session-store surface. Optional capabilities
// are exposed by SessionLister, SessionSummaryLister, SessionDeleter, and
// SessionSubkeyLister.
type SessionStore interface {
	Append(ctx context.Context, key SessionKey, entries []SessionStoreEntry) error
	Load(ctx context.Context, key SessionKey) ([]SessionStoreEntry, error)
}

type SessionLister interface {
	ListSessions(ctx context.Context, projectKey string) ([]SessionStoreListEntry, error)
}

type SessionSummaryLister interface {
	ListSessionSummaries(ctx context.Context, projectKey string) ([]SessionSummaryEntry, error)
}

type SessionDeleter interface {
	Delete(ctx context.Context, key SessionKey) error
}

type SessionSubkeyLister interface {
	ListSubkeys(ctx context.Context, key SessionListSubkeysKey) ([]string, error)
}

// InMemorySessionStore is a process-local SessionStore implementation for
// tests, examples, and development.
type InMemorySessionStore struct {
	mu        sync.RWMutex
	store     map[string][]SessionStoreEntry
	mtimes    map[string]int64
	summaries map[sessionSummaryKey]SessionSummaryEntry
	lastMTime int64
}

type sessionSummaryKey struct {
	projectKey string
	sessionID  string
}

// NewInMemorySessionStore creates an empty in-memory session store.
func NewInMemorySessionStore() *InMemorySessionStore {
	return &InMemorySessionStore{
		store:     make(map[string][]SessionStoreEntry),
		mtimes:    make(map[string]int64),
		summaries: make(map[sessionSummaryKey]SessionSummaryEntry),
	}
}

func (s *InMemorySessionStore) Append(ctx context.Context, key SessionKey, entries []SessionStoreEntry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	storeKey := keyToString(key)
	s.store[storeKey] = append(s.store[storeKey], copyEntries(entries)...)
	now := s.nextMTimeLocked()
	s.mtimes[storeKey] = now
	if sessionKeySubpath(key) == "" {
		sk := sessionSummaryKey{projectKey: key.ProjectKey, sessionID: key.SessionID}
		existing := s.summaries[sk]
		count, _ := existing.Data["entries_appended"].(int)
		if count == 0 {
			if asFloat, ok := existing.Data["entries_appended"].(float64); ok {
				count = int(asFloat)
			}
		}
		s.summaries[sk] = SessionSummaryEntry{
			SessionID: key.SessionID,
			MTime:     now,
			Data: map[string]any{
				"entries_appended": count + len(entries),
			},
		}
	}
	return nil
}

func (s *InMemorySessionStore) Load(ctx context.Context, key SessionKey) ([]SessionStoreEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, ok := s.store[keyToString(key)]
	if !ok {
		return nil, nil
	}
	return copyEntries(entries), nil
}

func (s *InMemorySessionStore) ListSessions(ctx context.Context, projectKey string) ([]SessionStoreListEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	prefix := projectKey + "/"
	results := make([]SessionStoreListEntry, 0)
	for storeKey := range s.store {
		if !strings.HasPrefix(storeKey, prefix) {
			continue
		}
		rest := strings.TrimPrefix(storeKey, prefix)
		if strings.Contains(rest, "/") {
			continue
		}
		results = append(results, SessionStoreListEntry{
			SessionID: rest,
			MTime:     s.mtimes[storeKey],
		})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].MTime == results[j].MTime {
			return results[i].SessionID < results[j].SessionID
		}
		return results[i].MTime > results[j].MTime
	})
	return results, nil
}

func (s *InMemorySessionStore) ListSessionSummaries(ctx context.Context, projectKey string) ([]SessionSummaryEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([]SessionSummaryEntry, 0)
	for key, summary := range s.summaries {
		if key.projectKey != projectKey {
			continue
		}
		results = append(results, copySummary(summary))
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].MTime == results[j].MTime {
			return results[i].SessionID < results[j].SessionID
		}
		return results[i].MTime > results[j].MTime
	})
	return results, nil
}

func (s *InMemorySessionStore) Delete(ctx context.Context, key SessionKey) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	storeKey := keyToString(key)
	delete(s.store, storeKey)
	delete(s.mtimes, storeKey)
	if sessionKeySubpath(key) != "" {
		return nil
	}

	delete(s.summaries, sessionSummaryKey{projectKey: key.ProjectKey, sessionID: key.SessionID})
	prefix := storeKey + "/"
	for existingKey := range s.store {
		if strings.HasPrefix(existingKey, prefix) {
			delete(s.store, existingKey)
			delete(s.mtimes, existingKey)
		}
	}
	return nil
}

func (s *InMemorySessionStore) ListSubkeys(ctx context.Context, key SessionListSubkeysKey) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	prefix := key.ProjectKey + "/" + key.SessionID + "/"
	results := make([]string, 0)
	for storeKey := range s.store {
		if strings.HasPrefix(storeKey, prefix) {
			results = append(results, strings.TrimPrefix(storeKey, prefix))
		}
	}
	sort.Strings(results)
	return results, nil
}

// GetEntries returns entries for a key without requiring a context. It is
// intended for tests and examples.
func (s *InMemorySessionStore) GetEntries(key SessionKey) []SessionStoreEntry {
	entries, _ := s.Load(context.Background(), key)
	if entries == nil {
		return []SessionStoreEntry{}
	}
	return entries
}

// Size returns the number of main transcripts in the store.
func (s *InMemorySessionStore) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for storeKey := range s.store {
		firstSlash := strings.Index(storeKey, "/")
		if firstSlash >= 0 && !strings.Contains(storeKey[firstSlash+1:], "/") {
			count++
		}
	}
	return count
}

// Clear removes all entries and summary state.
func (s *InMemorySessionStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.store = make(map[string][]SessionStoreEntry)
	s.mtimes = make(map[string]int64)
	s.summaries = make(map[sessionSummaryKey]SessionSummaryEntry)
	s.lastMTime = 0
}

func (s *InMemorySessionStore) nextMTimeLocked() int64 {
	now := time.Now().UnixMilli()
	if now <= s.lastMTime {
		now = s.lastMTime + 1
	}
	s.lastMTime = now
	return now
}

// ProjectKeyForDirectory derives the default session-store project key for a
// directory using the same canonicalize-and-sanitize shape as local session
// transcript directories.
func ProjectKeyForDirectory(directory string) string {
	if directory == "" {
		directory = "."
	}
	return sessionStoreSanitizePath(sessionStoreCanonicalizePath(directory))
}

// FilePathToSessionKey derives a SessionKey from a transcript path under a
// Claude projects directory. It returns nil for paths outside projectsDir or
// for unrecognized shapes.
func FilePathToSessionKey(filePath string, projectsDir string) *SessionKey {
	rel, err := filepath.Rel(projectsDir, filePath)
	if err != nil || rel == "." {
		return nil
	}
	if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return nil
	}

	parts := splitPathParts(rel)
	if len(parts) < 2 {
		return nil
	}

	projectKey := parts[0]
	second := parts[1]
	if len(parts) == 2 {
		if !strings.HasSuffix(second, ".jsonl") {
			return nil
		}
		return &SessionKey{
			ProjectKey: projectKey,
			SessionID:  strings.TrimSuffix(second, ".jsonl"),
		}
	}
	if len(parts) < 4 {
		return nil
	}

	subpathParts := append([]string(nil), parts[2:]...)
	last := subpathParts[len(subpathParts)-1]
	if strings.HasSuffix(last, ".jsonl") {
		subpathParts[len(subpathParts)-1] = strings.TrimSuffix(last, ".jsonl")
	}
	subpath := strings.Join(subpathParts, "/")
	return &SessionKey{
		ProjectKey: projectKey,
		SessionID:  second,
		Subpath:    &subpath,
	}
}

var sessionStoreSanitizeRe = regexp.MustCompile(`[^a-zA-Z0-9]`)

const sessionStoreMaxSanitizedLength = 200

func sessionStoreCanonicalizePath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil && resolved != "" {
		if absolute, err := filepath.Abs(resolved); err == nil {
			return absolute
		}
		return resolved
	}
	if absolute, err := filepath.Abs(path); err == nil {
		return absolute
	}
	return path
}

func sessionStoreSanitizePath(name string) string {
	sanitized := sessionStoreSanitizeRe.ReplaceAllString(name, "-")
	if len(sanitized) <= sessionStoreMaxSanitizedLength {
		return sanitized
	}
	return sanitized[:sessionStoreMaxSanitizedLength] + "-" + sessionStoreSimpleHash(name)
}

func sessionStoreSimpleHash(value string) string {
	var hash int32
	for _, r := range value {
		hash = int32((int64(hash) << 5) - int64(hash) + int64(r))
	}
	if hash < 0 {
		hash = -hash
	}
	if hash == 0 {
		return "0"
	}
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	out := make([]byte, 0, 8)
	for hash > 0 {
		out = append(out, digits[hash%36])
		hash /= 36
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

func splitPathParts(path string) []string {
	clean := filepath.Clean(path)
	if clean == "." {
		return nil
	}
	parts := make([]string, 0)
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	return parts
}

func keyToString(key SessionKey) string {
	parts := []string{key.ProjectKey, key.SessionID}
	if subpath := sessionKeySubpath(key); subpath != "" {
		parts = append(parts, subpath)
	}
	return strings.Join(parts, "/")
}

func sessionKeySubpath(key SessionKey) string {
	if key.Subpath == nil {
		return ""
	}
	return *key.Subpath
}

func copyEntries(entries []SessionStoreEntry) []SessionStoreEntry {
	copied := make([]SessionStoreEntry, 0, len(entries))
	for _, entry := range entries {
		copiedEntry := make(SessionStoreEntry, len(entry))
		for key, value := range entry {
			copiedEntry[key] = value
		}
		copied = append(copied, copiedEntry)
	}
	return copied
}

func copySummary(summary SessionSummaryEntry) SessionSummaryEntry {
	data := make(map[string]any, len(summary.Data))
	for key, value := range summary.Data {
		data[key] = value
	}
	return SessionSummaryEntry{
		SessionID: summary.SessionID,
		MTime:     summary.MTime,
		Data:      data,
	}
}
