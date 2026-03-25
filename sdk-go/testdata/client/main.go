package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type state struct {
	mode           string
	hookCallbackID string
	pending        string
	mcpServerName  string
	mcpServerInfo  map[string]any
}

func main() {
	st := &state{
		mode:          getenvDefault("FAKE_CLAUDE_MODE", "happy"),
		mcpServerName: "sdk-test",
		mcpServerInfo: map[string]any{"name": "sdk-test", "version": "1.0.0"},
	}
	st.parseArgs(os.Args[1:])

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			fmt.Fprintln(os.Stderr, "invalid stdin JSON:", err)
			os.Exit(2)
		}

		switch payload["type"] {
		case "control_request":
			st.handleControlRequest(payload)
		case "control_response":
			st.handleControlResponse(payload)
		case "user":
			st.handleUser(payload)
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(3)
	}
}

func (s *state) handleControlRequest(payload map[string]any) {
	requestID, _ := payload["request_id"].(string)
	request, _ := payload["request"].(map[string]any)
	subtype, _ := request["subtype"].(string)

	switch subtype {
	case "initialize":
		if hooks, ok := request["hooks"].(map[string]any); ok {
			for _, rawMatchers := range hooks {
				matchers, _ := rawMatchers.([]any)
				for _, rawMatcher := range matchers {
					matcher, _ := rawMatcher.(map[string]any)
					callbackIDs, _ := matcher["hookCallbackIds"].([]any)
					if len(callbackIDs) > 0 {
						if id, ok := callbackIDs[0].(string); ok {
							s.hookCallbackID = id
							break
						}
					}
				}
				if s.hookCallbackID != "" {
					break
				}
			}
		}
		writeJSON(map[string]any{
			"type": "control_response",
			"response": map[string]any{
				"subtype":    "success",
				"request_id": requestID,
				"response": map[string]any{
					"commands":                []string{"/help", "/model"},
					"output_style":            "default",
					"available_output_styles": []string{"default", "json"},
				},
			},
		})
	case "interrupt", "set_permission_mode", "set_model", "rewind_files", "mcp_reconnect", "mcp_toggle", "stop_task":
		writeJSON(map[string]any{
			"type": "control_response",
			"response": map[string]any{
				"subtype":    "success",
				"request_id": requestID,
				"response": map[string]any{
					"ack":     subtype,
					"request": request,
				},
			},
		})
	case "mcp_status":
		writeJSON(map[string]any{
			"type": "control_response",
			"response": map[string]any{
				"subtype":    "success",
				"request_id": requestID,
				"response": map[string]any{
					"mcpServers": []map[string]any{
						{
							"name":   s.mcpServerName,
							"status": "connected",
							"serverInfo": map[string]any{
								"name":    s.mcpServerInfo["name"],
								"version": s.mcpServerInfo["version"],
							},
							"config": map[string]any{
								"type": "sdk",
								"name": s.mcpServerName,
							},
							"tools": []map[string]any{
								{
									"name":        "echo",
									"description": "Echo tool",
									"annotations": map[string]any{"readOnly": true},
								},
							},
						},
						{
							"name":   "oauth-server",
							"status": "needs-auth",
							"config": map[string]any{
								"type": "http",
								"url":  "https://example.test/mcp",
							},
						},
					},
				},
			},
		})
	default:
		writeJSON(map[string]any{
			"type": "control_response",
			"response": map[string]any{
				"subtype":    "error",
				"request_id": requestID,
				"error":      "unsupported control request",
			},
		})
	}
}

func (s *state) handleControlResponse(payload map[string]any) {
	response, _ := payload["response"].(map[string]any)
	subtype, _ := response["subtype"].(string)
	if subtype == "error" {
		writeJSON(assistantPayload("control-error:"+stringValue(response["error"]), ""))
		writeJSON(resultPayload())
		s.pending = ""
		return
	}

	body, _ := response["response"].(map[string]any)
	switch s.pending {
	case "permission":
		behavior := stringValue(body["behavior"])
		text := "permission:" + behavior
		if updatedInput, ok := body["updatedInput"].(map[string]any); ok {
			if value, ok := updatedInput["approved_path"].(string); ok {
				text += ":" + value
			}
		}
		if updates, ok := body["updatedPermissions"].([]any); ok {
			text += fmt.Sprintf(":%d", len(updates))
		} else {
			text += ":0"
		}
		if message, ok := body["message"].(string); ok && message != "" {
			text += ":" + message
		}
		if interrupt, ok := body["interrupt"].(bool); ok && interrupt {
			text += ":interrupt"
		}
		writeJSON(assistantPayload(text, ""))
		writeJSON(resultPayload())
	case "hook":
		text := "hook"
		if value, ok := body["continue"].(bool); ok {
			text += fmt.Sprintf(":continue=%t", value)
		}
		if value, ok := body["decision"].(string); ok {
			text += ":decision=" + value
		}
		if value, ok := body["systemMessage"].(string); ok {
			text += ":system=" + value
		}
		if output, ok := body["hookSpecificOutput"].(map[string]any); ok {
			if value, ok := output["additionalContext"].(string); ok {
				text += ":context=" + value
			}
		}
		writeJSON(assistantPayload(text, ""))
		writeJSON(resultPayload())
	case "mcp":
		mcpResponse, _ := body["mcp_response"].(map[string]any)
		writeJSON(assistantPayload(s.summarizeMCPResponse(mcpResponse), ""))
		writeJSON(resultPayload())
	}
	s.pending = ""
}

