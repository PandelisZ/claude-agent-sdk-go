package messageparser

import (
	"encoding/json"
	"errors"
	"testing"

	claudeagentsdk "github.com/PandelisZ/claude-agent-sdk-go"
)

func TestParseJSONAssistantAuthFailure(t *testing.T) {
	payload := []byte(`{
		"type":"assistant",
		"message":{
			"content":[{"type":"text","text":"Invalid API key"}],
			"model":"<synthetic>",
			"id":"msg_123",
			"stop_reason":"end_turn",
			"usage":{"input_tokens":10,"output_tokens":1}
		},
		"session_id":"session-1",
		"uuid":"uuid-1",
		"error":"authentication_failed"
	}`)

	message, err := ParseJSON(payload)
	if err != nil {
		t.Fatalf("ParseJSON returned error: %v", err)
	}

	assistant, ok := message.(*claudeagentsdk.AssistantMessage)
	if !ok {
		t.Fatalf("expected AssistantMessage, got %T", message)
	}
	if assistant.Error == nil || *assistant.Error != claudeagentsdk.AssistantMessageErrorAuthenticationFailed {
		t.Fatalf("unexpected assistant error: %#v", assistant.Error)
	}
	if assistant.MessageID == nil || *assistant.MessageID != "msg_123" {
		t.Fatalf("unexpected assistant message id: %#v", assistant.MessageID)
	}
	if assistant.SessionID == nil || *assistant.SessionID != "session-1" {
		t.Fatalf("unexpected assistant session id: %#v", assistant.SessionID)
	}
}

func TestParsePayloadTaskSystemMessages(t *testing.T) {
	startedPayload := map[string]any{
		"type":        "system",
		"subtype":     "task_started",
		"task_id":     "task-1",
		"description": "Working",
		"uuid":        "uuid-1",
		"session_id":  "session-1",
		"tool_use_id": "toolu_1",
		"task_type":   "background",
	}

	startedMessage, err := ParsePayload(startedPayload)
	if err != nil {
		t.Fatalf("ParsePayload task_started returned error: %v", err)
	}
	started, ok := startedMessage.(*claudeagentsdk.TaskStartedMessage)
	if !ok {
		t.Fatalf("expected TaskStartedMessage, got %T", startedMessage)
	}
	if started.ToolUseID == nil || *started.ToolUseID != "toolu_1" {
		t.Fatalf("unexpected task_started tool_use_id: %#v", started.ToolUseID)
	}
	if started.SystemMessage.Subtype != "task_started" || started.SystemMessage.Data["task_id"] != "task-1" {
		t.Fatalf("base system payload not preserved: %#v", started.SystemMessage)
	}

	progressPayload := map[string]any{
		"type":        "system",
		"subtype":     "task_progress",
		"task_id":     "task-1",
		"description": "Halfway",
		"usage": map[string]any{
			"total_tokens": float64(321),
			"tool_uses":    float64(4),
			"duration_ms":  float64(1200),
		},
		"last_tool_name": "Read",
		"uuid":           "uuid-2",
		"session_id":     "session-1",
	}

	progressMessage, err := ParsePayload(progressPayload)
	if err != nil {
		t.Fatalf("ParsePayload task_progress returned error: %v", err)
	}
	progress, ok := progressMessage.(*claudeagentsdk.TaskProgressMessage)
	if !ok {
		t.Fatalf("expected TaskProgressMessage, got %T", progressMessage)
	}
	if progress.Usage.TotalTokens != 321 || progress.LastToolName == nil || *progress.LastToolName != "Read" {
		t.Fatalf("unexpected task progress payload: %#v", progress)
	}

	notificationPayload := map[string]any{
		"type":        "system",
		"subtype":     "task_notification",
		"task_id":     "task-1",
		"description": "Done",
		"status":      "completed",
		"output_file": "/tmp/out.md",
		"summary":     "All done",
		"usage": map[string]any{
			"total_tokens": float64(400),
			"tool_uses":    float64(6),
			"duration_ms":  float64(2000),
		},
		"uuid":       "uuid-3",
		"session_id": "session-1",
	}

	notificationMessage, err := ParsePayload(notificationPayload)
	if err != nil {
		t.Fatalf("ParsePayload task_notification returned error: %v", err)
	}
	notification, ok := notificationMessage.(*claudeagentsdk.TaskNotificationMessage)
	if !ok {
		t.Fatalf("expected TaskNotificationMessage, got %T", notificationMessage)
	}
	if notification.Status != claudeagentsdk.TaskNotificationStatusCompleted || notification.Usage == nil || notification.Usage.ToolUses != 6 {
		t.Fatalf("unexpected task notification payload: %#v", notification)
	}
}

func TestParsePayloadTaskNotificationAllowsMissingDescription(t *testing.T) {
	payload := map[string]any{
		"type":        "system",
		"subtype":     "task_notification",
		"task_id":     "task-1",
		"status":      "completed",
		"output_file": "/tmp/out.md",
		"summary":     "All done",
		"uuid":        "uuid-3",
		"session_id":  "session-1",
	}

	message, err := ParsePayload(payload)
	if err != nil {
		t.Fatalf("ParsePayload returned error: %v", err)
	}
	notification, ok := message.(*claudeagentsdk.TaskNotificationMessage)
	if !ok {
		t.Fatalf("expected TaskNotificationMessage, got %T", message)
	}
	if notification.TaskID != "task-1" || notification.Summary != "All done" {
		t.Fatalf("unexpected task notification payload: %#v", notification)
	}
}

