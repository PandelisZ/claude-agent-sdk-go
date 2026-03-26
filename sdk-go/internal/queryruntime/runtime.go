package queryruntime

import (
	"context"
	"encoding/json"
	"io"

	internaltransport "github.com/PandelisZ/claude-agent-sdk-go/sdk-go/internal/transport"
)

// Runner coordinates one-shot prompt execution over a transport.
type Runner struct {
	transport internaltransport.Transport
}

// NewRunner builds a one-shot runtime runner.
func NewRunner(transport internaltransport.Transport) *Runner {
	return &Runner{transport: transport}
}

// Run sends a one-shot user prompt and streams raw JSON payloads to handler.
func (r *Runner) Run(ctx context.Context, prompt string, handler func([]byte) error) error {
	if err := r.transport.Connect(ctx); err != nil {
		return err
	}
	defer r.transport.Close()

	envelope, err := json.Marshal(map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": prompt,
		},
	})
	if err != nil {
		return err
	}

	if err := r.transport.Write(ctx, append(envelope, '\n')); err != nil {
		return err
	}
	if err := r.transport.CloseInput(); err != nil {
		return err
	}

	for {
		payload, err := r.transport.Read(ctx)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		if err := handler(payload); err != nil {
			return err
		}
	}
}
