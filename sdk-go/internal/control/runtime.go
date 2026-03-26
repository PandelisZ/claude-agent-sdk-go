package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/PandelisZ/claude-agent-sdk-go/sdk-go/internal/hooks"
	"github.com/PandelisZ/claude-agent-sdk-go/sdk-go/internal/protocol"
	internaltransport "github.com/PandelisZ/claude-agent-sdk-go/sdk-go/internal/transport"
)

type MCPHandler func(context.Context, map[string]any) (map[string]any, error)

type Options struct {
	Transport         internaltransport.Transport
	InitializeTimeout time.Duration
	PermissionHandler hooks.PermissionHandler
	HookRegistry      *hooks.Registry
	MCPHandlers       map[string]MCPHandler
}

type RequestError struct {
	Subtype string
	Message string
}

func (e *RequestError) Error() string {
	if e == nil {
		return ""
	}
	if e.Subtype == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Subtype, e.Message)
}

type Runtime struct {
	transport         internaltransport.Transport
	initializeTimeout time.Duration
	permissionHandler hooks.PermissionHandler
	hookRegistry      *hooks.Registry
	mcpHandlers       map[string]MCPHandler

	writeMu sync.Mutex

	stateMu    sync.RWMutex
	serverInfo map[string]any
	connected  bool
	closed     bool
	readErr    error

	messages chan []byte
	done     chan struct{}

	readerCtx    context.Context
	readerCancel context.CancelFunc

	pendingMu sync.Mutex
	pending   map[string]chan controlResult

	requestCounter uint64
	closeOnce      sync.Once
	wg             sync.WaitGroup
}

type controlResult struct {
	response map[string]any
	err      error
}

func NewRuntime(options Options) *Runtime {
	initializeTimeout := options.InitializeTimeout
	if initializeTimeout <= 0 {
		initializeTimeout = defaultInitializeTimeout()
	}

	mcpHandlers := make(map[string]MCPHandler, len(options.MCPHandlers))
	for name, handler := range options.MCPHandlers {
		mcpHandlers[name] = handler
	}

	return &Runtime{
		transport:         options.Transport,
		initializeTimeout: initializeTimeout,
		permissionHandler: options.PermissionHandler,
		hookRegistry:      options.HookRegistry,
		mcpHandlers:       mcpHandlers,
		messages:          make(chan []byte, 100),
		done:              make(chan struct{}),
		pending:           make(map[string]chan controlResult),
	}
}

func (r *Runtime) Connect(ctx context.Context) error {
	r.stateMu.Lock()
	if r.closed {
		r.stateMu.Unlock()
		return &internaltransport.CLIConnectionError{Message: "transport is not connected"}
	}
	if r.connected {
		r.stateMu.Unlock()
		return nil
	}
	r.stateMu.Unlock()

	if err := r.transport.Connect(ctx); err != nil {
		return err
	}

	r.readerCtx, r.readerCancel = context.WithCancel(context.Background())

	r.stateMu.Lock()
	r.connected = true
	r.stateMu.Unlock()

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.readLoop()
	}()

	request := map[string]any{
		"subtype": "initialize",
	}
	if config := r.hookRegistry.InitializeConfig(); len(config) > 0 {
		request["hooks"] = config
	}

	response, err := r.SendControl(ctx, request, r.initializeTimeout)
	if err != nil {
		_ = r.Close()
		return err
	}

	r.stateMu.Lock()
	r.serverInfo = cloneMap(response)
	r.stateMu.Unlock()
	return nil
}

func (r *Runtime) SendUser(ctx context.Context, payload map[string]any) error {
	if err := r.ensureConnected(); err != nil {
		return err
	}
	return r.writeJSON(ctx, payload)
}

func (r *Runtime) Receive(ctx context.Context) ([]byte, error) {
	if err := r.ensureConnected(); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case payload, ok := <-r.messages:
		if ok {
			return payload, nil
		}
		r.stateMu.RLock()
		err := r.readErr
		r.stateMu.RUnlock()
		if err == nil {
			return nil, io.EOF
		}
		return nil, err
	}
}

func (r *Runtime) SendControl(ctx context.Context, request map[string]any, timeout time.Duration) (map[string]any, error) {
	if err := r.ensureConnected(); err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	requestID := fmt.Sprintf("req_%d_%d", time.Now().UnixNano(), atomic.AddUint64(&r.requestCounter, 1))
	waiter := make(chan controlResult, 1)

	r.pendingMu.Lock()
	r.pending[requestID] = waiter
	r.pendingMu.Unlock()

	envelope := map[string]any{
		"type":       "control_request",
		"request_id": requestID,
		"request":    request,
	}
	if err := r.writeJSON(ctx, envelope); err != nil {
		r.pendingMu.Lock()
		delete(r.pending, requestID)
		r.pendingMu.Unlock()
		return nil, err
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		r.pendingMu.Lock()
		delete(r.pending, requestID)
		r.pendingMu.Unlock()
		return nil, ctx.Err()
	case <-timer.C:
		r.pendingMu.Lock()
		delete(r.pending, requestID)
		r.pendingMu.Unlock()
		subtype, _ := request["subtype"].(string)
		return nil, &RequestError{Subtype: subtype, Message: "control request timeout"}
	case result := <-waiter:
		if result.err != nil {
			return nil, result.err
		}
		return result.response, nil
	}
}

