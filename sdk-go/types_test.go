package claudeagentsdk

import (
	"context"
	"encoding/json"
	"testing"

	internalhooks "github.com/PandelisZ/claude-agent-sdk-go/sdk-go/internal/hooks"
)

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

func TestUpstreamV078OptionAndTypeContracts(t *testing.T) {
	modeDontAsk := PermissionModeDontAsk
	modeAuto := PermissionModeAuto
	sessionID := "11111111-1111-4111-8111-111111111111"
	taskBudgetTotal := 12000
	thinkingDisplay := ThinkingDisplaySummarized
	loadTimeout := 250
	stderrSeen := false
	matcher := "Write"

	options := ClaudeAgentOptions{
		SystemPromptPreset: &SystemPromptPreset{
			Type:                   "preset",
			Preset:                 "claude_code",
			ExcludeDynamicSections: testBoolPtr(true),
		},
		PermissionMode:    &modeDontAsk,
		StrictMCPConfig:   true,
		SessionID:         &sessionID,
		IncludeHookEvents: true,
		Stderr: func(line string) {
			stderrSeen = line != ""
		},
		CanUseTool: func(ctx context.Context, toolName string, input map[string]any, permissionCtx ToolPermissionContext) (PermissionResult, error) {
			return PermissionResultDeny{Message: "no"}, nil
		},
		Hooks: map[HookEvent][]HookMatcher{
			HookEventPreToolUse: {
				{Matcher: &matcher},
			},
		},
		Skills:            []string{"reviewer", "planner"},
		LoadTimeoutMS:     loadTimeout,
		SessionStoreFlush: SessionStoreFlushModeEager,
		TaskBudget:        &TaskBudget{Total: taskBudgetTotal},
		Thinking: &ThinkingConfig{
			Type:    ThinkingConfigAdaptive,
			Display: &thinkingDisplay,
		},
		Sandbox: &SandboxSettings{
			Enabled: testBoolPtr(true),
			Network: &SandboxNetworkConfig{
				Allow: []string{"api.example.test"},
			},
			IgnoreViolations: &SandboxIgnoreViolations{
				Read: []string{"/tmp/cache"},
			},
		},
		Agents: map[string]AgentDefinition{
			"reviewer": {
				Description:    "Review code",
				Prompt:         "Be precise",
				Tools:          []string{"Read"},
				Skills:         []string{"code-review"},
				Memory:         testStringPtr("project"),
				PermissionMode: &modeAuto,
			},
		},
	}

	if options.PermissionMode == nil || *options.PermissionMode != PermissionModeDontAsk {
		t.Fatalf("unexpected permission mode: %#v", options.PermissionMode)
	}
	if modeAuto != PermissionModeAuto {
		t.Fatalf("unexpected auto permission mode constant: %q", modeAuto)
	}
	if options.SystemPromptPreset == nil || options.SystemPromptPreset.ExcludeDynamicSections == nil || !*options.SystemPromptPreset.ExcludeDynamicSections {
		t.Fatalf("expected exclude dynamic sections on system prompt preset: %#v", options.SystemPromptPreset)
	}
	if options.TaskBudget == nil || options.TaskBudget.Total != taskBudgetTotal {
		t.Fatalf("unexpected task budget: %#v", options.TaskBudget)
	}
	if options.Thinking == nil || options.Thinking.Display == nil || *options.Thinking.Display != ThinkingDisplaySummarized {
		t.Fatalf("unexpected thinking display: %#v", options.Thinking)
	}
	if options.Sandbox == nil || options.Sandbox.Network == nil || len(options.Sandbox.Network.Allow) != 1 {
		t.Fatalf("unexpected sandbox config: %#v", options.Sandbox)
	}
	if options.SessionStoreFlush != SessionStoreFlushModeEager {
		t.Fatalf("unexpected session store flush mode: %q", options.SessionStoreFlush)
	}
	options.Stderr("stderr")
	if !stderrSeen || options.CanUseTool == nil || len(options.Hooks[HookEventPreToolUse]) != 1 {
		t.Fatalf("expected stderr, can_use_tool, and hooks options to be available")
	}
	if len(options.Agents) != 1 || options.Agents["reviewer"].Description == "" {
		t.Fatalf("unexpected agents: %#v", options.Agents)
	}

	_ = modeAuto
}

