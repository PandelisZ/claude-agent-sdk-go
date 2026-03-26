package messageparser

import (
	"fmt"

	claudeagentsdk "github.com/PandelisZ/claude-agent-sdk-go"
	"github.com/PandelisZ/claude-agent-sdk-go/internal/protocol"
)

// ParseJSON parses one raw CLI JSON payload into a public message value.
func ParseJSON(data []byte) (claudeagentsdk.Message, error) {
	payload, err := protocol.DecodeJSONBytes(data)
	if err != nil {
		return nil, claudeagentsdk.NewCLIJSONDecodeError(string(data), err)
	}
	return ParsePayload(payload)
}

// ParsePayload parses a decoded CLI payload map into a public message value.
func ParsePayload(payload map[string]any) (claudeagentsdk.Message, error) {
	if payload == nil {
		return nil, claudeagentsdk.NewMessageParseError("invalid message data type (expected map, got <nil>)", nil)
	}

	messageType, ok := protocol.StringValue(payload, "type")
	if !ok || messageType == "" {
		return nil, claudeagentsdk.NewMessageParseError("message missing 'type' field", payload)
	}

	switch messageType {
	case protocol.MessageTypeUser:
		return parseUserMessage(payload)
	case protocol.MessageTypeAssistant:
		return parseAssistantMessage(payload)
	case protocol.MessageTypeSystem:
		return parseSystemMessage(payload)
	case protocol.MessageTypeResult:
		return parseResultMessage(payload)
	case protocol.MessageTypeStreamEvent:
		return parseStreamEvent(payload)
	case protocol.MessageTypeRateLimitEvent:
		return parseRateLimitEvent(payload)
	default:
		return &claudeagentsdk.UnknownMessage{
			Type: messageType,
			Raw:  protocol.CloneMap(payload),
		}, nil
	}
}

// ParseRaw parses a decoded raw payload into a public message value.
func ParseRaw(raw any) (claudeagentsdk.Message, error) {
	payload, ok := raw.(map[string]any)
	if !ok {
		return nil, claudeagentsdk.NewMessageParseError(
			fmt.Sprintf("invalid message data type (expected map, got %T)", raw),
			raw,
		)
	}
	return ParsePayload(payload)
}

func parseUserMessage(payload map[string]any) (claudeagentsdk.Message, error) {
	message, err := protocol.RequireMap(payload, "message")
	if err != nil {
		return nil, claudeagentsdk.NewMessageParseError("missing required field in user message: 'message'", payload)
	}

	contentRaw, ok := message["content"]
	if !ok {
		return nil, claudeagentsdk.NewMessageParseError("missing required field in user message: 'content'", payload)
	}

	var content claudeagentsdk.UserContent
	switch value := contentRaw.(type) {
	case string:
		content = claudeagentsdk.UserContent{
			Kind: claudeagentsdk.UserContentKindText,
			Text: value,
		}
	case []any:
		blocks, err := parseContentBlocks(value)
		if err != nil {
			return nil, claudeagentsdk.NewMessageParseError(
				fmt.Sprintf("invalid content block in user message: %v", err),
				payload,
			)
		}
		content = claudeagentsdk.UserContent{
			Kind:   claudeagentsdk.UserContentKindBlocks,
			Blocks: blocks,
		}
	default:
		return nil, claudeagentsdk.NewMessageParseError("invalid content field in user message", payload)
	}

	return &claudeagentsdk.UserMessage{
		Content:         content,
		UUID:            optionalString(payload, "uuid"),
		ParentToolUseID: optionalString(payload, "parent_tool_use_id"),
		ToolUseResult:   optionalMap(payload, "tool_use_result"),
	}, nil
}

func parseAssistantMessage(payload map[string]any) (claudeagentsdk.Message, error) {
	message, err := protocol.RequireMap(payload, "message")
	if err != nil {
		return nil, claudeagentsdk.NewMessageParseError("missing required field in assistant message: 'message'", payload)
	}

	contentRaw, err := protocol.RequireSlice(message, "content")
	if err != nil {
		return nil, claudeagentsdk.NewMessageParseError("missing required field in assistant message: 'content'", payload)
	}
	model, err := protocol.RequireString(message, "model")
	if err != nil {
		return nil, claudeagentsdk.NewMessageParseError("missing required field in assistant message: 'model'", payload)
	}

	content, err := parseContentBlocks(contentRaw)
	if err != nil {
		return nil, claudeagentsdk.NewMessageParseError(
			fmt.Sprintf("invalid content block in assistant message: %v", err),
			payload,
		)
	}

	var assistantErr *claudeagentsdk.AssistantMessageErrorKind
	if value, ok := protocol.StringValue(payload, "error"); ok {
		errKind := claudeagentsdk.AssistantMessageErrorKind(value)
		assistantErr = &errKind
	}

	return &claudeagentsdk.AssistantMessage{
		Content:         content,
		Model:           model,
		ParentToolUseID: optionalString(payload, "parent_tool_use_id"),
		Error:           assistantErr,
		Usage:           optionalMap(message, "usage"),
		MessageID:       optionalString(message, "id"),
		StopReason:      optionalString(message, "stop_reason"),
		SessionID:       optionalString(payload, "session_id"),
		UUID:            optionalString(payload, "uuid"),
	}, nil
}

