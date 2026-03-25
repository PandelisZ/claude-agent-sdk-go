package claudeagentsdk

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	projectAlphaPath = "/tmp/workspace/project-alpha"
	projectBetaPath  = "/tmp/workspace/project-beta"

	alphaMetadataSessionID   = "11111111-1111-4111-8111-111111111111"
	alphaSidechainSessionID  = "22222222-2222-4222-8222-222222222222"
	alphaMetaOnlySessionID   = "33333333-3333-4333-8333-333333333333"
	betaAITitleSessionID     = "44444444-4444-4444-8444-444444444444"
	alphaTranscriptSessionID = "55555555-5555-4555-8555-555555555555"
)

func TestListSessionsHonorsClaudeConfigDirAndExtractsMetadata(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "claude-config")
	stageFixtureProject(t, "project-alpha", configDir, projectAlphaPath)
	stageFixtureProject(t, "project-beta", configDir, projectBetaPath)

	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	setSessionMtime(t, configDir, projectAlphaPath, alphaMetadataSessionID, time.Unix(1_700_000_000, 0))
	setSessionMtime(t, configDir, projectAlphaPath, alphaTranscriptSessionID, time.Unix(1_700_000_100, 0))
	setSessionMtime(t, configDir, projectBetaPath, betaAITitleSessionID, time.Unix(1_700_000_200, 0))

	falseValue := false
	sessions, err := ListSessions(ListSessionsOptions{
		Directory:        projectAlphaPath,
		IncludeWorktrees: &falseValue,
	})
	if err != nil {
		t.Fatalf("ListSessions returned error: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 visible sessions, got %d", len(sessions))
	}

	if sessions[0].SessionID != alphaTranscriptSessionID {
		t.Fatalf("expected newest alpha transcript session first, got %s", sessions[0].SessionID)
	}
	if sessions[1].SessionID != alphaMetadataSessionID {
		t.Fatalf("expected metadata session second, got %s", sessions[1].SessionID)
	}
	for _, session := range sessions {
		if session.SessionID == alphaSidechainSessionID || session.SessionID == alphaMetaOnlySessionID {
			t.Fatalf("unexpected filtered session in results: %s", session.SessionID)
		}
	}

	info := sessions[1]
	if info.Summary != "Renamed alpha" {
		t.Fatalf("expected custom title summary, got %q", info.Summary)
	}
	if info.CustomTitle == nil || *info.CustomTitle != "Renamed alpha" {
		t.Fatalf("expected custom title, got %#v", info.CustomTitle)
	}
	if info.FirstPrompt == nil || *info.FirstPrompt != "Draft a release plan" {
		t.Fatalf("unexpected first prompt: %#v", info.FirstPrompt)
	}
	if info.GitBranch == nil || *info.GitBranch != "release/1.2" {
		t.Fatalf("unexpected git branch: %#v", info.GitBranch)
	}
	if info.Cwd == nil || *info.Cwd != projectAlphaPath {
		t.Fatalf("unexpected cwd: %#v", info.Cwd)
	}
	if info.Tag == nil || *info.Tag != "alpha-tag" {
		t.Fatalf("unexpected tag: %#v", info.Tag)
	}
	if info.CreatedAt == nil {
		t.Fatal("expected created_at to be populated")
	}
	wantCreatedAt := mustParseRFC3339Millis(t, "2026-01-02T03:04:05.000Z")
	if *info.CreatedAt != wantCreatedAt {
		t.Fatalf("unexpected created_at: got %d want %d", *info.CreatedAt, wantCreatedAt)
	}
	if info.FileSize == 0 || info.LastModified == 0 {
		t.Fatalf("expected file metadata to be populated, got size=%d mtime=%d", info.FileSize, info.LastModified)
	}

	allSessions, err := ListSessions(ListSessionsOptions{Limit: 2})
	if err != nil {
		t.Fatalf("ListSessions(all) returned error: %v", err)
	}
	if len(allSessions) != 2 {
		t.Fatalf("expected limit=2 to return 2 sessions, got %d", len(allSessions))
	}
	if allSessions[0].SessionID != betaAITitleSessionID {
		t.Fatalf("expected newest session first across all projects, got %s", allSessions[0].SessionID)
	}
}

