package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"godex/internal/tools"

	openai "github.com/sashabaranov/go-openai"
	"github.com/sashabaranov/go-openai/jsonschema"
)

type WriteFileArgs struct {
	AbsolutePath string `json:"absolute_path"`
	Content      string `json:"content"`
}

type WriteFileHandler struct{}

// NewWriteFileHandler creates a handler exclusively dedicated to over-writing/creating files.
func NewWriteFileHandler() *WriteFileHandler {
	return &WriteFileHandler{}
}

func (h *WriteFileHandler) Kind() tools.ToolKind {
	return tools.ToolKindCustom
}

func (h *WriteFileHandler) MatchesKind(payload *tools.ToolPayload) bool {
	return true
}

func (h *WriteFileHandler) PreToolUsePayload(invocation *tools.ToolInvocation) *tools.PreToolUsePayload {
	return &tools.PreToolUsePayload{Command: "write_file"}
}

func (h *WriteFileHandler) PostToolUsePayload(callID string, payload *tools.ToolPayload, result tools.ToolOutput) *tools.PostToolUsePayload {
	return &tools.PostToolUsePayload{Command: "write_file", ToolResponse: result.ToJSON()}
}

func (h *WriteFileHandler) GetToolSpec() openai.Tool {
	return openai.Tool{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "write_file",
			Description: "Create or completely overwrite the content of a specified file on the local OS. Will create parent directories automatically.",
			Parameters: jsonschema.Definition{
				Type: jsonschema.Object,
				Properties: map[string]jsonschema.Definition{
					"absolute_path": {Type: jsonschema.String, Description: "Absolute or relative path to the file to construct/overwrite."},
					"content":       {Type: jsonschema.String, Description: "Full raw string payload to inject."},
				},
				Required: []string{"absolute_path", "content"},
			},
		},
	}
}

// IsMutating defines that this operation writes to disk and requires mutex gates on Registry execution.
func (h *WriteFileHandler) IsMutating(_ context.Context, _ *tools.ToolInvocation) bool {
	return true // Writing directly mutates the OS, enforcing the concurrency lock in the registry gate.
}

// Handle performs the logic of reading path configurations and overriding file context.
func (h *WriteFileHandler) Handle(ctx context.Context, invocation *tools.ToolInvocation) (tools.ToolOutput, error) {
	var args WriteFileArgs
	if err := json.Unmarshal(invocation.Payload.Arguments, &args); err != nil {
		return nil, fmt.Errorf("failed to parse write_file arguments: %w", err)
	}

	if args.AbsolutePath == "" {
		return nil, fmt.Errorf("argument 'absolute_path' is required")
	}

	dir := filepath.Dir(args.AbsolutePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create target file parent directories: %w", err)
	}

	if err := os.WriteFile(args.AbsolutePath, []byte(args.Content), 0644); err != nil {
		return nil, fmt.Errorf("failed to write to file '%s': %w", args.AbsolutePath, err)
	}

	m := map[string]interface{}{
		"exit_code": 0,
		"message":   fmt.Sprintf("Successfully wrote %d bytes to %s", len(args.Content), args.AbsolutePath),
	}
	outBytes, _ := json.Marshal(m)

	return &tools.GenericToolOutput{
		Success: true,
		Data:    outBytes,
	}, nil
}
