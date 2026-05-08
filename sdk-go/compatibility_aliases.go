package claudeagentsdk

import (
	"context"
)

type ClaudeSDKClient = Client

type Transport interface {
	Connect(context.Context) error
	Write(context.Context, []byte) error
	CloseInput() error
	Read(context.Context) ([]byte, error)
	Close() error
}

type McpServerConfig = MCPServerConfig
type McpSdkServerConfig = SDKMCPServerConfig
type McpServerStatus = MCPServerStatus
type McpServerStatusConfig = MCPServerStatusConfig
type McpServerConnectionStatus = MCPServerConnectionStatus
type McpServerInfo = MCPServerInfo
type McpStatusResponse = MCPStatusResponse
type McpToolAnnotations = MCPToolAnnotations
type McpToolInfo = MCPToolInfo
type CanUseTool = CanUseToolCallback
type HookInput map[string]any
type BaseHookInput map[string]any
type PreToolUseHookInput map[string]any
type PostToolUseHookInput map[string]any
type PostToolUseFailureHookInput map[string]any
type PostToolUseFailureHookSpecificOutput map[string]any
type UserPromptSubmitHookInput map[string]any
type StopHookInput map[string]any
type SubagentStopHookInput map[string]any
type PreCompactHookInput map[string]any
type NotificationHookInput map[string]any
type SubagentStartHookInput map[string]any
type PermissionRequestHookInput map[string]any
type NotificationHookSpecificOutput map[string]any
type SubagentStartHookSpecificOutput map[string]any
type PermissionRequestHookSpecificOutput map[string]any
type HookJSONOutput map[string]any
type SdkPluginConfig = SDKPluginConfig
type SdkMcpTool = MCPTool
type ToolAnnotations = MCPToolAnnotations

const (
	McpServerStatusConnected McpServerConnectionStatus = McpServerConnectionStatus(MCPServerStatusConnected)
	McpServerStatusFailed    McpServerConnectionStatus = McpServerConnectionStatus(MCPServerStatusFailed)
	McpServerStatusNeedsAuth McpServerConnectionStatus = McpServerConnectionStatus(MCPServerStatusNeedsAuth)
	McpServerStatusPending   McpServerConnectionStatus = McpServerConnectionStatus(MCPServerStatusPending)
	McpServerStatusDisabled  McpServerConnectionStatus = McpServerConnectionStatus(MCPServerStatusDisabled)
)

func CreateSDKMCPServer(name string, version string, tools []SdkMcpTool) McpSdkServerConfig {
	if version == "" {
		version = "1.0.0"
	}
	return McpSdkServerConfig{
		Type:     "sdk",
		Name:     name,
		Instance: NewSimpleMCPServer(MCPServerInfo{Name: name, Version: version}, tools),
	}
}

func Tool(name string, description string, inputSchema map[string]any, handler func(context.Context, map[string]any) (MCPToolResult, error)) SdkMcpTool {
	return SdkMcpTool{
		Name:        name,
		Description: &description,
		InputSchema: inputSchema,
		Handler:     handler,
	}
}
