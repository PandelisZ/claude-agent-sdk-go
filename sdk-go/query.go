package claudeagentsdk

import (
	"context"
	"errors"
	"fmt"

	"github.com/anthropics/claude-agent-sdk-python/sdk-go/internal/protocol"
	"github.com/anthropics/claude-agent-sdk-python/sdk-go/internal/queryruntime"
	internaltransport "github.com/anthropics/claude-agent-sdk-python/sdk-go/internal/transport"
)

// QueryHandler receives one parsed CLI message at a time.
type QueryHandler func(Message) error

// Query executes a one-shot prompt against the local Claude CLI and collects
// all emitted messages.
func Query(ctx context.Context, prompt string, options ClaudeAgentOptions) ([]Message, error) {
	messages := make([]Message, 0, 8)

	err := QueryWithCallback(ctx, prompt, options, func(message Message) error {
		messages = append(messages, message)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return messages, nil
}

// QueryWithCallback executes a one-shot prompt against the local Claude CLI and
// streams each parsed message to handler.
func QueryWithCallback(ctx context.Context, prompt string, options ClaudeAgentOptions, handler QueryHandler) error {
	if handler == nil {
		handler = func(Message) error { return nil }
	}

	runner := queryruntime.NewRunner(internaltransport.NewSubprocessCLITransport(toInternalTransportOptions(options)))
	err := runner.Run(ctx, prompt, func(payload []byte) error {
		message, err := parseQueryJSON(payload)
		if err != nil {
			return err
		}
		return handler(message)
	})
	return mapInternalTransportError(err)
}

func toInternalTransportOptions(options ClaudeAgentOptions) internaltransport.Options {
	internalOptions := internaltransport.Options{
		Tools:                    append([]string(nil), options.Tools...),
		AllowedTools:             append([]string(nil), options.AllowedTools...),
		SystemPrompt:             options.SystemPrompt,
		ContinueConversation:     options.ContinueConversation,
		Resume:                   options.Resume,
		ForkSession:              options.ForkSession,
		MaxTurns:                 options.MaxTurns,
		MaxBudgetUSD:             options.MaxBudgetUSD,
		DisallowedTools:          append([]string(nil), options.DisallowedTools...),
		Model:                    options.Model,
		FallbackModel:            options.FallbackModel,
		PermissionPromptToolName: options.PermissionPromptToolName,
		Cwd:                      options.Cwd,
		CLIPath:                  options.CLIPath,
		Settings:                 options.Settings,
		AddDirs:                  append([]string(nil), options.AddDirs...),
		Env:                      cloneStringMap(options.Env),
		ExtraArgs:                cloneOptionalStringMap(options.ExtraArgs),
		MaxBufferSize:            options.MaxBufferSize,
		User:                     options.User,
		IncludePartialMessages:   options.IncludePartialMessages,
		Plugins:                  make([]internaltransport.SDKPluginConfig, 0, len(options.Plugins)),
		Effort:                   options.Effort,
		OutputFormat:             cloneAnyMap(options.OutputFormat),
		EnableFileCheckpointing:  options.EnableFileCheckpointing,
	}

	if options.ToolsPreset != nil {
		internalOptions.ToolsPreset = &internaltransport.ToolsPreset{
			Type:   options.ToolsPreset.Type,
			Preset: options.ToolsPreset.Preset,
		}
	}
	if options.SystemPromptPreset != nil {
		internalOptions.SystemPromptPreset = &internaltransport.SystemPromptPreset{
			Type:   options.SystemPromptPreset.Type,
			Preset: options.SystemPromptPreset.Preset,
			Append: options.SystemPromptPreset.Append,
		}
	}
	if options.SystemPromptFile != nil {
		internalOptions.SystemPromptFile = &internaltransport.SystemPromptFile{
			Type: options.SystemPromptFile.Type,
			Path: options.SystemPromptFile.Path,
		}
	}
	if options.PermissionMode != nil {
		mode := internaltransport.PermissionMode(*options.PermissionMode)
		internalOptions.PermissionMode = &mode
	}
	if len(options.Betas) > 0 {
		internalOptions.Betas = make([]internaltransport.SdkBeta, 0, len(options.Betas))
		for _, beta := range options.Betas {
			internalOptions.Betas = append(internalOptions.Betas, internaltransport.SdkBeta(beta))
		}
	}
	if len(options.MCPServers) > 0 {
		internalOptions.MCPServers = make(map[string]internaltransport.MCPServerConfig, len(options.MCPServers))
		for name, config := range options.MCPServers {
			switch typed := config.(type) {
			case MCPStdioServerConfig:
				internalOptions.MCPServers[name] = internaltransport.MCPStdioServerConfig{
					Type:    typed.Type,
					Command: typed.Command,
					Args:    append([]string(nil), typed.Args...),
					Env:     cloneStringMap(typed.Env),
				}
			case MCPSSEServerConfig:
				internalOptions.MCPServers[name] = internaltransport.MCPSSEServerConfig{
					Type:    typed.Type,
					URL:     typed.URL,
					Headers: cloneStringMap(typed.Headers),
				}
			case MCPHTTPServerConfig:
				internalOptions.MCPServers[name] = internaltransport.MCPHTTPServerConfig{
					Type:    typed.Type,
					URL:     typed.URL,
					Headers: cloneStringMap(typed.Headers),
				}
			case MCPSDKServerConfig:
				internalOptions.MCPServers[name] = internaltransport.MCPSDKServerConfig{
					Type: typed.Type,
					Name: typed.Name,
				}
			}
		}
	}
	if len(options.SettingSources) > 0 {
		internalOptions.SettingSources = make([]internaltransport.SettingSource, 0, len(options.SettingSources))
		for _, source := range options.SettingSources {
			internalOptions.SettingSources = append(internalOptions.SettingSources, internaltransport.SettingSource(source))
		}
	}
	for _, plugin := range options.Plugins {
		internalOptions.Plugins = append(internalOptions.Plugins, internaltransport.SDKPluginConfig{
			Type: plugin.Type,
			Path: plugin.Path,
		})
	}
	if options.Thinking != nil {
		internalOptions.Thinking = &internaltransport.ThinkingConfig{
			Type:         internaltransport.ThinkingConfigType(options.Thinking.Type),
			BudgetTokens: options.Thinking.BudgetTokens,
		}
	}
	internalOptions.MaxThinkingTokens = options.MaxThinkingTokens

	return internalOptions
}

func mapInternalTransportError(err error) error {
	if err == nil {
		return nil
	}

	var notFoundErr *internaltransport.CLINotFoundError
	if errors.As(err, &notFoundErr) {
		return NewCLINotFoundError(notFoundErr.Message, notFoundErr.CLIPath)
	}
	var connectionErr *internaltransport.CLIConnectionError
	if errors.As(err, &connectionErr) {
		return NewCLIConnectionError(connectionErr.Message)
	}
	var processErr *internaltransport.ProcessError
	if errors.As(err, &processErr) {
		return NewProcessError(processErr.Message, processErr.ExitCode, processErr.Stderr)
	}
	var decodeErr *internaltransport.CLIJSONDecodeError
	if errors.As(err, &decodeErr) {
		return NewCLIJSONDecodeError(decodeErr.Line, decodeErr.OriginalError)
	}

	return err
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func cloneOptionalStringMap(values map[string]*string) map[string]*string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]*string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func cloneAnyMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func parseQueryJSON(data []byte) (Message, error) {
	payload, err := protocol.DecodeJSONBytes(data)
	if err != nil {
		return nil, NewCLIJSONDecodeError(string(data), err)
	}
	return parseQueryPayload(payload)
}

func parseQueryPayload(payload map[string]any) (Message, error) {
	if payload == nil {
		return nil, NewMessageParseError("invalid message data type (expected map, got <nil>)", nil)
	}

	messageType, ok := protocol.StringValue(payload, "type")
	if !ok || messageType == "" {
		return nil, NewMessageParseError("message missing 'type' field", payload)
	}

	switch messageType {
	case protocol.MessageTypeUser:
		return parseQueryUserMessage(payload)
	case protocol.MessageTypeAssistant:
		return parseQueryAssistantMessage(payload)
	case protocol.MessageTypeSystem:
		return parseQuerySystemMessage(payload)
	case protocol.MessageTypeResult:
		return parseQueryResultMessage(payload)
	case protocol.MessageTypeStreamEvent:
		return parseQueryStreamEvent(payload)
	case protocol.MessageTypeRateLimitEvent:
		return parseQueryRateLimitEvent(payload)
	default:
		return &UnknownMessage{
			Type: messageType,
			Raw:  protocol.CloneMap(payload),
		}, nil
	}
}

func parseQueryUserMessage(payload map[string]any) (Message, error) {
	message, err := protocol.RequireMap(payload, "message")
	if err != nil {
		return nil, NewMessageParseError("missing required field in user message: 'message'", payload)
	}

	contentRaw, ok := message["content"]
	if !ok {
		return nil, NewMessageParseError("missing required field in user message: 'content'", payload)
	}

	var content UserContent
	switch value := contentRaw.(type) {
	case string:
		content = UserContent{
			Kind: UserContentKindText,
			Text: value,
		}
	case []any:
		blocks, err := parseQueryContentBlocks(value)
		if err != nil {
			return nil, NewMessageParseError(fmt.Sprintf("invalid content block in user message: %v", err), payload)
		}
		content = UserContent{
			Kind:   UserContentKindBlocks,
			Blocks: blocks,
		}
	default:
		return nil, NewMessageParseError("invalid content field in user message", payload)
	}

	return &UserMessage{
		Content:         content,
		UUID:            optionalQueryString(payload, "uuid"),
		ParentToolUseID: optionalQueryString(payload, "parent_tool_use_id"),
		ToolUseResult:   optionalQueryMap(payload, "tool_use_result"),
	}, nil
}

func parseQueryAssistantMessage(payload map[string]any) (Message, error) {
	message, err := protocol.RequireMap(payload, "message")
	if err != nil {
		return nil, NewMessageParseError("missing required field in assistant message: 'message'", payload)
	}

	contentRaw, err := protocol.RequireSlice(message, "content")
	if err != nil {
		return nil, NewMessageParseError("missing required field in assistant message: 'content'", payload)
	}
	model, err := protocol.RequireString(message, "model")
	if err != nil {
		return nil, NewMessageParseError("missing required field in assistant message: 'model'", payload)
	}

	content, err := parseQueryContentBlocks(contentRaw)
	if err != nil {
		return nil, NewMessageParseError(fmt.Sprintf("invalid content block in assistant message: %v", err), payload)
	}

	var assistantErr *AssistantMessageErrorKind
	if value, ok := protocol.StringValue(payload, "error"); ok {
		errKind := AssistantMessageErrorKind(value)
		assistantErr = &errKind
	}

	return &AssistantMessage{
		Content:         content,
		Model:           model,
		ParentToolUseID: optionalQueryString(payload, "parent_tool_use_id"),
		Error:           assistantErr,
		Usage:           optionalQueryMap(message, "usage"),
		MessageID:       optionalQueryString(message, "id"),
		StopReason:      optionalQueryString(message, "stop_reason"),
		SessionID:       optionalQueryString(payload, "session_id"),
		UUID:            optionalQueryString(payload, "uuid"),
	}, nil
}

func parseQuerySystemMessage(payload map[string]any) (Message, error) {
	subtype, err := protocol.RequireString(payload, "subtype")
	if err != nil {
		return nil, NewMessageParseError("missing required field in system message: 'subtype'", payload)
	}

	base := SystemMessage{
		Subtype: subtype,
		Data:    protocol.CloneMap(payload),
	}

	switch subtype {
	case protocol.SystemSubtypeTaskStarted:
		taskID, description, uuid, sessionID, err := requiredQueryTaskCommon(payload)
		if err != nil {
			return nil, err
		}
		return &TaskStartedMessage{
			SystemMessage: base,
			TaskID:        taskID,
			Description:   description,
			UUID:          uuid,
			SessionID:     sessionID,
			ToolUseID:     optionalQueryString(payload, "tool_use_id"),
			TaskType:      optionalQueryString(payload, "task_type"),
		}, nil
	case protocol.SystemSubtypeTaskProgress:
		taskID, description, uuid, sessionID, err := requiredQueryTaskCommon(payload)
		if err != nil {
			return nil, err
		}
		usage, err := parseQueryTaskUsageField(payload, "usage", "system")
		if err != nil {
			return nil, err
		}
		return &TaskProgressMessage{
			SystemMessage: base,
			TaskID:        taskID,
			Description:   description,
			Usage:         usage,
			UUID:          uuid,
			SessionID:     sessionID,
			ToolUseID:     optionalQueryString(payload, "tool_use_id"),
			LastToolName:  optionalQueryString(payload, "last_tool_name"),
		}, nil
	case protocol.SystemSubtypeTaskNotification:
		taskID, _, uuid, sessionID, err := requiredQueryTaskCommon(payload)
		if err != nil {
			return nil, err
		}
		status, err := protocol.RequireString(payload, "status")
		if err != nil {
			return nil, NewMessageParseError("missing required field in system message: 'status'", payload)
		}
		outputFile, err := protocol.RequireString(payload, "output_file")
		if err != nil {
			return nil, NewMessageParseError("missing required field in system message: 'output_file'", payload)
		}
		summary, err := protocol.RequireString(payload, "summary")
		if err != nil {
			return nil, NewMessageParseError("missing required field in system message: 'summary'", payload)
		}
		usage, err := optionalQueryTaskUsageField(payload, "usage")
		if err != nil {
			return nil, err
		}
		return &TaskNotificationMessage{
			SystemMessage: base,
			TaskID:        taskID,
			Status:        TaskNotificationStatus(status),
			OutputFile:    outputFile,
			Summary:       summary,
			UUID:          uuid,
			SessionID:     sessionID,
			ToolUseID:     optionalQueryString(payload, "tool_use_id"),
			Usage:         usage,
		}, nil
	default:
		return &base, nil
	}
}

func parseQueryResultMessage(payload map[string]any) (Message, error) {
	subtype, err := protocol.RequireString(payload, "subtype")
	if err != nil {
		return nil, NewMessageParseError("missing required field in result message: 'subtype'", payload)
	}
	durationMS, err := requiredQueryInt(payload, "duration_ms", "result message")
	if err != nil {
		return nil, err
	}
	durationAPIMS, err := requiredQueryInt(payload, "duration_api_ms", "result message")
	if err != nil {
		return nil, err
	}
	isError, ok := protocol.BoolValue(payload, "is_error")
	if !ok || isError == nil {
		return nil, NewMessageParseError("missing required field in result message: 'is_error'", payload)
	}
	numTurns, err := requiredQueryInt(payload, "num_turns", "result message")
	if err != nil {
		return nil, err
	}
	sessionID, err := protocol.RequireString(payload, "session_id")
	if err != nil {
		return nil, NewMessageParseError("missing required field in result message: 'session_id'", payload)
	}

	return &ResultMessage{
		Subtype:           subtype,
		DurationMS:        durationMS,
		DurationAPIMS:     durationAPIMS,
		IsError:           *isError,
		NumTurns:          numTurns,
		SessionID:         sessionID,
		StopReason:        optionalQueryString(payload, "stop_reason"),
		TotalCostUSD:      optionalQueryFloat(payload, "total_cost_usd"),
		Usage:             optionalQueryMap(payload, "usage"),
		Result:            optionalQueryString(payload, "result"),
		StructuredOutput:  payload["structured_output"],
		ModelUsage:        optionalQueryMap(payload, "modelUsage"),
		PermissionDenials: optionalQuerySlice(payload, "permission_denials"),
		UUID:              optionalQueryString(payload, "uuid"),
	}, nil
}

func parseQueryStreamEvent(payload map[string]any) (Message, error) {
	uuid, err := protocol.RequireString(payload, "uuid")
	if err != nil {
		return nil, NewMessageParseError("missing required field in stream_event message: 'uuid'", payload)
	}
	sessionID, err := protocol.RequireString(payload, "session_id")
	if err != nil {
		return nil, NewMessageParseError("missing required field in stream_event message: 'session_id'", payload)
	}
	event, err := protocol.RequireMap(payload, "event")
	if err != nil {
		return nil, NewMessageParseError("missing required field in stream_event message: 'event'", payload)
	}

	return &StreamEvent{
		UUID:            uuid,
		SessionID:       sessionID,
		Event:           event,
		ParentToolUseID: optionalQueryString(payload, "parent_tool_use_id"),
	}, nil
}

func parseQueryRateLimitEvent(payload map[string]any) (Message, error) {
	infoMap, err := protocol.RequireMap(payload, "rate_limit_info")
	if err != nil {
		return nil, NewMessageParseError("missing required field in rate_limit_event message: 'rate_limit_info'", payload)
	}
	status, err := protocol.RequireString(infoMap, "status")
	if err != nil {
		return nil, NewMessageParseError("missing required field in rate_limit_event message: 'status'", payload)
	}
	uuid, err := protocol.RequireString(payload, "uuid")
	if err != nil {
		return nil, NewMessageParseError("missing required field in rate_limit_event message: 'uuid'", payload)
	}
	sessionID, err := protocol.RequireString(payload, "session_id")
	if err != nil {
		return nil, NewMessageParseError("missing required field in rate_limit_event message: 'session_id'", payload)
	}

	var limitType *RateLimitType
	if value, ok := protocol.StringValue(infoMap, "rateLimitType"); ok {
		typed := RateLimitType(value)
		limitType = &typed
	}
	var overageStatus *RateLimitStatus
	if value, ok := protocol.StringValue(infoMap, "overageStatus"); ok {
		typed := RateLimitStatus(value)
		overageStatus = &typed
	}

	return &RateLimitEvent{
		RateLimitInfo: RateLimitInfo{
			Status:                RateLimitStatus(status),
			ResetsAt:              optionalQueryInt64(infoMap, "resetsAt"),
			RateLimitType:         limitType,
			Utilization:           optionalQueryFloat(infoMap, "utilization"),
			OverageStatus:         overageStatus,
			OverageResetsAt:       optionalQueryInt64(infoMap, "overageResetsAt"),
			OverageDisabledReason: optionalQueryString(infoMap, "overageDisabledReason"),
			Raw:                   protocol.CloneMap(infoMap),
		},
		UUID:      uuid,
		SessionID: sessionID,
	}, nil
}

func parseQueryContentBlocks(items []any) ([]ContentBlock, error) {
	blocks := make([]ContentBlock, 0, len(items))
	for _, item := range items {
		raw, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("content block must be an object")
		}
		blockType, ok := protocol.StringValue(raw, "type")
		if !ok || blockType == "" {
			return nil, fmt.Errorf("content block missing 'type'")
		}
		switch blockType {
		case protocol.ContentBlockTypeText:
			text, err := protocol.RequireString(raw, "text")
			if err != nil {
				return nil, fmt.Errorf("text block missing 'text'")
			}
			blocks = append(blocks, TextBlock{Text: text})
		case protocol.ContentBlockTypeThinking:
			thinking, err := protocol.RequireString(raw, "thinking")
			if err != nil {
				return nil, fmt.Errorf("thinking block missing 'thinking'")
			}
			signature, err := protocol.RequireString(raw, "signature")
			if err != nil {
				return nil, fmt.Errorf("thinking block missing 'signature'")
			}
			blocks = append(blocks, ThinkingBlock{
				Thinking:  thinking,
				Signature: signature,
			})
		case protocol.ContentBlockTypeToolUse:
			id, err := protocol.RequireString(raw, "id")
			if err != nil {
				return nil, fmt.Errorf("tool_use block missing 'id'")
			}
			name, err := protocol.RequireString(raw, "name")
			if err != nil {
				return nil, fmt.Errorf("tool_use block missing 'name'")
			}
			input, err := protocol.RequireMap(raw, "input")
			if err != nil {
				return nil, fmt.Errorf("tool_use block missing 'input'")
			}
			blocks = append(blocks, ToolUseBlock{
				ID:    id,
				Name:  name,
				Input: input,
			})
		case protocol.ContentBlockTypeToolResult:
			toolUseID, err := protocol.RequireString(raw, "tool_use_id")
			if err != nil {
				return nil, fmt.Errorf("tool_result block missing 'tool_use_id'")
			}
			block := ToolResultBlock{
				ToolUseID: toolUseID,
				Content:   raw["content"],
			}
			if isError, ok := protocol.BoolValue(raw, "is_error"); ok {
				block.IsError = isError
			}
			blocks = append(blocks, block)
		default:
			blocks = append(blocks, UnknownContentBlock{
				Type: blockType,
				Raw:  protocol.CloneMap(raw),
			})
		}
	}
	return blocks, nil
}

func requiredQueryTaskCommon(payload map[string]any) (taskID string, description string, uuid string, sessionID string, err error) {
	taskID, err = protocol.RequireString(payload, "task_id")
	if err != nil {
		return "", "", "", "", NewMessageParseError("missing required field in system message: 'task_id'", payload)
	}
	description, err = protocol.RequireString(payload, "description")
	if err != nil {
		return "", "", "", "", NewMessageParseError("missing required field in system message: 'description'", payload)
	}
	uuid, err = protocol.RequireString(payload, "uuid")
	if err != nil {
		return "", "", "", "", NewMessageParseError("missing required field in system message: 'uuid'", payload)
	}
	sessionID, err = protocol.RequireString(payload, "session_id")
	if err != nil {
		return "", "", "", "", NewMessageParseError("missing required field in system message: 'session_id'", payload)
	}
	return taskID, description, uuid, sessionID, nil
}

func parseQueryTaskUsageField(payload map[string]any, key string, messageKind string) (TaskUsage, error) {
	usageMap, err := protocol.RequireMap(payload, key)
	if err != nil {
		return TaskUsage{}, NewMessageParseError(fmt.Sprintf("missing required field in %s message: '%s'", messageKind, key), payload)
	}
	return parseQueryTaskUsage(usageMap, payload)
}

func optionalQueryTaskUsageField(payload map[string]any, key string) (*TaskUsage, error) {
	usageMap, ok := protocol.MapValue(payload, key)
	if !ok {
		return nil, nil
	}
	usage, err := parseQueryTaskUsage(usageMap, payload)
	if err != nil {
		return nil, err
	}
	return &usage, nil
}

func parseQueryTaskUsage(usageMap map[string]any, payload map[string]any) (TaskUsage, error) {
	totalTokens, err := requiredQueryInt(usageMap, "total_tokens", "task usage")
	if err != nil {
		return TaskUsage{}, err
	}
	toolUses, err := requiredQueryInt(usageMap, "tool_uses", "task usage")
	if err != nil {
		return TaskUsage{}, err
	}
	durationMS, err := requiredQueryInt(usageMap, "duration_ms", "task usage")
	if err != nil {
		return TaskUsage{}, err
	}
	return TaskUsage{
		TotalTokens: totalTokens,
		ToolUses:    toolUses,
		DurationMS:  durationMS,
	}, nil
}

func requiredQueryInt(payload map[string]any, key string, messageKind string) (int, error) {
	value, ok := protocol.IntValue(payload, key)
	if !ok || value == nil {
		return 0, NewMessageParseError(fmt.Sprintf("missing required field in %s: '%s'", messageKind, key), payload)
	}
	return *value, nil
}

func optionalQueryString(payload map[string]any, key string) *string {
	if value, ok := protocol.StringValue(payload, key); ok {
		return &value
	}
	return nil
}

func optionalQueryMap(payload map[string]any, key string) map[string]any {
	if value, ok := protocol.MapValue(payload, key); ok {
		return value
	}
	return nil
}

func optionalQueryFloat(payload map[string]any, key string) *float64 {
	if value, ok := protocol.FloatValue(payload, key); ok {
		return value
	}
	return nil
}

func optionalQueryInt64(payload map[string]any, key string) *int64 {
	if value, ok := protocol.Int64Value(payload, key); ok {
		return value
	}
	return nil
}

func optionalQuerySlice(payload map[string]any, key string) []any {
	if value, ok := protocol.SliceValue(payload, key); ok {
		return value
	}
	return nil
}
