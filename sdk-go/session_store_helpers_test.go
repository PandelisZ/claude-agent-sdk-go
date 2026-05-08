package claudeagentsdk

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSessionStoreBackedListingAndMessages(t *testing.T) {
	ctx := context.Background()
	store := NewInMemorySessionStore()
	directory := filepath.Join(t.TempDir(), "project")
	projectKey := ProjectKeyForDirectory(directory)
	sessionID := "66666666-6666-4666-8666-666666666666"
	key := SessionKey{ProjectKey: projectKey, SessionID: sessionID}

	entries := []SessionStoreEntry{
		{"type": "user", "uuid": "user-1", "sessionId": sessionID, "timestamp": "2026-01-02T03:04:05.000Z", "cwd": directory, "message": map[string]any{"role": "user", "content": "First prompt"}},
		{"type": "assistant", "uuid": "assistant-1", "parentUuid": "user-1", "sessionId": sessionID, "message": map[string]any{"role": "assistant", "content": "Answer"}},
		{"type": "custom-title", "customTitle": "Stored title", "sessionId": sessionID},
	}
	if err := store.Append(ctx, key, entries); err != nil {
		t.Fatalf("Append returned error: %v", err)
	}
	subpath := "subagents/workflows/run-1/agent-reviewer"
	if err := store.Append(ctx, SessionKey{ProjectKey: projectKey, SessionID: sessionID, Subpath: &subpath}, []SessionStoreEntry{
		{"type": "agent_metadata", "agentType": "reviewer"},
		{"type": "user", "uuid": "sub-user", "sessionId": sessionID, "message": map[string]any{"role": "user", "content": "Inspect"}, "parentUuid": ""},
		{"type": "assistant", "uuid": "sub-assistant", "sessionId": sessionID, "message": map[string]any{"role": "assistant", "content": "Done"}, "parentUuid": "sub-user"},
	}); err != nil {
		t.Fatalf("Append subagent returned error: %v", err)
	}

	sessions, err := ListSessionsFromStore(ctx, store, StoreSessionQueryOptions{Directory: directory})
	if err != nil {
		t.Fatalf("ListSessionsFromStore returned error: %v", err)
	}
	if len(sessions) != 1 || sessions[0].SessionID != sessionID || sessions[0].Summary != "Stored title" {
		t.Fatalf("unexpected store sessions: %#v", sessions)
	}

	info, err := GetSessionInfoFromStore(ctx, store, sessionID, StoreSessionQueryOptions{Directory: directory})
	if err != nil {
		t.Fatalf("GetSessionInfoFromStore returned error: %v", err)
	}
	if info == nil || info.FirstPrompt == nil || *info.FirstPrompt != "First prompt" {
		t.Fatalf("unexpected store session info: %#v", info)
	}

	messages, err := GetSessionMessagesFromStore(ctx, store, sessionID, StoreSessionQueryOptions{Directory: directory, Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("GetSessionMessagesFromStore returned error: %v", err)
	}
	if len(messages) != 1 || messages[0].UUID != "assistant-1" {
		t.Fatalf("unexpected paged store messages: %#v", messages)
	}

	agents, err := ListSubagentsFromStore(ctx, store, sessionID, StoreSessionQueryOptions{Directory: directory})
	if err != nil {
		t.Fatalf("ListSubagentsFromStore returned error: %v", err)
	}
	if !reflect.DeepEqual(agents, []string{"reviewer"}) {
		t.Fatalf("unexpected store subagents: %#v", agents)
	}

	subMessages, err := GetSubagentMessagesFromStore(ctx, store, sessionID, "reviewer", StoreSessionQueryOptions{Directory: directory})
	if err != nil {
		t.Fatalf("GetSubagentMessagesFromStore returned error: %v", err)
	}
	if len(subMessages) != 2 || subMessages[1].UUID != "sub-assistant" {
		t.Fatalf("unexpected store subagent messages: %#v", subMessages)
	}
}

func TestSessionStoreBackedMutationsAndImport(t *testing.T) {
	ctx := context.Background()
	store := NewInMemorySessionStore()
	configDir := filepath.Join(t.TempDir(), "claude-config")
	stageFixtureProject(t, "project-alpha", configDir, projectAlphaPath)
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	if err := ImportSessionToStore(ctx, alphaMetadataSessionID, store, ImportSessionOptions{Directory: projectAlphaPath}); err != nil {
		t.Fatalf("ImportSessionToStore returned error: %v", err)
	}
	imported, err := GetSessionInfoFromStore(ctx, store, alphaMetadataSessionID, StoreSessionQueryOptions{Directory: projectAlphaPath})
	if err != nil {
		t.Fatalf("GetSessionInfoFromStore(imported) returned error: %v", err)
	}
	if imported == nil || imported.Summary != "Renamed alpha" {
		t.Fatalf("unexpected imported session info: %#v", imported)
	}

	if err := RenameSessionViaStore(ctx, store, alphaMetadataSessionID, "Store title", StoreSessionMutationOptions{Directory: projectAlphaPath}); err != nil {
		t.Fatalf("RenameSessionViaStore returned error: %v", err)
	}
	tag := "store-tag"
	if err := TagSessionViaStore(ctx, store, alphaMetadataSessionID, &tag, StoreSessionMutationOptions{Directory: projectAlphaPath}); err != nil {
		t.Fatalf("TagSessionViaStore returned error: %v", err)
	}
	renamed, err := GetSessionInfoFromStore(ctx, store, alphaMetadataSessionID, StoreSessionQueryOptions{Directory: projectAlphaPath})
	if err != nil {
		t.Fatalf("GetSessionInfoFromStore(renamed) returned error: %v", err)
	}
	if renamed == nil || renamed.Summary != "Store title" || renamed.Tag == nil || *renamed.Tag != "store-tag" {
		t.Fatalf("unexpected renamed store info: %#v", renamed)
	}

	fork, err := ForkSessionViaStore(ctx, store, alphaMetadataSessionID, StoreSessionMutationOptions{Directory: projectAlphaPath, Title: "Store fork"})
	if err != nil {
		t.Fatalf("ForkSessionViaStore returned error: %v", err)
	}
	if fork.SessionID == "" || fork.SessionID == alphaMetadataSessionID {
		t.Fatalf("unexpected store fork result: %#v", fork)
	}
	forked, err := GetSessionMessagesFromStore(ctx, store, fork.SessionID, StoreSessionQueryOptions{Directory: projectAlphaPath})
	if err != nil {
		t.Fatalf("GetSessionMessagesFromStore(fork) returned error: %v", err)
	}
	if len(forked) != 2 || forked[0].SessionID != fork.SessionID {
		t.Fatalf("unexpected forked store messages: %#v", forked)
	}

	if err := DeleteSessionViaStore(ctx, store, alphaMetadataSessionID, StoreSessionMutationOptions{Directory: projectAlphaPath}); err != nil {
		t.Fatalf("DeleteSessionViaStore returned error: %v", err)
	}
	deleted, err := store.Load(ctx, SessionKey{ProjectKey: ProjectKeyForDirectory(projectAlphaPath), SessionID: alphaMetadataSessionID})
	if err != nil {
		t.Fatalf("Load after delete returned error: %v", err)
	}
	if deleted != nil {
		t.Fatalf("expected deleted store session, got %#v", deleted)
	}
}

func TestImportSessionToStoreIncludesSubagents(t *testing.T) {
	ctx := context.Background()
	store := NewInMemorySessionStore()
	configDir := filepath.Join(t.TempDir(), "claude-config")
	stageFixtureProject(t, "project-alpha", configDir, projectAlphaPath)
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	projectDir := filepath.Join(configDir, "projects", sanitizePathForTests(canonicalizePathForTests(projectAlphaPath)))
	subagentsDir := filepath.Join(projectDir, alphaMetadataSessionID, "subagents")
	if err := os.MkdirAll(subagentsDir, 0o755); err != nil {
		t.Fatalf("failed to create subagents dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subagentsDir, "agent-reviewer.jsonl"), []byte(`{"type":"user","uuid":"sub-user","sessionId":"`+alphaMetadataSessionID+`","message":{"role":"user","content":"Review"}}`+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write subagent transcript: %v", err)
	}

	if err := ImportSessionToStore(ctx, alphaMetadataSessionID, store, ImportSessionOptions{Directory: projectAlphaPath, IncludeSubagents: true}); err != nil {
		t.Fatalf("ImportSessionToStore returned error: %v", err)
	}
	agents, err := ListSubagentsFromStore(ctx, store, alphaMetadataSessionID, StoreSessionQueryOptions{Directory: projectAlphaPath})
	if err != nil {
		t.Fatalf("ListSubagentsFromStore returned error: %v", err)
	}
	if !reflect.DeepEqual(agents, []string{"reviewer"}) {
		t.Fatalf("unexpected imported subagents: %#v", agents)
	}
}

func TestFoldSessionSummaryDerivesStoreSidecar(t *testing.T) {
	key := SessionKey{ProjectKey: "proj", SessionID: "77777777-7777-4777-8777-777777777777"}
	summary := FoldSessionSummary(nil, key, []SessionStoreEntry{
		{
			"type":      "user",
			"uuid":      "user-1",
			"timestamp": "2026-01-02T03:04:05.000Z",
			"cwd":       "/tmp/project",
			"gitBranch": "main",
			"message": map[string]any{
				"role":    "user",
				"content": "Draft a release plan",
			},
		},
		{
			"type":        "custom-title",
			"customTitle": "Release plan",
		},
	})
	summary.MTime = 1234

	if summary.SessionID != key.SessionID {
		t.Fatalf("unexpected summary session id: %#v", summary)
	}
	if summary.Data["first_prompt"] != "Draft a release plan" || summary.Data["custom_title"] != "Release plan" {
		t.Fatalf("unexpected folded summary data: %#v", summary.Data)
	}

	next := FoldSessionSummary(&summary, key, []SessionStoreEntry{
		{"type": "tag", "tag": "release"},
		{"type": "metadata", "lastPrompt": "Latest prompt"},
	})
	if next.MTime != 1234 {
		t.Fatalf("fold should preserve mtime placeholder, got %d", next.MTime)
	}
	if next.Data["tag"] != "release" || next.Data["last_prompt"] != "Latest prompt" {
		t.Fatalf("unexpected updated summary data: %#v", next.Data)
	}

	info := SummaryEntryToSDKInfo(next, "/tmp/project")
	if info == nil || info.Summary != "Release plan" || info.FirstPrompt == nil || *info.FirstPrompt != "Draft a release plan" {
		t.Fatalf("unexpected summary info: %#v", info)
	}
}
