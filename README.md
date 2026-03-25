# Claude Agent SDK for Go

This repository is a fork of the upstream Python Claude Agent SDK repository and adds a Go SDK for Claude Agent.

- Go SDK docs: [sdk-go/README.md](sdk-go/README.md)
- Upstream Python SDK docs: [Claude Agent SDK documentation](https://platform.claude.com/docs/en/agent-sdk/python)

## Installation

```bash
go get github.com/anthropics/claude-agent-sdk-python/sdk-go
```

**Prerequisites:**

- Go 1.21+
- A local Claude Code CLI installation
- An authenticated Claude CLI session

Unlike the upstream Python SDK, the Go SDK does not bundle the Claude CLI or provide an SDK-level login flow. It uses your local `claude` installation exactly as configured on your machine.

## Quick Start

Use `Query` for buffered one-shot requests and `QueryWithCallback` when you want streaming callbacks.

```go
package main

import (
	"context"
	"fmt"
	"log"

	claudeagentsdk "github.com/anthropics/claude-agent-sdk-python/sdk-go"
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
- `sdk-go/examples/sessions/`

For full usage details, typed errors, interactive client controls, and session APIs, see [sdk-go/README.md](sdk-go/README.md).

## Development

The development and release notes below are mostly inherited from the upstream Python SDK repository and are primarily relevant if you're working on the Python packaging side of this fork.

If you're contributing to this project, run the initial setup script to install git hooks:

```bash
./scripts/initial-setup.sh
```

This installs a pre-push hook that runs lint checks before pushing, matching the CI workflow. To skip the hook temporarily, use `git push --no-verify`.

### Building Wheels Locally

To build wheels with the bundled Claude Code CLI:

```bash
# Install build dependencies
pip install build twine

# Build wheel with bundled CLI
python scripts/build_wheel.py

# Build with specific version
python scripts/build_wheel.py --version 0.1.4

# Build with specific CLI version
python scripts/build_wheel.py --cli-version 2.0.0

# Clean bundled CLI after building
python scripts/build_wheel.py --clean

# Skip CLI download (use existing)
python scripts/build_wheel.py --skip-download
```

The build script:

1. Downloads Claude Code CLI for your platform
2. Bundles it in the wheel
3. Builds both wheel and source distribution
4. Checks the package with twine

See `python scripts/build_wheel.py --help` for all options.

### Release Workflow

The package is published to PyPI via the GitHub Actions workflow in `.github/workflows/publish.yml`. To create a new release:

1. **Trigger the workflow** manually from the Actions tab with two inputs:
   - `version`: The package version to publish (e.g., `0.1.5`)
   - `claude_code_version`: The Claude Code CLI version to bundle (e.g., `2.0.0` or `latest`)

2. **The workflow will**:
   - Build platform-specific wheels for macOS, Linux, and Windows
   - Bundle the specified Claude Code CLI version in each wheel
   - Build a source distribution
   - Publish all artifacts to PyPI
   - Create a release branch with version updates
   - Open a PR to main with:
     - Updated `pyproject.toml` version
     - Updated `src/claude_agent_sdk/_version.py`
     - Updated `src/claude_agent_sdk/_cli_version.py` with bundled CLI version
     - Auto-generated `CHANGELOG.md` entry

3. **Review and merge** the release PR to update main with the new version information

The workflow tracks both the package version and the bundled CLI version separately, allowing you to release a new package version with an updated CLI without code changes.

## License and terms

Use of this SDK is governed by Anthropic's [Commercial Terms of Service](https://www.anthropic.com/legal/commercial-terms), including when you use it to power products and services that you make available to your own customers and end users, except to the extent a specific component or dependency is covered by a different license as indicated in that component's LICENSE file.
