package claudeagentsdk

// PermissionMode controls how the CLI handles tool permission prompts.
type PermissionMode string

const (
	PermissionModeDefault           PermissionMode = "default"
	PermissionModeAcceptEdits       PermissionMode = "acceptEdits"
	PermissionModePlan              PermissionMode = "plan"
	PermissionModeBypassPermissions PermissionMode = "bypassPermissions"
)

// SdkBeta identifies a CLI beta feature flag.
type SdkBeta string

const (
	SdkBetaContext1M20250807 SdkBeta = "context-1m-2025-08-07"
)

// SettingSource identifies a settings source loaded by the CLI.
type SettingSource string

const (
	SettingSourceUser    SettingSource = "user"
	SettingSourceProject SettingSource = "project"
	SettingSourceLocal   SettingSource = "local"
)

// ToolsPreset models the preset-based tools configuration exposed by the Python SDK.
type ToolsPreset struct {
	Type   string `json:"type"`
	Preset string `json:"preset"`
}

// SystemPromptPreset configures a built-in system prompt preset.
type SystemPromptPreset struct {
	Type   string  `json:"type"`
	Preset string  `json:"preset"`
	Append *string `json:"append,omitempty"`
}

// SystemPromptFile configures a system prompt loaded from disk.
type SystemPromptFile struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

// ThinkingConfigType controls extended thinking behavior.
type ThinkingConfigType string

const (
	ThinkingConfigAdaptive ThinkingConfigType = "adaptive"
	ThinkingConfigEnabled  ThinkingConfigType = "enabled"
	ThinkingConfigDisabled ThinkingConfigType = "disabled"
)

// ThinkingConfig matches the Python SDK's tagged thinking configuration.
type ThinkingConfig struct {
	Type         ThinkingConfigType `json:"type"`
	BudgetTokens *int               `json:"budget_tokens,omitempty"`
}

// SDKPluginConfig configures a local SDK plugin.
type SDKPluginConfig struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

// MCPServerConfig is implemented by MCP server configuration variants accepted
// by ClaudeAgentOptions.
type MCPServerConfig interface {
	mcpServerConfigType() string
}

