package main

import (
	"context"
	"fmt"
	"log"

	claudeagentsdk "github.com/PandelisZ/claude-agent-sdk-go"
)

func main() {
	ctx := context.Background()
	client := claudeagentsdk.NewClient(claudeagentsdk.ClientOptions{})
	if err := client.Connect(ctx); err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	if err := client.SetPermissionMode(ctx, claudeagentsdk.PermissionModeAcceptEdits); err != nil {
		log.Fatal(err)
	}
	if status, err := client.MCPStatus(ctx); err == nil {
		fmt.Printf("connected MCP servers: %d\n", len(status.MCPServers))
	}
	if err := client.Query(ctx, "Inspect the repository and summarize the next change"); err != nil {
		log.Fatal(err)
	}

	for {
		message, err := client.Receive(ctx)
		if err != nil {
			log.Fatal(err)
		}

		switch typed := message.(type) {
		case *claudeagentsdk.AssistantMessage:
			for _, block := range typed.Content {
				if text, ok := block.(claudeagentsdk.TextBlock); ok {
					fmt.Println(text.Text)
				}
			}
		case *claudeagentsdk.ResultMessage:
			fmt.Printf("done: session=%s\n", typed.SessionID)
			return
		}
	}
}
