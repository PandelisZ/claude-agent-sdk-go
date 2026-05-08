package claudeagentsdk

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type materializedStoreSession struct {
	configDir string
	sessionID string
}

func prepareSessionStoreOptions(ctx context.Context, options ClaudeAgentOptions) (ClaudeAgentOptions, *materializedStoreSession, error) {
	if err := validateSessionStoreOptions(options); err != nil {
		return options, nil, err
	}
	materialized, err := materializeStoreResume(ctx, options)
	if err != nil || materialized == nil {
		return options, materialized, err
	}

	env := cloneStringMap(options.Env)
	if env == nil {
		env = make(map[string]string)
	}
	env["CLAUDE_CONFIG_DIR"] = materialized.configDir
	options.Env = env
	options.Resume = &materialized.sessionID
	options.ContinueConversation = false
	return options, materialized, nil
}

func validateSessionStoreOptions(options ClaudeAgentOptions) error {
	if options.SessionStore == nil {
		return nil
	}
	if options.ContinueConversation && options.Resume == nil {
		if _, ok := options.SessionStore.(SessionLister); !ok {
			return fmt.Errorf("continue_conversation with session_store requires the store to implement ListSessions")
		}
	}
	if options.EnableFileCheckpointing {
		return fmt.Errorf("session_store cannot be combined with enable_file_checkpointing")
	}
	return nil
}

func materializeStoreResume(ctx context.Context, options ClaudeAgentOptions) (*materializedStoreSession, error) {
	store := options.SessionStore
	if store == nil {
		return nil, nil
	}
	if options.Resume == nil && !options.ContinueConversation {
		return nil, nil
	}

	projectKey := ProjectKeyForDirectory(stringPtrValue(options.Cwd))
	timeout := sessionStoreLoadTimeout(options.LoadTimeoutMS)
	resolvedSessionID, entries, err := resolveStoreResumeEntries(ctx, store, projectKey, options, timeout)
	if err != nil || len(entries) == 0 {
		return nil, err
	}

	configDir, err := os.MkdirTemp("", "claude-resume-")
	if err != nil {
		return nil, err
	}
	cleanupOnError := true
	defer func() {
		if cleanupOnError {
			_ = os.RemoveAll(configDir)
		}
	}()

	projectDir := filepath.Join(configDir, "projects", projectKey)
	if err := writeStoreJSONL(filepath.Join(projectDir, resolvedSessionID+".jsonl"), entries); err != nil {
		return nil, err
	}
	copyStoreResumeAuthFiles(configDir, options.Env)
	if subkeyLister, ok := store.(SessionSubkeyLister); ok {
		subkeys, err := subkeyLister.ListSubkeys(ctx, SessionListSubkeysKey{ProjectKey: projectKey, SessionID: resolvedSessionID})
		if err != nil {
			return nil, fmt.Errorf("SessionStore.ListSubkeys() failed during resume materialization: %w", err)
		}
		for _, subkey := range subkeys {
			if !isSafeStoreSubpath(subkey) {
				continue
			}
			subEntries, err := loadStoreEntriesWithTimeout(ctx, store, SessionKey{ProjectKey: projectKey, SessionID: resolvedSessionID, Subpath: &subkey}, timeout)
			if err != nil {
				return nil, err
			}
			if len(subEntries) == 0 {
				continue
			}
			path := filepath.Join(projectDir, resolvedSessionID, filepath.FromSlash(subkey)+".jsonl")
			if err := writeStoreJSONL(path, subEntries); err != nil {
				return nil, err
			}
		}
	}

	cleanupOnError = false
	return &materializedStoreSession{configDir: configDir, sessionID: resolvedSessionID}, nil
}

func copyStoreResumeAuthFiles(configDir string, env map[string]string) {
	callerConfigDir := ""
	if env != nil {
		callerConfigDir = env["CLAUDE_CONFIG_DIR"]
	}
	if callerConfigDir == "" {
		callerConfigDir = os.Getenv("CLAUDE_CONFIG_DIR")
	}

	sourceConfigDir := callerConfigDir
	if sourceConfigDir == "" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			sourceConfigDir = filepath.Join(home, ".claude")
		}
	}
	if sourceConfigDir != "" {
		copyStoreResumeFile(filepath.Join(sourceConfigDir, ".credentials.json"), filepath.Join(configDir, ".credentials.json"))
	}

	claudeJSONSource := ""
	if callerConfigDir != "" {
		claudeJSONSource = filepath.Join(callerConfigDir, ".claude.json")
	} else if home, err := os.UserHomeDir(); err == nil && home != "" {
		claudeJSONSource = filepath.Join(home, ".claude.json")
	}
	if claudeJSONSource != "" {
		copyStoreResumeFile(claudeJSONSource, filepath.Join(configDir, ".claude.json"))
	}
}

