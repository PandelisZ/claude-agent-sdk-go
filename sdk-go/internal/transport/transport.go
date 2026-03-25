package transport

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const (
	defaultCLIName       = "claude"
	defaultEntryPoint    = "sdk-go"
	defaultMaxBufferSize = 1024 * 1024
)

type PermissionMode string
type SdkBeta string
type SettingSource string
type ThinkingConfigType string

const (
	ThinkingConfigAdaptive ThinkingConfigType = "adaptive"
	ThinkingConfigEnabled  ThinkingConfigType = "enabled"
	ThinkingConfigDisabled ThinkingConfigType = "disabled"
)

type ToolsPreset struct {
	Type   string
	Preset string
}

type SystemPromptPreset struct {
	Type   string
	Preset string
	Append *string
}

type SystemPromptFile struct {
	Type string
	Path string
}

type ThinkingConfig struct {
	Type         ThinkingConfigType
	BudgetTokens *int
}

type SDKPluginConfig struct {
	Type string
	Path string
}

type MCPServerConfig interface{}

type MCPStdioServerConfig struct {
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

type MCPSSEServerConfig struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

type MCPHTTPServerConfig struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

type MCPSDKServerConfig struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

type Options struct {
	Tools                    []string
	ToolsPreset              *ToolsPreset
	AllowedTools             []string
	SystemPrompt             *string
	SystemPromptPreset       *SystemPromptPreset
	SystemPromptFile         *SystemPromptFile
	MCPServers               map[string]MCPServerConfig
	PermissionMode           *PermissionMode
	ContinueConversation     bool
	Resume                   *string
	ForkSession              bool
	MaxTurns                 *int
	MaxBudgetUSD             *float64
	DisallowedTools          []string
	Model                    *string
	FallbackModel            *string
	Betas                    []SdkBeta
	PermissionPromptToolName *string
	Cwd                      *string
	CLIPath                  *string
	Settings                 *string
	AddDirs                  []string
	Env                      map[string]string
	ExtraArgs                map[string]*string
	MaxBufferSize            *int
	User                     *string
	IncludePartialMessages   bool
	SettingSources           []SettingSource
	Plugins                  []SDKPluginConfig
	MaxThinkingTokens        *int
	Thinking                 *ThinkingConfig
	Effort                   *string
	OutputFormat             map[string]any
	EnableFileCheckpointing  bool
}

type CLIConnectionError struct {
	Message string
}

func (e *CLIConnectionError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type CLINotFoundError struct {
	Message string
	CLIPath string
}

func (e *CLINotFoundError) Error() string {
	if e == nil {
		return ""
	}
	if e.CLIPath == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Message, e.CLIPath)
}

type ProcessError struct {
	Message  string
	ExitCode *int
	Stderr   string
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

type CLIJSONDecodeError struct {
	Line          string
	OriginalError error
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

// Transport is the reusable runtime-facing transport contract for one-shot
// stream-json execution.
type Transport interface {
	Connect(context.Context) error
	Write(context.Context, []byte) error
	CloseInput() error
	Read(context.Context) ([]byte, error)
	Close() error
}

// SubprocessCLITransport runs the local Claude CLI as a subprocess.
type SubprocessCLITransport struct {
	options       Options
	maxBufferSize int

	cmd         *exec.Cmd
	stdin       io.WriteCloser
	stdout      *bufio.Reader
	stdoutPipe  io.ReadCloser
	stderrPipe  io.ReadCloser
	stderrBuf   bytes.Buffer
	stderrDone  chan struct{}
	waitOnce    sync.Once
	waitErr     error
	closed      bool
	connected   bool
	readDrained bool
}

// NewSubprocessCLITransport builds a subprocess-backed transport.
func NewSubprocessCLITransport(options Options) *SubprocessCLITransport {
	maxBufferSize := defaultMaxBufferSize
	if options.MaxBufferSize != nil && *options.MaxBufferSize > 0 {
		maxBufferSize = *options.MaxBufferSize
	}

	return &SubprocessCLITransport{
		options:       options,
		maxBufferSize: maxBufferSize,
	}
}

// Connect starts the CLI subprocess and wires stdin/stdout/stderr.
func (t *SubprocessCLITransport) Connect(ctx context.Context) error {
	if t.connected {
		return nil
	}

	cliPath, err := t.resolveCLIPath()
	if err != nil {
		return err
	}

	if t.options.Cwd != nil {
		info, statErr := os.Stat(*t.options.Cwd)
		if statErr != nil || !info.IsDir() {
			return &CLIConnectionError{Message: fmt.Sprintf("working directory does not exist: %s", *t.options.Cwd)}
		}
	}

	args, err := t.buildCommandArgs()
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, cliPath, args...)
	if t.options.Cwd != nil {
		cmd.Dir = *t.options.Cwd
	}
	cmd.Env = envMapToSlice(t.buildEnvMap())

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return &CLIConnectionError{Message: fmt.Sprintf("failed to open CLI stdin: %v", err)}
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return &CLIConnectionError{Message: fmt.Sprintf("failed to open CLI stdout: %v", err)}
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return &CLIConnectionError{Message: fmt.Sprintf("failed to open CLI stderr: %v", err)}
	}

	if err := cmd.Start(); err != nil {
		var execErr *exec.Error
		switch {
		case errors.As(err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound):
			return &CLINotFoundError{Message: "Claude Code not found", CLIPath: cliPath}
		case errors.Is(err, exec.ErrNotFound):
			return &CLINotFoundError{Message: "Claude Code not found", CLIPath: cliPath}
		default:
			return &CLIConnectionError{Message: fmt.Sprintf("failed to start Claude Code: %v", err)}
		}
	}

	t.cmd = cmd
	t.stdin = stdin
	t.stdoutPipe = stdoutPipe
	t.stdout = bufio.NewReader(stdoutPipe)
	t.stderrPipe = stderrPipe
	t.stderrDone = make(chan struct{})
	go func() {
		_, _ = io.Copy(&t.stderrBuf, stderrPipe)
		close(t.stderrDone)
	}()
	t.connected = true

	return nil
}

// Write sends raw stream-json bytes to the CLI stdin.
func (t *SubprocessCLITransport) Write(ctx context.Context, data []byte) error {
	if !t.connected || t.stdin == nil {
		return &CLIConnectionError{Message: "transport is not connected"}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := t.stdin.Write(data); err != nil {
		return &CLIConnectionError{Message: fmt.Sprintf("failed to write to Claude Code stdin: %v", err)}
	}
	return nil
}

// CloseInput closes stdin to signal end-of-input to the CLI.
func (t *SubprocessCLITransport) CloseInput() error {
	if t.stdin == nil {
		return nil
	}
	err := t.stdin.Close()
	t.stdin = nil
	if err != nil && !errors.Is(err, os.ErrClosed) {
		return &CLIConnectionError{Message: fmt.Sprintf("failed to close Claude Code stdin: %v", err)}
	}
	return nil
}

// Read returns the next raw JSON payload from stdout.
func (t *SubprocessCLITransport) Read(ctx context.Context) ([]byte, error) {
	if !t.connected || t.stdout == nil {
		return nil, &CLIConnectionError{Message: "transport is not connected"}
	}
	if t.readDrained {
		return nil, io.EOF
	}

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		line, err := t.stdout.ReadBytes('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				if payload := bytes.TrimSpace(line); len(payload) > 0 {
					if payload[0] != '{' {
						return t.finishRead()
					}
					return t.validateJSON(payload)
				}
				return t.finishRead()
			}
			return nil, &CLIConnectionError{Message: fmt.Sprintf("failed reading Claude Code stdout: %v", err)}
		}

		payload := bytes.TrimSpace(line)
		if len(payload) == 0 {
			continue
		}
		if payload[0] != '{' {
			continue
		}

		return t.validateJSON(payload)
	}
}

