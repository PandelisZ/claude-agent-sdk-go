package claudeagentsdk

import (
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestQueryCollectsAssistantAndResult(t *testing.T) {
	cliPath := buildFakeCLI(t)
	messages, err := Query(context.Background(), "say hello", ClaudeAgentOptions{
		CLIPath: &cliPath,
		Env:     map[string]string{"FAKE_CLAUDE_MODE": "success"},
	})
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	assistant, ok := messages[0].(*AssistantMessage)
	if !ok {
		t.Fatalf("expected AssistantMessage, got %T", messages[0])
	}
	if len(assistant.Content) != 1 {
		t.Fatalf("unexpected assistant content: %#v", assistant.Content)
	}
	textBlock, ok := assistant.Content[0].(TextBlock)
	if !ok {
		t.Fatalf("expected TextBlock, got %T", assistant.Content[0])
	}
	if textBlock.Text != "Echo: say hello" {
		t.Fatalf("unexpected assistant text: %q", textBlock.Text)
	}

	if _, ok := messages[1].(*ResultMessage); !ok {
		t.Fatalf("expected ResultMessage, got %T", messages[1])
	}
}

func TestQueryWithCallbackStreamsMessagesInOrder(t *testing.T) {
	cliPath := buildFakeCLI(t)
	gotTypes := make([]string, 0, 2)

	err := QueryWithCallback(context.Background(), "stream this", ClaudeAgentOptions{
		CLIPath: &cliPath,
		Env:     map[string]string{"FAKE_CLAUDE_MODE": "fragmented"},
	}, func(message Message) error {
		gotTypes = append(gotTypes, message.MessageType())
		return nil
	})
	if err != nil {
		t.Fatalf("QueryWithCallback returned error: %v", err)
	}

	if len(gotTypes) != 2 || gotTypes[0] != "assistant" || gotTypes[1] != "result" {
		t.Fatalf("unexpected message order: %#v", gotTypes)
	}
}

func TestQueryWithTransportUsesCustomTransport(t *testing.T) {
	transport := &scriptedQueryTransport{
		responses: [][]byte{
			mustJSONLine(t, assistantPayloadForTest("custom transport")),
			mustJSONLine(t, resultPayloadForTest()),
		},
	}

	messages, err := QueryWithTransport(context.Background(), "from custom", ClaudeAgentOptions{}, transport)
	if err != nil {
		t.Fatalf("QueryWithTransport returned error: %v", err)
	}
	if !transport.connected || !transport.inputClosed {
		t.Fatalf("custom transport was not driven through connect/write/close-input: %#v", transport)
	}
	if !strings.Contains(string(transport.writes[0]), "from custom") {
		t.Fatalf("custom transport did not receive prompt write: %s", string(transport.writes[0]))
	}
	if len(messages) != 2 || assistantText(t, messages[0].(*AssistantMessage)) != "custom transport" {
		t.Fatalf("unexpected custom transport messages: %#v", messages)
	}
}

func TestQueryAuthAssistantMessageRemainsTypedMessage(t *testing.T) {
	cliPath := buildFakeCLI(t)
	messages, err := Query(context.Background(), "auth please", ClaudeAgentOptions{
		CLIPath: &cliPath,
		Env:     map[string]string{"FAKE_CLAUDE_MODE": "auth"},
	})
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}

	assistant, ok := messages[0].(*AssistantMessage)
	if !ok {
		t.Fatalf("expected AssistantMessage, got %T", messages[0])
	}
	if assistant.Error == nil || *assistant.Error != AssistantMessageErrorAuthenticationFailed {
		t.Fatalf("unexpected assistant auth error: %#v", assistant.Error)
	}
}

func TestQueryStderrCallbackReceivesSubprocessStderr(t *testing.T) {
	cliPath := buildFakeCLI(t)
	var stderr string
	_, err := Query(context.Background(), "fail", ClaudeAgentOptions{
		CLIPath: &cliPath,
		Env:     map[string]string{"FAKE_CLAUDE_MODE": "stderr_exit"},
		Stderr: func(line string) {
			stderr += line
		},
	})
	if err == nil {
		t.Fatal("expected Query to return process error")
	}
	if !strings.Contains(stderr, "fixture stderr failure") {
		t.Fatalf("stderr callback did not receive fixture output: %q", stderr)
	}
}