func copyStoreResumeFile(src string, dst string) {
	data, err := os.ReadFile(src)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(dst, data, 0o600)
}

func cleanupMaterializedStoreSession(materialized *materializedStoreSession) error {
	if materialized == nil || materialized.configDir == "" {
		return nil
	}
	return os.RemoveAll(materialized.configDir)
}

func resolveStoreResumeEntries(ctx context.Context, store SessionStore, projectKey string, options ClaudeAgentOptions, timeout time.Duration) (string, []SessionStoreEntry, error) {
	if options.Resume != nil {
		sessionID := *options.Resume
		if !isValidSessionUUID(sessionID) {
			return "", nil, nil
		}
		entries, err := loadStoreEntriesWithTimeout(ctx, store, SessionKey{ProjectKey: projectKey, SessionID: sessionID}, timeout)
		return sessionID, entries, err
	}

	lister, ok := store.(SessionLister)
	if !ok {
		return "", nil, nil
	}
	sessions, err := listStoreSessionsWithTimeout(ctx, lister, projectKey, timeout)
	if err != nil {
		return "", nil, err
	}
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].MTime == sessions[j].MTime {
			return sessions[i].SessionID < sessions[j].SessionID
		}
		return sessions[i].MTime > sessions[j].MTime
	})
	for _, candidate := range sessions {
		if !isValidSessionUUID(candidate.SessionID) {
			continue
		}
		entries, err := loadStoreEntriesWithTimeout(ctx, store, SessionKey{ProjectKey: projectKey, SessionID: candidate.SessionID}, timeout)
		if err != nil {
			return "", nil, err
		}
		if len(entries) == 0 || boolFromAny(entries[0]["isSidechain"]) {
			continue
		}
		return candidate.SessionID, entries, nil
	}
	return "", nil, nil
}

func loadStoreEntriesWithTimeout(ctx context.Context, store SessionStore, key SessionKey, timeout time.Duration) ([]SessionStoreEntry, error) {
	type result struct {
		entries []SessionStoreEntry
		err     error
	}
	resultCh := make(chan result, 1)
	go func() {
		entries, err := store.Load(ctx, key)
		resultCh <- result{entries: entries, err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(timeout):
		return nil, fmt.Errorf("SessionStore.Load() for session %s timed out after %dms during resume materialization", key.SessionID, timeout.Milliseconds())
	case result := <-resultCh:
		if result.err != nil {
			return nil, fmt.Errorf("SessionStore.Load() for session %s failed during resume materialization: %w", key.SessionID, result.err)
		}
		return result.entries, nil
	}
}

func listStoreSessionsWithTimeout(ctx context.Context, lister SessionLister, projectKey string, timeout time.Duration) ([]SessionStoreListEntry, error) {
	type result struct {
		sessions []SessionStoreListEntry
		err      error
	}
	resultCh := make(chan result, 1)
	go func() {
		sessions, err := lister.ListSessions(ctx, projectKey)
		resultCh <- result{sessions: sessions, err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(timeout):
		return nil, fmt.Errorf("SessionStore.ListSessions() timed out after %dms during resume materialization", timeout.Milliseconds())
	case result := <-resultCh:
		if result.err != nil {
			return nil, fmt.Errorf("SessionStore.ListSessions() failed during resume materialization: %w", result.err)
		}
		return result.sessions, nil
	}
}

func writeStoreJSONL(path string, entries []SessionStoreEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	for _, entry := range entries {
		if err := encoder.Encode(entry); err != nil {
			return err
		}
	}
	return nil
}

func sessionStoreLoadTimeout(timeoutMS int) time.Duration {
	if timeoutMS <= 0 {
		timeoutMS = 60_000
	}
	return time.Duration(timeoutMS) * time.Millisecond
}

func isSafeStoreSubpath(subpath string) bool {
	if subpath == "" || filepath.IsAbs(subpath) {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(subpath))
	return clean != "." && clean != ".." && !stringsHasPathTraversal(clean)
}

func stringsHasPathTraversal(path string) bool {
	for _, part := range splitPathParts(path) {
		if part == ".." {
			return true
		}
	}
	return false
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