func TestPythonPermissionUpdateWireRoundTrip(t *testing.T) {
	behavior := PermissionBehaviorAllow
	destination := PermissionUpdateDestinationLocalSettings
	ruleContent := "npm *"
	update := PermissionUpdate{
		Type:        "addRules",
		Destination: &destination,
		Behavior:    &behavior,
		Rules: []PermissionRuleValue{
			{ToolName: "Bash", RuleContent: &ruleContent},
			{ToolName: "Read"},
		},
	}

	encoded := update.ToMap()
	if encoded["type"] != "addRules" || encoded["destination"] != "localSettings" || encoded["behavior"] != "allow" {
		t.Fatalf("unexpected encoded permission update: %#v", encoded)
	}
	rules, ok := encoded["rules"].([]map[string]any)
	if !ok || len(rules) != 2 {
		t.Fatalf("unexpected encoded rules: %#v", encoded["rules"])
	}
	if rules[0]["toolName"] != "Bash" || rules[0]["ruleContent"] != "npm *" {
		t.Fatalf("unexpected first rule: %#v", rules[0])
	}
	if _, ok := rules[1]["ruleContent"]; ok {
		t.Fatalf("nil ruleContent should be omitted: %#v", rules[1])
	}

	parsed, err := internalhooks.ParsePermissionUpdates([]any{
		map[string]any{
			"type":        "setMode",
			"mode":        "acceptEdits",
			"destination": "session",
		},
		map[string]any{
			"type":        "addDirectories",
			"directories": []any{"/tmp/a", "/tmp/b"},
			"destination": "userSettings",
		},
	})
	if err != nil {
		t.Fatalf("ParsePermissionUpdates returned error: %v", err)
	}
	if parsed[0].Mode == nil || *parsed[0].Mode != "acceptEdits" {
		t.Fatalf("unexpected setMode update: %#v", parsed[0])
	}
	if len(parsed[1].Directories) != 2 || parsed[1].Directories[1] != "/tmp/b" {
		t.Fatalf("unexpected directories update: %#v", parsed[1])
	}
}

func TestAgentDefinitionSerializesWithPythonCLIKeys(t *testing.T) {
	model := "claude-opus-4-5"
	memory := "project"
	initialPrompt := "/review-pr 123"
	maxTurns := 10
	agent := AgentDefinition{
		Description:     "test",
		Prompt:          "p",
		DisallowedTools: []string{"Bash", "Write"},
		Model:           &model,
		Skills:          []string{"skill-a", "skill-b"},
		Memory:          &memory,
		MCPServers: []any{
			"slack",
			map[string]any{"local": map[string]any{"command": "python", "args": []string{"server.py"}}},
		},
		InitialPrompt: &initialPrompt,
		MaxTurns:      &maxTurns,
	}

	raw, err := json.Marshal(agent)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	if _, ok := payload["disallowed_tools"]; ok {
		t.Fatalf("unexpected snake_case disallowed_tools key: %#v", payload)
	}
	if _, ok := payload["max_turns"]; ok {
		t.Fatalf("unexpected snake_case max_turns key: %#v", payload)
	}
	if _, ok := payload["initial_prompt"]; ok {
		t.Fatalf("unexpected snake_case initial_prompt key: %#v", payload)
	}
	if _, ok := payload["mcp_servers"]; ok {
		t.Fatalf("unexpected snake_case mcp_servers key: %#v", payload)
	}
	if payload["initialPrompt"] != "/review-pr 123" || payload["model"] != "claude-opus-4-5" {
		t.Fatalf("unexpected agent payload: %#v", payload)
	}
	if servers, ok := payload["mcpServers"].([]any); !ok || len(servers) != 2 {
		t.Fatalf("unexpected mcpServers payload: %#v", payload["mcpServers"])
	}
}

