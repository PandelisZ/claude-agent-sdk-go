package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	claudeagentsdk "github.com/PandelisZ/claude-agent-sdk-go/sdk-go"
)

func main() {
	var (
		prompt     = flag.String("prompt", "", "Prompt to send to Claude Code")
		model      = flag.String("model", "", "Optional model override")
		cwd        = flag.String("cwd", "", "Optional working directory for the Claude CLI")
		cliPath    = flag.String("claude-path", "", "Optional path to the Claude CLI binary")
		maxTurns   = flag.Int("max-turns", 1, "Maximum turns to allow for the request")
		showEvents = flag.Bool("show-events", false, "Print raw stream/system events to stderr")
		rawEvents  = flag.Bool("raw-stream-events", false, "Print each raw stream_event payload as JSON to stderr")
	)
	flag.Parse()

	if strings.TrimSpace(*prompt) == "" {
		flag.Usage()
		os.Exit(2)
	}

	options := claudeagentsdk.ClaudeAgentOptions{
		IncludePartialMessages: true,
		Env:                    map[string]string{},
	}
	if configDir := os.Getenv("CLAUDE_CONFIG_DIR"); configDir != "" {
		options.Env["CLAUDE_CONFIG_DIR"] = configDir
	}
	if *model != "" {
		options.Model = model
	}
	if *cwd != "" {
		options.Cwd = cwd
	}
	if *cliPath != "" {
		options.CLIPath = cliPath
	}
	if *maxTurns > 0 {
		options.MaxTurns = maxTurns
	}

	streamer := newAssistantTextStreamer(os.Stdout)
	var resultErr error

	err := claudeagentsdk.QueryWithCallback(context.Background(), *prompt, options, func(message claudeagentsdk.Message) error {
		switch typed := message.(type) {
		case *claudeagentsdk.AssistantMessage:
			if typed.Error != nil {
				if *typed.Error == claudeagentsdk.AssistantMessageErrorAuthenticationFailed {
					return fmt.Errorf("claude CLI is not authenticated; run `claude login`")
				}
				if *showEvents {
					fmt.Fprintf(os.Stderr, "[assistant_error] kind=%s\n", *typed.Error)
				}
			}
			if err := streamer.WriteAssistant(typed); err != nil {
				return err
			}
			if typed.Error != nil && *typed.Error != claudeagentsdk.AssistantMessageErrorAuthenticationFailed {
				resultErr = fmt.Errorf("assistant returned error: %s", *typed.Error)
			}
			return nil
		case *claudeagentsdk.ResultMessage:
			streamer.Finish()
			fmt.Fprintf(os.Stderr, "session=%s turns=%d error=%v\n", typed.SessionID, typed.NumTurns, typed.IsError)
			if typed.IsError {
				if typed.Result != nil && strings.TrimSpace(*typed.Result) != "" {
					resultErr = errors.New(*typed.Result)
				} else if resultErr == nil {
					resultErr = errors.New("request failed")
				}
			}
			return nil
		case *claudeagentsdk.SystemMessage:
			if *showEvents {
				fmt.Fprintf(os.Stderr, "[system] subtype=%s\n", typed.Subtype)
			}
			return nil
		case *claudeagentsdk.TaskStartedMessage:
			if *showEvents {
				fmt.Fprintf(os.Stderr, "[task_started] %s\n", typed.Description)
			}
			return nil
		case *claudeagentsdk.TaskProgressMessage:
			if *showEvents {
				fmt.Fprintf(os.Stderr, "[task_progress] %s tokens=%d tools=%d\n", typed.Description, typed.Usage.TotalTokens, typed.Usage.ToolUses)
			}
			return nil
		case *claudeagentsdk.TaskNotificationMessage:
			if *showEvents {
				fmt.Fprintf(os.Stderr, "[task_notification] status=%s summary=%s\n", typed.Status, typed.Summary)
			}
			return nil
		case *claudeagentsdk.StreamEvent:
			if *rawEvents {
				encoded, err := json.Marshal(typed.Event)
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "[stream_event.raw] %s\n", encoded)
			} else if *showEvents {
				fmt.Fprintf(os.Stderr, "[stream_event] %v\n", typed.Event)
			}
			return nil
		case *claudeagentsdk.RateLimitEvent:
			if *showEvents {
				fmt.Fprintf(os.Stderr, "[rate_limit] status=%s\n", typed.RateLimitInfo.Status)
			}
			return nil
		default:
			if *showEvents {
				fmt.Fprintf(os.Stderr, "[%s]\n", message.MessageType())
			}
			return nil
		}
	})
	if err != nil {
		var notFound *claudeagentsdk.CLINotFoundError
		if errors.As(err, &notFound) {
			log.Fatalf("claude CLI not found: %s", notFound)
		}
		var processErr *claudeagentsdk.ProcessError
		if resultErr != nil && errors.As(err, &processErr) {
			log.Fatal(resultErr)
		}
		log.Fatal(err)
	}
	if resultErr != nil {
		log.Fatal(resultErr)
	}
}

type assistantTextStreamer struct {
	writer           *os.File
	lastTextByKey    map[string]string
	wroteToStdout    bool
	lastEndedNewline bool
}

func newAssistantTextStreamer(writer *os.File) *assistantTextStreamer {
	return &assistantTextStreamer{
		writer:        writer,
		lastTextByKey: make(map[string]string),
	}
}

func (s *assistantTextStreamer) WriteAssistant(message *claudeagentsdk.AssistantMessage) error {
	key := assistantMessageKey(message)
	current := assistantText(message)
	previous := s.lastTextByKey[key]

	chunk := current
	if strings.HasPrefix(current, previous) {
		chunk = current[len(previous):]
	}
	if chunk == "" {
		s.lastTextByKey[key] = current
		return nil
	}

	if _, err := fmt.Fprint(s.writer, chunk); err != nil {
		return err
	}
	s.wroteToStdout = true
	s.lastEndedNewline = strings.HasSuffix(chunk, "\n")
	s.lastTextByKey[key] = current
	return nil
}

func (s *assistantTextStreamer) Finish() {
	if s.wroteToStdout && !s.lastEndedNewline {
		fmt.Fprintln(s.writer)
	}
}

func assistantMessageKey(message *claudeagentsdk.AssistantMessage) string {
	if message.MessageID != nil && *message.MessageID != "" {
		return "message_id:" + *message.MessageID
	}
	if message.UUID != nil && *message.UUID != "" {
		return "uuid:" + *message.UUID
	}
	if message.SessionID != nil && *message.SessionID != "" {
		return "session_id:" + *message.SessionID
	}
	return "assistant"
}

func assistantText(message *claudeagentsdk.AssistantMessage) string {
	var builder strings.Builder
	for _, block := range message.Content {
		switch typed := block.(type) {
		case claudeagentsdk.TextBlock:
			builder.WriteString(typed.Text)
		case claudeagentsdk.ThinkingBlock:
			continue
		}
	}
	return builder.String()
}
