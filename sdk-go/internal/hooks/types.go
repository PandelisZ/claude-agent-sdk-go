package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type PermissionBehavior string

const (
	PermissionBehaviorAllow PermissionBehavior = "allow"
	PermissionBehaviorDeny  PermissionBehavior = "deny"
	PermissionBehaviorAsk   PermissionBehavior = "ask"
)

type PermissionUpdateDestination string

const (
	PermissionUpdateDestinationUserSettings    PermissionUpdateDestination = "userSettings"
	PermissionUpdateDestinationProjectSettings PermissionUpdateDestination = "projectSettings"
	PermissionUpdateDestinationLocalSettings   PermissionUpdateDestination = "localSettings"
	PermissionUpdateDestinationSession         PermissionUpdateDestination = "session"
)

type PermissionRuleValue struct {
	ToolName    string  `json:"toolName"`
	RuleContent *string `json:"ruleContent,omitempty"`
}

type PermissionUpdate struct {
	Type        string                       `json:"type"`
	Rules       []PermissionRuleValue        `json:"rules,omitempty"`
	Behavior    *PermissionBehavior          `json:"behavior,omitempty"`
	Mode        *string                      `json:"mode,omitempty"`
	Directories []string                     `json:"directories,omitempty"`
	Destination *PermissionUpdateDestination `json:"destination,omitempty"`
}

func (u PermissionUpdate) ToMap() map[string]any {
	result := map[string]any{
		"type": u.Type,
	}
	if u.Destination != nil {
		result["destination"] = string(*u.Destination)
	}
	switch u.Type {
	case "addRules", "replaceRules", "removeRules":
		if len(u.Rules) > 0 {
			rules := make([]map[string]any, 0, len(u.Rules))
			for _, rule := range u.Rules {
				item := map[string]any{"toolName": rule.ToolName}
				if rule.RuleContent != nil {
					item["ruleContent"] = *rule.RuleContent
				}
				rules = append(rules, item)
			}
			result["rules"] = rules
		}
		if u.Behavior != nil {
			result["behavior"] = string(*u.Behavior)
		}
	case "setMode":
		if u.Mode != nil {
			result["mode"] = *u.Mode
		}
	case "addDirectories", "removeDirectories":
		if len(u.Directories) > 0 {
			result["directories"] = append([]string(nil), u.Directories...)
		}
	}
	return result
}

type ToolPermissionContext struct {
	Signal      any
	Suggestions []PermissionUpdate
}

type PermissionResult interface {
	isPermissionResult()
}

type PermissionResultAllow struct {
	UpdatedInput       map[string]any
	UpdatedPermissions []PermissionUpdate
}

func (PermissionResultAllow) isPermissionResult() {}

type PermissionResultDeny struct {
	Message   string
	Interrupt bool
}

func (PermissionResultDeny) isPermissionResult() {}

type PermissionHandler func(context.Context, string, map[string]any, ToolPermissionContext) (PermissionResult, error)

type Event string

const (
	EventPreToolUse        Event = "PreToolUse"
	EventPostToolUse       Event = "PostToolUse"
	EventPostToolUseFail   Event = "PostToolUseFailure"
	EventUserPromptSubmit  Event = "UserPromptSubmit"
	EventStop              Event = "Stop"
	EventSubagentStop      Event = "SubagentStop"
	EventPreCompact        Event = "PreCompact"
	EventNotification      Event = "Notification"
	EventSubagentStart     Event = "SubagentStart"
	EventPermissionRequest Event = "PermissionRequest"
)

type Context struct {
	Signal any
}

type Result struct {
	Async              *bool   `json:"async,omitempty"`
	AsyncTimeout       *int    `json:"asyncTimeout,omitempty"`
	Continue           *bool   `json:"continue,omitempty"`
	SuppressOutput     *bool   `json:"suppressOutput,omitempty"`
	StopReason         *string `json:"stopReason,omitempty"`
	Decision           *string `json:"decision,omitempty"`
	SystemMessage      *string `json:"systemMessage,omitempty"`
	Reason             *string `json:"reason,omitempty"`
	HookSpecificOutput any     `json:"hookSpecificOutput,omitempty"`
}

type Callback func(context.Context, map[string]any, *string, Context) (Result, error)

type Matcher struct {
	Matcher *string
	Hooks   []Callback
	Timeout time.Duration
}

func EncodeResult(result Result) (map[string]any, error) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		return nil, err
	}
	if payload == nil {
		payload = make(map[string]any)
	}
	return payload, nil
}

func ParsePermissionUpdates(raw []any) ([]PermissionUpdate, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	updates := make([]PermissionUpdate, 0, len(raw))
	for _, item := range raw {
		payload, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("permission update must be an object")
		}

		update := PermissionUpdate{}
		if value, ok := payload["type"].(string); ok {
			update.Type = value
		}
		if value, ok := payload["behavior"].(string); ok {
			behavior := PermissionBehavior(value)
			update.Behavior = &behavior
		}
		if value, ok := payload["mode"].(string); ok {
			update.Mode = &value
		}
		if value, ok := payload["destination"].(string); ok {
			destination := PermissionUpdateDestination(value)
			update.Destination = &destination
		}
		if items, ok := payload["directories"].([]any); ok {
			update.Directories = make([]string, 0, len(items))
			for _, dir := range items {
				if text, ok := dir.(string); ok {
					update.Directories = append(update.Directories, text)
				}
			}
		}
		if items, ok := payload["rules"].([]any); ok {
			update.Rules = make([]PermissionRuleValue, 0, len(items))
			for _, entry := range items {
				ruleMap, ok := entry.(map[string]any)
				if !ok {
					continue
				}
				rule := PermissionRuleValue{}
				if value, ok := ruleMap["toolName"].(string); ok {
					rule.ToolName = value
				}
				if value, ok := ruleMap["ruleContent"].(string); ok {
					rule.RuleContent = &value
				}
				update.Rules = append(update.Rules, rule)
			}
		}

		updates = append(updates, update)
	}

	return updates, nil
}