func TestParsePayloadRateLimitEvent(t *testing.T) {
	payload := map[string]any{
		"type": "rate_limit_event",
		"rate_limit_info": map[string]any{
			"status":                "allowed_warning",
			"resetsAt":              float64(1700000000),
			"rateLimitType":         "five_hour",
			"utilization":           0.91,
			"overageStatus":         "rejected",
			"overageDisabledReason": "out_of_credits",
			"isUsingOverage":        false,
		},
		"uuid":       "uuid-1",
		"session_id": "session-1",
	}

	message, err := ParsePayload(payload)
	if err != nil {
		t.Fatalf("ParsePayload returned error: %v", err)
	}

	rateLimitEvent, ok := message.(*claudeagentsdk.RateLimitEvent)
	if !ok {
		t.Fatalf("expected RateLimitEvent, got %T", message)
	}
	if rateLimitEvent.RateLimitInfo.RateLimitType == nil || *rateLimitEvent.RateLimitInfo.RateLimitType != claudeagentsdk.RateLimitTypeFiveHour {
		t.Fatalf("unexpected rate limit type: %#v", rateLimitEvent.RateLimitInfo.RateLimitType)
	}
	if rateLimitEvent.RateLimitInfo.OverageStatus == nil || *rateLimitEvent.RateLimitInfo.OverageStatus != claudeagentsdk.RateLimitStatusRejected {
		t.Fatalf("unexpected overage status: %#v", rateLimitEvent.RateLimitInfo.OverageStatus)
	}
	if got, ok := rateLimitEvent.RateLimitInfo.Raw["isUsingOverage"].(bool); !ok || got {
		t.Fatalf("unexpected raw preservation: %#v", rateLimitEvent.RateLimitInfo.Raw)
	}
}

func TestParsePayloadUnknownMessageAndUnknownContent(t *testing.T) {
	unknownMessagePayload := map[string]any{
		"type":       "future_event",
		"session_id": "session-1",
		"feature":    "preview",
	}

	message, err := ParsePayload(unknownMessagePayload)
	if err != nil {
		t.Fatalf("ParsePayload returned error: %v", err)
	}
	unknownMessage, ok := message.(*claudeagentsdk.UnknownMessage)
	if !ok {
		t.Fatalf("expected UnknownMessage, got %T", message)
	}
	if unknownMessage.Type != "future_event" || unknownMessage.Raw["feature"] != "preview" {
		t.Fatalf("unexpected unknown message payload: %#v", unknownMessage)
	}

	assistantPayload := map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"content": []any{
				map[string]any{"type": "text", "text": "hello"},
				map[string]any{"type": "future_block", "value": "x"},
			},
			"model": "claude-sonnet-4-5",
		},
	}

	assistantMessage, err := ParsePayload(assistantPayload)
	if err != nil {
		t.Fatalf("ParsePayload returned error: %v", err)
	}
	assistant, ok := assistantMessage.(*claudeagentsdk.AssistantMessage)
	if !ok {
		t.Fatalf("expected AssistantMessage, got %T", assistantMessage)
	}
	if _, ok := assistant.Content[1].(claudeagentsdk.UnknownContentBlock); !ok {
		t.Fatalf("expected UnknownContentBlock, got %T", assistant.Content[1])
	}
}

func TestParsePayloadMalformedKnownMessage(t *testing.T) {
	_, err := ParsePayload(map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"content": []any{map[string]any{"type": "text"}},
			"model":   "claude-sonnet-4-5",
		},
	})
	if err == nil {
		t.Fatalf("expected parse error")
	}

	var parseErr *claudeagentsdk.MessageParseError
	if !errors.As(err, &parseErr) {
		t.Fatalf("expected MessageParseError, got %T", err)
	}
	if parseErr.Data == nil {
		t.Fatalf("expected raw payload on parse error")
	}
}

func TestParseJSONInvalidJSON(t *testing.T) {
	_, err := ParseJSON([]byte("{invalid json}"))
	if err == nil {
		t.Fatalf("expected parse error")
	}
	var decodeErr *claudeagentsdk.CLIJSONDecodeError
	if !errors.As(err, &decodeErr) {
		t.Fatalf("expected CLIJSONDecodeError, got %T", err)
	}
}

func TestParseRawAcceptsDecodedMap(t *testing.T) {
	var decoded map[string]any
	if err := json.Unmarshal([]byte(`{
		"type":"result",
		"subtype":"success",
		"duration_ms":1000,
		"duration_api_ms":500,
		"is_error":false,
		"num_turns":2,
		"session_id":"session-1",
		"modelUsage":{"claude-sonnet-4-5":{"costUSD":0.1}}
	}`), &decoded); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}

	message, err := ParseRaw(decoded)
	if err != nil {
		t.Fatalf("ParseRaw returned error: %v", err)
	}

	result, ok := message.(*claudeagentsdk.ResultMessage)
	if !ok {
		t.Fatalf("expected ResultMessage, got %T", message)
	}
	if result.ModelUsage["claude-sonnet-4-5"] == nil {
		t.Fatalf("expected model usage to be preserved: %#v", result.ModelUsage)
	}
}