// Close terminates any still-running subprocess and releases pipe resources.
func (t *SubprocessCLITransport) Close() error {
	if t.closed {
		return nil
	}
	t.closed = true

	_ = t.CloseInput()

	if t.stdoutPipe != nil {
		_ = t.stdoutPipe.Close()
		t.stdoutPipe = nil
	}
	if t.stderrPipe != nil {
		_ = t.stderrPipe.Close()
		t.stderrPipe = nil
	}

	if t.cmd != nil && t.cmd.Process != nil && t.cmd.ProcessState == nil {
		_ = t.cmd.Process.Kill()
		_, _ = t.waitForExit()
	}

	return nil
}

func (t *SubprocessCLITransport) buildCommandArgs() ([]string, error) {
	args := []string{
		"--output-format", "stream-json",
		"--verbose",
		"--input-format", "stream-json",
	}

	switch {
	case t.options.SystemPromptFile != nil:
		args = append(args, "--system-prompt-file", t.options.SystemPromptFile.Path)
	case t.options.SystemPromptPreset != nil:
		if t.options.SystemPromptPreset.Append != nil {
			args = append(args, "--append-system-prompt", *t.options.SystemPromptPreset.Append)
		}
	default:
		systemPrompt := ""
		if t.options.SystemPrompt != nil {
			systemPrompt = *t.options.SystemPrompt
		}
		args = append(args, "--system-prompt", systemPrompt)
	}

	switch {
	case t.options.ToolsPreset != nil:
		preset := t.options.ToolsPreset.Preset
		if preset == "" || preset == "claude_code" {
			preset = "default"
		}
		args = append(args, "--tools", preset)
	case t.options.Tools != nil:
		args = append(args, "--tools", strings.Join(t.options.Tools, ","))
	}

	if len(t.options.AllowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(t.options.AllowedTools, ","))
	}
	if len(t.options.DisallowedTools) > 0 {
		args = append(args, "--disallowedTools", strings.Join(t.options.DisallowedTools, ","))
	}
	if t.options.MaxTurns != nil {
		args = append(args, "--max-turns", strconv.Itoa(*t.options.MaxTurns))
	}
	if t.options.MaxBudgetUSD != nil {
		args = append(args, "--max-budget-usd", strconv.FormatFloat(*t.options.MaxBudgetUSD, 'f', -1, 64))
	}
	if t.options.Model != nil {
		args = append(args, "--model", *t.options.Model)
	}
	if t.options.FallbackModel != nil {
		args = append(args, "--fallback-model", *t.options.FallbackModel)
	}
	if len(t.options.Betas) > 0 {
		betas := make([]string, 0, len(t.options.Betas))
		for _, beta := range t.options.Betas {
			betas = append(betas, string(beta))
		}
		args = append(args, "--betas", strings.Join(betas, ","))
	}
	if t.options.PermissionPromptToolName != nil {
		args = append(args, "--permission-prompt-tool", *t.options.PermissionPromptToolName)
	}
	if t.options.PermissionMode != nil {
		args = append(args, "--permission-mode", string(*t.options.PermissionMode))
	}
	if t.options.ContinueConversation {
		args = append(args, "--continue")
	}
	if t.options.Resume != nil {
		args = append(args, "--resume", *t.options.Resume)
	}
	if t.options.Settings != nil {
		args = append(args, "--settings", *t.options.Settings)
	}
	for _, dir := range t.options.AddDirs {
		args = append(args, "--add-dir", dir)
	}
	if len(t.options.MCPServers) > 0 {
		value, err := serializeMCPServers(t.options.MCPServers)
		if err != nil {
			return nil, err
		}
		args = append(args, "--mcp-config", value)
	}
	if t.options.IncludePartialMessages {
		args = append(args, "--include-partial-messages")
	}
	if t.options.ForkSession {
		args = append(args, "--fork-session")
	}

	args = append(args, "--setting-sources", joinSettingSources(t.options.SettingSources))

	for _, plugin := range t.options.Plugins {
		if plugin.Path == "" {
			continue
		}
		args = append(args, "--plugin-dir", plugin.Path)
	}

	args = append(args, buildExtraArgs(t.options.ExtraArgs)...)

	if maxThinkingTokens := resolveMaxThinkingTokens(t.options); maxThinkingTokens != nil {
		args = append(args, "--max-thinking-tokens", strconv.Itoa(*maxThinkingTokens))
	}
	if t.options.Effort != nil {
		args = append(args, "--effort", *t.options.Effort)
	}
	if schema := extractJSONSchema(t.options.OutputFormat); schema != "" {
		args = append(args, "--json-schema", schema)
	}

	return args, nil
}

