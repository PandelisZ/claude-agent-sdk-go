package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	claudeagentsdk "github.com/anthropics/claude-agent-sdk-python/sdk-go"
)

func main() {
	ctx := context.Background()
	options := claudeagentsdk.ClaudeAgentOptions{
		Env: map[string]string{},
	}
	if configDir := os.Getenv("CLAUDE_CONFIG_DIR"); configDir != "" {
		options.Env["CLAUDE_CONFIG_DIR"] = configDir
	}

	err := claudeagentsdk.QueryWithCallback(ctx, "Say hello", options, func(message claudeagentsdk.Message) error {
		assistant, ok := message.(*claudeagentsdk.AssistantMessage)
		if !ok || assistant.Error == nil {
			return nil
		}
		if *assistant.Error == claudeagentsdk.AssistantMessageErrorAuthenticationFailed {
			return fmt.Errorf("local claude CLI is not authenticated; run `claude login`")
		}
		return nil
	})
	if err != nil {
		var notFound *claudeagentsdk.CLINotFoundError
		if errors.As(err, &notFound) {
			log.Fatalf("claude CLI not found: %s", notFound)
		}
		log.Fatal(err)
	}
}
