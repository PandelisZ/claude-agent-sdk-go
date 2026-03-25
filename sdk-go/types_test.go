package claudeagentsdk

import "testing"

func TestClaudeAgentOptionsContractFields(t *testing.T) {
	systemPrompt := "Be concise."
	model := "claude-sonnet-4-5"
	fallbackModel := "claude-haiku-4-5"
	permissionMode := PermissionModeBypassPermissions
	maxTurns := 8
	maxBudget := 12.5
	cwd := "/workspace"
	cliPath := "/usr/local/bin/claude"
	resume := "session-123"
	extraFlagValue := "json"
	thinkingBudget := 4096
	effort := "high"

	options := ClaudeAgentOptions{
		Tools: []string{"Read", "Write"},
		ToolsPreset: &ToolsPreset{
			Type:   "preset",
			Preset: "claude_code",
		},
		AllowedTools: []string{"Read", "Write"},
		SystemPrompt: &systemPrompt,
		SystemPromptPreset: &SystemPromptPreset{
			Type:   "preset",
			Preset: "claude_code",
		},
		SystemPromptFile: &SystemPromptFile{
			Type: "file",
			Path: "/tmp/prompt.md",
		},
		MCPServers: map[string]MCPServerConfig{
			"stdio": MCPStdioServerConfig{
				Command: "server",
				Args:    []string{"--serve"},
			},
		},
		PermissionMode:         &permissionMode,
		ContinueConversation:   true,
		Resume:                 &resume,
		ForkSession:            true,
		MaxTurns:               &maxTurns,
		MaxBudgetUSD:           &maxBudget,
		Model:                  &model,
		FallbackModel:          &fallbackModel,
		Cwd:                    &cwd,
		CLIPath:                &cliPath,
		AddDirs:                []string{"/tmp/extra"},
		Env:                    map[string]string{"FOO": "bar"},
		ExtraArgs:              map[string]*string{"output-format": &extraFlagValue},
		IncludePartialMessages: true,
		Thinking: &ThinkingConfig{
			Type:         ThinkingConfigEnabled,
			BudgetTokens: &thinkingBudget,
		},
		Effort:       &effort,
		OutputFormat: map[string]any{"type": "json_schema"},
	}

	if options.PermissionMode == nil || *options.PermissionMode != PermissionModeBypassPermissions {
		t.Fatalf("unexpected permission mode: %#v", options.PermissionMode)
	}
	if options.Resume == nil || *options.Resume != "session-123" {
		t.Fatalf("unexpected resume option: %#v", options.Resume)
	}
	if options.Thinking == nil || options.Thinking.Type != ThinkingConfigEnabled {
		t.Fatalf("unexpected thinking config: %#v", options.Thinking)
	}
	if _, ok := options.MCPServers["stdio"].(MCPStdioServerConfig); !ok {
		t.Fatalf("expected stdio MCP server config")
	}
	if got := options.ExtraArgs["output-format"]; got == nil || *got != "json" {
		t.Fatalf("unexpected extra args: %#v", options.ExtraArgs)
	}
}

func TestContentBlockContracts(t *testing.T) {
	blocks := []ContentBlock{
		TextBlock{Text: "hello"},
		ThinkingBlock{Thinking: "hmm", Signature: "sig"},
		ToolUseBlock{ID: "tool-1", Name: "Read", Input: map[string]any{"path": "a.txt"}},
		ToolResultBlock{ToolUseID: "tool-1", Content: "done"},
		UnknownContentBlock{Type: "future_block", Raw: map[string]any{"type": "future_block"}},
	}

	wantTypes := []string{"text", "thinking", "tool_use", "tool_result", "future_block"}
	for i, block := range blocks {
		if got := block.ContentBlockType(); got != wantTypes[i] {
			t.Fatalf("block %d type = %q, want %q", i, got, wantTypes[i])
		}
	}
}

func TestAssistantAuthAndMCPStatusContracts(t *testing.T) {
	if AssistantMessageErrorAuthenticationFailed != "authentication_failed" {
		t.Fatalf("unexpected assistant auth error constant")
	}
	if MCPServerStatusNeedsAuth != "needs-auth" {
		t.Fatalf("unexpected MCP needs-auth constant")
	}

	description := "Tool"
	scope := "project"
	status := MCPServerStatus{
		Name:   "proxy-server",
		Status: MCPServerStatusNeedsAuth,
		Config: MCPClaudeAIProxyServerConfig{
			Type: "claudeai-proxy",
			URL:  "https://claude.ai/proxy",
			ID:   "proxy-1",
		},
		Scope: &scope,
		Tools: []MCPToolInfo{
			{
				Name:        "search",
				Description: &description,
				Annotations: &MCPToolAnnotations{ReadOnly: true},
			},
		},
	}

	if status.Status != MCPServerStatusNeedsAuth {
		t.Fatalf("unexpected MCP status: %q", status.Status)
	}
	if got := status.Config.mcpServerStatusConfigType(); got != "claudeai-proxy" {
		t.Fatalf("unexpected MCP config type: %q", got)
	}
	if len(status.Tools) != 1 || status.Tools[0].Annotations == nil || !status.Tools[0].Annotations.ReadOnly {
		t.Fatalf("unexpected tool annotations: %#v", status.Tools)
	}
}
