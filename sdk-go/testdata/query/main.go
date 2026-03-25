package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

func main() {
	mode := os.Getenv("FAKE_CLAUDE_MODE")
	if mode == "" {
		mode = "success"
	}

	prompt, err := readPrompt(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(2)
	}

	switch mode {
	case "success":
		writeJSON(os.Stdout, assistantPayload("Echo: "+prompt, ""))
		writeJSON(os.Stdout, resultPayload())
	case "auth":
		writeJSON(os.Stdout, assistantPayload("Invalid credentials", "authentication_failed"))
		writeJSON(os.Stdout, resultPayload())
	case "fragmented":
		fmt.Fprintln(os.Stdout, "[debug] not-json noise")
		writeFragmentedJSON(os.Stdout, assistantPayload("Echo: "+prompt, ""))
		fmt.Fprintln(os.Stdout, "warning: also noise")
		writeFragmentedJSON(os.Stdout, resultPayload())
	case "stderr_exit":
		fmt.Fprintln(os.Stderr, "fixture stderr failure")
		os.Exit(23)
	default:
		fmt.Fprintf(os.Stderr, "unknown mode: %s\n", mode)
		os.Exit(3)
	}
}

func readPrompt(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			return "", fmt.Errorf("invalid stdin JSON: %w", err)
		}

		message, _ := payload["message"].(map[string]any)
		content, _ := message["content"].(string)
		return content, nil
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", nil
}

func assistantPayload(text string, assistantErr string) map[string]any {
	payload := map[string]any{
		"type":       "assistant",
		"session_id": "session-1",
		"uuid":       "uuid-assistant-1",
		"message": map[string]any{
			"id":    "msg-1",
			"model": "fake-claude",
			"content": []map[string]any{
				{"type": "text", "text": text},
			},
			"stop_reason": "end_turn",
			"usage": map[string]any{
				"input_tokens":  10,
				"output_tokens": 5,
			},
		},
	}
	if assistantErr != "" {
		payload["error"] = assistantErr
	}
	return payload
}

func resultPayload() map[string]any {
	return map[string]any{
		"type":            "result",
		"subtype":         "success",
		"duration_ms":     5,
		"duration_api_ms": 4,
		"is_error":        false,
		"num_turns":       1,
		"session_id":      "session-1",
		"result":          "done",
	}
}

func writeJSON(w io.Writer, payload map[string]any) {
	encoded, _ := json.Marshal(payload)
	fmt.Fprintln(w, string(encoded))
}

func writeFragmentedJSON(w io.Writer, payload map[string]any) {
	encoded, _ := json.Marshal(payload)
	mid := len(encoded) / 2
	fmt.Fprint(w, string(encoded[:mid]))
	time.Sleep(5 * time.Millisecond)
	fmt.Fprint(w, string(encoded[mid:]))
	fmt.Fprint(w, "\n")
}
