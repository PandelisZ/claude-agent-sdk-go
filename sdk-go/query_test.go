package claudeagentsdk

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
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
