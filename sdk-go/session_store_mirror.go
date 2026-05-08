package claudeagentsdk

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/PandelisZ/claude-agent-sdk-go/sdk-go/internal/protocol"
)

type sessionStoreMirror struct {
	store     SessionStore
	env       map[string]string
	flushMode SessionStoreFlushMode
	pending   []sessionStoreMirrorBatch
}

type sessionStoreMirrorBatch struct {
	key     SessionKey
	entries []SessionStoreEntry
}

func newSessionStoreMirror(options ClaudeAgentOptions) *sessionStoreMirror {
	if options.SessionStore == nil {
		return nil
	}
	return &sessionStoreMirror{
		store:     options.SessionStore,
		env:       cloneStringMap(options.Env),
		flushMode: options.SessionStoreFlush,
	}
}

func (m *sessionStoreMirror) handlePayload(ctx context.Context, payload map[string]any) (bool, Message, error) {
	if m == nil || m.store == nil {
		return false, nil, nil
	}
	messageType, _ := protocol.StringValue(payload, "type")
	if messageType != "transcript_mirror" {
		return false, nil, nil
	}

	key, entries, err := sessionStoreMirrorFrame(m.env, payload)
	if err != nil {
		return true, &MirrorErrorMessage{
			SystemMessage: SystemMessage{Subtype: "mirror_error", Data: map[string]any{"type": "system", "subtype": "mirror_error", "error": err.Error()}},
			Error:         err.Error(),
		}, nil
	}
	if key == nil || len(entries) == 0 {
		return true, nil, nil
	}
	if m.flushMode == SessionStoreFlushModeEager {
		return true, m.append(ctx, *key, entries), nil
	}
	m.pending = append(m.pending, sessionStoreMirrorBatch{key: *key, entries: entries})
	return true, nil, nil
}

func (m *sessionStoreMirror) flush(ctx context.Context) Message {
	if m == nil || len(m.pending) == 0 {
		return nil
	}
	pending := m.pending
	m.pending = nil
	for _, batch := range pending {
		if message := m.append(ctx, batch.key, batch.entries); message != nil {
			return message
		}
	}
	return nil
}

func (m *sessionStoreMirror) append(ctx context.Context, key SessionKey, entries []SessionStoreEntry) Message {
	if err := m.store.Append(ctx, key, entries); err != nil {
		return &MirrorErrorMessage{
			SystemMessage: SystemMessage{Subtype: "mirror_error", Data: map[string]any{"type": "system", "subtype": "mirror_error", "error": err.Error(), "key": keyToMap(key)}},
			Key:           &key,
			Error:         err.Error(),
		}
	}
	return nil
}

func sessionStoreMirrorFrame(env map[string]string, payload map[string]any) (*SessionKey, []SessionStoreEntry, error) {
	filePath := firstQueryStringValue(payload, "filePath", "file_path")
	if filePath == "" {
		return nil, nil, fmt.Errorf("transcript_mirror frame missing filePath")
	}
	key := FilePathToSessionKey(filePath, sessionStoreProjectsDir(env))
	if key == nil {
		return nil, nil, nil
	}
	rawEntries, ok := protocol.SliceValue(payload, "entries")
	if !ok {
		return nil, nil, fmt.Errorf("transcript_mirror frame missing entries")
	}
	entries := make([]SessionStoreEntry, 0, len(rawEntries))
	for _, raw := range rawEntries {
		entry, ok := raw.(map[string]any)
		if !ok {
			return nil, nil, fmt.Errorf("transcript_mirror entries must be objects")
		}
		entries = append(entries, SessionStoreEntry(entry))
	}
	return key, entries, nil
}

func sessionStoreProjectsDir(env map[string]string) string {
	configDir := ""
	if env != nil {
		configDir = env["CLAUDE_CONFIG_DIR"]
	}
	if configDir == "" {
		configDir = os.Getenv("CLAUDE_CONFIG_DIR")
	}
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err == nil && home != "" {
			configDir = filepath.Join(home, ".claude")
		} else {
			configDir = filepath.Join(string(filepath.Separator), ".claude")
		}
	}
	return filepath.Join(configDir, "projects")
}

func keyToMap(key SessionKey) map[string]any {
	out := map[string]any{
		"project_key": key.ProjectKey,
		"session_id":  key.SessionID,
	}
	if key.Subpath != nil {
		out["subpath"] = *key.Subpath
	}
	return out
}
