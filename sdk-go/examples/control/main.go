package main

import (
	"context"
	"fmt"
	"log"
	"os"

	claudeagentsdk "github.com/PandelisZ/claude-agent-sdk-go/sdk-go"
)

func main() {
	ctx := context.Background()
	options := claudeagentsdk.ClientOptions{}
	if configDir := os.Getenv("CLAUDE_CONFIG_DIR"); configDir != "" {
		options.Env = map[string]string{
			"CLAUDE_CONFIG_DIR": configDir,
		}
	}

	client := claudeagentsdk.NewClient(options)
	if err := client.Connect(ctx); err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	if err := client.SetPermissionMode(ctx, claudeagentsdk.PermissionModePlan); err != nil {
		log.Fatal(err)
	}

	model := "sonnet"
	if err := client.SetModel(ctx, &model); err != nil {
		log.Fatal(err)
	}

	status, err := client.MCPStatus(ctx)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("permission mode updated; MCP servers=%d\n", len(status.MCPServers))
}
