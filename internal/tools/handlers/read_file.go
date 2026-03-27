package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"godex/internal/tools"

	openai "github.com/sashabaranov/go-openai"
	"github.com/sashabaranov/go-openai/jsonschema"
)

type ReadFileArgs struct {
	AbsolutePath string `json:"absolute_path"`
	StartLine    *int   `json:"start_line,omitempty"`
	EndLine      *int   `json:"end_line,omitempty"`
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
			Description: "Read the content of a specified file on the local OS. Will automatically prefix output with line numbers. Use start_line and end_line for extremely large files.",
			Parameters: jsonschema.Definition{
				Type: jsonschema.Object,
				Properties: map[string]jsonschema.Definition{
					"absolute_path": {Type: jsonschema.String, Description: "Absolute or relative path to the file."},
					"start_line":    {Type: jsonschema.Integer, Description: "The specific line number to start reading from (1-indexed). Inclusive."},
					"end_line":      {Type: jsonschema.Integer, Description: "The specific line number to stop reading at (1-indexed). Inclusive."},
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

	lines := strings.Split(string(content), "\n")
	
	start := 1
	end := len(lines)
	
	if args.StartLine != nil && *args.StartLine >= 1 {
		start = *args.StartLine
	}
	if args.EndLine != nil && *args.EndLine <= len(lines) {
		end = *args.EndLine
	}
	if end > len(lines) {
		end = len(lines)
	}
	if start > end {
		return nil, fmt.Errorf("start_line %d cannot be greater than end_line %d", start, end)
	}

	// Safety truncation to avoid overwhelming the LLM Context Window
	const MAX_LINES = 1000
	truncated := false
	if end-start+1 > MAX_LINES {
		end = start + MAX_LINES - 1
		truncated = true
	}

	var builder strings.Builder
	for i := start; i <= end; i++ {
		builder.WriteString(fmt.Sprintf("%d: %s\n", i, lines[i-1]))
	}

	if truncated {
		builder.WriteString(fmt.Sprintf("\n... [Notice: File output severely truncated at maximum %d lines to preserve your context limits. Please use 'start_line' and 'end_line' parameters properly to paginate code.]\n", MAX_LINES))
	} else if start > 1 || end < len(lines) {
		builder.WriteString(fmt.Sprintf("\n... [Notice: Showing strictly lines %d to %d of %d total lines in '%s']\n", start, end, len(lines), filepath.Base(args.AbsolutePath)))
	} else {
		builder.WriteString(fmt.Sprintf("\n... [EOF: Total %d lines successfully read]\n", len(lines)))
	}

	outBytes := []byte(builder.String())

	return &tools.GenericToolOutput{
		Success: true,
		Data:    outBytes,
	}, nil
}
