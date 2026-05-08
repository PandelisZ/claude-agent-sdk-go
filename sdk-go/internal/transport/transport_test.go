package transport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSubprocessCLITransportReadsFragmentedJSONAndSkipsNoise(t *testing.T) {
	cliPath := buildFakeCLI(t)
	transport := NewSubprocessCLITransport(Options{
		CLIPath: &cliPath,
		Env:     map[string]string{"FAKE_CLAUDE_MODE": "fragmented"},
	})

	ctx := context.Background()
	if err := transport.Connect(ctx); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	defer transport.Close()

	if err := transport.Write(ctx, []byte(`{"type":"user","message":{"role":"user","content":"buffer test"}}`+"\n")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if err := transport.CloseInput(); err != nil {
		t.Fatalf("CloseInput returned error: %v", err)
	}

	payload1, err := transport.Read(ctx)
	if err != nil {
		t.Fatalf("first Read returned error: %v", err)
	}
	payload2, err := transport.Read(ctx)
	if err != nil {
		t.Fatalf("second Read returned error: %v", err)
	}

	var message1 map[string]any
	if err := json.Unmarshal(payload1, &message1); err != nil {
		t.Fatalf("failed to decode first payload: %v", err)
	}
	var message2 map[string]any
	if err := json.Unmarshal(payload2, &message2); err != nil {
		t.Fatalf("failed to decode second payload: %v", err)
	}

	if message1["type"] != "assistant" || message2["type"] != "result" {
		t.Fatalf("unexpected payload sequence: %#v %#v", message1, message2)
	}

	_, err = transport.Read(ctx)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF after payloads, got %v", err)
	}
}