func (t *SubprocessCLITransport) buildEnvMap() map[string]string {
	env := make(map[string]string)
	for _, raw := range os.Environ() {
		parts := strings.SplitN(raw, "=", 2)
		key := parts[0]
		value := ""
		if len(parts) == 2 {
			value = parts[1]
		}
		env[key] = value
	}

	env["CLAUDE_CODE_ENTRYPOINT"] = defaultEntryPoint
	if t.options.EnableFileCheckpointing {
		env["CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING"] = "true"
	}
	if t.options.Cwd != nil {
		env["PWD"] = *t.options.Cwd
	}
	for key, value := range t.options.Env {
		env[key] = value
	}

	return env
}

func (t *SubprocessCLITransport) resolveCLIPath() (string, error) {
	if t.options.CLIPath != nil && *t.options.CLIPath != "" {
		return *t.options.CLIPath, nil
	}

	cliPath, err := exec.LookPath(defaultCLIName)
	if err != nil {
		return "", &CLINotFoundError{Message: "Claude Code not found", CLIPath: defaultCLIName}
	}
	return cliPath, nil
}

func (t *SubprocessCLITransport) finishRead() ([]byte, error) {
	t.readDrained = true

	exitCode, err := t.waitForExit()
	if err != nil {
		stderr := strings.TrimSpace(t.stderrBuf.String())
		return nil, &ProcessError{Message: "Claude Code process exited with error", ExitCode: exitCode, Stderr: stderr}
	}

	return nil, io.EOF
}

