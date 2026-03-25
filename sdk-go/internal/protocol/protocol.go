package protocol

import (
	"encoding/json"
	"fmt"
)

const (
	MessageTypeUser           = "user"
	MessageTypeAssistant      = "assistant"
	MessageTypeSystem         = "system"
	MessageTypeResult         = "result"
	MessageTypeStreamEvent    = "stream_event"
	MessageTypeRateLimitEvent = "rate_limit_event"
)

const (
	ContentBlockTypeText       = "text"
	ContentBlockTypeThinking   = "thinking"
	ContentBlockTypeToolUse    = "tool_use"
	ContentBlockTypeToolResult = "tool_result"
)

const (
	SystemSubtypeTaskStarted      = "task_started"
	SystemSubtypeTaskProgress     = "task_progress"
	SystemSubtypeTaskNotification = "task_notification"
)

// Envelope captures the common top-level dispatch fields present on all payloads.
type Envelope struct {
	Type    string         `json:"type"`
	Subtype string         `json:"subtype,omitempty"`
	Message map[string]any `json:"message,omitempty"`
}

// DecodeJSONBytes decodes a single raw CLI JSON message into a map payload.
func DecodeJSONBytes(data []byte) (map[string]any, error) {
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// CloneMap makes a shallow copy suitable for preserving raw payloads.
func CloneMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	cloned := make(map[string]any, len(input))
	for k, v := range input {
		cloned[k] = v
	}
	return cloned
}

func StringValue(data map[string]any, key string) (string, bool) {
	if data == nil {
		return "", false
	}
	raw, ok := data[key]
	if !ok || raw == nil {
		return "", false
	}
	value, ok := raw.(string)
	return value, ok
}

func RequireString(data map[string]any, key string) (string, error) {
	value, ok := StringValue(data, key)
	if !ok {
		return "", fmt.Errorf("missing required field %q", key)
	}
	return value, nil
}

func MapValue(data map[string]any, key string) (map[string]any, bool) {
	if data == nil {
		return nil, false
	}
	raw, ok := data[key]
	if !ok || raw == nil {
		return nil, false
	}
	value, ok := raw.(map[string]any)
	return value, ok
}

func RequireMap(data map[string]any, key string) (map[string]any, error) {
	value, ok := MapValue(data, key)
	if !ok {
		return nil, fmt.Errorf("missing required field %q", key)
	}
	return value, nil
}

func SliceValue(data map[string]any, key string) ([]any, bool) {
	if data == nil {
		return nil, false
	}
	raw, ok := data[key]
	if !ok || raw == nil {
		return nil, false
	}
	value, ok := raw.([]any)
	return value, ok
}

func RequireSlice(data map[string]any, key string) ([]any, error) {
	value, ok := SliceValue(data, key)
	if !ok {
		return nil, fmt.Errorf("missing required field %q", key)
	}
	return value, nil
}

func BoolValue(data map[string]any, key string) (*bool, bool) {
	if data == nil {
		return nil, false
	}
	raw, ok := data[key]
	if !ok || raw == nil {
		return nil, false
	}
	value, ok := raw.(bool)
	if !ok {
		return nil, false
	}
	return &value, true
}

func IntValue(data map[string]any, key string) (*int, bool) {
	floatValue, ok := FloatValue(data, key)
	if !ok || floatValue == nil {
		return nil, false
	}
	value := int(*floatValue)
	return &value, true
}

func Int64Value(data map[string]any, key string) (*int64, bool) {
	floatValue, ok := FloatValue(data, key)
	if !ok || floatValue == nil {
		return nil, false
	}
	value := int64(*floatValue)
	return &value, true
}

func FloatValue(data map[string]any, key string) (*float64, bool) {
	if data == nil {
		return nil, false
	}
	raw, ok := data[key]
	if !ok || raw == nil {
		return nil, false
	}
	value, ok := raw.(float64)
	if !ok {
		return nil, false
	}
	return &value, true
}
