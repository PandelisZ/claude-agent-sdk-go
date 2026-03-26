# Claude Agent SDK for Go

Go SDK for driving a local Claude Code CLI process.

The module covers the core Go-facing SDK flows:

- one-shot inferencing via `Query` / `QueryWithCallback`
- interactive client sessions via `Client`
- permission callbacks, hooks, and SDK-backed MCP servers for interactive runs
- local session listing, transcript reads, and metadata mutations

## Installation

```bash
go get github.com/PandelisZ/claude-agent-sdk-go/sdk-go
```

**Prerequisites**

- Go 1.21+
- A local Claude Code CLI installation
- The CLI already authenticated for the account you want to use

The Go SDK does not bundle the Claude CLI or provide an SDK-level login flow.
It uses the local CLI exactly as installed on your machine.

## CLI and auth expectations

By default the SDK looks for `claude` on `PATH`.

- Set `ClaudeAgentOptions.CLIPath` if you want to pin a specific CLI binary.
- Use `ClaudeAgentOptions.Env` to pass environment such as
  `CLAUDE_CONFIG_DIR`.
- If you use a separate Claude config directory, pass the same
  `CLAUDE_CONFIG_DIR` value to query/client calls and to local session helpers
  so runtime traffic and session inspection point at the same store.
- Session helpers (`ListSessions`, `GetSessionInfo`, `GetSessionMessages`,
  `RenameSession`, `TagSession`) read the local Claude config directory from
  `CLAUDE_CONFIG_DIR` or `~/.claude`.
- `ListSessionsOptions.Directory` filters sessions to a working tree. When
  `Directory` is set, worktrees are included by default unless
  `IncludeWorktrees` is set to `false`.

If the CLI is not installed, startup returns a typed `CLINotFoundError`. If the
CLI starts but exits unexpectedly, you'll get `CLIConnectionError` or
`ProcessError`. Auth failures returned by the CLI remain typed assistant
messages, so you can detect them without string matching both in streaming
callbacks and in buffered `Query` responses.

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
	if assistant, ok := message.(*claudeagentsdk.AssistantMessage); ok {
		if assistant.Error != nil && *assistant.Error == claudeagentsdk.AssistantMessageErrorAuthenticationFailed {
			return fmt.Errorf("claude CLI is not authenticated; run `claude login`")
		}
	}
	return nil
})
if err != nil {
	log.Fatal(err)
}
```

## One-shot inferencing

Use `Query` when you want the complete response buffered in memory, or
`QueryWithCallback` when you want to process messages as they stream from the
CLI. Both APIs shell out to the local Claude CLI and therefore inherit its
authentication, config, and model availability.

```go
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
```

## Interactive client and controls

Use `Client` when you need a long-lived connection, incremental receive loops,
control messages, tool permission callbacks, hooks, or SDK-backed MCP servers.

```go
ctx := context.Background()
client := claudeagentsdk.NewClient(claudeagentsdk.ClientOptions{})
if err := client.Connect(ctx); err != nil {
	log.Fatal(err)
}
defer client.Close()

if err := client.SetPermissionMode(ctx, claudeagentsdk.PermissionModeAcceptEdits); err != nil {
	log.Fatal(err)
}
if err := client.Query(ctx, "Inspect this repository and suggest the next change"); err != nil {
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
```

Control calls are explicit methods on `Client`:

- `SetPermissionMode` updates the CLI permission mode for the live session.
- `SetModel` switches models for later turns.
- `Interrupt` stops the active assistant turn.
- `RewindFiles`, `StopTask`, `ReconnectMCPServer`, `ToggleMCPServer`, and
  `MCPStatus` expose the corresponding local CLI controls.

For in-process MCP tools, see `NewSimpleMCPServer` and `SDKMCPServerConfig`.
For runnable snippets, see:

- `examples/auth/main.go`
- `examples/client/main.go`
- `examples/control/main.go`
- `examples/inference-cli/main.go`
- `examples/query/main.go`
- `examples/sessions/main.go`

## Local session APIs

The session helpers work against the local Claude session store. They do not hit
remote services or require a running client connection.

```go
cwd, err := os.Getwd()
if err != nil {
	log.Fatal(err)
}

sessions, err := claudeagentsdk.ListSessions(claudeagentsdk.ListSessionsOptions{
	Directory: cwd,
	Limit:     10,
})
if err != nil {
	log.Fatal(err)
}
if len(sessions) == 0 {
	return
}

sessionID := sessions[0].SessionID
info, err := claudeagentsdk.GetSessionInfo(sessionID, claudeagentsdk.SessionQueryOptions{
	Directory: cwd,
})
if err != nil {
	log.Fatal(err)
}
messages, err := claudeagentsdk.GetSessionMessages(sessionID, claudeagentsdk.SessionQueryOptions{
	Directory: cwd,
	Limit:     20,
})
if err != nil {
	log.Fatal(err)
}

fmt.Printf("session=%s summary=%s messages=%d\n", info.SessionID, info.Summary, len(messages))
```

Use `RenameSession` and `TagSession` for small local metadata edits:

```go
if err := claudeagentsdk.RenameSession(sessionID, "Repository triage", claudeagentsdk.SessionMutationOptions{
	Directory: cwd,
}); err != nil {
	log.Fatal(err)
}
tag := "important"
if err := claudeagentsdk.TagSession(sessionID, &tag, claudeagentsdk.SessionMutationOptions{
	Directory: cwd,
}); err != nil {
	log.Fatal(err)
}
```

## Current coverage

The Go SDK currently covers:

- public options, message/content types, and typed SDK errors
- one-shot query/inferencing against the local Claude CLI
- interactive client sessions, control calls, hooks, permission callbacks, and
  SDK-backed MCP servers
- local session discovery, transcript reconstruction, and metadata mutation

## Current limitations

- no bundled Claude CLI; the Go SDK expects a local CLI installation
- no SDK-managed auth/login helper; auth is whatever the local CLI is already
  configured for
- synchronous/blocking API shape
- the Go package docs and `examples/` directory focus on the main query,
  auth/environment, interactive control, and local session flows rather than
  covering every helper surface
