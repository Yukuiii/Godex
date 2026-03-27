package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"godex/internal/tools"

	openai "github.com/sashabaranov/go-openai"
	"github.com/sashabaranov/go-openai/jsonschema"
)

type GlobArgs struct {
	Pattern string `json:"pattern"`
	DirPath string `json:"dir_path,omitempty"`
}

// GlobHandler allows the Agent to discover files matching glob patterns across an entire project tree.
type GlobHandler struct{}

func NewGlobHandler() *GlobHandler {
	return &GlobHandler{}
}

func (h *GlobHandler) Kind() tools.ToolKind {
	return tools.ToolKindCustom
}

func (h *GlobHandler) MatchesKind(payload *tools.ToolPayload) bool {
	return true
}

func (h *GlobHandler) PreToolUsePayload(invocation *tools.ToolInvocation) *tools.PreToolUsePayload {
	return &tools.PreToolUsePayload{Command: "glob"}
}

func (h *GlobHandler) PostToolUsePayload(callID string, payload *tools.ToolPayload, result tools.ToolOutput) *tools.PostToolUsePayload {
	return &tools.PostToolUsePayload{Command: "glob", ToolResponse: result.ToJSON()}
}

func (h *GlobHandler) GetToolSpec() openai.Tool {
	return openai.Tool{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "glob",
			Description: "Find files matching a glob pattern recursively across the project tree. Common patterns: '*.go' for Go files, '*_test.go' for test files, 'Dockerfile*' for Dockerfiles.",
			Parameters: jsonschema.Definition{
				Type: jsonschema.Object,
				Properties: map[string]jsonschema.Definition{
					"pattern":  {Type: jsonschema.String, Description: "Glob pattern to match filenames against (e.g. '*.go', '*_test.py', 'Makefile')."},
					"dir_path": {Type: jsonschema.String, Description: "Root directory to search from. Defaults to current working directory if omitted."},
				},
				Required: []string{"pattern"},
			},
		},
	}
}

func (h *GlobHandler) IsMutating(_ context.Context, _ *tools.ToolInvocation) bool {
	return false
}

func (h *GlobHandler) Handle(_ context.Context, invocation *tools.ToolInvocation) (tools.ToolOutput, error) {
	var args GlobArgs
	if err := json.Unmarshal(invocation.Payload.Arguments, &args); err != nil {
		return nil, fmt.Errorf("failed to decode glob args: %w", err)
	}

	root := args.DirPath
	if root == "" {
		root = "."
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("invalid root path: %w", err)
	}

	var matches []string
	const maxResults = 200

	_ = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, walkerErr error) error {
		if walkerErr != nil {
			return nil
		}

		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "vendor" || d.Name() == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}

		matched, _ := filepath.Match(args.Pattern, d.Name())
		if matched {
			rel, _ := filepath.Rel(absRoot, path)
			matches = append(matches, rel)
			if len(matches) >= maxResults {
				return fmt.Errorf("max_results_reached")
			}
		}
		return nil
	})

	if len(matches) == 0 {
		return &tools.GenericToolOutput{
			Success: true,
			Data:    []byte(fmt.Sprintf("No files matching pattern '%s' found.", args.Pattern)),
		}, nil
	}

	var builder strings.Builder
	for _, m := range matches {
		builder.WriteString(m + "\n")
	}

	if len(matches) >= maxResults {
		builder.WriteString(fmt.Sprintf("\n... [Truncated: showing first %d results. Refine your pattern to narrow down.]", maxResults))
	} else {
		builder.WriteString(fmt.Sprintf("\n--- %d file(s) matched ---", len(matches)))
	}

	return &tools.GenericToolOutput{
		Success: true,
		Data:    []byte(builder.String()),
	}, nil
}
