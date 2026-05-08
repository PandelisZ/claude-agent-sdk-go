package claudeagentsdk

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestFilePathToSessionKeyMatchesPythonShapes(t *testing.T) {
	projectsDir := filepath.Join(string(filepath.Separator), "tmp", "claude", "projects")

	tests := []struct {
		name string
		path string
		want *SessionKey
	}{
		{
			name: "main transcript",
			path: filepath.Join(projectsDir, "-home-user-repo", "abc-123.jsonl"),
			want: &SessionKey{ProjectKey: "-home-user-repo", SessionID: "abc-123"},
		},
		{
			name: "subagent transcript",
			path: filepath.Join(projectsDir, "-home-user-repo", "abc-123", "subagents", "agent-xyz.jsonl"),
			want: &SessionKey{ProjectKey: "-home-user-repo", SessionID: "abc-123", Subpath: sessionStoreStringPtr("subagents/agent-xyz")},
		},
		{
			name: "nested subagent subpath",
			path: filepath.Join(projectsDir, "proj", "sess", "subagents", "nested", "agent-1.jsonl"),
			want: &SessionKey{ProjectKey: "proj", SessionID: "sess", Subpath: sessionStoreStringPtr("subagents/nested/agent-1")},
		},
		{
			name: "outside projects dir",
			path: filepath.Join(string(filepath.Separator), "elsewhere", "proj", "sess.jsonl"),
		},
		{
			name: "too few parts",
			path: filepath.Join(projectsDir, "proj-only.jsonl"),
		},
		{
			name: "three parts returns nil",
			path: filepath.Join(projectsDir, "proj", "sess", "weird.jsonl"),
		},
		{
			name: "main transcript requires jsonl suffix",
			path: filepath.Join(projectsDir, "proj", "sess.txt"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilePathToSessionKey(tt.path, projectsDir+string(filepath.Separator))
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("FilePathToSessionKey() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestProjectKeyForDirectoryMatchesSessionDirectorySanitization(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "workspace with spaces")
	t.Setenv("PWD", dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	want := sanitizePathForTests(canonicalizePathForTests(dir))
	got := ProjectKeyForDirectory(dir)
	if got != want {
		t.Fatalf("ProjectKeyForDirectory() = %q, want %q", got, want)
	}
}

func TestInMemorySessionStoreConformanceCoreAndOptionalMethods(t *testing.T) {
	ctx := context.Background()
	store := NewInMemorySessionStore()
	var _ SessionStore = store
	var _ SessionLister = store
	var _ SessionSummaryLister = store
	var _ SessionDeleter = store
	var _ SessionSubkeyLister = store

	key := SessionKey{ProjectKey: "proj", SessionID: "sess"}
	sub1 := SessionKey{ProjectKey: "proj", SessionID: "sess", Subpath: sessionStoreStringPtr("subagents/agent-1")}
	sub2 := SessionKey{ProjectKey: "proj", SessionID: "sess", Subpath: sessionStoreStringPtr("subagents/agent-2")}
	otherProject := SessionKey{ProjectKey: "other", SessionID: "sess"}

	if loaded, err := store.Load(ctx, key); err != nil || loaded != nil {
		t.Fatalf("Load unknown = %#v, %v; want nil, nil", loaded, err)
	}

	first := []SessionStoreEntry{{"type": "x", "uuid": "z", "n": 1}}
	second := []SessionStoreEntry{{"type": "x", "uuid": "a", "n": 2}, {"type": "x", "uuid": "m", "n": 3}}
	if err := store.Append(ctx, key, first); err != nil {
		t.Fatalf("Append first returned error: %v", err)
	}
	if err := store.Append(ctx, key, nil); err != nil {
		t.Fatalf("Append empty returned error: %v", err)
	}
	if err := store.Append(ctx, key, second); err != nil {
		t.Fatalf("Append second returned error: %v", err)
	}
	if err := store.Append(ctx, sub1, []SessionStoreEntry{{"type": "x", "uuid": "s1"}}); err != nil {
		t.Fatalf("Append sub1 returned error: %v", err)
	}
	if err := store.Append(ctx, sub2, []SessionStoreEntry{{"type": "x", "uuid": "s2"}}); err != nil {
		t.Fatalf("Append sub2 returned error: %v", err)
	}
	if err := store.Append(ctx, otherProject, []SessionStoreEntry{{"type": "x", "from": "other"}}); err != nil {
		t.Fatalf("Append other project returned error: %v", err)
	}

	loaded, err := store.Load(ctx, key)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	wantLoaded := []SessionStoreEntry{
		{"type": "x", "uuid": "z", "n": 1},
		{"type": "x", "uuid": "a", "n": 2},
		{"type": "x", "uuid": "m", "n": 3},
	}
	if !reflect.DeepEqual(loaded, wantLoaded) {
		t.Fatalf("Load returned %#v, want %#v", loaded, wantLoaded)
	}

	listed, err := store.ListSessions(ctx, "proj")
	if err != nil {
		t.Fatalf("ListSessions returned error: %v", err)
	}
	if len(listed) != 1 || listed[0].SessionID != "sess" || listed[0].MTime <= 1_000_000_000_000 {
		t.Fatalf("unexpected ListSessions result: %#v", listed)
	}

	summaries, err := store.ListSessionSummaries(ctx, "proj")
	if err != nil {
		t.Fatalf("ListSessionSummaries returned error: %v", err)
	}
	if len(summaries) != 1 || summaries[0].SessionID != "sess" || summaries[0].MTime <= 1_000_000_000_000 {
		t.Fatalf("unexpected ListSessionSummaries result: %#v", summaries)
	}
	if _, ok := summaries[0].Data["entries_appended"]; !ok {
		t.Fatalf("expected opaque summary data to be maintained, got %#v", summaries[0].Data)
	}

	subkeys, err := store.ListSubkeys(ctx, SessionListSubkeysKey{ProjectKey: "proj", SessionID: "sess"})
	if err != nil {
		t.Fatalf("ListSubkeys returned error: %v", err)
	}
	sort.Strings(subkeys)
	if !reflect.DeepEqual(subkeys, []string{"subagents/agent-1", "subagents/agent-2"}) {
		t.Fatalf("ListSubkeys returned %#v", subkeys)
	}

	if store.Size() != 2 {
		t.Fatalf("Size() = %d, want 2 main transcripts", store.Size())
	}

	if err := store.Delete(ctx, sub1); err != nil {
		t.Fatalf("Delete subpath returned error: %v", err)
	}
	if loaded, err := store.Load(ctx, sub1); err != nil || loaded != nil {
		t.Fatalf("Load deleted subpath = %#v, %v; want nil, nil", loaded, err)
	}
	if loaded, err := store.Load(ctx, sub2); err != nil || len(loaded) != 1 {
		t.Fatalf("Load sibling subpath = %#v, %v; want existing entry", loaded, err)
	}

	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete main returned error: %v", err)
	}
	for _, deletedKey := range []SessionKey{key, sub2} {
		if loaded, err := store.Load(ctx, deletedKey); err != nil || loaded != nil {
			t.Fatalf("Load deleted key %#v = %#v, %v; want nil, nil", deletedKey, loaded, err)
		}
	}
	if loaded, err := store.Load(ctx, otherProject); err != nil || len(loaded) != 1 {
		t.Fatalf("Load other project = %#v, %v; want existing entry", loaded, err)
	}

	store.Clear()
	if store.Size() != 0 {
		t.Fatalf("Size after Clear() = %d, want 0", store.Size())
	}
}

func TestStoreBackedSessionHelpers(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "store workspace")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	projectKey := ProjectKeyForDirectory(dir)
	sessionID := "11111111-1111-4111-8111-111111111111"
	agentSubpath := sessionStoreStringPtr("subagents/workflows/run-1/agent-reviewer")
	store := NewInMemorySessionStore()

	mainKey := SessionKey{ProjectKey: projectKey, SessionID: sessionID}
	mainEntries := []SessionStoreEntry{
		{
			"type":      "user",
			"uuid":      "user-1",
			"sessionId": sessionID,
			"timestamp": "2026-01-02T03:04:05.000Z",
			"cwd":       dir,
			"message": map[string]any{
				"role":    "user",
				"content": "Plan the migration",
			},
		},
		{
			"type":       "assistant",
			"uuid":       "assistant-1",
			"parentUuid": "user-1",
			"sessionId":  sessionID,
			"timestamp":  "2026-01-02T03:05:05.000Z",
			"message": map[string]any{
				"role":    "assistant",
				"content": "Plan ready",
			},
		},
		{
			"type":        "custom-title",
			"uuid":        "title-1",
			"sessionId":   sessionID,
			"timestamp":   "2026-01-02T03:06:05.000Z",
			"customTitle": "Migration plan",
		},
	}
	if err := store.Append(ctx, mainKey, mainEntries); err != nil {
		t.Fatalf("Append main returned error: %v", err)
	}
	if err := store.Append(ctx, SessionKey{ProjectKey: projectKey, SessionID: sessionID, Subpath: agentSubpath}, []SessionStoreEntry{
		{
			"type":      "user",
			"uuid":      "agent-user-1",
			"sessionId": sessionID,
			"message": map[string]any{
				"role":    "user",
				"content": "Inspect",
			},
		},
		{
			"type":       "assistant",
			"uuid":       "agent-assistant-1",
			"parentUuid": "agent-user-1",
			"sessionId":  sessionID,
			"message": map[string]any{
				"role":    "assistant",
				"content": "Inspected",
			},
		},
	}); err != nil {
		t.Fatalf("Append subagent returned error: %v", err)
	}

	sessions, err := ListSessionsFromStore(ctx, store, StoreSessionQueryOptions{Directory: dir})
	if err != nil {
		t.Fatalf("ListSessionsFromStore returned error: %v", err)
	}
	if len(sessions) != 1 || sessions[0].SessionID != sessionID || sessions[0].Summary != "Migration plan" {
		t.Fatalf("unexpected store sessions: %#v", sessions)
	}

	info, err := GetSessionInfoFromStore(ctx, store, sessionID, StoreSessionQueryOptions{Directory: dir})
	if err != nil {
		t.Fatalf("GetSessionInfoFromStore returned error: %v", err)
	}
	if info == nil || info.FirstPrompt == nil || *info.FirstPrompt != "Plan the migration" {
		t.Fatalf("unexpected store session info: %#v", info)
	}

	messages, err := GetSessionMessagesFromStore(ctx, store, sessionID, StoreSessionQueryOptions{Directory: dir})
	if err != nil {
		t.Fatalf("GetSessionMessagesFromStore returned error: %v", err)
	}
	if len(messages) != 2 || messages[0].UUID != "user-1" || messages[1].UUID != "assistant-1" {
		t.Fatalf("unexpected store session messages: %#v", messages)
	}

	agents, err := ListSubagentsFromStore(ctx, store, sessionID, StoreSessionQueryOptions{Directory: dir})
	if err != nil {
		t.Fatalf("ListSubagentsFromStore returned error: %v", err)
	}
	if !reflect.DeepEqual(agents, []string{"reviewer"}) {
		t.Fatalf("unexpected store subagents: %#v", agents)
	}

	agentMessages, err := GetSubagentMessagesFromStore(ctx, store, sessionID, "reviewer", StoreSessionQueryOptions{Directory: dir})
	if err != nil {
		t.Fatalf("GetSubagentMessagesFromStore returned error: %v", err)
	}
	if len(agentMessages) != 2 || agentMessages[0].UUID != "agent-user-1" || agentMessages[1].UUID != "agent-assistant-1" {
		t.Fatalf("unexpected store subagent messages: %#v", agentMessages)
	}
}

func sessionStoreStringPtr(value string) *string {
	return &value
}
