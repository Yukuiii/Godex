package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"godex/internal/tools"

	openai "github.com/sashabaranov/go-openai"
	"github.com/sashabaranov/go-openai/jsonschema"
)

type ReadFileArgs struct {
	AbsolutePath string `json:"absolute_path"`
}

type ReadFileHandler struct{}

// NewReadFileHandler creates a handler for reading local files safely.
func NewReadFileHandler() *ReadFileHandler {
	return &ReadFileHandler{}
}

func (h *ReadFileHandler) Kind() tools.ToolKind {
	return tools.ToolKindCustom
}

func (h *ReadFileHandler) MatchesKind(payload *tools.ToolPayload) bool {
	return true
}

func (h *ReadFileHandler) PreToolUsePayload(invocation *tools.ToolInvocation) *tools.PreToolUsePayload {
	return &tools.PreToolUsePayload{Command: "read_file"}
}

func (h *ReadFileHandler) PostToolUsePayload(callID string, payload *tools.ToolPayload, result tools.ToolOutput) *tools.PostToolUsePayload {
	return &tools.PostToolUsePayload{Command: "read_file", ToolResponse: result.ToJSON()}
}

func (h *ReadFileHandler) GetToolSpec() openai.Tool {
	return openai.Tool{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "read_file",
			Description: "Read the entire content of a specified file on the local OS.",
			Parameters: jsonschema.Definition{
				Type: jsonschema.Object,
				Properties: map[string]jsonschema.Definition{
					"absolute_path": {Type: jsonschema.String, Description: "Absolute or relative path to the file."},
				},
				Required: []string{"absolute_path"},
			},
		},
	}
}

// IsMutating establishes whether executing this tool has side effects.
func (h *ReadFileHandler) IsMutating(_ context.Context, _ *tools.ToolInvocation) bool {
	return false // Reading a file is non-destructive
}

// Handle performs the logic of parsing and reading the target file payload.
func (h *ReadFileHandler) Handle(ctx context.Context, invocation *tools.ToolInvocation) (tools.ToolOutput, error) {
	var args ReadFileArgs
	if err := json.Unmarshal(invocation.Payload.Arguments, &args); err != nil {
		return nil, fmt.Errorf("failed to parse read_file arguments: %w", err)
	}

	if args.AbsolutePath == "" {
		return nil, fmt.Errorf("argument 'absolute_path' is required")
	}

	content, err := os.ReadFile(args.AbsolutePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file '%s': %w", args.AbsolutePath, err)
	}

	m := map[string]interface{}{
		"exit_code": 0,
		"content":   string(content),
	}
	outBytes, _ := json.Marshal(m)

	return &tools.GenericToolOutput{
		Success: true,
		Data:    outBytes,
	}, nil
}
