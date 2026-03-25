// Package claudeagentsdk provides a Go SDK for driving a local Claude Code CLI
// process.
//
// The main entry points are:
//   - Query and QueryWithCallback for one-shot inferencing
//   - Client for long-lived interactive sessions and control calls such as
//     SetPermissionMode, SetModel, Interrupt, and MCPStatus
//   - ListSessions, GetSessionInfo, GetSessionMessages, RenameSession, and
//     TagSession for working with local Claude session data
//
// The SDK shells out to an installed Claude Code CLI. By default it looks for a
// `claude` binary on PATH; set ClaudeAgentOptions.CLIPath to target a specific
// binary. Authentication is owned by the CLI, not the SDK, so make sure the
// local CLI is already logged in. Use ClaudeAgentOptions.Env to pass runtime
// environment such as CLAUDE_CONFIG_DIR.
//
// Query-style authentication failures are surfaced as assistant messages with
// Error == AssistantMessageErrorAuthenticationFailed, while startup and process
// failures are returned as typed Go errors such as CLINotFoundError and
// ProcessError.
//
// See README.md, example_test.go, and the examples/ directory for concise
// end-to-end usage patterns.
package claudeagentsdk
