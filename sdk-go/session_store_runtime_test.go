package claudeagentsdk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestQueryMirrorsTranscriptFramesToSessionStore(t *testing.T) {
	ctx := context.Background()
	cliPath := buildFakeCLI(t)
	store := NewInMemorySessionStore()
	configDir := t.TempDir()
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	projectKey := ProjectKeyForDirectory(workspace)

	messages, err := Query(ctx, "mirror me", ClaudeAgentOptions{
		CLIPath:      &cliPath,
		Cwd:          &workspace,
		SessionStore: store,
		Env: map[string]string{
			"FAKE_CLAUDE_MODE":  "mirror",
			"CLAUDE_CONFIG_DIR": configDir,
			"FAKE_PROJECT_KEY":  projectKey,
		},
	})
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected transcript_mirror to be hidden from consumers, got %#v", messages)
	}

	entries, err := store.Load(ctx, SessionKey{ProjectKey: projectKey, SessionID: "11111111-1111-4111-8111-111111111111"})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(entries) != 1 || entries[0]["uuid"] != "mirror-user" {
		t.Fatalf("unexpected mirrored entries: %#v", entries)
	}
}

func TestQueryBatchedSessionStoreFlushesOnResult(t *testing.T) {
	ctx := context.Background()
	cliPath := buildFakeCLI(t)
	store := NewInMemorySessionStore()
	configDir := t.TempDir()
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	projectKey := ProjectKeyForDirectory(workspace)
	key := SessionKey{ProjectKey: projectKey, SessionID: "11111111-1111-4111-8111-111111111111"}
	assistantSawEmptyStore := false
	resultSawFlushedStore := false

	err := QueryWithCallback(ctx, "mirror me", ClaudeAgentOptions{
		CLIPath:      &cliPath,
		Cwd:          &workspace,
		SessionStore: store,
		Env: map[string]string{
			"FAKE_CLAUDE_MODE":  "mirror",
			"CLAUDE_CONFIG_DIR": configDir,
			"FAKE_PROJECT_KEY":  projectKey,
		},
	}, func(message Message) error {
		entries, err := store.Load(ctx, key)
		if err != nil {
			return err
		}
		switch message.(type) {
		case *AssistantMessage:
			assistantSawEmptyStore = len(entries) == 0
		case *ResultMessage:
			resultSawFlushedStore = len(entries) == 1
		}
		return nil
	})
	if err != nil {
		t.Fatalf("QueryWithCallback returned error: %v", err)
	}
	if !assistantSawEmptyStore || !resultSawFlushedStore {
		t.Fatalf("expected batched flush on result, assistant empty=%t result flushed=%t", assistantSawEmptyStore, resultSawFlushedStore)
	}
}

func TestQueryMaterializesStoreBackedResume(t *testing.T) {
	ctx := context.Background()
	cliPath := buildFakeCLI(t)
	store := NewInMemorySessionStore()
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	sourceConfigDir := filepath.Join(t.TempDir(), "source-claude")
	if err := os.MkdirAll(sourceConfigDir, 0o755); err != nil {
		t.Fatalf("failed to create source config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceConfigDir, ".credentials.json"), []byte(`{"token":"test"}`), 0o600); err != nil {
		t.Fatalf("failed to write source credentials: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceConfigDir, ".claude.json"), []byte(`{"projects":{}}`), 0o600); err != nil {
		t.Fatalf("failed to write source claude config: %v", err)
	}
	projectKey := ProjectKeyForDirectory(workspace)
	sessionID := "33333333-3333-4333-8333-333333333333"
	if err := store.Append(ctx, SessionKey{ProjectKey: projectKey, SessionID: sessionID}, []SessionStoreEntry{
		{
			"type":      "user",
			"uuid":      "stored-user",
			"sessionId": sessionID,
			"message": map[string]any{
				"role":    "user",
				"content": "stored prompt",
			},
		},
	}); err != nil {
		t.Fatalf("Append returned error: %v", err)
	}

	messages, err := Query(ctx, "resume", ClaudeAgentOptions{
		CLIPath:      &cliPath,
		Cwd:          &workspace,
		Resume:       &sessionID,
		SessionStore: store,
		Env: map[string]string{
			"FAKE_CLAUDE_MODE":       "materialized",
			"FAKE_PROJECT_KEY":       projectKey,
			"FAKE_EXPECT_SESSION":    sessionID,
			"FAKE_EXPECT_AUTH_FILES": "1",
			"CLAUDE_CONFIG_DIR":      sourceConfigDir,
		},
	})
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if got := assistantText(t, messages[0].(*AssistantMessage)); got != "materialized" {
		t.Fatalf("unexpected assistant text: %q", got)
	}
}

func TestClientMirrorsTranscriptFramesToSessionStore(t *testing.T) {
	store := NewInMemorySessionStore()
	configDir := t.TempDir()
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	projectKey := ProjectKeyForDirectory(workspace)
	client := newFakeClient(t, "mirror", ClientOptions{
		ClaudeAgentOptions: ClaudeAgentOptions{
			Cwd:          &workspace,
			SessionStore: store,
			Env: map[string]string{
				"CLAUDE_CONFIG_DIR": configDir,
				"FAKE_PROJECT_KEY":  projectKey,
			},
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	defer client.Close()
	if err := client.Query(ctx, "client mirror"); err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	messages := receiveResponse(t, client)
	if len(messages) != 2 {
		t.Fatalf("expected transcript_mirror to be hidden from client Receive, got %#v", messages)
	}

	entries, err := store.Load(ctx, SessionKey{ProjectKey: projectKey, SessionID: "22222222-2222-4222-8222-222222222222"})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(entries) != 1 || entries[0]["uuid"] != "client-mirror-user" {
		t.Fatalf("unexpected mirrored entries: %#v", entries)
	}
}

func TestSessionStoreOptionValidation(t *testing.T) {
	minimal := minimalSessionStore{}
	_, err := Query(context.Background(), "hello", ClaudeAgentOptions{
		ContinueConversation: true,
		SessionStore:         minimal,
	})
	if err == nil || !stringsContains(err.Error(), "requires the store to implement ListSessions") {
		t.Fatalf("expected ListSessions validation error, got %v", err)
	}

	_, err = Query(context.Background(), "hello", ClaudeAgentOptions{
		EnableFileCheckpointing: true,
		SessionStore:            minimal,
	})
	if err == nil || !stringsContains(err.Error(), "cannot be combined with enable_file_checkpointing") {
		t.Fatalf("expected file checkpoint validation error, got %v", err)
	}
}

type minimalSessionStore struct{}

func (minimalSessionStore) Append(context.Context, SessionKey, []SessionStoreEntry) error {
	return errors.New("not used")
}

func (minimalSessionStore) Load(context.Context, SessionKey) ([]SessionStoreEntry, error) {
	return nil, nil
}

func stringsContains(value string, needle string) bool {
	return len(needle) == 0 || (len(value) >= len(needle) && containsSubstring(value, needle))
}

func containsSubstring(value string, needle string) bool {
	for i := 0; i+len(needle) <= len(value); i++ {
		if value[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
