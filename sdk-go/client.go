package claudeagentsdk

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/anthropics/claude-agent-sdk-python/sdk-go/internal/control"
	internalhooks "github.com/anthropics/claude-agent-sdk-python/sdk-go/internal/hooks"
	internaltransport "github.com/anthropics/claude-agent-sdk-python/sdk-go/internal/transport"
)

type ToolPermissionContext = internalhooks.ToolPermissionContext
type PermissionBehavior = internalhooks.PermissionBehavior
type PermissionUpdateDestination = internalhooks.PermissionUpdateDestination
type PermissionRuleValue = internalhooks.PermissionRuleValue
type PermissionUpdate = internalhooks.PermissionUpdate
type PermissionResult = internalhooks.PermissionResult
type PermissionResultAllow = internalhooks.PermissionResultAllow
type PermissionResultDeny = internalhooks.PermissionResultDeny
type CanUseToolCallback = internalhooks.PermissionHandler

const (
	PermissionBehaviorAllow PermissionBehavior = internalhooks.PermissionBehaviorAllow
	PermissionBehaviorDeny  PermissionBehavior = internalhooks.PermissionBehaviorDeny
	PermissionBehaviorAsk   PermissionBehavior = internalhooks.PermissionBehaviorAsk
)

const (
	PermissionUpdateDestinationUserSettings    PermissionUpdateDestination = internalhooks.PermissionUpdateDestinationUserSettings
	PermissionUpdateDestinationProjectSettings PermissionUpdateDestination = internalhooks.PermissionUpdateDestinationProjectSettings
	PermissionUpdateDestinationLocalSettings   PermissionUpdateDestination = internalhooks.PermissionUpdateDestinationLocalSettings
	PermissionUpdateDestinationSession         PermissionUpdateDestination = internalhooks.PermissionUpdateDestinationSession
)

type HookEvent = internalhooks.Event
type HookContext = internalhooks.Context
type HookResult = internalhooks.Result
type HookCallback = internalhooks.Callback
type HookMatcher = internalhooks.Matcher

const (
	HookEventPreToolUse        HookEvent = internalhooks.EventPreToolUse
	HookEventPostToolUse       HookEvent = internalhooks.EventPostToolUse
	HookEventPostToolUseFail   HookEvent = internalhooks.EventPostToolUseFail
	HookEventUserPromptSubmit  HookEvent = internalhooks.EventUserPromptSubmit
	HookEventStop              HookEvent = internalhooks.EventStop
	HookEventSubagentStop      HookEvent = internalhooks.EventSubagentStop
	HookEventPreCompact        HookEvent = internalhooks.EventPreCompact
	HookEventNotification      HookEvent = internalhooks.EventNotification
	HookEventSubagentStart     HookEvent = internalhooks.EventSubagentStart
	HookEventPermissionRequest HookEvent = internalhooks.EventPermissionRequest
)

type ClientOptions struct {
	ClaudeAgentOptions
	CanUseTool        CanUseToolCallback
	Hooks             map[HookEvent][]HookMatcher
	InitializeTimeout time.Duration
}

type ClientMessage struct {
	SessionID       string
	Content         UserContent
	ParentToolUseID *string
	ToolUseResult   map[string]any
}

type Client struct {
	options ClientOptions

	mu      sync.RWMutex
	runtime *control.Runtime
}

func NewClient(options ClientOptions) *Client {
	return &Client{options: options}
}

func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.runtime != nil {
		return mapInternalTransportError(c.runtime.Connect(ctx))
	}

	transportOptions, handlers, err := c.prepareTransport()
	if err != nil {
		return err
	}

	var hookRegistry *internalhooks.Registry
	if len(c.options.Hooks) > 0 {
		hookRegistry = internalhooks.NewRegistry(c.options.Hooks)
	}

	runtime := control.NewRuntime(control.Options{
		Transport:         internaltransport.NewSubprocessCLITransport(transportOptions),
		InitializeTimeout: c.options.InitializeTimeout,
		PermissionHandler: c.options.CanUseTool,
		HookRegistry:      hookRegistry,
		MCPHandlers:       handlers,
	})
	if err := runtime.Connect(ctx); err != nil {
		return mapInternalTransportError(err)
	}

	c.runtime = runtime
	return nil
}

