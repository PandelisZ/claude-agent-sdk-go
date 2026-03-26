package claudeagentsdk

import (
	"context"
	"fmt"

	"github.com/PandelisZ/claude-agent-sdk-go/sdk-go/internal/protocol"
)

type SDKMCPServer interface {
	HandleMCPMessage(context.Context, map[string]any) (map[string]any, error)
}

type SDKMCPServerConfig struct {
	Type     string       `json:"type"`
	Name     string       `json:"name"`
	Instance SDKMCPServer `json:"-"`
}

func (c SDKMCPServerConfig) mcpServerConfigType() string {
	if c.Type != "" {
		return c.Type
	}
	return "sdk"
}

type MCPTool struct {
	Name        string
	Description *string
	InputSchema map[string]any
	Annotations *MCPToolAnnotations
	Handler     func(context.Context, map[string]any) (MCPToolResult, error)
}

type MCPToolResult struct {
	Content []MCPContent
	IsError bool
}

type MCPContent interface {
	mcpContentPayload() map[string]any
}

type MCPTextContent struct {
	Text string
}

func (c MCPTextContent) mcpContentPayload() map[string]any {
	return map[string]any{
		"type": "text",
		"text": c.Text,
	}
}

type MCPRawContent map[string]any

func (c MCPRawContent) mcpContentPayload() map[string]any {
	payload := make(map[string]any, len(c))
	for key, value := range c {
		payload[key] = value
	}
	return payload
}

type SimpleMCPServer struct {
	info  MCPServerInfo
	tools map[string]MCPTool
}

func NewSimpleMCPServer(info MCPServerInfo, tools []MCPTool) *SimpleMCPServer {
	index := make(map[string]MCPTool, len(tools))
	for _, tool := range tools {
		index[tool.Name] = tool
	}
	return &SimpleMCPServer{
		info:  info,
		tools: index,
	}
}

func (s *SimpleMCPServer) HandleMCPMessage(ctx context.Context, message map[string]any) (map[string]any, error) {
	if s == nil {
		return jsonRPCError(message, -32603, "server is nil"), nil
	}

	method, _ := protocol.StringValue(message, "method")
	params, _ := protocol.MapValue(message, "params")

	switch method {
	case "initialize":
		return map[string]any{
			"jsonrpc": "2.0",
			"id":      message["id"],
			"result": map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]any{
					"tools": map[string]any{},
				},
				"serverInfo": map[string]any{
					"name":    s.info.Name,
					"version": s.info.Version,
				},
			},
		}, nil
	case "tools/list":
		tools := make([]map[string]any, 0, len(s.tools))
		for _, tool := range s.tools {
			item := map[string]any{
				"name":        tool.Name,
				"inputSchema": cloneAnyMap(tool.InputSchema),
			}
			if item["inputSchema"] == nil {
				item["inputSchema"] = map[string]any{}
			}
			if tool.Description != nil {
				item["description"] = *tool.Description
			}
			if tool.Annotations != nil {
				annotations := map[string]any{}
				if tool.Annotations.ReadOnly {
					annotations["readOnly"] = true
				}
				if tool.Annotations.Destructive {
					annotations["destructive"] = true
				}
				if tool.Annotations.OpenWorld {
					annotations["openWorld"] = true
				}
				if len(annotations) > 0 {
					item["annotations"] = annotations
				}
			}
			tools = append(tools, item)
		}
		return map[string]any{
			"jsonrpc": "2.0",
			"id":      message["id"],
			"result":  map[string]any{"tools": tools},
		}, nil
	case "tools/call":
		toolName, _ := protocol.StringValue(params, "name")
		arguments, _ := protocol.MapValue(params, "arguments")
		tool, ok := s.tools[toolName]
		if !ok {
			return jsonRPCError(message, -32601, fmt.Sprintf("Tool '%s' not found", toolName)), nil
		}
		if tool.Handler == nil {
			return jsonRPCError(message, -32603, fmt.Sprintf("Tool '%s' is not callable", toolName)), nil
		}
		result, err := tool.Handler(ctx, cloneAnyMap(arguments))
		if err != nil {
			return jsonRPCError(message, -32603, err.Error()), nil
		}
		content := make([]map[string]any, 0, len(result.Content))
		for _, item := range result.Content {
			content = append(content, item.mcpContentPayload())
		}
		payload := map[string]any{"content": content}
		if result.IsError {
			payload["isError"] = true
		}
		return map[string]any{
			"jsonrpc": "2.0",
			"id":      message["id"],
			"result":  payload,
		}, nil
	case "notifications/initialized":
		return map[string]any{
			"jsonrpc": "2.0",
			"result":  map[string]any{},
		}, nil
	default:
		return jsonRPCError(message, -32601, fmt.Sprintf("Method '%s' not found", method)), nil
	}
}

