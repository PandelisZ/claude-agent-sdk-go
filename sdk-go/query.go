package claudeagentsdk

import (
	"context"
	"errors"
	"fmt"

	"github.com/PandelisZ/claude-agent-sdk-go/sdk-go/internal/protocol"
	"github.com/PandelisZ/claude-agent-sdk-go/sdk-go/internal/queryruntime"
	internaltransport "github.com/PandelisZ/claude-agent-sdk-go/sdk-go/internal/transport"
)

// QueryHandler receives one parsed CLI message at a time.
type QueryHandler func(Message) error

// Query executes a one-shot prompt against the local Claude CLI and collects
// all emitted messages.
func Query(ctx context.Context, prompt string, options ClaudeAgentOptions) ([]Message, error) {
	return queryWithTransport(ctx, prompt, options, nil)
}

// QueryWithTransport executes a one-shot prompt using a caller-provided
// transport instead of spawning the Claude CLI subprocess.
func QueryWithTransport(ctx context.Context, prompt string, options ClaudeAgentOptions, transport Transport) ([]Message, error) {
	return queryWithTransport(ctx, prompt, options, transport)
}

func queryWithTransport(ctx context.Context, prompt string, options ClaudeAgentOptions, transport Transport) ([]Message, error) {
	messages := make([]Message, 0, 8)

	err := queryWithCallbackAndTransport(ctx, prompt, options, transport, func(message Message) error {
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
	return queryWithCallbackAndTransport(ctx, prompt, options, nil, handler)
}

// QueryWithCallbackAndTransport executes a one-shot prompt using a
// caller-provided transport and streams parsed messages to handler.
func QueryWithCallbackAndTransport(ctx context.Context, prompt string, options ClaudeAgentOptions, transport Transport, handler QueryHandler) error {
	return queryWithCallbackAndTransport(ctx, prompt, options, transport, handler)
}

func queryWithCallbackAndTransport(ctx context.Context, prompt string, options ClaudeAgentOptions, transport Transport, handler QueryHandler) error {
	if handler == nil {
		handler = func(Message) error { return nil }
	}
	if options.CanUseTool != nil {
		return fmt.Errorf("can_use_tool callback requires streaming input; use Client for interactive permission handling")
	}
	if queryNeedsControlRuntime(options) {
		return queryWithClientRuntime(ctx, prompt, options, transport, handler)
	}

	preparedOptions := options
	var materialized *materializedStoreSession
	var err error
	if transport == nil {
		preparedOptions, materialized, err = prepareSessionStoreOptions(ctx, options)
		if err != nil {
			return err
		}
	} else if err := validateSessionStoreOptions(options); err != nil {
		return err
	}
	defer func() {
		_ = cleanupMaterializedStoreSession(materialized)
	}()

	var runtimeTransport internaltransport.Transport
	if transport != nil {
		runtimeTransport = transport
	} else {
		runtimeTransport = internaltransport.NewSubprocessCLITransport(toInternalTransportOptions(preparedOptions))
	}
	mirror := newSessionStoreMirror(preparedOptions)
	runner := queryruntime.NewRunner(runtimeTransport)
	err = runner.Run(ctx, prompt, func(payload []byte) error {
		raw, err := protocol.DecodeJSONBytes(payload)
		if err != nil {
			return NewCLIJSONDecodeError(string(payload), err)
		}
		if handled, mirrorMessage, err := mirror.handlePayload(ctx, raw); handled {
			if err != nil {
				return err
			}
			if mirrorMessage != nil {
				return handler(mirrorMessage)
			}
			return nil
		}
		if messageType, _ := protocol.StringValue(raw, "type"); messageType == protocol.MessageTypeResult {
			if mirrorMessage := mirror.flush(ctx); mirrorMessage != nil {
				if err := handler(mirrorMessage); err != nil {
					return err
				}
			}
		}
		message, err := parseQueryJSON(payload)
		if err != nil {
			return err
		}
		return handler(message)
	})
	return mapInternalTransportError(err)
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
	case "mirror_error":
		return &MirrorErrorMessage{
			SystemMessage: base,
			Key:           parseQuerySessionKey(payload["key"]),
			Error:         queryStringValueOrEmpty(payload, "error"),
		}, nil
	case "hook_started", "hook_response":
		return &HookEventMessage{
			SystemMessage: base,
			HookEventName: firstQueryStringValue(payload, "hook_event", "hook_name", "hook_event_name"),
			SessionID:     optionalQueryString(payload, "session_id"),
			UUID:          optionalQueryString(payload, "uuid"),
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
	sessionID, err := protocol.RequireString(payload, "session_id")
	if err != nil {
		return nil, NewMessageParseError("missing required field in result message: 'session_id'", payload)
	}

	return &ResultMessage{
		Subtype:           subtype,
		DurationMS:        optionalQueryIntValue(payload, "duration_ms"),
		DurationAPIMS:     optionalQueryIntValue(payload, "duration_api_ms"),
		IsError:           optionalQueryBoolValue(payload, "is_error"),
		NumTurns:          optionalQueryIntValue(payload, "num_turns"),
		SessionID:         sessionID,
		StopReason:        optionalQueryString(payload, "stop_reason"),
		TotalCostUSD:      optionalQueryFloat(payload, "total_cost_usd"),
		Usage:             optionalQueryMap(payload, "usage"),
		Result:            optionalQueryString(payload, "result"),
		StructuredOutput:  payload["structured_output"],
		ModelUsage:        optionalQueryMap(payload, "modelUsage"),
		PermissionDenials: optionalQuerySlice(payload, "permission_denials"),
		DeferredToolUse:   parseQueryDeferredToolUse(payload["deferred_tool_use"]),
		Errors:            optionalQueryStringSlice(payload, "errors"),
		APIErrorStatus:    optionalQueryIntPtr(payload, "api_error_status"),
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
		case "server_tool_use":
			id, err := protocol.RequireString(raw, "id")
			if err != nil {
				return nil, fmt.Errorf("server_tool_use block missing 'id'")
			}
			name, err := protocol.RequireString(raw, "name")
			if err != nil {
				return nil, fmt.Errorf("server_tool_use block missing 'name'")
			}
			input, err := protocol.RequireMap(raw, "input")
			if err != nil {
				return nil, fmt.Errorf("server_tool_use block missing 'input'")
			}
			blocks = append(blocks, ServerToolUseBlock{
				ID:    id,
				Name:  ServerToolName(name),
				Input: input,
			})
		case "advisor_tool_result", "server_tool_result":
			toolUseID, err := protocol.RequireString(raw, "tool_use_id")
			if err != nil {
				return nil, fmt.Errorf("%s block missing 'tool_use_id'", blockType)
			}
			content, err := protocol.RequireMap(raw, "content")
			if err != nil {
				return nil, fmt.Errorf("%s block missing 'content'", blockType)
			}
			blocks = append(blocks, ServerToolResultBlock{
				ToolUseID: toolUseID,
				Content:   content,
			})
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
	description, _ = protocol.StringValue(payload, "description")
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

func firstQueryStringValue(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := protocol.StringValue(payload, key); ok {
			return value
		}
	}
	return ""
}

func queryStringValueOrEmpty(payload map[string]any, key string) string {
	if value, ok := protocol.StringValue(payload, key); ok {
		return value
	}
	return ""
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

func optionalQueryIntPtr(payload map[string]any, key string) *int {
	if value, ok := protocol.IntValue(payload, key); ok {
		return value
	}
	return nil
}

func optionalQueryIntValue(payload map[string]any, key string) int {
	if value, ok := protocol.IntValue(payload, key); ok && value != nil {
		return *value
	}
	return 0
}

func optionalQueryBoolValue(payload map[string]any, key string) bool {
	if value, ok := protocol.BoolValue(payload, key); ok && value != nil {
		return *value
	}
	return false
}

func optionalQuerySlice(payload map[string]any, key string) []any {
	if value, ok := protocol.SliceValue(payload, key); ok {
		return value
	}
	return nil
}

func optionalQueryStringSlice(payload map[string]any, key string) []string {
	items, ok := protocol.SliceValue(payload, key)
	if !ok {
		return nil
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		if value, ok := item.(string); ok {
			values = append(values, value)
		}
	}
	return values
}

func parseQueryDeferredToolUse(raw any) *DeferredToolUse {
	payload, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	id, _ := protocol.StringValue(payload, "id")
	name, _ := protocol.StringValue(payload, "name")
	input, _ := protocol.MapValue(payload, "input")
	return &DeferredToolUse{
		ID:    id,
		Name:  name,
		Input: input,
	}
}

func parseQuerySessionKey(raw any) *SessionKey {
	payload, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	projectKey, _ := protocol.StringValue(payload, "project_key")
	sessionID, _ := protocol.StringValue(payload, "session_id")
	key := &SessionKey{
		ProjectKey: projectKey,
		SessionID:  sessionID,
	}
	if subpath, ok := protocol.StringValue(payload, "subpath"); ok {
		key.Subpath = &subpath
	}
	return key
}