func TestContentBlockContracts(t *testing.T) {
	blocks := []ContentBlock{
		TextBlock{Text: "hello"},
		ThinkingBlock{Thinking: "hmm", Signature: "sig"},
		ToolUseBlock{ID: "tool-1", Name: "Read", Input: map[string]any{"path": "a.txt"}},
		ToolResultBlock{ToolUseID: "tool-1", Content: "done"},
		ServerToolUseBlock{ID: "server-tool-1", Name: ServerToolNameWebSearch, Input: map[string]any{"query": "sdk"}},
		ServerToolResultBlock{ToolUseID: "server-tool-1", Content: map[string]any{"type": "web_search_result"}},
		UnknownContentBlock{Type: "future_block", Raw: map[string]any{"type": "future_block"}},
	}

	wantTypes := []string{"text", "thinking", "tool_use", "tool_result", "server_tool_use", "server_tool_result", "future_block"}
	for i, block := range blocks {
		if got := block.ContentBlockType(); got != wantTypes[i] {
			t.Fatalf("block %d type = %q, want %q", i, got, wantTypes[i])
		}
	}
}

func TestUpstreamV078MessageAndContextContracts(t *testing.T) {
	sessionKey := &SessionKey{
		ProjectKey: "project",
		SessionID:  "session-1",
		Subpath:    testStringPtr("subagents/agent-reviewer"),
	}
	mirrorError := &MirrorErrorMessage{
		SystemMessage: SystemMessage{Subtype: "mirror_error", Data: map[string]any{"error": "boom"}},
		Key:           sessionKey,
		Error:         "boom",
	}
	hookEvent := &HookEventMessage{
		SystemMessage: SystemMessage{Subtype: "hook_response", Data: map[string]any{"hook_event": "PreToolUse"}},
		HookEventName: "PreToolUse",
		SessionID:     testStringPtr("session-1"),
		UUID:          testStringPtr("uuid-hook-1"),
	}
	deferred := &DeferredToolUse{
		ID:    "toolu_1",
		Name:  "Bash",
		Input: map[string]any{"command": "make test"},
	}
	result := &ResultMessage{
		Subtype:         "success",
		DurationMS:      1000,
		DurationAPIMS:   900,
		IsError:         true,
		NumTurns:        1,
		SessionID:       "session-1",
		DeferredToolUse: deferred,
		Errors:          []string{"rate limited"},
		APIErrorStatus:  testIntPtr(429),
	}
	contextUsage := ContextUsageResponse{
		Categories: []ContextUsageCategory{
			{Name: "messages", Tokens: 42, Color: "blue", IsDeferred: testBoolPtr(true)},
		},
		TotalTokens:          42,
		MaxTokens:            200000,
		RawMaxTokens:         200000,
		Percentage:           0.021,
		Model:                "claude-sonnet-4-5",
		IsAutoCompactEnabled: true,
		MemoryFiles:          []map[string]any{{"path": "CLAUDE.md"}},
		MCPTools:             []map[string]any{{"name": "search"}},
		Agents:               []map[string]any{{"agentType": "reviewer"}},
		GridRows:             [][]map[string]any{{{"name": "messages"}}},
		AutoCompactThreshold: testIntPtr(180000),
		DeferredBuiltinTools: []map[string]any{{"name": "Bash"}},
		SystemTools:          []map[string]any{{"name": "TodoWrite"}},
		SystemPromptSections: []map[string]any{{"name": "main"}},
		SlashCommands:        map[string]any{"tokens": 1},
		Skills:               map[string]any{"tokens": 2},
		MessageBreakdown:     map[string]any{"messages": 42},
		APIUsage:             map[string]any{"input_tokens": 40},
	}
	permissionContext := ToolPermissionContext{
		ToolUseID:      testStringPtr("toolu_1"),
		AgentID:        testStringPtr("agent-reviewer"),
		BlockedPath:    testStringPtr("/private/file"),
		DecisionReason: testStringPtr("hook requested review"),
		Title:          testStringPtr("Claude wants to read /private/file"),
		DisplayName:    testStringPtr("Read file"),
		Description:    testStringPtr("Outside the allowed workspace"),
	}

	if mirrorError.MessageType() != "system" || mirrorError.Key == nil || mirrorError.Key.Subpath == nil {
		t.Fatalf("unexpected mirror error message: %#v", mirrorError)
	}
	if hookEvent.MessageType() != "system" || hookEvent.SessionID == nil || *hookEvent.SessionID != "session-1" {
		t.Fatalf("unexpected hook event message: %#v", hookEvent)
	}
	if result.DeferredToolUse == nil || result.DeferredToolUse.Input["command"] != "make test" {
		t.Fatalf("unexpected deferred tool use: %#v", result.DeferredToolUse)
	}
	if result.APIErrorStatus == nil || *result.APIErrorStatus != 429 {
		t.Fatalf("unexpected api error status: %#v", result.APIErrorStatus)
	}
	if contextUsage.Categories[0].IsDeferred == nil || !*contextUsage.Categories[0].IsDeferred {
		t.Fatalf("unexpected context usage: %#v", contextUsage)
	}
	if permissionContext.ToolUseID == nil || permissionContext.DisplayName == nil {
		t.Fatalf("unexpected permission context: %#v", permissionContext)
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

func TestPythonV078ExportCompatibilityAliases(t *testing.T) {
	if Version != "0.1.78" {
		t.Fatalf("Version = %q, want upstream v0.1.78", Version)
	}

	client := ClaudeSDKClient{}
	if client.ServerInfo() != nil {
		t.Fatalf("zero-value client should not have server info")
	}

	var _ McpServerConfig = MCPStdioServerConfig{Command: "server"}
	var _ McpServerStatusConfig = MCPClaudeAIProxyServerConfig{Type: "claudeai-proxy"}
	var _ McpServerConnectionStatus = McpServerStatusConnected
	var _ McpServerInfo = MCPServerInfo{Name: "server", Version: "1.0.0"}
	var _ McpStatusResponse = MCPStatusResponse{}
	var _ McpToolAnnotations = MCPToolAnnotations{ReadOnly: true}
	var _ McpToolInfo = MCPToolInfo{Name: "tool"}
	var _ = ThinkingConfig{Type: ThinkingConfigAdaptive}
	var _ = ThinkingConfig{Type: ThinkingConfigEnabled, BudgetTokens: testIntPtr(1000)}
	var _ = ThinkingConfig{Type: ThinkingConfigDisabled}
	var _ CanUseTool
	var _ HookInput = HookInput{"hook_event_name": "PreToolUse"}
	var _ BaseHookInput = BaseHookInput{"session_id": "session-1"}
	var _ HookJSONOutput = HookJSONOutput{"decision": "approve"}
	var _ SdkPluginConfig = SDKPluginConfig{Type: "local", Path: "/tmp/plugin"}
	var _ SdkMcpTool = Tool("echo", "Echo input", map[string]any{"type": "object"}, nil)
	var _ ToolAnnotations = ToolAnnotations{ReadOnly: true}

	server := CreateSDKMCPServer("compat", "1.0.0", nil)
	if server.Type != "sdk" || server.Name != "compat" || server.Instance == nil {
		t.Fatalf("unexpected SDK MCP server config: %#v", server)
	}
}

func testBoolPtr(value bool) *bool       { return &value }
func testStringPtr(value string) *string { return &value }
func testIntPtr(value int) *int          { return &value }
