package claudeagentsdk

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCLINotFoundErrorFormattingAndIs(t *testing.T) {
	err := NewCLINotFoundError("Claude Code not found", "/custom/claude")

	if got := err.Error(); got != "Claude Code not found: /custom/claude" {
		t.Fatalf("unexpected error string: %q", got)
	}
	if !errors.Is(err, &CLINotFoundError{}) {
		t.Fatalf("expected errors.Is(..., CLINotFoundError)")
	}
	if !errors.Is(err, &CLIConnectionError{}) {
		t.Fatalf("expected CLINotFoundError to match CLIConnectionError")
	}
	if !errors.Is(err, &ClaudeSDKError{}) {
		t.Fatalf("expected CLINotFoundError to match ClaudeSDKError")
	}
}

func TestProcessErrorFormattingAndAs(t *testing.T) {
	exitCode := 7
	err := NewProcessError("process failed", &exitCode, "permission denied")

	if err.ExitCode == nil || *err.ExitCode != 7 {
		t.Fatalf("unexpected exit code: %#v", err.ExitCode)
	}
	if !strings.Contains(err.Error(), "exit code: 7") || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("unexpected error string: %q", err.Error())
	}

	var processErr *ProcessError
	if !errors.As(err, &processErr) {
		t.Fatalf("expected errors.As to extract ProcessError")
	}
}

func TestCLIJSONDecodeErrorPreservesOriginalError(t *testing.T) {
	_, decodeErr := json.Marshal(make(chan int))
	if decodeErr == nil {
		t.Fatalf("expected a marshal error")
	}
	err := NewCLIJSONDecodeError("{bad json}", decodeErr)

	if err.Line != "{bad json}" {
		t.Fatalf("unexpected line: %q", err.Line)
	}
	if !errors.Is(err, &ClaudeSDKError{}) {
		t.Fatalf("expected CLIJSONDecodeError to match ClaudeSDKError")
	}
	if !errors.Is(err, decodeErr) {
		t.Fatalf("expected CLIJSONDecodeError to unwrap original error")
	}
	if !strings.Contains(err.Error(), "Failed to decode JSON") {
		t.Fatalf("unexpected error string: %q", err.Error())
	}
}

func TestMessageParseErrorCarriesRawData(t *testing.T) {
	payload := map[string]any{"type": "assistant"}
	err := NewMessageParseError("bad message", payload)

	if err.Data == nil {
		t.Fatalf("expected raw data to be preserved")
	}
	if !errors.Is(err, &ClaudeSDKError{}) {
		t.Fatalf("expected MessageParseError to match ClaudeSDKError")
	}
}