func parseSystemMessage(payload map[string]any) (claudeagentsdk.Message, error) {
	subtype, err := protocol.RequireString(payload, "subtype")
	if err != nil {
		return nil, claudeagentsdk.NewMessageParseError("missing required field in system message: 'subtype'", payload)
	}

	base := claudeagentsdk.SystemMessage{
		Subtype: subtype,
		Data:    protocol.CloneMap(payload),
	}

	switch subtype {
	case protocol.SystemSubtypeTaskStarted:
		taskID, description, uuid, sessionID, err := requiredTaskCommon(payload)
		if err != nil {
			return nil, err
		}
		return &claudeagentsdk.TaskStartedMessage{
			SystemMessage: base,
			TaskID:        taskID,
			Description:   description,
			UUID:          uuid,
			SessionID:     sessionID,
			ToolUseID:     optionalString(payload, "tool_use_id"),
			TaskType:      optionalString(payload, "task_type"),
		}, nil
	case protocol.SystemSubtypeTaskProgress:
		taskID, description, uuid, sessionID, err := requiredTaskCommon(payload)
		if err != nil {
			return nil, err
		}
		usage, err := parseTaskUsageField(payload, "usage", "system")
		if err != nil {
			return nil, err
		}
		return &claudeagentsdk.TaskProgressMessage{
			SystemMessage: base,
			TaskID:        taskID,
			Description:   description,
			Usage:         usage,
			UUID:          uuid,
			SessionID:     sessionID,
			ToolUseID:     optionalString(payload, "tool_use_id"),
			LastToolName:  optionalString(payload, "last_tool_name"),
		}, nil
	case protocol.SystemSubtypeTaskNotification:
		taskID, _, uuid, sessionID, err := requiredTaskCommon(payload)
		if err != nil {
			return nil, err
		}
		status, err := protocol.RequireString(payload, "status")
		if err != nil {
			return nil, claudeagentsdk.NewMessageParseError("missing required field in system message: 'status'", payload)
		}
		outputFile, err := protocol.RequireString(payload, "output_file")
		if err != nil {
			return nil, claudeagentsdk.NewMessageParseError("missing required field in system message: 'output_file'", payload)
		}
		summary, err := protocol.RequireString(payload, "summary")
		if err != nil {
			return nil, claudeagentsdk.NewMessageParseError("missing required field in system message: 'summary'", payload)
		}
		usage, err := optionalTaskUsageField(payload, "usage")
		if err != nil {
			return nil, err
		}
		return &claudeagentsdk.TaskNotificationMessage{
			SystemMessage: base,
			TaskID:        taskID,
			Status:        claudeagentsdk.TaskNotificationStatus(status),
			OutputFile:    outputFile,
			Summary:       summary,
			UUID:          uuid,
			SessionID:     sessionID,
			ToolUseID:     optionalString(payload, "tool_use_id"),
			Usage:         usage,
		}, nil
	default:
		return &base, nil
	}
}

func parseResultMessage(payload map[string]any) (claudeagentsdk.Message, error) {
	subtype, err := protocol.RequireString(payload, "subtype")
	if err != nil {
		return nil, claudeagentsdk.NewMessageParseError("missing required field in result message: 'subtype'", payload)
	}
	durationMS, err := requiredInt(payload, "duration_ms", "result message")
	if err != nil {
		return nil, err
	}
	durationAPIMS, err := requiredInt(payload, "duration_api_ms", "result message")
	if err != nil {
		return nil, err
	}
	isError, ok := protocol.BoolValue(payload, "is_error")
	if !ok || isError == nil {
		return nil, claudeagentsdk.NewMessageParseError("missing required field in result message: 'is_error'", payload)
	}
	numTurns, err := requiredInt(payload, "num_turns", "result message")
	if err != nil {
		return nil, err
	}
	sessionID, err := protocol.RequireString(payload, "session_id")
	if err != nil {
		return nil, claudeagentsdk.NewMessageParseError("missing required field in result message: 'session_id'", payload)
	}

	totalCost := optionalFloat(payload, "total_cost_usd")
	return &claudeagentsdk.ResultMessage{
		Subtype:           subtype,
		DurationMS:        durationMS,
		DurationAPIMS:     durationAPIMS,
		IsError:           *isError,
		NumTurns:          numTurns,
		SessionID:         sessionID,
		StopReason:        optionalString(payload, "stop_reason"),
		TotalCostUSD:      totalCost,
		Usage:             optionalMap(payload, "usage"),
		Result:            optionalString(payload, "result"),
		StructuredOutput:  payload["structured_output"],
		ModelUsage:        optionalMap(payload, "modelUsage"),
		PermissionDenials: optionalSlice(payload, "permission_denials"),
		UUID:              optionalString(payload, "uuid"),
	}, nil
}

