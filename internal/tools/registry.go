package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"github.com/sashabaranov/go-openai"
	"time"
)

// ToolRegistry manages all registered handlers and orchestrates execution,
// enforcing mutation gates and hooks.
type ToolRegistry struct {
	mu           sync.RWMutex
	handlers     map[string]ToolHandler
	mutatingGate sync.Mutex // A generic barrier to prevent parallel state-altering tasks
	readFiles    map[string]bool // Tracks files that have been read, enforcing read-before-edit policy.
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		handlers:  make(map[string]ToolHandler),
		readFiles: make(map[string]bool),
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

// GetAllToolSpecs collects and returns the OpenAPI function definitions of all registered tools.
func (r *ToolRegistry) GetAllToolSpecs() []openai.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var specs []openai.Tool
	for _, handler := range r.handlers {
		specs = append(specs, handler.GetToolSpec())
	}
	return specs
}

func (r *ToolRegistry) getHandler(name, namespace string) ToolHandler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	key := buildHandlerKey(name, namespace)
	return r.handlers[key]
}

// normalizePath resolves a file path to its absolute, clean form for consistent tracking.
func normalizePath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	return abs
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

	// 1. Hook Verification (Pre-Tool Use) — enforce read-before-edit policy.
	pre := handler.PreToolUsePayload(invocation)
	if pre != nil && pre.FilePath != "" {
		normalized := normalizePath(pre.FilePath)

		switch pre.Command {
		case "edit_file":
			// edit_file always requires the file to have been read first.
			if !r.readFiles[normalized] {
				return &GenericToolOutput{
					Success: false,
					Data:    []byte(fmt.Sprintf("Policy violation: you must read_file '%s' before editing it. Please read the file first to understand its content.", pre.FilePath)),
				}, nil
			}
		case "write_file":
			// write_file requires prior read only if the file already exists (overwrite scenario).
			if _, err := os.Stat(pre.FilePath); err == nil {
				if !r.readFiles[normalized] {
					return &GenericToolOutput{
						Success: false,
						Data:    []byte(fmt.Sprintf("Policy violation: file '%s' already exists. You must read_file it before overwriting. Please read the file first.", pre.FilePath)),
					}, nil
				}
			}
		}
	}

	// 2. Mutating Gate implementation (Barrier)
	isMutating := handler.IsMutating(ctx, invocation)
	if isMutating {
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

	// 4. Hook Notification (Post-Tool Use) — track successful file reads.
	if pre != nil && pre.Command == "read_file" && pre.FilePath != "" && result.IsSuccess() {
		r.readFiles[normalizePath(pre.FilePath)] = true
	}

	if post := handler.PostToolUsePayload(invocation.CallID, invocation.Payload, result); post != nil {
		// Future: dispatch_after_tool_use_hook
	}

	return result, nil
}