func (c *Client) Query(ctx context.Context, prompt string) error {
	return c.Send(ctx, ClientMessage{
		SessionID: "default",
		Content: UserContent{
			Kind: UserContentKindText,
			Text: prompt,
		},
	})
}

func (c *Client) Send(ctx context.Context, msg ClientMessage) error {
	runtime, err := c.getRuntime()
	if err != nil {
		return err
	}

	payload, err := c.clientMessageEnvelope(msg)
	if err != nil {
		return err
	}
	return mapInternalTransportError(runtime.SendUser(ctx, payload))
}

func (c *Client) Receive(ctx context.Context) (Message, error) {
	runtime, err := c.getRuntime()
	if err != nil {
		return nil, err
	}

	payload, err := runtime.Receive(ctx)
	if err != nil {
		return nil, mapInternalTransportError(err)
	}
	return parseQueryJSON(payload)
}

func (c *Client) Interrupt(ctx context.Context) error {
	_, err := c.sendControl(ctx, map[string]any{"subtype": "interrupt"})
	return err
}

func (c *Client) SetPermissionMode(ctx context.Context, mode PermissionMode) error {
	_, err := c.sendControl(ctx, map[string]any{
		"subtype": "set_permission_mode",
		"mode":    mode,
	})
	return err
}

func (c *Client) SetModel(ctx context.Context, model *string) error {
	_, err := c.sendControl(ctx, map[string]any{
		"subtype": "set_model",
		"model":   model,
	})
	return err
}

func (c *Client) RewindFiles(ctx context.Context, userMessageID string) error {
	_, err := c.sendControl(ctx, map[string]any{
		"subtype":         "rewind_files",
		"user_message_id": userMessageID,
	})
	return err
}

func (c *Client) ReconnectMCPServer(ctx context.Context, serverName string) error {
	_, err := c.sendControl(ctx, map[string]any{
		"subtype":    "mcp_reconnect",
		"serverName": serverName,
	})
	return err
}

func (c *Client) ToggleMCPServer(ctx context.Context, serverName string, enabled bool) error {
	_, err := c.sendControl(ctx, map[string]any{
		"subtype":    "mcp_toggle",
		"serverName": serverName,
		"enabled":    enabled,
	})
	return err
}

func (c *Client) StopTask(ctx context.Context, taskID string) error {
	_, err := c.sendControl(ctx, map[string]any{
		"subtype": "stop_task",
		"task_id": taskID,
	})
	return err
}

func (c *Client) MCPStatus(ctx context.Context) (MCPStatusResponse, error) {
	response, err := c.sendControl(ctx, map[string]any{"subtype": "mcp_status"})
	if err != nil {
		return MCPStatusResponse{}, err
	}
	return ParseMCPStatusResponse(response)
}

func (c *Client) ServerInfo() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.runtime == nil {
		return nil
	}
	return c.runtime.ServerInfo()
}

func (c *Client) Close() error {
	c.mu.Lock()
	runtime := c.runtime
	c.runtime = nil
	c.mu.Unlock()

	if runtime == nil {
		return nil
	}
	return mapInternalTransportError(runtime.Close())
}

func (c *Client) sendControl(ctx context.Context, request map[string]any) (map[string]any, error) {
	runtime, err := c.getRuntime()
	if err != nil {
		return nil, err
	}
	response, err := runtime.SendControl(ctx, request, 60*time.Second)
	if err != nil {
		return nil, mapInternalTransportError(err)
	}
	return response, nil
}