func TestListSessionsUsesDefaultHomeWhenEnvUnset(t *testing.T) {
	homeDir := filepath.Join(t.TempDir(), "home")
	configDir := filepath.Join(homeDir, ".claude")
	stageFixtureProject(t, "project-beta", configDir, projectBetaPath)

	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)

	falseValue := false
	sessions, err := ListSessions(ListSessionsOptions{
		Directory:        projectBetaPath,
		IncludeWorktrees: &falseValue,
	})
	if err != nil {
		t.Fatalf("ListSessions returned error: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 beta session, got %d", len(sessions))
	}
	if sessions[0].Summary != "AI generated beta title" {
		t.Fatalf("expected aiTitle fallback summary, got %q", sessions[0].Summary)
	}
	if sessions[0].CustomTitle == nil || *sessions[0].CustomTitle != "AI generated beta title" {
		t.Fatalf("expected aiTitle to populate CustomTitle, got %#v", sessions[0].CustomTitle)
	}
}

func TestListSessionsIncludesWorktreesByDefault(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for worktree coverage")
	}

	configDir := filepath.Join(t.TempDir(), "claude-config")
	repoRoot := filepath.Join(t.TempDir(), "repo")
	worktreePath := filepath.Join(t.TempDir(), "repo-worktree")
	createGitWorktree(t, repoRoot, worktreePath)

	stageFixtureProject(t, "project-alpha", configDir, repoRoot)
	stageFixtureProject(t, "project-beta", configDir, worktreePath)
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	withWorktrees, err := ListSessions(ListSessionsOptions{Directory: repoRoot})
	if err != nil {
		t.Fatalf("ListSessions with worktrees returned error: %v", err)
	}
	if len(withWorktrees) != 3 {
		t.Fatalf("expected main repo + worktree sessions, got %d", len(withWorktrees))
	}

	falseValue := false
	withoutWorktrees, err := ListSessions(ListSessionsOptions{
		Directory:        repoRoot,
		IncludeWorktrees: &falseValue,
	})
	if err != nil {
		t.Fatalf("ListSessions without worktrees returned error: %v", err)
	}
	if len(withoutWorktrees) != 2 {
		t.Fatalf("expected only root project sessions when worktrees disabled, got %d", len(withoutWorktrees))
	}
}

func TestGetSessionInfoValidatesUUIDAndReturnsNilWhenMissing(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "claude-config")
	stageFixtureProject(t, "project-alpha", configDir, projectAlphaPath)
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	if _, err := GetSessionInfo("not-a-uuid", SessionQueryOptions{}); err == nil {
		t.Fatal("expected invalid UUID error")
	}

	info, err := GetSessionInfo(alphaMetadataSessionID, SessionQueryOptions{Directory: projectAlphaPath})
	if err != nil {
		t.Fatalf("GetSessionInfo returned error: %v", err)
	}
	if info == nil || info.SessionID != alphaMetadataSessionID {
		t.Fatalf("unexpected session info: %#v", info)
	}

	missing, err := GetSessionInfo("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", SessionQueryOptions{Directory: projectAlphaPath})
	if err != nil {
		t.Fatalf("expected missing session to return nil without error, got %v", err)
	}
	if missing != nil {
		t.Fatalf("expected nil for missing session, got %#v", missing)
	}

	sidechainInfo, err := GetSessionInfo(alphaSidechainSessionID, SessionQueryOptions{Directory: projectAlphaPath})
	if err != nil {
		t.Fatalf("expected sidechain lookup to return nil without error, got %v", err)
	}
	if sidechainInfo != nil {
		t.Fatalf("expected sidechain session info to be filtered, got %#v", sidechainInfo)
	}
}