func TestSubprocessCLITransportReturnsProcessErrorWithStderr(t *testing.T) {
	cliPath := buildFakeCLI(t)
	transport := NewSubprocessCLITransport(Options{
		CLIPath: &cliPath,
		Env:     map[string]string{"FAKE_CLAUDE_MODE": "stderr_exit"},
	})

	ctx := context.Background()
	if err := transport.Connect(ctx); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	defer transport.Close()

	if err := transport.Write(ctx, []byte(`{"type":"user","message":{"role":"user","content":"fail"}}`+"\n")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if err := transport.CloseInput(); err != nil {
		t.Fatalf("CloseInput returned error: %v", err)
	}

	_, err := transport.Read(ctx)
	var processErr *ProcessError
	if !errors.As(err, &processErr) {
		t.Fatalf("expected ProcessError, got %T (%v)", err, err)
	}
	if processErr.ExitCode == nil || *processErr.ExitCode != 23 {
		t.Fatalf("unexpected exit code: %#v", processErr.ExitCode)
	}
	if processErr.Stderr != "fixture stderr failure" {
		t.Fatalf("unexpected stderr: %q", processErr.Stderr)
	}
}

func TestSubprocessCLITransportBuildEnvInheritsAndOverrides(t *testing.T) {
	t.Setenv("HOME", "/tmp/home-base")
	t.Setenv("CLAUDE_CONFIG_DIR", "/tmp/config-base")
	t.Setenv("ANTHROPIC_API_KEY", "base-key")

	cwd := "/workspace/project"
	transport := NewSubprocessCLITransport(Options{
		Cwd: &cwd,
		Env: map[string]string{
			"ANTHROPIC_API_KEY":      "override-key",
			"CLAUDE_CODE_ENTRYPOINT": "custom-entry",
			"XDG_CONFIG_HOME":        "/tmp/xdg",
		},
		EnableFileCheckpointing: true,
	})

	env := transport.buildEnvMap()
	if env["HOME"] != "/tmp/home-base" {
		t.Fatalf("expected HOME inheritance, got %q", env["HOME"])
	}
	if env["CLAUDE_CONFIG_DIR"] != "/tmp/config-base" {
		t.Fatalf("expected CLAUDE_CONFIG_DIR inheritance, got %q", env["CLAUDE_CONFIG_DIR"])
	}
	if env["ANTHROPIC_API_KEY"] != "override-key" {
		t.Fatalf("expected ANTHROPIC_API_KEY override, got %q", env["ANTHROPIC_API_KEY"])
	}
	if env["CLAUDE_CODE_ENTRYPOINT"] != "custom-entry" {
		t.Fatalf("expected caller entrypoint override, got %q", env["CLAUDE_CODE_ENTRYPOINT"])
	}
	if env["PWD"] != cwd {
		t.Fatalf("expected PWD override, got %q", env["PWD"])
	}
	if env["CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING"] != "true" {
		t.Fatalf("expected checkpointing env flag, got %q", env["CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING"])
	}
}

func TestSubprocessCLITransportBuildCommandIncludesRepresentativeOptions(t *testing.T) {
	systemPrompt := "Be concise"
	resume := "session-123"
	model := "claude-sonnet-4-5"
	fallbackModel := "claude-haiku-4-5"
	permissionMode := PermissionMode("bypassPermissions")
	permissionTool := "Bash"
	maxTurns := 7
	maxBudget := 3.5
	extraValue := "trace"
	appendPrompt := "Focus on tests"
	thinkingBudget := 2048
	effort := "high"

	transport := NewSubprocessCLITransport(Options{
		SystemPrompt: &systemPrompt,
		SystemPromptPreset: &SystemPromptPreset{
			Type:   "preset",
			Preset: "claude_code",
			Append: &appendPrompt,
		},
		ToolsPreset: &ToolsPreset{
			Type:   "preset",
			Preset: "claude_code",
		},
		AllowedTools:             []string{"Read", "Write"},
		DisallowedTools:          []string{"Bash"},
		MaxTurns:                 &maxTurns,
		MaxBudgetUSD:             &maxBudget,
		Model:                    &model,
		FallbackModel:            &fallbackModel,
		Betas:                    []SdkBeta{SdkBeta("context-1m-2025-08-07")},
		PermissionPromptToolName: &permissionTool,
		PermissionMode:           &permissionMode,
		ContinueConversation:     true,
		Resume:                   &resume,
		Settings:                 stringPtr(`{"permissions":{"allow":["Read"]}}`),
		AddDirs:                  []string{"/repo", "/repo/sub"},
		MCPServers: map[string]MCPServerConfig{
			"stdio": MCPStdioServerConfig{
				Command: "mcp-server",
				Args:    []string{"--serve"},
			},
		},
		IncludePartialMessages: true,
		ForkSession:            true,
		SettingSources: []SettingSource{
			SettingSource("user"),
			SettingSource("project"),
		},
		Plugins: []SDKPluginConfig{
			{Type: "local", Path: "/plugins/one"},
		},
		ExtraArgs: map[string]*string{
			"custom-flag":  &extraValue,
			"verbose-json": nil,
		},
		Thinking: &ThinkingConfig{
			Type:         ThinkingConfigEnabled,
			BudgetTokens: &thinkingBudget,
		},
		Effort: &effort,
		OutputFormat: map[string]any{
			"type": "json_schema",
			"schema": map[string]any{
				"type": "object",
			},
		},
	})

	args, err := transport.buildCommandArgs()
	if err != nil {
		t.Fatalf("buildCommandArgs returned error: %v", err)
	}

	assertContainsPair(t, args, "--output-format", "stream-json")
	assertContainsPair(t, args, "--verbose", "")
	assertContainsPair(t, args, "--input-format", "stream-json")
	assertContainsPair(t, args, "--append-system-prompt", appendPrompt)
	assertContainsPair(t, args, "--tools", "default")
	assertContainsPair(t, args, "--allowedTools", "Read,Write")
	assertContainsPair(t, args, "--disallowedTools", "Bash")
	assertContainsPair(t, args, "--max-turns", "7")
	assertContainsPair(t, args, "--max-budget-usd", "3.5")
	assertContainsPair(t, args, "--model", model)
	assertContainsPair(t, args, "--fallback-model", fallbackModel)
	assertContainsPair(t, args, "--betas", "context-1m-2025-08-07")
	assertContainsPair(t, args, "--permission-prompt-tool", permissionTool)
	assertContainsPair(t, args, "--permission-mode", string(permissionMode))
	assertContainsPair(t, args, "--continue", "")
	assertContainsPair(t, args, "--resume", resume)
	assertContainsPair(t, args, "--settings", `{"permissions":{"allow":["Read"]}}`)
	assertContainsPair(t, args, "--add-dir", "/repo")
	assertContainsPair(t, args, "--add-dir", "/repo/sub")
	assertContainsPair(t, args, "--include-partial-messages", "")
	assertContainsPair(t, args, "--fork-session", "")
	assertContainsPair(t, args, "--setting-sources", "user,project")
	assertContainsPair(t, args, "--plugin-dir", "/plugins/one")
	assertContainsPair(t, args, "--custom-flag", extraValue)
	assertContainsPair(t, args, "--verbose-json", "")
	assertContainsPair(t, args, "--max-thinking-tokens", "2048")
	assertContainsPair(t, args, "--effort", effort)
	assertContainsPair(t, args, "--json-schema", `{"type":"object"}`)
	assertContainsPair(t, args, "--mcp-config", `{"mcpServers":{"stdio":{"args":["--serve"],"command":"mcp-server","type":"stdio"}}}`)
}

func TestSubprocessCLITransportBuildCommandIncludesUpstreamV078Options(t *testing.T) {
	sessionID := "11111111-1111-4111-8111-111111111111"
	taskBudgetTotal := 12000
	thinkingDisplay := ThinkingDisplaySummarized
	transport := NewSubprocessCLITransport(Options{
		AllowedTools: []string{"Read"},
		SessionID:    &sessionID,
		Settings:     stringPtr(`{"permissions":{"allow":["Read"]}}`),
		Sandbox: &SandboxSettings{
			Enabled: boolPtr(true),
			Network: &SandboxNetworkConfig{
				AllowedDomains: []string{"api.example.test"},
			},
			IgnoreViolations: &SandboxIgnoreViolations{
				File: []string{"/tmp/cache"},
			},
		},
		StrictMCPConfig:   true,
		IncludeHookEvents: true,
		SessionMirror:     true,
		TaskBudget:        &TaskBudget{Total: taskBudgetTotal},
		Skills:            []string{"reviewer"},
		Thinking:          &ThinkingConfig{Type: ThinkingConfigAdaptive, Display: &thinkingDisplay},
	})

	args, err := transport.buildCommandArgs()
	if err != nil {
		t.Fatalf("buildCommandArgs returned error: %v", err)
	}

	assertContainsPair(t, args, "--session-id", sessionID)
	assertContainsPair(t, args, "--include-hook-events", "")
	assertContainsPair(t, args, "--strict-mcp-config", "")
	assertContainsPair(t, args, "--session-mirror", "")
	assertContainsPair(t, args, "--task-budget", "12000")
	assertContainsPair(t, args, "--allowedTools", "Read,Skill(reviewer)")
	assertContainsPair(t, args, "--setting-sources", "user,project")
	assertContainsPair(t, args, "--thinking", "adaptive")
	assertContainsPair(t, args, "--thinking-display", "summarized")

	settingsValue := valueAfterFlag(t, args, "--settings")
	var settings map[string]any
	if err := json.Unmarshal([]byte(settingsValue), &settings); err != nil {
		t.Fatalf("settings value was not JSON: %v\n%s", err, settingsValue)
	}
	sandbox, ok := settings["sandbox"].(map[string]any)
	if !ok || sandbox["enabled"] != true {
		t.Fatalf("settings did not include merged sandbox: %#v", settings)
	}
	network, ok := sandbox["network"].(map[string]any)
	if !ok || network["allowedDomains"] == nil {
		t.Fatalf("settings did not include sandbox network: %#v", settings)
	}
	ignoreViolations, ok := sandbox["ignoreViolations"].(map[string]any)
	if !ok || ignoreViolations["file"] == nil {
		t.Fatalf("settings did not include sandbox ignore violations: %#v", settings)
	}
}

func TestSubprocessCLITransportBuildCommandSupportsAllSkillsDefault(t *testing.T) {
	transport := NewSubprocessCLITransport(Options{
		Skills: "all",
	})

	args, err := transport.buildCommandArgs()
	if err != nil {
		t.Fatalf("buildCommandArgs returned error: %v", err)
	}

	assertContainsPair(t, args, "--allowedTools", "Skill")
	assertContainsPair(t, args, "--setting-sources", "user,project")
}

func TestSubprocessCLITransportBuildCommandPreservesExplicitEmptySettingSourcesForSkills(t *testing.T) {
	transport := NewSubprocessCLITransport(Options{
		Skills:         []string{"reviewer"},
		SettingSources: []SettingSource{},
	})

	args, err := transport.buildCommandArgs()
	if err != nil {
		t.Fatalf("buildCommandArgs returned error: %v", err)
	}

	assertContainsPair(t, args, "--allowedTools", "Skill(reviewer)")
	assertContainsPair(t, args, "--setting-sources", "")
}

func TestSubprocessCLITransportBuildCommandSupportsDisabledThinking(t *testing.T) {
	display := ThinkingDisplaySummarized
	transport := NewSubprocessCLITransport(Options{
		Thinking: &ThinkingConfig{Type: ThinkingConfigDisabled, Display: &display},
	})

	args, err := transport.buildCommandArgs()
	if err != nil {
		t.Fatalf("buildCommandArgs returned error: %v", err)
	}

	assertContainsPair(t, args, "--thinking", "disabled")
	assertNotContains(t, args, "--thinking-display")
	assertNotContains(t, args, "--max-thinking-tokens")
}

func buildFakeCLI(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve caller path")
	}

	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
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

func assertContainsPair(t *testing.T, args []string, flag string, value string) {
	t.Helper()

	for idx, arg := range args {
		if arg != flag {
			continue
		}
		if value == "" {
			return
		}
		if idx+1 < len(args) && args[idx+1] == value {
			return
		}
	}

	t.Fatalf("expected %q %q in args: %#v", flag, value, args)
}

func valueAfterFlag(t *testing.T, args []string, flag string) string {
	t.Helper()
	for idx, arg := range args {
		if arg == flag && idx+1 < len(args) {
			return args[idx+1]
		}
	}
	t.Fatalf("expected %q in args: %#v", flag, args)
	return ""
}

func assertNotContains(t *testing.T, args []string, flag string) {
	t.Helper()
	for _, arg := range args {
		if arg == flag {
			t.Fatalf("did not expect %q in args: %#v", flag, args)
		}
	}
}

func stringPtr(value string) *string {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}