func (s *state) handleUser(payload map[string]any) {
	message, _ := payload["message"].(map[string]any)
	prompt := stringValue(message["content"])

	switch s.mode {
	case "happy":
		writeJSON(assistantPayload("Echo: "+prompt, ""))
		writeJSON(resultPayload())
	case "auth":
		writeJSON(assistantPayload("Invalid credentials", "authentication_failed"))
		writeJSON(resultPayload())
	case "permission_allow", "permission_deny":
		s.pending = "permission"
		writeJSON(map[string]any{
			"type":       "control_request",
			"request_id": "tool-request-1",
			"request": map[string]any{
				"subtype":   "can_use_tool",
				"tool_name": "Write",
				"input": map[string]any{
					"path":   "draft.txt",
					"prompt": prompt,
				},
				"permission_suggestions": []map[string]any{
					{
						"type":        "addRules",
						"destination": "session",
						"behavior":    "allow",
						"rules": []map[string]any{
							{"toolName": "Write", "ruleContent": "*.txt"},
						},
					},
				},
			},
		})
	case "hook":
		s.pending = "hook"
		writeJSON(map[string]any{
			"type":       "control_request",
			"request_id": "hook-request-1",
			"request": map[string]any{
				"subtype":     "hook_callback",
				"callback_id": s.hookCallbackID,
				"tool_use_id": "toolu_123",
				"input": map[string]any{
					"hook_event_name": "PreToolUse",
					"tool_name":       "Write",
					"tool_input": map[string]any{
						"prompt": prompt,
					},
				},
			},
		})
	case "mcp":
		s.pending = "mcp"
		writeJSON(map[string]any{
			"type":       "control_request",
			"request_id": "mcp-request-1",
			"request": map[string]any{
				"subtype":     "mcp_message",
				"server_name": s.mcpServerName,
				"message":     s.mcpMessageForPrompt(prompt),
			},
		})
	}
}

func (s *state) summarizeMCPResponse(response map[string]any) string {
	if response == nil {
		return "mcp:nil"
	}
	if errorMap, ok := response["error"].(map[string]any); ok {
		return fmt.Sprintf("mcp-error:%v:%s", errorMap["code"], stringValue(errorMap["message"]))
	}
	result, _ := response["result"].(map[string]any)
	if result == nil {
		return "mcp:empty"
	}
	if serverInfo, ok := result["serverInfo"].(map[string]any); ok {
		return fmt.Sprintf("mcp-init:%s:%s", stringValue(serverInfo["name"]), stringValue(serverInfo["version"]))
	}
	if tools, ok := result["tools"].([]any); ok && len(tools) > 0 {
		first, _ := tools[0].(map[string]any)
		readOnly := false
		if annotations, ok := first["annotations"].(map[string]any); ok {
			readOnly, _ = annotations["readOnly"].(bool)
		}
		return fmt.Sprintf("mcp-list:%s:%t", stringValue(first["name"]), readOnly)
	}
	if content, ok := result["content"].([]any); ok && len(content) > 0 {
		first, _ := content[0].(map[string]any)
		return fmt.Sprintf("mcp-call:%s:%t", stringValue(first["text"]), boolValue(result["isError"]))
	}
	return "mcp:ok"
}

func (s *state) mcpMessageForPrompt(prompt string) map[string]any {
	switch prompt {
	case "initialize":
		return map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize"}
	case "tools/list":
		return map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"}
	case "tools/call":
		return map[string]any{
			"jsonrpc": "2.0",
			"id":      3,
			"method":  "tools/call",
			"params": map[string]any{
				"name":      "echo",
				"arguments": map[string]any{"text": "from-client"},
			},
		}
	default:
		return map[string]any{"jsonrpc": "2.0", "id": 4, "method": prompt}
	}
}

func (s *state) parseArgs(args []string) {
	for index := 0; index < len(args); index++ {
		if args[index] != "--mcp-config" || index+1 >= len(args) {
			continue
		}
		index++
		var payload map[string]any
		if err := json.Unmarshal([]byte(args[index]), &payload); err != nil {
			continue
		}
		servers, _ := payload["mcpServers"].(map[string]any)
		for name, raw := range servers {
			config, _ := raw.(map[string]any)
			if stringValue(config["type"]) == "sdk" {
				s.mcpServerName = name
				if serverName := stringValue(config["name"]); serverName != "" {
					s.mcpServerInfo["name"] = serverName
				}
				break
			}
		}
	}
}

func assistantPayload(text string, assistantErr string) map[string]any {
	payload := map[string]any{
		"type":       "assistant",
		"session_id": "session-1",
		"uuid":       "uuid-assistant-1",
		"message": map[string]any{
			"id":    "msg-1",
			"model": "fake-claude",
			"content": []map[string]any{
				{"type": "text", "text": text},
			},
			"stop_reason": "end_turn",
		},
	}
	if assistantErr != "" {
		payload["error"] = assistantErr
	}
	return payload
}

func resultPayload() map[string]any {
	return map[string]any{
		"type":            "result",
		"subtype":         "success",
		"duration_ms":     5,
		"duration_api_ms": 4,
		"is_error":        false,
		"num_turns":       1,
		"session_id":      "session-1",
		"result":          "done",
	}
}

func writeJSON(payload map[string]any) {
	encoded, _ := json.Marshal(payload)
	fmt.Fprintln(os.Stdout, string(encoded))
}

func stringValue(raw any) string {
	value, _ := raw.(string)
	return value
}

func boolValue(raw any) bool {
	value, _ := raw.(bool)
	return value
}

func getenvDefault(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
