# Claude Agent SDK for Go

Go SDK for driving a local Claude Code CLI process.

- Go SDK docs: [sdk-go/README.md](sdk-go/README.md)

## Installation

```bash
go get github.com/PandelisZ/claude-agent-sdk-go
```

**Prerequisites:**

- Go 1.21+
- A local Claude Code CLI installation
- An authenticated Claude CLI session

The Go SDK does not bundle the Claude CLI or provide an SDK-level login flow.
It uses your local `claude` installation exactly as configured on your machine.

## Quick Start

Use `Query` for buffered one-shot requests and `QueryWithCallback` when you want streaming callbacks.

```go
package main

import (
	"context"
	"fmt"
	"log"

	claudeagentsdk "github.com/PandelisZ/claude-agent-sdk-go"
)

func main() {
	ctx := context.Background()
	maxTurns := 1

	messages, err := claudeagentsdk.Query(ctx, "Summarize this repository", claudeagentsdk.ClaudeAgentOptions{
		MaxTurns: &maxTurns,
	})
	if err != nil {
		log.Fatal(err)
	}

	for _, message := range messages {
		if assistant, ok := message.(*claudeagentsdk.AssistantMessage); ok {
			for _, block := range assistant.Content {
				if text, ok := block.(claudeagentsdk.TextBlock); ok {
					fmt.Println(text.Text)
				}
			}
		}
	}
}
```

## CLI configuration

By default, the SDK looks for `claude` on your `PATH`.

- Set `ClaudeAgentOptions.CLIPath` to pin a specific Claude CLI binary.
- Set `ClaudeAgentOptions.Env` to pass environment such as `CLAUDE_CONFIG_DIR`.
- Use the same `CLAUDE_CONFIG_DIR` for both runtime calls and local session helpers if you want them to point at the same local Claude state.

```go
ctx := context.Background()
cliPath := "/usr/local/bin/claude"
configDir := os.Getenv("CLAUDE_CONFIG_DIR")

err := claudeagentsdk.QueryWithCallback(ctx, "Hello", claudeagentsdk.ClaudeAgentOptions{
	CLIPath: &cliPath,
	Env: map[string]string{
		"CLAUDE_CONFIG_DIR": configDir,
	},
}, func(message claudeagentsdk.Message) error {
	return nil
})
if err != nil {
	log.Fatal(err)
}
```

## What the Go SDK covers

- one-shot inferencing with `Query` and `QueryWithCallback`
- interactive sessions with `Client`
- permission callbacks, hooks, and SDK-backed MCP servers
- local session listing, transcript reads, and metadata updates

Runnable examples live in [sdk-go/examples/](sdk-go/examples/):

- `sdk-go/examples/query/`
- `sdk-go/examples/client/`
- `sdk-go/examples/control/`
- `sdk-go/examples/auth/`
- `sdk-go/examples/inference-cli/`
- `sdk-go/examples/sessions/`

For full usage details, typed errors, interactive client controls, and session APIs, see [sdk-go/README.md](sdk-go/README.md).

## Development

The Go SDK lives under `sdk-go/`.

Useful commands:

```bash
cd sdk-go && go test ./...
```

For runnable examples, see:

- `sdk-go/examples/query/`
- `sdk-go/examples/client/`
- `sdk-go/examples/control/`
- `sdk-go/examples/auth/`
- `sdk-go/examples/inference-cli/`
- `sdk-go/examples/sessions/`

## License and terms

Use of this SDK is governed by Anthropic's [Commercial Terms of Service](https://www.anthropic.com/legal/commercial-terms), including when you use it to power products and services that you make available to your own customers and end users, except to the extent a specific component or dependency is covered by a different license as indicated in that component's LICENSE file.