// MCPStdioServerConfig configures an MCP server launched over stdio.
type MCPStdioServerConfig struct {
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

func (c MCPStdioServerConfig) mcpServerConfigType() string {
	if c.Type != "" {
		return c.Type
	}
	return "stdio"
}

// MCPSSEServerConfig configures an MCP SSE server.
type MCPSSEServerConfig struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

func (c MCPSSEServerConfig) mcpServerConfigType() string {
	return c.Type
}

// MCPHTTPServerConfig configures an MCP HTTP server.
type MCPHTTPServerConfig struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

func (c MCPHTTPServerConfig) mcpServerConfigType() string {
	return c.Type
}

// MCPSDKServerConfig configures an in-process SDK-backed MCP server.
type MCPSDKServerConfig struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

func (c MCPSDKServerConfig) mcpServerConfigType() string {
	return c.Type
}

// MCPServerConnectionStatus is the wire-format connection status for an MCP server.
type MCPServerConnectionStatus string

const (
	MCPServerStatusConnected MCPServerConnectionStatus = "connected"
	MCPServerStatusFailed    MCPServerConnectionStatus = "failed"
	MCPServerStatusNeedsAuth MCPServerConnectionStatus = "needs-auth"
	MCPServerStatusPending   MCPServerConnectionStatus = "pending"
	MCPServerStatusDisabled  MCPServerConnectionStatus = "disabled"
)

// MCPToolAnnotations describe operational hints for an MCP tool.
type MCPToolAnnotations struct {
	ReadOnly    bool `json:"readOnly,omitempty"`
	Destructive bool `json:"destructive,omitempty"`
	OpenWorld   bool `json:"openWorld,omitempty"`
}

// MCPToolInfo describes a tool advertised by an MCP server.
type MCPToolInfo struct {
	Name        string              `json:"name"`
	Description *string             `json:"description,omitempty"`
	Annotations *MCPToolAnnotations `json:"annotations,omitempty"`
}

// MCPServerInfo is returned by the MCP initialize handshake.
type MCPServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// MCPServerStatusConfig is implemented by serializable MCP status config variants.
type MCPServerStatusConfig interface {
	mcpServerStatusConfigType() string
}

// MCPSDKServerConfigStatus is the serializable status variant for an SDK-backed server.
type MCPSDKServerConfigStatus struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

func (c MCPSDKServerConfigStatus) mcpServerStatusConfigType() string {
	return c.Type
}

// MCPClaudeAIProxyServerConfig is the output-only status config for claude.ai-proxied servers.
type MCPClaudeAIProxyServerConfig struct {
	Type string `json:"type"`
	URL  string `json:"url"`
	ID   string `json:"id"`
}

func (c MCPClaudeAIProxyServerConfig) mcpServerStatusConfigType() string {
	return c.Type
}

func (c MCPStdioServerConfig) mcpServerStatusConfigType() string { return c.mcpServerConfigType() }
func (c MCPSSEServerConfig) mcpServerStatusConfigType() string   { return c.Type }
func (c MCPHTTPServerConfig) mcpServerStatusConfigType() string  { return c.Type }

// MCPServerStatus reports connection and discovery state for a single MCP server.
type MCPServerStatus struct {
	Name       string                    `json:"name"`
	Status     MCPServerConnectionStatus `json:"status"`
	ServerInfo *MCPServerInfo            `json:"serverInfo,omitempty"`
	Error      *string                   `json:"error,omitempty"`
	Config     MCPServerStatusConfig     `json:"config,omitempty"`
	Scope      *string                   `json:"scope,omitempty"`
	Tools      []MCPToolInfo             `json:"tools,omitempty"`
}

// MCPStatusResponse wraps the list returned by get_mcp_status().
type MCPStatusResponse struct {
	MCPServers []MCPServerStatus `json:"mcpServers"`
}

// ClaudeAgentOptions carries the public query/session configuration shared by later waves.
type ClaudeAgentOptions struct {
	Tools                    []string
	ToolsPreset              *ToolsPreset
	AllowedTools             []string
	SystemPrompt             *string
	SystemPromptPreset       *SystemPromptPreset
	SystemPromptFile         *SystemPromptFile
	MCPServers               map[string]MCPServerConfig
	PermissionMode           *PermissionMode
	ContinueConversation     bool
	Resume                   *string
	ForkSession              bool
	MaxTurns                 *int
	MaxBudgetUSD             *float64
	DisallowedTools          []string
	Model                    *string
	FallbackModel            *string
	Betas                    []SdkBeta
	PermissionPromptToolName *string
	Cwd                      *string
	CLIPath                  *string
	Settings                 *string
	AddDirs                  []string
	Env                      map[string]string
	ExtraArgs                map[string]*string
	MaxBufferSize            *int
	User                     *string
	IncludePartialMessages   bool
	SettingSources           []SettingSource
	Plugins                  []SDKPluginConfig
	MaxThinkingTokens        *int
	Thinking                 *ThinkingConfig
	Effort                   *string
	OutputFormat             map[string]any
	EnableFileCheckpointing  bool
}

// ContentBlock represents an assistant or user content block.
type ContentBlock interface {
	ContentBlockType() string
}

// TextBlock is a plain text content block.
type TextBlock struct {
	Text string
}

func (TextBlock) ContentBlockType() string { return "text" }

// ThinkingBlock is an extended thinking content block.
type ThinkingBlock struct {
	Thinking  string
	Signature string
}

func (ThinkingBlock) ContentBlockType() string { return "thinking" }

// ToolUseBlock is a tool call content block.
type ToolUseBlock struct {
	ID    string
	Name  string
	Input map[string]any
}

func (ToolUseBlock) ContentBlockType() string { return "tool_use" }

// ToolResultBlock is a tool result content block.
type ToolResultBlock struct {
	ToolUseID string
	Content   any
	IsError   *bool
}

func (ToolResultBlock) ContentBlockType() string { return "tool_result" }

// UnknownContentBlock preserves a forward-compatible block payload.
type UnknownContentBlock struct {
	Type string
	Raw  map[string]any
}

func (b UnknownContentBlock) ContentBlockType() string { return b.Type }

// UserContentKind describes how user content is represented.
type UserContentKind string

const (
	UserContentKindText   UserContentKind = "text"
	UserContentKindBlocks UserContentKind = "blocks"
)

// UserContent preserves the Python SDK's string-or-block-list user content shape.
type UserContent struct {
	Kind   UserContentKind
	Text   string
	Blocks []ContentBlock
}

// AssistantMessageErrorKind is the top-level assistant error kind exposed by the CLI.
type AssistantMessageErrorKind string

const (
	AssistantMessageErrorAuthenticationFailed AssistantMessageErrorKind = "authentication_failed"
	AssistantMessageErrorBillingError         AssistantMessageErrorKind = "billing_error"
	AssistantMessageErrorRateLimit            AssistantMessageErrorKind = "rate_limit"
	AssistantMessageErrorInvalidRequest       AssistantMessageErrorKind = "invalid_request"
	AssistantMessageErrorServerError          AssistantMessageErrorKind = "server_error"
	AssistantMessageErrorUnknown              AssistantMessageErrorKind = "unknown"
)

// Message is implemented by all parsed top-level CLI message types.
type Message interface {
	MessageType() string
}

// UserMessage is a user-authored message emitted by the CLI.
type UserMessage struct {
	Content         UserContent
	UUID            *string
	ParentToolUseID *string
	ToolUseResult   map[string]any
}

func (*UserMessage) MessageType() string { return "user" }

// AssistantMessage is an assistant-authored message emitted by the CLI.
type AssistantMessage struct {
	Content         []ContentBlock
	Model           string
	ParentToolUseID *string
	Error           *AssistantMessageErrorKind
	Usage           map[string]any
	MessageID       *string
	StopReason      *string
	SessionID       *string
	UUID            *string
}

func (*AssistantMessage) MessageType() string { return "assistant" }

// SystemMessage preserves a raw system message subtype and payload.
type SystemMessage struct {
	Subtype string
	Data    map[string]any
}

func (*SystemMessage) MessageType() string { return "system" }

// TaskUsage is emitted on task progress and task notification messages.
type TaskUsage struct {
	TotalTokens int
	ToolUses    int
	DurationMS  int
}

// TaskNotificationStatus is the status field for task_notification messages.
type TaskNotificationStatus string

const (
	TaskNotificationStatusCompleted TaskNotificationStatus = "completed"
	TaskNotificationStatusFailed    TaskNotificationStatus = "failed"
	TaskNotificationStatusStopped   TaskNotificationStatus = "stopped"
)

// TaskStartedMessage is the typed task_started system message.
type TaskStartedMessage struct {
	SystemMessage
	TaskID      string
	Description string
	UUID        string
	SessionID   string
	ToolUseID   *string
	TaskType    *string
}

// TaskProgressMessage is the typed task_progress system message.
type TaskProgressMessage struct {
	SystemMessage
	TaskID       string
	Description  string
	Usage        TaskUsage
	UUID         string
	SessionID    string
	ToolUseID    *string
	LastToolName *string
}

// TaskNotificationMessage is the typed task_notification system message.
type TaskNotificationMessage struct {
	SystemMessage
	TaskID     string
	Status     TaskNotificationStatus
	OutputFile string
	Summary    string
	UUID       string
	SessionID  string
	ToolUseID  *string
	Usage      *TaskUsage
}

// ResultMessage is the final result payload for a session.
type ResultMessage struct {
	Subtype           string
	DurationMS        int
	DurationAPIMS     int
	IsError           bool
	NumTurns          int
	SessionID         string
	StopReason        *string
	TotalCostUSD      *float64
	Usage             map[string]any
	Result            *string
	StructuredOutput  any
	ModelUsage        map[string]any
	PermissionDenials []any
	UUID              *string
}

func (*ResultMessage) MessageType() string { return "result" }

// StreamEvent preserves a raw Anthropic streaming event.
type StreamEvent struct {
	UUID            string
	SessionID       string
	Event           map[string]any
	ParentToolUseID *string
}

func (*StreamEvent) MessageType() string { return "stream_event" }

// RateLimitStatus is the current rate limit state reported by the CLI.
type RateLimitStatus string

const (
	RateLimitStatusAllowed        RateLimitStatus = "allowed"
	RateLimitStatusAllowedWarning RateLimitStatus = "allowed_warning"
	RateLimitStatusRejected       RateLimitStatus = "rejected"
)

// RateLimitType identifies the active rate limit window.
type RateLimitType string

const (
	RateLimitTypeFiveHour       RateLimitType = "five_hour"
	RateLimitTypeSevenDay       RateLimitType = "seven_day"
	RateLimitTypeSevenDayOpus   RateLimitType = "seven_day_opus"
	RateLimitTypeSevenDaySonnet RateLimitType = "seven_day_sonnet"
	RateLimitTypeOverage        RateLimitType = "overage"
)

// RateLimitInfo models the typed portion of a rate_limit_event payload while
// preserving the raw map for forward compatibility.
type RateLimitInfo struct {
	Status                RateLimitStatus
	ResetsAt              *int64
	RateLimitType         *RateLimitType
	Utilization           *float64
	OverageStatus         *RateLimitStatus
	OverageResetsAt       *int64
	OverageDisabledReason *string
	Raw                   map[string]any
}

// RateLimitEvent is emitted when the CLI's rate limit state changes.
type RateLimitEvent struct {
	RateLimitInfo RateLimitInfo
	UUID          string
	SessionID     string
}

func (*RateLimitEvent) MessageType() string { return "rate_limit_event" }

// UnknownMessage preserves a forward-compatible top-level message payload.
type UnknownMessage struct {
	Type string
	Raw  map[string]any
}

func (m *UnknownMessage) MessageType() string { return m.Type }