func parseStreamEvent(payload map[string]any) (claudeagentsdk.Message, error) {
	uuid, err := protocol.RequireString(payload, "uuid")
	if err != nil {
		return nil, claudeagentsdk.NewMessageParseError("missing required field in stream_event message: 'uuid'", payload)
	}
	sessionID, err := protocol.RequireString(payload, "session_id")
	if err != nil {
		return nil, claudeagentsdk.NewMessageParseError("missing required field in stream_event message: 'session_id'", payload)
	}
	event, err := protocol.RequireMap(payload, "event")
	if err != nil {
		return nil, claudeagentsdk.NewMessageParseError("missing required field in stream_event message: 'event'", payload)
	}

	return &claudeagentsdk.StreamEvent{
		UUID:            uuid,
		SessionID:       sessionID,
		Event:           event,
		ParentToolUseID: optionalString(payload, "parent_tool_use_id"),
	}, nil
}

func parseRateLimitEvent(payload map[string]any) (claudeagentsdk.Message, error) {
	infoMap, err := protocol.RequireMap(payload, "rate_limit_info")
	if err != nil {
		return nil, claudeagentsdk.NewMessageParseError("missing required field in rate_limit_event message: 'rate_limit_info'", payload)
	}
	status, err := protocol.RequireString(infoMap, "status")
	if err != nil {
		return nil, claudeagentsdk.NewMessageParseError("missing required field in rate_limit_event message: 'status'", payload)
	}
	uuid, err := protocol.RequireString(payload, "uuid")
	if err != nil {
		return nil, claudeagentsdk.NewMessageParseError("missing required field in rate_limit_event message: 'uuid'", payload)
	}
	sessionID, err := protocol.RequireString(payload, "session_id")
	if err != nil {
		return nil, claudeagentsdk.NewMessageParseError("missing required field in rate_limit_event message: 'session_id'", payload)
	}

	var limitType *claudeagentsdk.RateLimitType
	if value, ok := protocol.StringValue(infoMap, "rateLimitType"); ok {
		typed := claudeagentsdk.RateLimitType(value)
		limitType = &typed
	}
	var overageStatus *claudeagentsdk.RateLimitStatus
	if value, ok := protocol.StringValue(infoMap, "overageStatus"); ok {
		typed := claudeagentsdk.RateLimitStatus(value)
		overageStatus = &typed
	}

	return &claudeagentsdk.RateLimitEvent{
		RateLimitInfo: claudeagentsdk.RateLimitInfo{
			Status:                claudeagentsdk.RateLimitStatus(status),
			ResetsAt:              optionalInt64(infoMap, "resetsAt"),
			RateLimitType:         limitType,
			Utilization:           optionalFloat(infoMap, "utilization"),
			OverageStatus:         overageStatus,
			OverageResetsAt:       optionalInt64(infoMap, "overageResetsAt"),
			OverageDisabledReason: optionalString(infoMap, "overageDisabledReason"),
			Raw:                   protocol.CloneMap(infoMap),
		},
		UUID:      uuid,
		SessionID: sessionID,
	}, nil
}

