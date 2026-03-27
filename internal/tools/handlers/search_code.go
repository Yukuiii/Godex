package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"godex/internal/tools"

	openai "github.com/sashabaranov/go-openai"
	"github.com/sashabaranov/go-openai/jsonschema"
)

type SearchCodeArgs struct {
	Query   string `json:"query"`
	DirPath string `json:"dir_path"`
}

// SearchCodeHandler provides highly structured and robust grep capabilities across repositories.
type SearchCodeHandler struct{}

func NewSearchCodeHandler() *SearchCodeHandler {
	return &SearchCodeHandler{}
}

func (h *SearchCodeHandler) Kind() tools.ToolKind {
	return tools.ToolKindCustom
}

func (h *SearchCodeHandler) MatchesKind(payload *tools.ToolPayload) bool {
	return true
}

func (h *SearchCodeHandler) PreToolUsePayload(invocation *tools.ToolInvocation) *tools.PreToolUsePayload {
	return &tools.PreToolUsePayload{Command: "search_code"}
}

func (h *SearchCodeHandler) PostToolUsePayload(callID string, payload *tools.ToolPayload, result tools.ToolOutput) *tools.PostToolUsePayload {
	return &tools.PostToolUsePayload{Command: "search_code", ToolResponse: result.ToJSON()}
}

func (h *SearchCodeHandler) GetToolSpec() openai.Tool {
	return openai.Tool{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "search_code",
			Description: "Recursively searches for a specific string query or snippet text across all non-ignored project files.",
			Parameters: jsonschema.Definition{
				Type: jsonschema.Object,
				Properties: map[string]jsonschema.Definition{
					"query":    {Type: jsonschema.String, Description: "The exact search query block or pattern."},
					"dir_path": {Type: jsonschema.String, Description: "Target root directory to evaluate from."},
				},
				Required: []string{"query", "dir_path"},
			},
		},
	}
}

func (h *SearchCodeHandler) IsMutating(_ context.Context, _ *tools.ToolInvocation) bool {
	return false 
}

func (h *SearchCodeHandler) Handle(ctx context.Context, invocation *tools.ToolInvocation) (tools.ToolOutput, error) {
	var args SearchCodeArgs
	if err := json.Unmarshal(invocation.Payload.Arguments, &args); err != nil {
		return nil, fmt.Errorf("failed decoding args: %s", err)
	}

	searchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(searchCtx, "grep", "-rnI", "--exclude-dir=.git", "--exclude-dir=node_modules", "--exclude-dir=vendor", args.Query, args.DirPath)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	if err != nil && outBuf.Len() == 0 {
		return &tools.GenericToolOutput{
			Success: true,
			Data:    []byte("No matching code lines found for query."),
		}, nil
	}

	res := outBuf.String()
	// Protection against dumping massive console limits and crashing LLM context windows
	if len(res) > 50000 {
		res = res[:50000] + "\n... [Output severely truncated due to length over matches]"
	}

	return &tools.GenericToolOutput{
		Success: true,
		Data:    []byte(res),
	}, nil
}
