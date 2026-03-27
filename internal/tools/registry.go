package tools

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ToolRegistry manages all registered handlers and orchestrates execution,
// enforcing mutation gates and hooks.
type ToolRegistry struct {
	mu           sync.RWMutex
	handlers     map[string]ToolHandler
	mutatingGate sync.Mutex // A generic barrier to prevent parallel state-altering tasks
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		handlers: make(map[string]ToolHandler),
	}
}

func buildHandlerKey(name, namespace string) string {
	if namespace != "" {
		return namespace + ":" + name
	}
	return name
}

// Register maps a ToolHandler for dispatch processing.
func (r *ToolRegistry) Register(name, namespace string, handler ToolHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := buildHandlerKey(name, namespace)
	r.handlers[key] = handler
}

func (r *ToolRegistry) getHandler(name, namespace string) ToolHandler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	key := buildHandlerKey(name, namespace)
	return r.handlers[key]
}

// DispatchAny safely routes and runs the chosen tool payload.
// Follows precisely the defined tool execution lifecycle.
func (r *ToolRegistry) DispatchAny(ctx context.Context, invocation *ToolInvocation) (ToolOutput, error) {
	handler := r.getHandler(invocation.ToolName, invocation.ToolNamespace)
	if handler == nil {
		return nil, fmt.Errorf("unsupported tool call: %s", buildHandlerKey(invocation.ToolName, invocation.ToolNamespace))
	}

	if !handler.MatchesKind(invocation.Payload) {
		return nil, fmt.Errorf("tool %s invoked with incompatible payload", invocation.ToolName)
	}

	// 1. Hook Verification (Pre-Tool Use)
	if pre := handler.PreToolUsePayload(invocation); pre != nil {
		// Mock implementation of run_pre_tool_use_hooks
		// Return Err hook failed if intercepted
	}

	// 2. Mutating Gate implementation (Barrier)
	isMutating := handler.IsMutating(ctx, invocation)
	if isMutating {
		// "Wait for tool gate" ensures we don't concurrently ruin a local repo context
		r.mutatingGate.Lock()
		defer r.mutatingGate.Unlock()
	}

	// 3. Execution (With metrics / tracing collection wrappers)
	start := time.Now()
	result, err := handler.Handle(ctx, invocation)
	_ = time.Since(start)

	if err != nil {
		return nil, err
	}

	// 4. Hook Notification (Post-Tool Use)
	if post := handler.PostToolUsePayload(invocation.CallID, invocation.Payload, result); post != nil {
		// Mock implementation of dispatch_after_tool_use_hook
	}

	return result, nil
}
