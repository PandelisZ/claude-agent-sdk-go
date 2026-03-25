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

func stringPtr(value string) *string {
	return &value
}
