package claudeagentsdk

import (
	"context"
	"io"
)

func queryNeedsControlRuntime(options ClaudeAgentOptions) bool {
	if len(options.Hooks) > 0 || len(options.Agents) > 0 || options.Skills != nil {
		return true
	}
	if options.SystemPromptPreset != nil && options.SystemPromptPreset.ExcludeDynamicSections != nil {
		return true
	}
	for _, config := range options.MCPServers {
		switch config.(type) {
		case SDKMCPServerConfig, *SDKMCPServerConfig:
			return true
		}
	}
	return false
}

func queryWithClientRuntime(ctx context.Context, prompt string, options ClaudeAgentOptions, transport Transport, handler QueryHandler) error {
	client := NewClient(ClientOptions{
		ClaudeAgentOptions: options,
		Transport:          transport,
	})
	if err := client.Connect(ctx); err != nil {
		return err
	}
	defer client.Close()

	if err := client.Query(ctx, prompt); err != nil {
		return err
	}
	for {
		message, err := client.Receive(ctx)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if err := handler(message); err != nil {
			return err
		}
		if _, ok := message.(*ResultMessage); ok {
			return nil
		}
	}
}
