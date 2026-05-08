package claudeagentsdk

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type StoreSessionQueryOptions = SessionQueryOptions
type StoreSessionMutationOptions = SessionMutationOptions

type ImportSessionOptions struct {
	Directory        string
	IncludeSubagents bool
	BatchSize        int
}

type storeEntry struct {
	Type        string
	UUID        string
	ParentUUID  string
	SessionID   string
	Message     any
	IsSidechain bool
	IsMeta      bool
	TeamName    string
	Raw         SessionStoreEntry
}

func ListSessionsFromStore(ctx context.Context, store SessionStore, options StoreSessionQueryOptions) ([]SDKSessionInfo, error) {
	lister, ok := store.(SessionLister)
	if !ok {
		return nil, fmt.Errorf("session store does not implement ListSessions")
	}
	projectKey := ProjectKeyForDirectory(options.Directory)
	listing, err := lister.ListSessions(ctx, projectKey)
	if err != nil {
		return nil, err
	}
	infos := make([]SDKSessionInfo, 0, len(listing))
	for _, item := range listing {
		info, err := GetSessionInfoFromStore(ctx, store, item.SessionID, options)
		if err != nil {
			return nil, err
		}
		if info == nil {
			continue
		}
		if info.LastModified == 0 {
			info.LastModified = item.MTime
		}
		infos = append(infos, *info)
	}
	sort.Slice(infos, func(i, j int) bool {
		if infos[i].LastModified == infos[j].LastModified {
			return infos[i].SessionID < infos[j].SessionID
		}
		return infos[i].LastModified > infos[j].LastModified
	})
	return applyStoreSessionPaging(infos, options.Limit, options.Offset), nil
}

func GetSessionInfoFromStore(ctx context.Context, store SessionStore, sessionID string, options StoreSessionQueryOptions) (*SDKSessionInfo, error) {
	if !isValidSessionUUID(sessionID) {
		return nil, nil
	}
	projectKey := ProjectKeyForDirectory(options.Directory)
	entries, err := store.Load(ctx, SessionKey{ProjectKey: projectKey, SessionID: sessionID})
	if err != nil || len(entries) == 0 {
		return nil, err
	}
	info := deriveStoreSessionInfo(sessionID, options.Directory, entries)
	return &info, nil
}

func GetSessionMessagesFromStore(ctx context.Context, store SessionStore, sessionID string, options StoreSessionQueryOptions) ([]SessionMessage, error) {
	if !isValidSessionUUID(sessionID) {
		return []SessionMessage{}, nil
	}
	projectKey := ProjectKeyForDirectory(options.Directory)
	entries, err := store.Load(ctx, SessionKey{ProjectKey: projectKey, SessionID: sessionID})
	if err != nil || len(entries) == 0 {
		return []SessionMessage{}, err
	}
	return storeEntriesToMessages(entries, options.Limit, options.Offset, false), nil
}

func ListSubagentsFromStore(ctx context.Context, store SessionStore, sessionID string, options StoreSessionQueryOptions) ([]string, error) {
	if !isValidSessionUUID(sessionID) {
		return []string{}, nil
	}
	subkeyLister, ok := store.(SessionSubkeyLister)
	if !ok {
		return nil, fmt.Errorf("session store does not implement ListSubkeys")
	}
	subkeys, err := subkeyLister.ListSubkeys(ctx, SessionListSubkeysKey{
		ProjectKey: ProjectKeyForDirectory(options.Directory),
		SessionID:  sessionID,
	})
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	agents := make([]string, 0)
	for _, subkey := range subkeys {
		if !strings.HasPrefix(subkey, "subagents/") {
			continue
		}
		last := subkey[strings.LastIndex(subkey, "/")+1:]
		if !strings.HasPrefix(last, "agent-") {
			continue
		}
		agentID := strings.TrimPrefix(last, "agent-")
		if _, ok := seen[agentID]; ok {
			continue
		}
		seen[agentID] = struct{}{}
		agents = append(agents, agentID)
	}
	return agents, nil
}

func GetSubagentMessagesFromStore(ctx context.Context, store SessionStore, sessionID string, agentID string, options StoreSessionQueryOptions) ([]SessionMessage, error) {
	if !isValidSessionUUID(sessionID) || agentID == "" {
		return []SessionMessage{}, nil
	}
	projectKey := ProjectKeyForDirectory(options.Directory)
	subpath := "subagents/agent-" + agentID
	if subkeyLister, ok := store.(SessionSubkeyLister); ok {
		subkeys, err := subkeyLister.ListSubkeys(ctx, SessionListSubkeysKey{ProjectKey: projectKey, SessionID: sessionID})
		if err != nil {
			return nil, err
		}
		for _, candidate := range subkeys {
			if strings.HasSuffix(candidate, "/agent-"+agentID) || candidate == "subagents/agent-"+agentID {
				subpath = candidate
				break
			}
		}
	}
	entries, err := store.Load(ctx, SessionKey{ProjectKey: projectKey, SessionID: sessionID, Subpath: &subpath})
	if err != nil || len(entries) == 0 {
		return []SessionMessage{}, err
	}
	return storeEntriesToMessages(entries, options.Limit, options.Offset, true), nil
}

