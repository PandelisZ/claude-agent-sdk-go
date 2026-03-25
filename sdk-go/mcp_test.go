package claudeagentsdk

import (
	"context"
	"testing"
)

func TestSimpleMCPServerHandlesInitializeListCallAndUnknownMethod(t *testing.T) {
	description := "Echo tool"
	server := NewSimpleMCPServer(MCPServerInfo{Name: "sdk-test", Version: "1.2.3"}, []MCPTool{
		{
			Name:        "echo",
			Description: &description,
			Annotations: &MCPToolAnnotations{ReadOnly: true},
			InputSchema: map[string]any{"type": "object"},
			Handler: func(ctx context.Context, arguments map[string]any) (MCPToolResult, error) {
				return MCPToolResult{
					Content: []MCPContent{
						MCPTextContent{Text: arguments["text"].(string)},
					},
				}, nil
			},
		},
	})

	testCases := []struct {
		name    string
		message map[string]any
		assert  func(*testing.T, map[string]any)
	}{
		{
			name:    "initialize",
			message: map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize"},
			assert: func(t *testing.T, response map[string]any) {
				result := response["result"].(map[string]any)
				serverInfo := result["serverInfo"].(map[string]any)
				if serverInfo["name"] != "sdk-test" || serverInfo["version"] != "1.2.3" {
					t.Fatalf("unexpected initialize response: %#v", response)
				}
			},
		},
		{
			name:    "tools/list",
			message: map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"},
			assert: func(t *testing.T, response map[string]any) {
				result := response["result"].(map[string]any)
				tools := result["tools"].([]map[string]any)
				if tools[0]["name"] != "echo" {
					t.Fatalf("unexpected tools/list response: %#v", response)
				}
				annotations := tools[0]["annotations"].(map[string]any)
				if annotations["readOnly"] != true {
					t.Fatalf("unexpected tool annotations: %#v", response)
				}
			},
		},
		{
			name: "tools/call",
			message: map[string]any{
				"jsonrpc": "2.0",
				"id":      3,
				"method":  "tools/call",
				"params":  map[string]any{"name": "echo", "arguments": map[string]any{"text": "hello"}},
			},
			assert: func(t *testing.T, response map[string]any) {
				result := response["result"].(map[string]any)
				content := result["content"].([]map[string]any)
				if content[0]["text"] != "hello" {
					t.Fatalf("unexpected tools/call response: %#v", response)
				}
			},
		},
		{
			name:    "unknown method",
			message: map[string]any{"jsonrpc": "2.0", "id": 4, "method": "unknown"},
			assert: func(t *testing.T, response map[string]any) {
				err := response["error"].(map[string]any)
				if err["code"] != -32601 {
					t.Fatalf("unexpected error response: %#v", response)
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			response, err := server.HandleMCPMessage(context.Background(), tc.message)
			if err != nil {
				t.Fatalf("HandleMCPMessage returned error: %v", err)
			}
			tc.assert(t, response)
		})
	}
}

func TestParseMCPStatusResponsePreservesNeedsAuthAndSDKConfig(t *testing.T) {
	response, err := ParseMCPStatusResponse(map[string]any{
		"mcpServers": []any{
			map[string]any{
				"name":   "sdk-test",
				"status": "connected",
				"serverInfo": map[string]any{
					"name":    "sdk-test",
					"version": "1.2.3",
				},
				"config": map[string]any{
					"type": "sdk",
					"name": "sdk-test",
				},
				"tools": []any{
					map[string]any{
						"name":        "echo",
						"description": "Echo tool",
						"annotations": map[string]any{"readOnly": true},
					},
				},
			},
			map[string]any{
				"name":   "oauth-server",
				"status": "needs-auth",
				"config": map[string]any{
					"type": "http",
					"url":  "https://example.test/mcp",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("ParseMCPStatusResponse returned error: %v", err)
	}
	if len(response.MCPServers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(response.MCPServers))
	}
	if response.MCPServers[1].Status != MCPServerStatusNeedsAuth {
		t.Fatalf("expected needs-auth status, got %#v", response.MCPServers[1])
	}
	if _, ok := response.MCPServers[0].Config.(MCPSDKServerConfigStatus); !ok {
		t.Fatalf("expected SDK status config, got %#v", response.MCPServers[0].Config)
	}
}
