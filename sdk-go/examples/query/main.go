package main

import (
	"context"
	"fmt"
	"log"

	claudeagentsdk "github.com/PandelisZ/claude-agent-sdk-go/sdk-go"
)

func main() {
	ctx := context.Background()
	maxTurns := 1

	messages, err := claudeagentsdk.Query(ctx, "Summarize the current repository", claudeagentsdk.ClaudeAgentOptions{
		MaxTurns: &maxTurns,
	})
	if err != nil {
		log.Fatal(err)
	}

	for _, message := range messages {
		switch typed := message.(type) {
		case *claudeagentsdk.AssistantMessage:
			for _, block := range typed.Content {
				if text, ok := block.(claudeagentsdk.TextBlock); ok {
					fmt.Println(text.Text)
				}
			}
		case *claudeagentsdk.ResultMessage:
			fmt.Printf("session=%s turns=%d error=%v\n", typed.SessionID, typed.NumTurns, typed.IsError)
		}
	}
}
