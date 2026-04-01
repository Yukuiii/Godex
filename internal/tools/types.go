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
	GetDisplayMeta() *ToolDisplayMeta
}

// ToolDisplayMeta 为 TUI 提供结构化的显示信息，工具层填充，UI 层直接消费。
type ToolDisplayMeta struct {
	Label    string // 显示标签，如 "Read"
	FilePath string // 操作的文件路径
	Summary  string // 摘要信息，如 "17 lines read"
}

// GenericToolOutput is an easy-to-use implementation of ToolOutput.
type GenericToolOutput struct {
	Success     bool
	Data        json.RawMessage
	DisplayMeta *ToolDisplayMeta
}

func (o *GenericToolOutput) ToJSON() json.RawMessage       { return o.Data }
func (o *GenericToolOutput) IsSuccess() bool               { return o.Success }
func (o *GenericToolOutput) GetDisplayMeta() *ToolDisplayMeta { return o.DisplayMeta }