func (c *Client) getRuntime() (*control.Runtime, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.runtime == nil {
		return nil, NewCLIConnectionError("Not connected. Call Connect() first.")
	}
	return c.runtime, nil
}

func (c *Client) prepareTransport() (internaltransport.Options, map[string]control.MCPHandler, error) {
	options := c.options.ClaudeAgentOptions
	if c.options.CanUseTool != nil {
		if options.PermissionPromptToolName != nil {
			return internaltransport.Options{}, nil, fmt.Errorf("can_use_tool callback cannot be used with permission_prompt_tool_name")
		}
		promptTool := "stdio"
		options.PermissionPromptToolName = &promptTool
	}

	handlers := make(map[string]control.MCPHandler)
	if len(options.MCPServers) > 0 {
		converted := make(map[string]MCPServerConfig, len(options.MCPServers))
		for name, raw := range options.MCPServers {
			switch typed := raw.(type) {
			case SDKMCPServerConfig:
				serverName := typed.Name
				if serverName == "" {
					serverName = name
				}
				if typed.Instance != nil {
					handlers[name] = typed.Instance.HandleMCPMessage
				}
				converted[name] = MCPSDKServerConfig{Type: coalesceString(typed.Type, "sdk"), Name: serverName}
			case *SDKMCPServerConfig:
				if typed == nil {
					continue
				}
				serverName := typed.Name
				if serverName == "" {
					serverName = name
				}
				if typed.Instance != nil {
					handlers[name] = typed.Instance.HandleMCPMessage
				}
				converted[name] = MCPSDKServerConfig{Type: coalesceString(typed.Type, "sdk"), Name: serverName}
			default:
				converted[name] = raw
			}
		}
		options.MCPServers = converted
	}

	return toInternalTransportOptions(options), handlers, nil
}

func (c *Client) clientMessageEnvelope(msg ClientMessage) (map[string]any, error) {
	sessionID := msg.SessionID
	if sessionID == "" {
		sessionID = "default"
	}

	content, err := userContentToWire(msg.Content)
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		"type":       "user",
		"session_id": sessionID,
		"message": map[string]any{
			"role":    "user",
			"content": content,
		},
	}
	if msg.ParentToolUseID != nil {
		payload["parent_tool_use_id"] = *msg.ParentToolUseID
	}
	if msg.ToolUseResult != nil {
		payload["tool_use_result"] = cloneAnyMap(msg.ToolUseResult)
	}
	return payload, nil
}

func userContentToWire(content UserContent) (any, error) {
	switch content.Kind {
	case UserContentKindText:
		return content.Text, nil
	case UserContentKindBlocks:
		blocks := make([]map[string]any, 0, len(content.Blocks))
		for _, block := range content.Blocks {
			switch typed := block.(type) {
			case TextBlock:
				blocks = append(blocks, map[string]any{"type": "text", "text": typed.Text})
			case ThinkingBlock:
				blocks = append(blocks, map[string]any{
					"type":      "thinking",
					"thinking":  typed.Thinking,
					"signature": typed.Signature,
				})
			case ToolUseBlock:
				blocks = append(blocks, map[string]any{
					"type":  "tool_use",
					"id":    typed.ID,
					"name":  typed.Name,
					"input": cloneAnyMap(typed.Input),
				})
			case ToolResultBlock:
				item := map[string]any{
					"type":        "tool_result",
					"tool_use_id": typed.ToolUseID,
					"content":     typed.Content,
				}
				if typed.IsError != nil {
					item["is_error"] = *typed.IsError
				}
				blocks = append(blocks, item)
			case UnknownContentBlock:
				item := cloneAnyMap(typed.Raw)
				if item == nil {
					item = map[string]any{"type": typed.Type}
				}
				blocks = append(blocks, item)
			default:
				return nil, fmt.Errorf("unsupported user content block type %T", block)
			}
		}
		return blocks, nil
	default:
		return nil, fmt.Errorf("unsupported user content kind %q", content.Kind)
	}
}

func coalesceString(value string, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