func ParseMCPStatusResponse(raw map[string]any) (MCPStatusResponse, error) {
	itemsRaw, ok := protocol.SliceValue(raw, "mcpServers")
	if !ok {
		return MCPStatusResponse{}, NewMessageParseError("missing required field in mcp status response: 'mcpServers'", raw)
	}

	servers := make([]MCPServerStatus, 0, len(itemsRaw))
	for _, item := range itemsRaw {
		payload, ok := item.(map[string]any)
		if !ok {
			return MCPStatusResponse{}, NewMessageParseError("invalid mcp status entry", raw)
		}
		name, err := protocol.RequireString(payload, "name")
		if err != nil {
			return MCPStatusResponse{}, NewMessageParseError("missing required field in mcp status entry: 'name'", payload)
		}
		statusValue, err := protocol.RequireString(payload, "status")
		if err != nil {
			return MCPStatusResponse{}, NewMessageParseError("missing required field in mcp status entry: 'status'", payload)
		}

		status := MCPServerStatus{
			Name:   name,
			Status: MCPServerConnectionStatus(statusValue),
		}
		if info, ok := protocol.MapValue(payload, "serverInfo"); ok {
			serverName, _ := protocol.StringValue(info, "name")
			version, _ := protocol.StringValue(info, "version")
			status.ServerInfo = &MCPServerInfo{Name: serverName, Version: version}
		}
		if value, ok := protocol.StringValue(payload, "error"); ok {
			status.Error = &value
		}
		if value, ok := protocol.StringValue(payload, "scope"); ok {
			status.Scope = &value
		}
		if config, ok := protocol.MapValue(payload, "config"); ok {
			status.Config = parseMCPStatusConfig(config)
		}
		if toolsRaw, ok := protocol.SliceValue(payload, "tools"); ok {
			tools := make([]MCPToolInfo, 0, len(toolsRaw))
			for _, rawTool := range toolsRaw {
				toolMap, ok := rawTool.(map[string]any)
				if !ok {
					continue
				}
				toolName, _ := protocol.StringValue(toolMap, "name")
				toolInfo := MCPToolInfo{Name: toolName}
				if description, ok := protocol.StringValue(toolMap, "description"); ok {
					toolInfo.Description = &description
				}
				if annotations, ok := protocol.MapValue(toolMap, "annotations"); ok {
					info := MCPToolAnnotations{}
					if value, ok := protocol.BoolValue(annotations, "readOnly"); ok && value != nil {
						info.ReadOnly = *value
					}
					if value, ok := protocol.BoolValue(annotations, "destructive"); ok && value != nil {
						info.Destructive = *value
					}
					if value, ok := protocol.BoolValue(annotations, "openWorld"); ok && value != nil {
						info.OpenWorld = *value
					}
					toolInfo.Annotations = &info
				}
				tools = append(tools, toolInfo)
			}
			status.Tools = tools
		}
		servers = append(servers, status)
	}

	return MCPStatusResponse{MCPServers: servers}, nil
}

func parseMCPStatusConfig(config map[string]any) MCPServerStatusConfig {
	configType, _ := protocol.StringValue(config, "type")
	switch configType {
	case "sdk":
		name, _ := protocol.StringValue(config, "name")
		return MCPSDKServerConfigStatus{Type: configType, Name: name}
	case "claudeai-proxy":
		url, _ := protocol.StringValue(config, "url")
		id, _ := protocol.StringValue(config, "id")
		return MCPClaudeAIProxyServerConfig{Type: configType, URL: url, ID: id}
	case "sse":
		url, _ := protocol.StringValue(config, "url")
		return MCPSSEServerConfig{Type: configType, URL: url, Headers: parseStringMap(config["headers"])}
	case "http":
		url, _ := protocol.StringValue(config, "url")
		return MCPHTTPServerConfig{Type: configType, URL: url, Headers: parseStringMap(config["headers"])}
	default:
		command, hasCommand := protocol.StringValue(config, "command")
		if hasCommand || configType == "stdio" || configType == "" {
			return MCPStdioServerConfig{
				Type:    configType,
				Command: command,
				Args:    parseStringSlice(config["args"]),
				Env:     parseStringMap(config["env"]),
			}
		}
	}
	return nil
}

func jsonRPCError(message map[string]any, code int, text string) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      message["id"],
		"error": map[string]any{
			"code":    code,
			"message": text,
		},
	}
}

func parseStringSlice(raw any) []string {
	items, ok := raw.([]any)
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

func parseStringMap(raw any) map[string]string {
	payload, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	values := make(map[string]string, len(payload))
	for key, value := range payload {
		text, ok := value.(string)
		if ok {
			values[key] = text
		}
	}
	return values
}