func (r *Runtime) ServerInfo() map[string]any {
	r.stateMu.RLock()
	defer r.stateMu.RUnlock()
	return cloneMap(r.serverInfo)
}

func (r *Runtime) Close() error {
	var err error
	r.closeOnce.Do(func() {
		r.stateMu.Lock()
		alreadyConnected := r.connected
		r.connected = false
		r.closed = true
		r.stateMu.Unlock()

		if r.readerCancel != nil {
			r.readerCancel()
		}
		if alreadyConnected {
			err = r.transport.Close()
		}
		r.failPending(io.EOF)
		r.wg.Wait()
	})
	return err
}

func (r *Runtime) readLoop() {
	defer close(r.done)

	for {
		payload, err := r.transport.Read(r.readerCtx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				r.finishRead(nil)
			} else if errors.Is(err, io.EOF) {
				r.finishRead(nil)
			} else {
				r.finishRead(err)
			}
			close(r.messages)
			return
		}

		envelope, err := protocol.DecodeJSONBytes(payload)
		if err != nil {
			decodeErr := &internaltransport.CLIJSONDecodeError{Line: string(payload), OriginalError: err}
			r.finishRead(decodeErr)
			close(r.messages)
			return
		}

		msgType, _ := protocol.StringValue(envelope, "type")
		switch msgType {
		case "control_response":
			r.handleControlResponse(envelope)
		case "control_request":
			r.wg.Add(1)
			go func(request map[string]any) {
				defer r.wg.Done()
				r.handleControlRequest(request)
			}(cloneMap(envelope))
		default:
			raw := make([]byte, len(payload))
			copy(raw, payload)
			r.messages <- raw
		}
	}
}

func (r *Runtime) handleControlResponse(envelope map[string]any) {
	response, ok := protocol.MapValue(envelope, "response")
	if !ok {
		return
	}
	requestID, _ := protocol.StringValue(response, "request_id")
	if requestID == "" {
		return
	}

	r.pendingMu.Lock()
	waiter, ok := r.pending[requestID]
	if ok {
		delete(r.pending, requestID)
	}
	r.pendingMu.Unlock()
	if !ok {
		return
	}

	subtype, _ := protocol.StringValue(response, "subtype")
	if subtype == "error" {
		message, _ := protocol.StringValue(response, "error")
		waiter <- controlResult{err: &RequestError{Subtype: subtype, Message: message}}
		return
	}

	body, _ := protocol.MapValue(response, "response")
	waiter <- controlResult{response: cloneMap(body)}
}

func (r *Runtime) handleControlRequest(envelope map[string]any) {
	requestID, _ := protocol.StringValue(envelope, "request_id")
	request, ok := protocol.MapValue(envelope, "request")
	if !ok {
		r.respondControlError(context.Background(), requestID, "missing request payload")
		return
	}

	subtype, _ := protocol.StringValue(request, "subtype")
	response := make(map[string]any)
	var err error

	switch subtype {
	case "can_use_tool":
		response, err = r.handlePermissionRequest(context.Background(), request)
	case "hook_callback":
		response, err = r.handleHookCallback(context.Background(), request)
	case "mcp_message":
		response, err = r.handleMCPMessage(context.Background(), request)
	default:
		err = fmt.Errorf("unsupported control request subtype: %s", subtype)
	}

	if err != nil {
		r.respondControlError(context.Background(), requestID, err.Error())
		return
	}
	r.respondControlSuccess(context.Background(), requestID, response)
}

