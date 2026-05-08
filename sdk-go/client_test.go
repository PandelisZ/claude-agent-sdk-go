package claudeagentsdk

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	internalcontrol "github.com/PandelisZ/claude-agent-sdk-go/sdk-go/internal/control"
	internalhooks "github.com/PandelisZ/claude-agent-sdk-go/sdk-go/internal/hooks"
)

func TestClientConnectQueryReceiveAndServerInfo(t *testing.T) {
	client := newFakeClient(t, "happy", ClientOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	defer client.Close()

	info := client.ServerInfo()
	if info["output_style"] != "default" {
		t.Fatalf("unexpected server info: %#v", info)
	}

	if err := client.Query(ctx, "say hello"); err != nil {
		t.Fatalf("Query returned error: %v", err)
	}

	messages := receiveResponse(t, client)
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	assistant, ok := messages[0].(*AssistantMessage)
	if !ok {
		t.Fatalf("expected AssistantMessage, got %T", messages[0])
	}
	if got := assistantText(t, assistant); got != "Echo: say hello" {
		t.Fatalf("unexpected assistant text: %q", got)
	}
	if _, ok := messages[1].(*ResultMessage); !ok {
		t.Fatalf("expected ResultMessage, got %T", messages[1])
	}
}

func TestClientControlMethodsAndMCPStatus(t *testing.T) {
	client := newFakeClient(t, "happy", ClientOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	defer client.Close()

	model := "claude-sonnet-4-5"
	operations := []func() error{
		func() error { return client.Interrupt(ctx) },
		func() error { return client.SetPermissionMode(ctx, PermissionModeAcceptEdits) },
		func() error { return client.SetModel(ctx, &model) },
		func() error { return client.RewindFiles(ctx, "user-message-1") },
		func() error { return client.ReconnectMCPServer(ctx, "sdk-test") },
		func() error { return client.ToggleMCPServer(ctx, "sdk-test", false) },
		func() error { return client.StopTask(ctx, "task-1") },
	}
	for _, operation := range operations {
		if err := operation(); err != nil {
			t.Fatalf("control method returned error: %v", err)
		}
	}

	status, err := client.MCPStatus(ctx)
	if err != nil {
		t.Fatalf("MCPStatus returned error: %v", err)
	}
	if len(status.MCPServers) != 2 {
		t.Fatalf("expected 2 MCP servers, got %d", len(status.MCPServers))
	}
	if status.MCPServers[0].Status != MCPServerStatusConnected {
		t.Fatalf("unexpected first MCP server status: %#v", status.MCPServers[0])
	}
	if status.MCPServers[1].Status != MCPServerStatusNeedsAuth {
		t.Fatalf("expected needs-auth status, got %#v", status.MCPServers[1])
	}
	httpConfig, ok := status.MCPServers[1].Config.(MCPHTTPServerConfig)
	if !ok || httpConfig.URL != "https://example.test/mcp" {
		t.Fatalf("unexpected needs-auth config: %#v", status.MCPServers[1].Config)
	}
}

func TestClientContextUsage(t *testing.T) {
	client := newFakeClient(t, "happy", ClientOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	defer client.Close()

	usage, err := client.ContextUsage(ctx)
	if err != nil {
		t.Fatalf("ContextUsage returned error: %v", err)
	}

	if usage.TotalTokens != 42 || usage.MaxTokens != 200000 || usage.Model != "claude-sonnet-4-5" {
		t.Fatalf("unexpected context usage summary: %#v", usage)
	}
	if len(usage.Categories) != 1 || usage.Categories[0].Name != "messages" || usage.Categories[0].Tokens != 42 {
		t.Fatalf("unexpected context usage categories: %#v", usage.Categories)
	}
	if len(usage.MCPTools) != 1 || usage.MCPTools[0]["name"] != "search" {
		t.Fatalf("unexpected context usage MCP tools: %#v", usage.MCPTools)
	}
}

func TestClientSendUsesExplicitSessionID(t *testing.T) {
	client := NewClient(ClientOptions{})

	payload, err := client.clientMessageEnvelope(ClientMessage{
		SessionID: "explicit-session",
		Content: UserContent{
			Kind: UserContentKindText,
			Text: "hello",
		},
	})
	if err != nil {
		t.Fatalf("clientMessageEnvelope returned error: %v", err)
	}
	if payload["session_id"] != "explicit-session" {
		t.Fatalf("Send payload did not preserve explicit session ID: %#v", payload)
	}
}

func TestRuntimeInitializeRequestIncludesHooksAgentsDynamicSectionsAndSkills(t *testing.T) {
	matcher := "Write"
	excludeDynamicSections := true
	transport := newControlCaptureTransport()
	runtime := internalcontrol.NewRuntime(internalcontrol.Options{
		Transport: transport,
		HookRegistry: internalhooks.NewRegistry(map[internalhooks.Event][]internalhooks.Matcher{
			internalhooks.EventPreToolUse: {
				{
					Matcher: &matcher,
					Hooks: []internalhooks.Callback{
						func(context.Context, map[string]any, *string, internalhooks.Context) (internalhooks.Result, error) {
							return internalhooks.Result{}, nil
						},
					},
				},
			},
		}),
		InitializeAgents: map[string]any{
			"reviewer": map[string]any{
				"description": "Review code",
				"prompt":      "Be precise",
				"skills":      []string{"code-review"},
			},
		},
		ExcludeDynamicSections: &excludeDynamicSections,
		InitializeSkills:       []string{"reviewer", "planner"},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := runtime.Connect(ctx); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	defer runtime.Close()

	request := transport.firstRequest(t)
	if request["subtype"] != "initialize" {
		t.Fatalf("unexpected initialize subtype: %#v", request)
	}
	if hooks, ok := request["hooks"].(map[string]any); !ok || len(hooks) != 1 {
		t.Fatalf("initialize request did not include hooks: %#v", request)
	}
	if !reflect.DeepEqual(request["agents"], map[string]any{
		"reviewer": map[string]any{
			"description": "Review code",
			"prompt":      "Be precise",
			"skills":      []any{"code-review"},
		},
	}) {
		t.Fatalf("initialize request did not include agents: %#v", request["agents"])
	}
	if request["excludeDynamicSections"] != true {
		t.Fatalf("initialize request did not include excludeDynamicSections: %#v", request)
	}
	if !reflect.DeepEqual(request["skills"], []any{"reviewer", "planner"}) {
		t.Fatalf("initialize request did not include skills list: %#v", request["skills"])
	}
}

func TestClientUsesCustomTransport(t *testing.T) {
	transport := newControlCaptureTransport()
	client := NewClient(ClientOptions{Transport: transport})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	defer client.Close()

	request := transport.firstRequest(t)
	if request["subtype"] != "initialize" {
		t.Fatalf("unexpected initialize request: %#v", request)
	}
}

func TestClientPermissionCallbackAllowAndDeny(t *testing.T) {
	t.Run("allow", func(t *testing.T) {
		var suggestionsSeen int
		client := newFakeClient(t, "permission_allow", ClientOptions{
			CanUseTool: func(ctx context.Context, toolName string, input map[string]any, permissionCtx ToolPermissionContext) (PermissionResult, error) {
				if toolName != "Write" {
					t.Fatalf("unexpected tool name: %q", toolName)
				}
				suggestionsSeen = len(permissionCtx.Suggestions)
				return PermissionResultAllow{
					UpdatedInput: map[string]any{
						"approved_path": "/tmp/approved.txt",
					},
					UpdatedPermissions: []PermissionUpdate{
						{
							Type: "setMode",
							Mode: stringPtr(string(PermissionModeAcceptEdits)),
						},
					},
				}, nil
			},
		})
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := client.Connect(ctx); err != nil {
			t.Fatalf("Connect returned error: %v", err)
		}
		defer client.Close()

		if err := client.Query(ctx, "allow it"); err != nil {
			t.Fatalf("Query returned error: %v", err)
		}

		messages := receiveResponse(t, client)
		if got := assistantText(t, messages[0].(*AssistantMessage)); got != "permission:allow:/tmp/approved.txt:1" {
			t.Fatalf("unexpected permission response text: %q", got)
		}
		if suggestionsSeen != 1 {
			t.Fatalf("expected 1 permission suggestion, got %d", suggestionsSeen)
		}
	})

	t.Run("deny", func(t *testing.T) {
		client := newFakeClient(t, "permission_deny", ClientOptions{
			CanUseTool: func(ctx context.Context, toolName string, input map[string]any, permissionCtx ToolPermissionContext) (PermissionResult, error) {
				return PermissionResultDeny{
					Message:   "denied",
					Interrupt: true,
				}, nil
			},
		})
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := client.Connect(ctx); err != nil {
			t.Fatalf("Connect returned error: %v", err)
		}
		defer client.Close()

		if err := client.Query(ctx, "deny it"); err != nil {
			t.Fatalf("Query returned error: %v", err)
		}

		messages := receiveResponse(t, client)
		if got := assistantText(t, messages[0].(*AssistantMessage)); got != "permission:deny:0:denied:interrupt" {
			t.Fatalf("unexpected permission deny text: %q", got)
		}
	})
}

func TestClientPermissionCallbackContextIncludesOptionalRequestFields(t *testing.T) {
	var permissionCtx ToolPermissionContext
	client := newFakeClient(t, "permission_context", ClientOptions{
		CanUseTool: func(ctx context.Context, toolName string, input map[string]any, ctxFromRequest ToolPermissionContext) (PermissionResult, error) {
			permissionCtx = ctxFromRequest
			return PermissionResultDeny{Message: "captured"}, nil
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	defer client.Close()

	if err := client.Query(ctx, "needs permission"); err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	_ = receiveResponse(t, client)

	assertPermissionContextValue(t, permissionCtx, "ToolUseID", "toolu_optional_123")
	assertPermissionContextValue(t, permissionCtx, "AgentID", "agent-reviewer-1")
	assertPermissionContextValue(t, permissionCtx, "BlockedPath", "/private/blocked.txt")
	assertPermissionContextValue(t, permissionCtx, "DecisionReason", "hook requested review")
	assertPermissionContextValue(t, permissionCtx, "Title", "Claude wants to edit blocked.txt")
	assertPermissionContextValue(t, permissionCtx, "DisplayName", "Edit file")
	assertPermissionContextValue(t, permissionCtx, "Description", "Writes outside the allowed directory")
}

func TestClientHookCallbackRegistrationAndResponse(t *testing.T) {
	matcher := "Write"
	client := newFakeClient(t, "hook", ClientOptions{
		Hooks: map[HookEvent][]HookMatcher{
			HookEventPreToolUse: {
				{
					Matcher: &matcher,
					Hooks: []HookCallback{
						func(ctx context.Context, input map[string]any, toolUseID *string, hookCtx HookContext) (HookResult, error) {
							if toolUseID == nil || *toolUseID != "toolu_123" {
								t.Fatalf("unexpected tool_use_id: %#v", toolUseID)
							}
							return HookResult{
								Continue:      boolPtr(false),
								Decision:      stringPtr("block"),
								SystemMessage: stringPtr("blocked"),
								HookSpecificOutput: map[string]any{
									"hookEventName":     "PreToolUse",
									"additionalContext": "extra",
								},
							}, nil
						},
					},
					Timeout: time.Second,
				},
			},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	defer client.Close()

	if err := client.Query(ctx, "run hook"); err != nil {
		t.Fatalf("Query returned error: %v", err)
	}

	messages := receiveResponse(t, client)
	if got := assistantText(t, messages[0].(*AssistantMessage)); got != "hook:continue=false:decision=block:system=blocked:context=extra" {
		t.Fatalf("unexpected hook response text: %q", got)
	}
}

func TestClientSDKMCPBridge(t *testing.T) {
	description := "Echo tool"
	client := newFakeClient(t, "mcp", ClientOptions{
		ClaudeAgentOptions: ClaudeAgentOptions{
			MCPServers: map[string]MCPServerConfig{
				"sdk-test": SDKMCPServerConfig{
					Name: "sdk-test",
					Instance: NewSimpleMCPServer(MCPServerInfo{Name: "sdk-test", Version: "1.2.3"}, []MCPTool{
						{
							Name:        "echo",
							Description: &description,
							Annotations: &MCPToolAnnotations{ReadOnly: true},
							InputSchema: map[string]any{"type": "object"},
							Handler: func(ctx context.Context, arguments map[string]any) (MCPToolResult, error) {
								return MCPToolResult{
									Content: []MCPContent{
										MCPTextContent{Text: arguments["text"].(string)},
									},
								}, nil
							},
						},
					}),
				},
			},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	defer client.Close()

	testCases := []struct {
		prompt string
		want   string
	}{
		{prompt: "initialize", want: "mcp-init:sdk-test:1.2.3"},
		{prompt: "tools/list", want: "mcp-list:echo:true"},
		{prompt: "tools/call", want: "mcp-call:from-client:false"},
		{prompt: "unknown/method", want: "mcp-error:-32601:Method 'unknown/method' not found"},
	}

	for _, tc := range testCases {
		if err := client.Query(ctx, tc.prompt); err != nil {
			t.Fatalf("Query(%q) returned error: %v", tc.prompt, err)
		}
		messages := receiveResponse(t, client)
		if got := assistantText(t, messages[0].(*AssistantMessage)); got != tc.want {
			t.Fatalf("unexpected MCP response for %q: got %q want %q", tc.prompt, got, tc.want)
		}
	}
}

func TestClientPreservesAuthenticationFailedAssistantMessage(t *testing.T) {
	client := newFakeClient(t, "auth", ClientOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	defer client.Close()

	if err := client.Query(ctx, "auth please"); err != nil {
		t.Fatalf("Query returned error: %v", err)
	}

	messages := receiveResponse(t, client)
	assistant, ok := messages[0].(*AssistantMessage)
	if !ok {
		t.Fatalf("expected AssistantMessage, got %T", messages[0])
	}
	if assistant.Error == nil || *assistant.Error != AssistantMessageErrorAuthenticationFailed {
		t.Fatalf("unexpected assistant auth error: %#v", assistant.Error)
	}
}

func TestClientDisconnectedMethodsReturnCLIConnectionError(t *testing.T) {
	client := NewClient(ClientOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	model := "claude-sonnet-4-5"
	operations := []struct {
		name string
		run  func() error
	}{
		{name: "Query", run: func() error { return client.Query(ctx, "hi") }},
		{name: "Send", run: func() error { return client.Send(ctx, ClientMessage{}) }},
		{name: "Interrupt", run: func() error { return client.Interrupt(ctx) }},
		{name: "SetPermissionMode", run: func() error { return client.SetPermissionMode(ctx, PermissionModeDefault) }},
		{name: "SetModel", run: func() error { return client.SetModel(ctx, &model) }},
		{name: "RewindFiles", run: func() error { return client.RewindFiles(ctx, "user-1") }},
		{name: "ReconnectMCPServer", run: func() error { return client.ReconnectMCPServer(ctx, "sdk") }},
		{name: "ToggleMCPServer", run: func() error { return client.ToggleMCPServer(ctx, "sdk", true) }},
		{name: "StopTask", run: func() error { return client.StopTask(ctx, "task-1") }},
		{name: "MCPStatus", run: func() error { _, err := client.MCPStatus(ctx); return err }},
	}
	for _, operation := range operations {
		if err := operation.run(); !errors.As(err, new(*CLIConnectionError)) {
			t.Fatalf("%s returned unexpected error: %v", operation.name, err)
		}
	}
	if _, err := client.Receive(ctx); !errors.As(err, new(*CLIConnectionError)) {
		t.Fatalf("Receive returned unexpected error: %v", err)
	}
	if client.ServerInfo() != nil {
		t.Fatalf("expected nil ServerInfo on disconnected client")
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
}

func newFakeClient(t *testing.T, mode string, options ClientOptions) *Client {
	t.Helper()

	cliPath := buildFakeInteractiveCLI(t)
	options.CLIPath = &cliPath
	env := cloneStringMap(options.Env)
	if env == nil {
		env = make(map[string]string)
	}
	env["FAKE_CLAUDE_MODE"] = mode
	options.Env = env
	return NewClient(options)
}

func buildFakeInteractiveCLI(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve caller path")
	}

	moduleRoot := filepath.Dir(file)
	fixtureDir := filepath.Join(moduleRoot, "testdata", "client")
	binPath := filepath.Join(t.TempDir(), "fake-claude-client")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}

	cmd := exec.Command("go", "build", "-o", binPath, fixtureDir)
	cmd.Dir = moduleRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build fake interactive CLI: %v\n%s", err, output)
	}

	return binPath
}

func receiveResponse(t *testing.T, client *Client) []Message {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var messages []Message
	for {
		message, err := client.Receive(ctx)
		if err != nil {
			t.Fatalf("Receive returned error: %v", err)
		}
		messages = append(messages, message)
		if _, ok := message.(*ResultMessage); ok {
			return messages
		}
	}
}

func assistantText(t *testing.T, message *AssistantMessage) string {
	t.Helper()
	if message == nil || len(message.Content) == 0 {
		t.Fatalf("assistant message had no content: %#v", message)
	}
	block, ok := message.Content[0].(TextBlock)
	if !ok {
		t.Fatalf("expected TextBlock, got %T", message.Content[0])
	}
	return strings.TrimSpace(block.Text)
}

type controlCaptureTransport struct {
	mu       sync.Mutex
	requests []map[string]any
	reads    chan []byte
	closed   chan struct{}
}

func newControlCaptureTransport() *controlCaptureTransport {
	return &controlCaptureTransport{
		reads:  make(chan []byte, 10),
		closed: make(chan struct{}),
	}
}

func (t *controlCaptureTransport) Connect(context.Context) error {
	return nil
}

func (t *controlCaptureTransport) Write(ctx context.Context, data []byte) error {
	var envelope map[string]any
	if err := json.Unmarshal(data, &envelope); err != nil {
		return err
	}
	request, _ := envelope["request"].(map[string]any)
	if request != nil {
		t.mu.Lock()
		t.requests = append(t.requests, cloneAnyMap(request))
		t.mu.Unlock()
	}

	requestID, _ := envelope["request_id"].(string)
	if requestID != "" {
		response := map[string]any{
			"type": "control_response",
			"response": map[string]any{
				"subtype":    "success",
				"request_id": requestID,
				"response":   map[string]any{"output_style": "default"},
			},
		}
		encoded, err := json.Marshal(response)
		if err != nil {
			return err
		}
		select {
		case t.reads <- encoded:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (t *controlCaptureTransport) Read(ctx context.Context) ([]byte, error) {
	select {
	case payload := <-t.reads:
		return payload, nil
	case <-t.closed:
		return nil, io.EOF
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (t *controlCaptureTransport) CloseInput() error {
	return nil
}

func (t *controlCaptureTransport) Close() error {
	select {
	case <-t.closed:
	default:
		close(t.closed)
	}
	return nil
}

func (t *controlCaptureTransport) firstRequest(tb testing.TB) map[string]any {
	tb.Helper()
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.requests) == 0 {
		tb.Fatal("expected at least one captured control request")
	}
	return cloneAnyMap(t.requests[0])
}

func assertPermissionContextValue(t *testing.T, permissionCtx ToolPermissionContext, fieldName string, want string) {
	t.Helper()
	if got, ok := permissionContextField(permissionCtx, fieldName); ok {
		if got != want {
			t.Fatalf("%s = %q, want %q", fieldName, got, want)
		}
		return
	}

	metadata, ok := permissionCtx.Signal.(map[string]any)
	if !ok {
		t.Fatalf("permission context had no %s field or signal metadata: %#v", fieldName, permissionCtx.Signal)
	}
	key := lowerInitialism(fieldName)
	if got, _ := metadata[key].(string); got != want {
		t.Fatalf("permission signal metadata %q = %q, want %q", key, got, want)
	}
}

func permissionContextField(permissionCtx ToolPermissionContext, fieldName string) (string, bool) {
	value := reflect.ValueOf(permissionCtx)
	field := value.FieldByName(fieldName)
	if !field.IsValid() {
		return "", false
	}
	switch field.Kind() {
	case reflect.String:
		return field.String(), true
	case reflect.Pointer:
		if field.Type().Elem().Kind() == reflect.String && !field.IsNil() {
			return field.Elem().String(), true
		}
	}
	return "", false
}

func lowerInitialism(value string) string {
	switch value {
	case "ToolUseID":
		return "tool_use_id"
	case "AgentID":
		return "agent_id"
	default:
		if value == "" {
			return value
		}
		var builder strings.Builder
		for index, r := range value {
			if index > 0 && r >= 'A' && r <= 'Z' {
				builder.WriteByte('_')
			}
			builder.WriteRune(r)
		}
		return strings.ToLower(builder.String())
	}
}

func boolPtr(value bool) *bool       { return &value }
func stringPtr(value string) *string { return &value }
