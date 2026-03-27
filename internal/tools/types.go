package tools

import "encoding/json"

// ToolKind represents the generic category of a tool deployment.
type ToolKind string

const (
	ToolKindFunction ToolKind = "Function"
	ToolKindMcp      ToolKind = "Mcp"
	ToolKindShell    ToolKind = "LocalShell"
	ToolKindCustom   ToolKind = "Custom"
)

// ToolPayload represents the arguments and metadata for a tool invocation.
type ToolPayload struct {
	Kind      ToolKind        `json:"kind"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolCall represents a unified parsed tool request decoded from the LLM.
type ToolCall struct {
	CallID        string       `json:"call_id"`
	ToolName      string       `json:"tool_name"`
	ToolNamespace string       `json:"tool_namespace,omitempty"`
	Payload       *ToolPayload `json:"payload"`
}

// ToolInvocation represents everything needed for a handler to execute safely, including session context.
type ToolInvocation struct {
	CallID        string
	ToolName      string
	ToolNamespace string
	Payload       *ToolPayload
	// TODO: Session reference and Contextual memory traces could be placed here
}

// ToolOutput represents the generic return type of a tool execution.
type ToolOutput interface {
	ToJSON() json.RawMessage
	IsSuccess() bool
}

// GenericToolOutput is an easy-to-use implementation of ToolOutput.
type GenericToolOutput struct {
	Success bool
	Data    json.RawMessage
}

func (o *GenericToolOutput) ToJSON() json.RawMessage { return o.Data }
func (o *GenericToolOutput) IsSuccess() bool         { return o.Success }