func (r *Runtime) handlePermissionRequest(ctx context.Context, request map[string]any) (map[string]any, error) {
	if r.permissionHandler == nil {
		return nil, fmt.Errorf("canUseTool callback is not provided")
	}

	toolName, _ := protocol.StringValue(request, "tool_name")
	input, _ := protocol.MapValue(request, "input")
	suggestionsRaw, _ := protocol.SliceValue(request, "permission_suggestions")
	suggestions, err := hooks.ParsePermissionUpdates(suggestionsRaw)
	if err != nil {
		return nil, err
	}

	result, err := r.permissionHandler(ctx, toolName, cloneMap(input), hooks.ToolPermissionContext{
		Signal:      nil,
		Suggestions: suggestions,
	})
	if err != nil {
		return nil, err
	}

	switch typed := result.(type) {
	case hooks.PermissionResultAllow:
		response := map[string]any{
			"behavior":     "allow",
			"updatedInput": cloneMap(input),
		}
		if typed.UpdatedInput != nil {
			response["updatedInput"] = cloneMap(typed.UpdatedInput)
		}
		if len(typed.UpdatedPermissions) > 0 {
			updates := make([]map[string]any, 0, len(typed.UpdatedPermissions))
			for _, update := range typed.UpdatedPermissions {
				updates = append(updates, update.ToMap())
			}
			response["updatedPermissions"] = updates
		}
		return response, nil
	case *hooks.PermissionResultAllow:
		if typed == nil {
			return nil, fmt.Errorf("tool permission callback returned nil allow result")
		}
		response := map[string]any{
			"behavior":     "allow",
			"updatedInput": cloneMap(input),
		}
		if typed.UpdatedInput != nil {
			response["updatedInput"] = cloneMap(typed.UpdatedInput)
		}
		if len(typed.UpdatedPermissions) > 0 {
			updates := make([]map[string]any, 0, len(typed.UpdatedPermissions))
			for _, update := range typed.UpdatedPermissions {
				updates = append(updates, update.ToMap())
			}
			response["updatedPermissions"] = updates
		}
		return response, nil
	case hooks.PermissionResultDeny:
		response := map[string]any{
			"behavior": "deny",
			"message":  typed.Message,
		}
		if typed.Interrupt {
			response["interrupt"] = true
		}
		return response, nil
	case *hooks.PermissionResultDeny:
		if typed == nil {
			return nil, fmt.Errorf("tool permission callback returned nil deny result")
		}
		response := map[string]any{
			"behavior": "deny",
			"message":  typed.Message,
		}
		if typed.Interrupt {
			response["interrupt"] = true
		}
		return response, nil
	default:
		return nil, fmt.Errorf("tool permission callback must return PermissionResult, got %T", result)
	}
}

func (r *Runtime) handleHookCallback(ctx context.Context, request map[string]any) (map[string]any, error) {
	if r.hookRegistry == nil {
		return nil, fmt.Errorf("no hook callbacks configured")
	}

	callbackID, _ := protocol.StringValue(request, "callback_id")
	input, _ := protocol.MapValue(request, "input")
	var toolUseID *string
	if value, ok := protocol.StringValue(request, "tool_use_id"); ok {
		toolUseID = &value
	}
	return r.hookRegistry.Handle(ctx, callbackID, cloneMap(input), toolUseID)
}

func (r *Runtime) handleMCPMessage(ctx context.Context, request map[string]any) (map[string]any, error) {
	serverName, _ := protocol.StringValue(request, "server_name")
	message, _ := protocol.MapValue(request, "message")
	if serverName == "" || message == nil {
		return nil, fmt.Errorf("missing server_name or message for MCP request")
	}

	handler, ok := r.mcpHandlers[serverName]
	if !ok {
		return map[string]any{
			"mcp_response": map[string]any{
				"jsonrpc": "2.0",
				"id":      message["id"],
				"error": map[string]any{
					"code":    -32601,
					"message": fmt.Sprintf("Server '%s' not found", serverName),
				},
			},
		}, nil
	}

	response, err := handler(ctx, cloneMap(message))
	if err != nil {
		return nil, err
	}
	return map[string]any{"mcp_response": response}, nil
}

func (r *Runtime) respondControlSuccess(ctx context.Context, requestID string, response map[string]any) {
	_ = r.writeJSON(ctx, map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "success",
			"request_id": requestID,
			"response":   response,
		},
	})
}

func (r *Runtime) respondControlError(ctx context.Context, requestID string, message string) {
	_ = r.writeJSON(ctx, map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "error",
			"request_id": requestID,
			"error":      message,
		},
	})
}

func (r *Runtime) writeJSON(ctx context.Context, payload map[string]any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	return r.transport.Write(ctx, append(encoded, '\n'))
}

func (r *Runtime) ensureConnected() error {
	r.stateMu.RLock()
	defer r.stateMu.RUnlock()
	if !r.connected || r.closed {
		return &internaltransport.CLIConnectionError{Message: "Not connected. Call Connect() first."}
	}
	return nil
}

func (r *Runtime) finishRead(err error) {
	r.stateMu.Lock()
	if err != nil {
		r.readErr = err
	}
	r.connected = false
	r.stateMu.Unlock()
	r.failPending(err)
}

func (r *Runtime) failPending(err error) {
	r.pendingMu.Lock()
	defer r.pendingMu.Unlock()
	if err == nil {
		err = io.EOF
	}
	for requestID, waiter := range r.pending {
		delete(r.pending, requestID)
		waiter <- controlResult{err: err}
	}
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func defaultInitializeTimeout() time.Duration {
	timeoutMS := 60000
	if raw := os.Getenv("CLAUDE_CODE_STREAM_CLOSE_TIMEOUT"); raw != "" {
		if parsed, err := time.ParseDuration(raw + "ms"); err == nil && parsed > 0 {
			if parsed < time.Minute {
				return time.Minute
			}
			return parsed
		}
	}
	return time.Duration(timeoutMS) * time.Millisecond
}
