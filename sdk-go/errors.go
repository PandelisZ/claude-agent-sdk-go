package claudeagentsdk

import "fmt"

// ClaudeSDKError is the base error type for SDK-specific failures.
type ClaudeSDKError struct {
	Message string
}

func (e *ClaudeSDKError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *ClaudeSDKError) Is(target error) bool {
	_, ok := target.(*ClaudeSDKError)
	return ok
}

// CLIConnectionError reports failures when the SDK cannot connect to the CLI.
type CLIConnectionError struct {
	Message string
}

func NewCLIConnectionError(message string) *CLIConnectionError {
	return &CLIConnectionError{Message: message}
}

func (e *CLIConnectionError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *CLIConnectionError) Is(target error) bool {
	switch target.(type) {
	case *CLIConnectionError, *ClaudeSDKError:
		return true
	default:
		return false
	}
}

// CLINotFoundError reports that the Claude CLI could not be located.
type CLINotFoundError struct {
	Message string
	CLIPath string
}

func NewCLINotFoundError(message string, cliPath string) *CLINotFoundError {
	if message == "" {
		message = "Claude Code not found"
	}
	return &CLINotFoundError{
		Message: message,
		CLIPath: cliPath,
	}
}

func (e *CLINotFoundError) Error() string {
	if e == nil {
		return ""
	}
	if e.CLIPath != "" {
		return fmt.Sprintf("%s: %s", e.Message, e.CLIPath)
	}
	return e.Message
}

func (e *CLINotFoundError) Is(target error) bool {
	switch target.(type) {
	case *CLINotFoundError, *CLIConnectionError, *ClaudeSDKError:
		return true
	default:
		return false
	}
}

// ProcessError reports CLI process failures.
type ProcessError struct {
	Message  string
	ExitCode *int
	Stderr   string
}

func NewProcessError(message string, exitCode *int, stderr string) *ProcessError {
	return &ProcessError{
		Message:  message,
		ExitCode: exitCode,
		Stderr:   stderr,
	}
}

func (e *ProcessError) Error() string {
	if e == nil {
		return ""
	}
	message := e.Message
	if e.ExitCode != nil {
		message = fmt.Sprintf("%s (exit code: %d)", message, *e.ExitCode)
	}
	if e.Stderr != "" {
		message = fmt.Sprintf("%s\nError output: %s", message, e.Stderr)
	}
	return message
}

func (e *ProcessError) Is(target error) bool {
	switch target.(type) {
	case *ProcessError, *ClaudeSDKError:
		return true
	default:
		return false
	}
}

// CLIJSONDecodeError reports malformed JSON from CLI stdout.
type CLIJSONDecodeError struct {
	Line          string
	OriginalError error
}

func NewCLIJSONDecodeError(line string, originalErr error) *CLIJSONDecodeError {
	return &CLIJSONDecodeError{
		Line:          line,
		OriginalError: originalErr,
	}
}

func (e *CLIJSONDecodeError) Error() string {
	if e == nil {
		return ""
	}
	line := e.Line
	if len(line) > 100 {
		line = line[:100]
	}
	return fmt.Sprintf("Failed to decode JSON: %s...", line)
}

func (e *CLIJSONDecodeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.OriginalError
}

func (e *CLIJSONDecodeError) Is(target error) bool {
	switch target.(type) {
	case *CLIJSONDecodeError, *ClaudeSDKError:
		return true
	default:
		return false
	}
}

// MessageParseError reports malformed, known CLI payloads.
type MessageParseError struct {
	Message string
	Data    any
}

func NewMessageParseError(message string, data any) *MessageParseError {
	return &MessageParseError{
		Message: message,
		Data:    data,
	}
}

func (e *MessageParseError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *MessageParseError) Is(target error) bool {
	switch target.(type) {
	case *MessageParseError, *ClaudeSDKError:
		return true
	default:
		return false
	}
}