func (t *SubprocessCLITransport) waitForExit() (*int, error) {
	t.waitOnce.Do(func() {
		if t.cmd == nil {
			return
		}
		t.waitErr = t.cmd.Wait()
		if t.stderrDone != nil {
			<-t.stderrDone
		}
	})

	if t.cmd == nil || t.cmd.ProcessState == nil {
		return nil, t.waitErr
	}

	exitCode := t.cmd.ProcessState.ExitCode()
	if exitCode == 0 {
		return nil, nil
	}

	return &exitCode, t.waitErr
}

func (t *SubprocessCLITransport) validateJSON(payload []byte) ([]byte, error) {
	if len(payload) > t.maxBufferSize {
		return nil, &CLIJSONDecodeError{
			Line:          fmt.Sprintf("JSON message exceeded maximum buffer size of %d bytes", t.maxBufferSize),
			OriginalError: fmt.Errorf("buffer size %d exceeds limit %d", len(payload), t.maxBufferSize),
		}
	}

	var decoded any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, &CLIJSONDecodeError{Line: string(payload), OriginalError: err}
	}

	cloned := make([]byte, len(payload))
	copy(cloned, payload)
	return cloned, nil
}

func serializeMCPServers(servers map[string]MCPServerConfig) (string, error) {
	normalized := make(map[string]any, len(servers))

	for name, config := range servers {
		encoded, err := json.Marshal(config)
		if err != nil {
			return "", err
		}

		var payload map[string]any
		if err := json.Unmarshal(encoded, &payload); err != nil {
			return "", err
		}
		if payload == nil {
			payload = make(map[string]any)
		}

		switch typed := config.(type) {
		case MCPStdioServerConfig:
			if typed.Type == "" {
				payload["type"] = "stdio"
			}
		case MCPSSEServerConfig:
			if typed.Type != "" {
				payload["type"] = typed.Type
			}
		case MCPHTTPServerConfig:
			if typed.Type != "" {
				payload["type"] = typed.Type
			}
		case MCPSDKServerConfig:
			if typed.Type != "" {
				payload["type"] = typed.Type
			}
		}

		normalized[name] = payload
	}

	wrapper := map[string]any{"mcpServers": normalized}
	encoded, err := json.Marshal(wrapper)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func joinSettingSources(sources []SettingSource) string {
	if len(sources) == 0 {
		return ""
	}

	values := make([]string, 0, len(sources))
	for _, source := range sources {
		values = append(values, string(source))
	}
	return strings.Join(values, ",")
}

func buildExtraArgs(extraArgs map[string]*string) []string {
	if len(extraArgs) == 0 {
		return nil
	}

	keys := make([]string, 0, len(extraArgs))
	for key := range extraArgs {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	args := make([]string, 0, len(extraArgs)*2)
	for _, key := range keys {
		args = append(args, "--"+key)
		if extraArgs[key] != nil {
			args = append(args, *extraArgs[key])
		}
	}
	return args
}

func resolveMaxThinkingTokens(options Options) *int {
	if options.Thinking == nil {
		return options.MaxThinkingTokens
	}

	switch options.Thinking.Type {
	case ThinkingConfigAdaptive:
		if options.MaxThinkingTokens != nil {
			return options.MaxThinkingTokens
		}
		value := 32000
		return &value
	case ThinkingConfigEnabled:
		return options.Thinking.BudgetTokens
	case ThinkingConfigDisabled:
		value := 0
		return &value
	default:
		return options.MaxThinkingTokens
	}
}

func extractJSONSchema(outputFormat map[string]any) string {
	if outputFormat == nil {
		return ""
	}
	outputType, _ := outputFormat["type"].(string)
	if outputType != "json_schema" {
		return ""
	}
	schema, ok := outputFormat["schema"]
	if !ok {
		return ""
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func envMapToSlice(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	values := make([]string, 0, len(keys))
	for _, key := range keys {
		values = append(values, key+"="+env[key])
	}
	return values
}