func parseContentBlocks(items []any) ([]claudeagentsdk.ContentBlock, error) {
	blocks := make([]claudeagentsdk.ContentBlock, 0, len(items))
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
			blocks = append(blocks, claudeagentsdk.TextBlock{Text: text})
		case protocol.ContentBlockTypeThinking:
			thinking, err := protocol.RequireString(raw, "thinking")
			if err != nil {
				return nil, fmt.Errorf("thinking block missing 'thinking'")
			}
			signature, err := protocol.RequireString(raw, "signature")
			if err != nil {
				return nil, fmt.Errorf("thinking block missing 'signature'")
			}
			blocks = append(blocks, claudeagentsdk.ThinkingBlock{
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
			blocks = append(blocks, claudeagentsdk.ToolUseBlock{
				ID:    id,
				Name:  name,
				Input: input,
			})
		case protocol.ContentBlockTypeToolResult:
			toolUseID, err := protocol.RequireString(raw, "tool_use_id")
			if err != nil {
				return nil, fmt.Errorf("tool_result block missing 'tool_use_id'")
			}
			block := claudeagentsdk.ToolResultBlock{
				ToolUseID: toolUseID,
				Content:   raw["content"],
			}
			if isError, ok := protocol.BoolValue(raw, "is_error"); ok {
				block.IsError = isError
			}
			blocks = append(blocks, block)
		default:
			blocks = append(blocks, claudeagentsdk.UnknownContentBlock{
				Type: blockType,
				Raw:  protocol.CloneMap(raw),
			})
		}
	}
	return blocks, nil
}

func requiredTaskCommon(payload map[string]any) (taskID string, description string, uuid string, sessionID string, err error) {
	taskID, err = protocol.RequireString(payload, "task_id")
	if err != nil {
		return "", "", "", "", claudeagentsdk.NewMessageParseError("missing required field in system message: 'task_id'", payload)
	}
	description, _ = protocol.StringValue(payload, "description")
	uuid, err = protocol.RequireString(payload, "uuid")
	if err != nil {
		return "", "", "", "", claudeagentsdk.NewMessageParseError("missing required field in system message: 'uuid'", payload)
	}
	sessionID, err = protocol.RequireString(payload, "session_id")
	if err != nil {
		return "", "", "", "", claudeagentsdk.NewMessageParseError("missing required field in system message: 'session_id'", payload)
	}
	return taskID, description, uuid, sessionID, nil
}

func parseTaskUsageField(payload map[string]any, key string, messageKind string) (claudeagentsdk.TaskUsage, error) {
	usageMap, err := protocol.RequireMap(payload, key)
	if err != nil {
		return claudeagentsdk.TaskUsage{}, claudeagentsdk.NewMessageParseError(
			fmt.Sprintf("missing required field in %s message: '%s'", messageKind, key),
			payload,
		)
	}
	return parseTaskUsage(usageMap, payload)
}

func optionalTaskUsageField(payload map[string]any, key string) (*claudeagentsdk.TaskUsage, error) {
	usageMap, ok := protocol.MapValue(payload, key)
	if !ok {
		return nil, nil
	}
	usage, err := parseTaskUsage(usageMap, payload)
	if err != nil {
		return nil, err
	}
	return &usage, nil
}

func parseTaskUsage(usageMap map[string]any, payload map[string]any) (claudeagentsdk.TaskUsage, error) {
	totalTokens, err := requiredInt(usageMap, "total_tokens", "task usage")
	if err != nil {
		return claudeagentsdk.TaskUsage{}, err
	}
	toolUses, err := requiredInt(usageMap, "tool_uses", "task usage")
	if err != nil {
		return claudeagentsdk.TaskUsage{}, err
	}
	durationMS, err := requiredInt(usageMap, "duration_ms", "task usage")
	if err != nil {
		return claudeagentsdk.TaskUsage{}, err
	}
	return claudeagentsdk.TaskUsage{
		TotalTokens: totalTokens,
		ToolUses:    toolUses,
		DurationMS:  durationMS,
	}, nil
}

func requiredInt(payload map[string]any, key string, messageKind string) (int, error) {
	value, ok := protocol.IntValue(payload, key)
	if !ok || value == nil {
		return 0, claudeagentsdk.NewMessageParseError(
			fmt.Sprintf("missing required field in %s: '%s'", messageKind, key),
			payload,
		)
	}
	return *value, nil
}

func optionalString(payload map[string]any, key string) *string {
	if value, ok := protocol.StringValue(payload, key); ok {
		return &value
	}
	return nil
}

func optionalMap(payload map[string]any, key string) map[string]any {
	if value, ok := protocol.MapValue(payload, key); ok {
		return value
	}
	return nil
}

func optionalFloat(payload map[string]any, key string) *float64 {
	if value, ok := protocol.FloatValue(payload, key); ok {
		return value
	}
	return nil
}

func optionalInt64(payload map[string]any, key string) *int64 {
	if value, ok := protocol.Int64Value(payload, key); ok {
		return value
	}
	return nil
}

func optionalSlice(payload map[string]any, key string) []any {
	if value, ok := protocol.SliceValue(payload, key); ok {
		return value
	}
	return nil
}
