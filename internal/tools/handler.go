package tools

import "context"

// PreToolUsePayload and PostToolUsePayload are used for executing safety and tracking hooks.
type PreToolUsePayload struct {
	Command string
}

type PostToolUsePayload struct {
	Command      string
	ToolResponse []byte
}

// ToolHandler provides the interface that any individual tool (e.g. bash_shell, apply_patch) must implement.
type ToolHandler interface {
	// Kind returns the overarching ToolKind this handler supports.
	Kind() ToolKind

	// MatchesKind validates whether the particular ToolPayload is supported by this handler.
	MatchesKind(payload *ToolPayload) bool

	// IsMutating checks whether this tool execution changes the environment.
	// Used by the Registry to gate concurrent modifications.
	IsMutating(ctx context.Context, invocation *ToolInvocation) bool

	// Handle performs the tool operation.
	Handle(ctx context.Context, invocation *ToolInvocation) (ToolOutput, error)

	PreToolUsePayload(invocation *ToolInvocation) *PreToolUsePayload
	PostToolUsePayload(callID string, payload *ToolPayload, result ToolOutput) *PostToolUsePayload
}