func TestGetSessionMessagesReconstructsTranscriptAndPaginates(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "claude-config")
	stageFixtureProject(t, "project-alpha", configDir, projectAlphaPath)
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	if _, err := GetSessionMessages("not-a-uuid", SessionQueryOptions{}); err == nil {
		t.Fatal("expected invalid UUID error")
	}

	messages, err := GetSessionMessages(alphaTranscriptSessionID, SessionQueryOptions{Directory: projectAlphaPath})
	if err != nil {
		t.Fatalf("GetSessionMessages returned error: %v", err)
	}
	if len(messages) != 5 {
		t.Fatalf("expected 5 visible transcript messages, got %d", len(messages))
	}

	wantTypes := []string{"user", "assistant", "user", "assistant", "assistant"}
	wantUUIDs := []string{
		"55555555-0000-4000-8000-000000000001",
		"55555555-0000-4000-8000-000000000002",
		"55555555-0000-4000-8000-000000000003",
		"55555555-0000-4000-8000-000000000004",
		"55555555-0000-4000-8000-000000000007",
	}
	for i, message := range messages {
		if message.Type != wantTypes[i] || message.UUID != wantUUIDs[i] {
			t.Fatalf("unexpected message[%d]: %#v", i, message)
		}
		if message.SessionID != alphaTranscriptSessionID {
			t.Fatalf("unexpected session id on message[%d]: %s", i, message.SessionID)
		}
		if message.ParentToolUseID != nil {
			t.Fatalf("expected parent tool use id to stay nil, got %#v", message.ParentToolUseID)
		}
	}

	firstMessageMap, ok := messages[0].Message.(map[string]any)
	if !ok {
		t.Fatalf("expected raw message payload map, got %T", messages[0].Message)
	}
	if firstMessageMap["role"] != "user" {
		t.Fatalf("unexpected first message payload: %#v", firstMessageMap)
	}

	page, err := GetSessionMessages(alphaTranscriptSessionID, SessionQueryOptions{
		Directory: projectAlphaPath,
		Offset:    1,
		Limit:     2,
	})
	if err != nil {
		t.Fatalf("GetSessionMessages pagination returned error: %v", err)
	}
	if len(page) != 2 || page[0].UUID != wantUUIDs[1] || page[1].UUID != wantUUIDs[2] {
		t.Fatalf("unexpected paginated messages: %#v", page)
	}

	allWithZeroLimit, err := GetSessionMessages(alphaTranscriptSessionID, SessionQueryOptions{
		Directory: projectAlphaPath,
		Limit:     0,
	})
	if err != nil {
		t.Fatalf("GetSessionMessages zero-limit returned error: %v", err)
	}
	if len(allWithZeroLimit) != len(messages) {
		t.Fatalf("expected zero limit to return all messages, got %d", len(allWithZeroLimit))
	}
}

func TestRenameAndTagSessionAppendMetadataAndClearTag(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "claude-config")
	stageFixtureProject(t, "project-alpha", configDir, projectAlphaPath)
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	if err := RenameSession(alphaMetadataSessionID, "  Final title  ", SessionMutationOptions{Directory: projectAlphaPath}); err != nil {
		t.Fatalf("RenameSession returned error: %v", err)
	}
	dirtyTag := " release\u200b-tag\ufeff "
	if err := TagSession(alphaMetadataSessionID, &dirtyTag, SessionMutationOptions{Directory: projectAlphaPath}); err != nil {
		t.Fatalf("TagSession returned error: %v", err)
	}
	if err := TagSession(alphaMetadataSessionID, nil, SessionMutationOptions{Directory: projectAlphaPath}); err != nil {
		t.Fatalf("TagSession(clear) returned error: %v", err)
	}

	info, err := GetSessionInfo(alphaMetadataSessionID, SessionQueryOptions{Directory: projectAlphaPath})
	if err != nil {
		t.Fatalf("GetSessionInfo after mutations returned error: %v", err)
	}
	if info == nil {
		t.Fatal("expected session info after mutations")
	}
	if info.CustomTitle == nil || *info.CustomTitle != "Final title" {
		t.Fatalf("unexpected custom title after rename: %#v", info.CustomTitle)
	}
	if info.Summary != "Final title" {
		t.Fatalf("expected summary to reflect renamed title, got %q", info.Summary)
	}
	if info.Tag != nil {
		t.Fatalf("expected cleared tag to read back as nil, got %#v", info.Tag)
	}

	sessionFile := filepath.Join(configDir, "projects", sanitizePathForTests(projectAlphaPath), alphaMetadataSessionID+".jsonl")
	lines := readNonEmptyLines(t, sessionFile)
	if !strings.Contains(lines[len(lines)-3], `"type":"custom-title","customTitle":"Final title","sessionId":"`+alphaMetadataSessionID+`"`) {
		t.Fatalf("expected compact custom-title entry near tail, got %q", lines[len(lines)-3])
	}
	if !strings.Contains(lines[len(lines)-2], `"type":"tag","tag":"release-tag","sessionId":"`+alphaMetadataSessionID+`"`) {
		t.Fatalf("expected sanitized compact tag entry, got %q", lines[len(lines)-2])
	}
	if !strings.Contains(lines[len(lines)-1], `"type":"tag","tag":"","sessionId":"`+alphaMetadataSessionID+`"`) {
		t.Fatalf("expected clear-tag entry at EOF, got %q", lines[len(lines)-1])
	}
}