func RenameSessionViaStore(ctx context.Context, store SessionStore, sessionID string, title string, options StoreSessionMutationOptions) error {
	if !isValidSessionUUID(sessionID) {
		return fmt.Errorf("invalid session ID %q", sessionID)
	}
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return fmt.Errorf("title must be non-empty")
	}
	return store.Append(ctx, storeMainKey(sessionID, options.Directory), []SessionStoreEntry{{
		"type":        "custom-title",
		"customTitle": trimmed,
		"sessionId":   sessionID,
		"uuid":        mustStoreUUID(),
		"timestamp":   time.Now().UTC().Format(time.RFC3339Nano),
	}})
}

func TagSessionViaStore(ctx context.Context, store SessionStore, sessionID string, tag *string, options StoreSessionMutationOptions) error {
	if !isValidSessionUUID(sessionID) {
		return fmt.Errorf("invalid session ID %q", sessionID)
	}
	value := ""
	if tag != nil {
		value = strings.TrimSpace(*tag)
		if value == "" {
			return fmt.Errorf("tag must be non-empty after sanitization (use nil to clear)")
		}
	}
	return store.Append(ctx, storeMainKey(sessionID, options.Directory), []SessionStoreEntry{{
		"type":      "tag",
		"tag":       value,
		"sessionId": sessionID,
		"uuid":      mustStoreUUID(),
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
	}})
}

func DeleteSessionViaStore(ctx context.Context, store SessionStore, sessionID string, options StoreSessionMutationOptions) error {
	if !isValidSessionUUID(sessionID) {
		return fmt.Errorf("invalid session ID %q", sessionID)
	}
	deleter, ok := store.(SessionDeleter)
	if !ok {
		return nil
	}
	return deleter.Delete(ctx, storeMainKey(sessionID, options.Directory))
}

func ForkSessionViaStore(ctx context.Context, store SessionStore, sessionID string, options StoreSessionMutationOptions) (ForkSessionResult, error) {
	if !isValidSessionUUID(sessionID) {
		return ForkSessionResult{}, fmt.Errorf("invalid session ID %q", sessionID)
	}
	if options.UpToMessageID != "" && !isValidSessionUUID(options.UpToMessageID) {
		return ForkSessionResult{}, fmt.Errorf("invalid up_to_message_id %q", options.UpToMessageID)
	}
	entries, err := store.Load(ctx, storeMainKey(sessionID, options.Directory))
	if err != nil {
		return ForkSessionResult{}, err
	}
	if len(entries) == 0 {
		return ForkSessionResult{}, fmt.Errorf("session %s not found", sessionID)
	}
	forkedSessionID := mustStoreUUID()
	lines, err := buildStoreForkLines(entries, sessionID, forkedSessionID, options.UpToMessageID, options.Title)
	if err != nil {
		return ForkSessionResult{}, err
	}
	forkedEntries := make([]SessionStoreEntry, 0, len(lines))
	for _, line := range lines {
		var entry SessionStoreEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return ForkSessionResult{}, err
		}
		forkedEntries = append(forkedEntries, entry)
	}
	if err := store.Append(ctx, storeMainKey(forkedSessionID, options.Directory), forkedEntries); err != nil {
		return ForkSessionResult{}, err
	}
	return ForkSessionResult{SessionID: forkedSessionID}, nil
}

func ImportSessionToStore(ctx context.Context, sessionID string, store SessionStore, options ImportSessionOptions) error {
	if !isValidSessionUUID(sessionID) {
		return fmt.Errorf("invalid session ID %q", sessionID)
	}
	sessionPath := localSessionPathForImport(sessionID, options.Directory)
	if sessionPath == "" {
		return fmt.Errorf("session %s not found: %w", sessionID, os.ErrNotExist)
	}
	projectKey := filepath.Base(filepath.Dir(sessionPath))
	if err := appendJSONLFileToStore(ctx, store, SessionKey{ProjectKey: projectKey, SessionID: sessionID}, sessionPath, options.BatchSize); err != nil {
		return err
	}
	if !options.IncludeSubagents {
		return nil
	}
	sessionDir := strings.TrimSuffix(sessionPath, ".jsonl")
	subagentsDir := filepath.Join(sessionDir, "subagents")
	return filepath.WalkDir(subagentsDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			return nil
		}
		rel, err := filepath.Rel(sessionDir, path)
		if err != nil {
			return err
		}
		subpath := strings.TrimSuffix(filepath.ToSlash(rel), ".jsonl")
		return appendJSONLFileToStore(ctx, store, SessionKey{ProjectKey: projectKey, SessionID: sessionID, Subpath: &subpath}, path, options.BatchSize)
	})
}

func storeMainKey(sessionID string, directory string) SessionKey {
	return SessionKey{ProjectKey: ProjectKeyForDirectory(directory), SessionID: sessionID}
}