func TestQueryCanUseToolStringPromptValidation(t *testing.T) {
	_, err := Query(context.Background(), "needs permission", ClaudeAgentOptions{
		CanUseTool: func(ctx context.Context, toolName string, input map[string]any, permissionCtx ToolPermissionContext) (PermissionResult, error) {
			return PermissionResultDeny{Message: "no"}, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "can_use_tool callback requires streaming input") {
		t.Fatalf("expected can_use_tool validation error, got %v", err)
	}
}

func TestQueryHooksUseControlRuntime(t *testing.T) {
	cliPath := buildFakeInteractiveCLI(t)
	matcher := "Write"
	messages, err := Query(context.Background(), "hook prompt", ClaudeAgentOptions{
		CLIPath: &cliPath,
		Env:     map[string]string{"FAKE_CLAUDE_MODE": "hook"},
		Hooks: map[HookEvent][]HookMatcher{
			HookEventPreToolUse: {
				{
					Matcher: &matcher,
					Hooks: []HookCallback{
						func(context.Context, map[string]any, *string, HookContext) (HookResult, error) {
							return HookResult{Continue: boolPtrForQueryTest(true)}, nil
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if len(messages) != 2 || !strings.Contains(assistantText(t, messages[0].(*AssistantMessage)), "hook:continue=true") {
		t.Fatalf("unexpected hook query messages: %#v", messages)
	}
}

func boolPtrForQueryTest(value bool) *bool {
	return &value
}

func TestParseQueryPayloadTaskNotificationAllowsMissingDescription(t *testing.T) {
	payload := map[string]any{
		"type":        "system",
		"subtype":     "task_notification",
		"task_id":     "task-1",
		"status":      "completed",
		"output_file": "/tmp/out.md",
		"summary":     "All done",
		"uuid":        "uuid-3",
		"session_id":  "session-1",
	}

	message, err := parseQueryPayload(payload)
	if err != nil {
		t.Fatalf("parseQueryPayload returned error: %v", err)
	}
	notification, ok := message.(*TaskNotificationMessage)
	if !ok {
		t.Fatalf("expected TaskNotificationMessage, got %T", message)
	}
	if notification.TaskID != "task-1" || notification.Summary != "All done" {
		t.Fatalf("unexpected task notification payload: %#v", notification)
	}
}

func buildFakeCLI(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve caller path")
	}

	moduleRoot := filepath.Dir(file)
	fixtureDir := filepath.Join(moduleRoot, "testdata", "query")
	binPath := filepath.Join(t.TempDir(), "fake-claude")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}

	cmd := exec.Command("go", "build", "-o", binPath, fixtureDir)
	cmd.Dir = moduleRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build fake CLI: %v\n%s", err, output)
	}

	return binPath
}

type scriptedQueryTransport struct {
	connected   bool
	inputClosed bool
	closed      bool
	writes      [][]byte
	responses   [][]byte
}

func (t *scriptedQueryTransport) Connect(context.Context) error {
	t.connected = true
	return nil
}

func (t *scriptedQueryTransport) Write(_ context.Context, data []byte) error {
	t.writes = append(t.writes, append([]byte(nil), data...))
	return nil
}

func (t *scriptedQueryTransport) CloseInput() error {
	t.inputClosed = true
	return nil
}

func (t *scriptedQueryTransport) Read(context.Context) ([]byte, error) {
	if len(t.responses) == 0 {
		return nil, io.EOF
	}
	next := t.responses[0]
	t.responses = t.responses[1:]
	return next, nil
}

func (t *scriptedQueryTransport) Close() error {
	t.closed = true
	return nil
}

func mustJSONLine(tb testing.TB, payload map[string]any) []byte {
	tb.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		tb.Fatalf("failed to marshal payload: %v", err)
	}
	return encoded
}

func assistantPayloadForTest(text string) map[string]any {
	return map[string]any{
		"type":       "assistant",
		"session_id": "session-1",
		"uuid":       "uuid-assistant-1",
		"message": map[string]any{
			"id":    "msg-1",
			"model": "fake-claude",
			"content": []map[string]any{
				{"type": "text", "text": text},
			},
			"stop_reason": "end_turn",
		},
	}
}

func resultPayloadForTest() map[string]any {
	return map[string]any{
		"type":            "result",
		"subtype":         "success",
		"duration_ms":     1,
		"duration_api_ms": 1,
		"is_error":        false,
		"num_turns":       1,
		"session_id":      "session-1",
	}
}