func TestMutationValidationAndNotFoundErrors(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "claude-config")
	stageFixtureProject(t, "project-alpha", configDir, projectAlphaPath)
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	if err := RenameSession("not-a-uuid", "title", SessionMutationOptions{}); err == nil {
		t.Fatal("expected invalid UUID error from RenameSession")
	}
	if err := RenameSession(alphaMetadataSessionID, "   ", SessionMutationOptions{Directory: projectAlphaPath}); err == nil {
		t.Fatal("expected empty title error")
	}

	badTag := "\u200b\u200c\ufeff"
	if err := TagSession(alphaMetadataSessionID, &badTag, SessionMutationOptions{Directory: projectAlphaPath}); err == nil {
		t.Fatal("expected sanitized empty tag error")
	}

	missingID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	err := RenameSession(missingID, "missing", SessionMutationOptions{Directory: projectAlphaPath})
	if err == nil {
		t.Fatal("expected not found error")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got %v", err)
	}
}

func stageFixtureProject(t *testing.T, fixtureName string, configDir string, projectPath string) {
	t.Helper()
	sourceDir := filepath.Join(moduleRoot(t), "testdata", "sessions", "fixtures", fixtureName)
	targetDir := filepath.Join(configDir, "projects", sanitizePathForTests(canonicalizePathForTests(projectPath)))
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("failed to create target fixture dir: %v", err)
	}
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		t.Fatalf("failed to read fixture dir %s: %v", sourceDir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(sourceDir, entry.Name()))
		if err != nil {
			t.Fatalf("failed to read fixture file %s: %v", entry.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(targetDir, entry.Name()), data, 0o644); err != nil {
			t.Fatalf("failed to stage fixture file %s: %v", entry.Name(), err)
		}
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve caller path")
	}
	return filepath.Dir(file)
}

func sanitizePathForTests(name string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9]`)
	sanitized := re.ReplaceAllString(name, "-")
	if len(sanitized) <= 200 {
		return sanitized
	}
	return sanitized[:200] + "-" + simpleHashForTests(name)
}

func canonicalizePathForTests(path string) string {
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

func simpleHashForTests(value string) string {
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

func setSessionMtime(t *testing.T, configDir string, projectPath string, sessionID string, value time.Time) {
	t.Helper()
	path := filepath.Join(configDir, "projects", sanitizePathForTests(canonicalizePathForTests(projectPath)), sessionID+".jsonl")
	if err := os.Chtimes(path, value, value); err != nil {
		t.Fatalf("failed to set mtime for %s: %v", sessionID, err)
	}
}

func mustParseRFC3339Millis(t *testing.T, value string) int64 {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatalf("failed to parse timestamp %q: %v", value, err)
	}
	return parsed.UnixMilli()
}

func readNonEmptyLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

func createGitWorktree(t *testing.T, repoRoot string, worktreePath string) {
	t.Helper()
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("failed to create repo root: %v", err)
	}
	runGit(t, repoRoot, "init")
	runGit(t, repoRoot, "config", "user.email", "sdk@example.com")
	runGit(t, repoRoot, "config", "user.name", "SDK Tests")
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatalf("failed to seed git repo: %v", err)
	}
	runGit(t, repoRoot, "add", "README.md")
	runGit(t, repoRoot, "commit", "-m", "init")
	runGit(t, repoRoot, "branch", "feature/worktree")
	runGit(t, repoRoot, "worktree", "add", worktreePath, "feature/worktree")
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func TestSessionMessageJSONPayloadShape(t *testing.T) {
	message := SessionMessage{
		Type:      "user",
		UUID:      "abc",
		SessionID: "sess",
		Message: map[string]any{
			"role":    "user",
			"content": "hi",
		},
	}
	data, err := json.Marshal(message)
	if err != nil {
		t.Fatalf("failed to marshal SessionMessage: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected marshaled session message data")
	}
}
