//go:build e2e

package claudeagentsdk

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const claudeHaiku45Model = "claude-haiku-4-5-20251001"

func hasRealClaudeAuth() bool {
	return os.Getenv("ANTHROPIC_API_KEY") != "" || os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") != ""
}

func TestRealClaudeCLIQuerySmoke(t *testing.T) {
	if !hasRealClaudeAuth() {
		t.Skip("ANTHROPIC_API_KEY or CLAUDE_CODE_OAUTH_TOKEN is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	maxTurns := 1
	messages, err := Query(ctx, "Reply with exactly: pong", ClaudeAgentOptions{
		MaxTurns: &maxTurns,
	})
	if err != nil {
		t.Fatalf("real Claude CLI query failed: %v", err)
	}

	var content string
	for _, message := range messages {
		if result, ok := message.(*ResultMessage); ok && result.Result != nil {
			content = *result.Result
		}
	}
	if !strings.Contains(strings.ToLower(content), "pong") {
		t.Fatalf("expected response to contain pong, got %q", content)
	}
}

func TestRealClaudeCLIHaiku45QuerySmoke(t *testing.T) {
	if !hasRealClaudeAuth() {
		t.Skip("ANTHROPIC_API_KEY or CLAUDE_CODE_OAUTH_TOKEN is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	maxTurns := 1
	messages, err := Query(ctx, "Reply with exactly: haiku-pong", ClaudeAgentOptions{
		Model:    stringPtrForE2E(claudeHaiku45Model),
		MaxTurns: &maxTurns,
	})
	if err != nil {
		t.Fatalf("real Claude CLI Haiku 4.5 query failed: %v", err)
	}

	var content string
	for _, message := range messages {
		if result, ok := message.(*ResultMessage); ok && result.Result != nil {
			content = strings.TrimSpace(*result.Result)
		}
	}
	if content != "haiku-pong" {
		t.Fatalf("expected exact Haiku 4.5 response, got %q", content)
	}
}

func TestRealClaudeCLIHaiku45StructuredOutput(t *testing.T) {
	if !hasRealClaudeAuth() {
		t.Skip("ANTHROPIC_API_KEY or CLAUDE_CODE_OAUTH_TOKEN is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	maxTurns := 2
	messages, err := Query(ctx, "Classify this word: excellent. Return the requested structured output.", ClaudeAgentOptions{
		Model:    stringPtrForE2E(claudeHaiku45Model),
		MaxTurns: &maxTurns,
		OutputFormat: map[string]any{
			"type": "json_schema",
			"schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"label": map[string]any{
						"type": "string",
						"enum": []string{"positive", "negative", "neutral"},
					},
					"confidence": map[string]any{
						"type":    "number",
						"minimum": 0,
						"maximum": 1,
					},
				},
				"required": []string{"label", "confidence"},
			},
		},
	})
	if err != nil {
		t.Fatalf("real Claude CLI Haiku 4.5 structured-output query failed: %v", err)
	}

	result := lastResultMessage(messages)
	if result == nil {
		t.Fatalf("expected result message, got %#v", messages)
	}
	output, ok := result.StructuredOutput.(map[string]any)
	if !ok {
		t.Fatalf("expected structured_output object, got %T %#v", result.StructuredOutput, result.StructuredOutput)
	}
	if output["label"] != "positive" {
		t.Fatalf("expected positive label, got %#v", output)
	}
	if _, ok := output["confidence"].(float64); !ok {
		t.Fatalf("expected numeric confidence, got %#v", output)
	}
}

func TestRealClaudeCLIHaiku45ClientControls(t *testing.T) {
	if !hasRealClaudeAuth() {
		t.Skip("ANTHROPIC_API_KEY or CLAUDE_CODE_OAUTH_TOKEN is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	mode := PermissionModeDefault
	client := NewClient(ClientOptions{
		ClaudeAgentOptions: ClaudeAgentOptions{
			Model:          stringPtrForE2E(claudeHaiku45Model),
			PermissionMode: &mode,
		},
	})
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	}()

	if err := client.SetPermissionMode(ctx, PermissionModeAcceptEdits); err != nil {
		t.Fatalf("SetPermissionMode acceptEdits failed: %v", err)
	}
	if err := client.Query(ctx, "What is 2+2? Reply with exactly: 4"); err != nil {
		t.Fatalf("Query after SetPermissionMode failed: %v", err)
	}
	if result := receiveUntilResultForE2E(ctx, t, client); result == nil || result.IsError {
		t.Fatalf("expected successful result after permission mode change, got %#v", result)
	}

	if err := client.SetModel(ctx, stringPtrForE2E(claudeHaiku45Model)); err != nil {
		t.Fatalf("SetModel Haiku failed: %v", err)
	}
	if err := client.Query(ctx, "Reply with exactly: model-ok"); err != nil {
		t.Fatalf("Query after SetModel failed: %v", err)
	}
	result := receiveUntilResultForE2E(ctx, t, client)
	if result == nil || result.Result == nil || strings.TrimSpace(*result.Result) != "model-ok" {
		t.Fatalf("expected model-ok result, got %#v", result)
	}

	if err := client.SetModel(ctx, nil); err != nil {
		t.Fatalf("SetModel nil failed: %v", err)
	}
	if err := client.Interrupt(ctx); err != nil {
		t.Fatalf("Interrupt control failed: %v", err)
	}
}

func TestRealClaudeCLIHaiku45SDKMCPToolExecution(t *testing.T) {
	if !hasRealClaudeAuth() {
		t.Skip("ANTHROPIC_API_KEY or CLAUDE_CODE_OAUTH_TOKEN is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	executions := 0
	description := "Echo back the input text"
	server := NewSimpleMCPServer(MCPServerInfo{Name: "goe2e", Version: "1.0.0"}, []MCPTool{
		{
			Name:        "echo",
			Description: &description,
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"text": map[string]any{"type": "string"}},
				"required":   []string{"text"},
			},
			Handler: func(ctx context.Context, args map[string]any) (MCPToolResult, error) {
				executions++
				return MCPToolResult{
					Content: []MCPContent{MCPTextContent{Text: "Echo: " + args["text"].(string)}},
				}, nil
			},
		},
	})

	client := NewClient(ClientOptions{
		ClaudeAgentOptions: ClaudeAgentOptions{
			Model:        stringPtrForE2E(claudeHaiku45Model),
			AllowedTools: []string{"mcp__goe2e__echo"},
			MCPServers: map[string]MCPServerConfig{
				"goe2e": SDKMCPServerConfig{Type: "sdk", Name: "goe2e", Instance: server},
			},
		},
	})
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	}()

	if err := client.Query(ctx, "Call the mcp__goe2e__echo tool with text='haiku tool ok'."); err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if result := receiveUntilResultForE2E(ctx, t, client); result == nil || result.IsError {
		t.Fatalf("expected successful SDK MCP result, got %#v", result)
	}
	if executions == 0 {
		t.Fatal("SDK MCP echo tool was not executed")
	}
}

func TestRealClaudeCLIHaiku45PermissionCallback(t *testing.T) {
	if !hasRealClaudeAuth() {
		t.Skip("ANTHROPIC_API_KEY or CLAUDE_CODE_OAUTH_TOKEN is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "permission-callback.txt")
	if err := os.WriteFile(filePath, []byte("permission callback ok\n"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	calls := 0
	client := NewClient(ClientOptions{
		ClaudeAgentOptions: ClaudeAgentOptions{
			Model: stringPtrForE2E(claudeHaiku45Model),
			Cwd:   &dir,
		},
		CanUseTool: func(ctx context.Context, toolName string, input map[string]any, permissionCtx ToolPermissionContext) (PermissionResult, error) {
			if toolName == "Read" {
				calls++
				if got, _ := input["file_path"].(string); got == "" || !strings.HasSuffix(got, "permission-callback.txt") {
					return PermissionResultDeny{Message: fmt.Sprintf("unexpected Read input: %#v", input)}, nil
				}
				return PermissionResultAllow{}, nil
			}
			return PermissionResultDeny{Message: "only Read is allowed in this test"}, nil
		},
	})
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	}()

	if err := client.Query(ctx, "Use the Read tool to read permission-callback.txt, then reply exactly: read-ok"); err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	result := receiveUntilResultForE2E(ctx, t, client)
	if result == nil || result.IsError {
		t.Fatalf("expected successful permission callback result, got %#v", result)
	}
	if calls == 0 {
		t.Fatal("permission callback was not invoked for Read")
	}
}

func lastResultMessage(messages []Message) *ResultMessage {
	for i := len(messages) - 1; i >= 0; i-- {
		if result, ok := messages[i].(*ResultMessage); ok {
			return result
		}
	}
	return nil
}

func receiveUntilResultForE2E(ctx context.Context, t *testing.T, client *Client) *ResultMessage {
	t.Helper()

	for {
		message, err := client.Receive(ctx)
		if err != nil {
			t.Fatalf("Receive failed: %v", err)
		}
		if result, ok := message.(*ResultMessage); ok {
			return result
		}
		if message == nil || reflect.ValueOf(message).IsNil() {
			t.Fatal("received nil message before result")
		}
	}
}

func stringPtrForE2E(value string) *string {
	return &value
}
