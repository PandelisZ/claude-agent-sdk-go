package claudeagentsdk_test

import (
	"context"
	"fmt"
	"os"

	claudeagentsdk "github.com/PandelisZ/claude-agent-sdk-go/sdk-go"
)

func ExampleQuery() {
	ctx := context.Background()
	maxTurns := 1

	messages, err := claudeagentsdk.Query(ctx, "Summarize the latest change", claudeagentsdk.ClaudeAgentOptions{
		MaxTurns: &maxTurns,
	})
	if err != nil {
		panic(err)
	}

	for _, message := range messages {
		switch typed := message.(type) {
		case *claudeagentsdk.AssistantMessage:
			printTextBlocks(typed.Content)
		case *claudeagentsdk.ResultMessage:
			fmt.Printf("session=%s turns=%d\n", typed.SessionID, typed.NumTurns)
		}
	}
}

func ExampleQueryWithCallback_authenticationHandling() {
	ctx := context.Background()
	options := claudeagentsdk.ClaudeAgentOptions{
		Env: map[string]string{},
	}
	if configDir := os.Getenv("CLAUDE_CONFIG_DIR"); configDir != "" {
		options.Env["CLAUDE_CONFIG_DIR"] = configDir
	}

	err := claudeagentsdk.QueryWithCallback(ctx, "Hello", options, func(message claudeagentsdk.Message) error {
		if assistant, ok := message.(*claudeagentsdk.AssistantMessage); ok {
			if assistant.Error != nil && *assistant.Error == claudeagentsdk.AssistantMessageErrorAuthenticationFailed {
				return fmt.Errorf("local claude CLI is not authenticated; run `claude login`")
			}
		}
		return nil
	})
	if err != nil {
		panic(err)
	}
}

func ExampleNewClient() {
	ctx := context.Background()
	client := claudeagentsdk.NewClient(claudeagentsdk.ClientOptions{})
	if err := client.Connect(ctx); err != nil {
		panic(err)
	}
	defer client.Close()

	if err := client.SetPermissionMode(ctx, claudeagentsdk.PermissionModeAcceptEdits); err != nil {
		panic(err)
	}
	if err := client.Query(ctx, "Inspect the repository and explain the next change"); err != nil {
		panic(err)
	}

	for {
		message, err := client.Receive(ctx)
		if err != nil {
			panic(err)
		}
		if assistant, ok := message.(*claudeagentsdk.AssistantMessage); ok {
			printTextBlocks(assistant.Content)
		}
		if _, ok := message.(*claudeagentsdk.ResultMessage); ok {
			break
		}
	}
}

func ExampleClient_controlCalls() {
	ctx := context.Background()
	client := claudeagentsdk.NewClient(claudeagentsdk.ClientOptions{})
	if err := client.Connect(ctx); err != nil {
		panic(err)
	}
	defer client.Close()

	model := "sonnet"
	if err := client.SetModel(ctx, &model); err != nil {
		panic(err)
	}
	if err := client.SetPermissionMode(ctx, claudeagentsdk.PermissionModePlan); err != nil {
		panic(err)
	}

	status, err := client.MCPStatus(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Printf("mcpServers=%d\n", len(status.MCPServers))
}

func ExampleListSessions() {
	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	sessions, err := claudeagentsdk.ListSessions(claudeagentsdk.ListSessionsOptions{
		Directory: cwd,
		Limit:     5,
	})
	if err != nil {
		panic(err)
	}
	if len(sessions) == 0 {
		return
	}

	sessionID := sessions[0].SessionID
	info, err := claudeagentsdk.GetSessionInfo(sessionID, claudeagentsdk.SessionQueryOptions{Directory: cwd})
	if err != nil {
		panic(err)
	}
	messages, err := claudeagentsdk.GetSessionMessages(sessionID, claudeagentsdk.SessionQueryOptions{
		Directory: cwd,
		Limit:     20,
	})
	if err != nil {
		panic(err)
	}

	fmt.Printf("session=%s summary=%s messages=%d\n", info.SessionID, info.Summary, len(messages))
}

func ExampleRenameSession() {
	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	sessions, err := claudeagentsdk.ListSessions(claudeagentsdk.ListSessionsOptions{
		Directory: cwd,
		Limit:     1,
	})
	if err != nil {
		panic(err)
	}
	if len(sessions) == 0 {
		return
	}

	sessionID := sessions[0].SessionID
	if err := claudeagentsdk.RenameSession(sessionID, "Repository triage", claudeagentsdk.SessionMutationOptions{
		Directory: cwd,
	}); err != nil {
		panic(err)
	}
	tag := "important"
	if err := claudeagentsdk.TagSession(sessionID, &tag, claudeagentsdk.SessionMutationOptions{
		Directory: cwd,
	}); err != nil {
		panic(err)
	}
}

func printTextBlocks(blocks []claudeagentsdk.ContentBlock) {
	for _, block := range blocks {
		if text, ok := block.(claudeagentsdk.TextBlock); ok {
			fmt.Println(text.Text)
		}
	}
}
