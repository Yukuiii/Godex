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

type ListDirArgs struct {
	DirPath string `json:"dir_path"`
	Depth   *int   `json:"depth,omitempty"`
}

type ListDirHandler struct{}

func NewListDirHandler() *ListDirHandler {
	return &ListDirHandler{}
}

func (h *ListDirHandler) Kind() tools.ToolKind {
	return tools.ToolKindCustom
}

func (h *ListDirHandler) MatchesKind(payload *tools.ToolPayload) bool {
	return true
}

func (h *ListDirHandler) PreToolUsePayload(invocation *tools.ToolInvocation) *tools.PreToolUsePayload {
	return &tools.PreToolUsePayload{Command: "list_dir"}
}

func (h *ListDirHandler) PostToolUsePayload(callID string, payload *tools.ToolPayload, result tools.ToolOutput) *tools.PostToolUsePayload {
	return &tools.PostToolUsePayload{Command: "list_dir", ToolResponse: result.ToJSON()}
}

func (h *ListDirHandler) GetToolSpec() openai.Tool {
	return openai.Tool{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "list_dir",
			Description: "Lists files and directories in a local directory up to a specified depth. Shows file sizes and modification times. Essential for orienting and navigating unknown repositories.",
			Parameters: jsonschema.Definition{
				Type: jsonschema.Object,
				Properties: map[string]jsonschema.Definition{
					"dir_path": {Type: jsonschema.String, Description: "Absolute or relative path to the target directory."},
					"depth":    {Type: jsonschema.Integer, Description: "Traversal depth levels (1 for current directory only. Default is 1)."},
				},
				Required: []string{"dir_path"},
			},
		},
	}
}

func (h *ListDirHandler) IsMutating(_ context.Context, _ *tools.ToolInvocation) bool {
	return false
}

func formatSize(size int64) string {
	switch {
	case size >= 1<<20:
		return fmt.Sprintf("%.1fM", float64(size)/float64(1<<20))
	case size >= 1<<10:
		return fmt.Sprintf("%.1fK", float64(size)/float64(1<<10))
	default:
		return fmt.Sprintf("%dB", size)
	}
}

func (h *ListDirHandler) Handle(_ context.Context, invocation *tools.ToolInvocation) (tools.ToolOutput, error) {
	var args ListDirArgs
	if err := json.Unmarshal(invocation.Payload.Arguments, &args); err != nil {
		return nil, fmt.Errorf("failed to decode args: %s", err)
	}

	maxDepth := 1
	if args.Depth != nil {
		maxDepth = *args.Depth
	}

	tgtPath, err := filepath.Abs(args.DirPath)
	if err != nil {
		return nil, fmt.Errorf("invalid path evaluation: %s", err)
	}

	var results []string
	fileCount := 0
	dirCount := 0
	baseLevel := len(strings.Split(tgtPath, string(filepath.Separator)))

	err = filepath.WalkDir(tgtPath, func(path string, d fs.DirEntry, walkerErr error) error {
		if walkerErr != nil {
			return nil
		}

		if path == tgtPath {
			return nil
		}

		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "vendor" || d.Name() == "__pycache__" {
				return filepath.SkipDir
			}
		}

		currentLvl := len(strings.Split(path, string(filepath.Separator)))
		depth := currentLvl - baseLevel
		if depth > maxDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		rel, _ := filepath.Rel(tgtPath, path)
		info, infoErr := d.Info()

		if d.IsDir() {
			dirCount++
			results = append(results, fmt.Sprintf("[DIR]  %s/", rel))
		} else {
			fileCount++
			if infoErr == nil {
				modTime := info.ModTime().Format("2006-01-02 15:04")
				results = append(results, fmt.Sprintf("[FILE] %-40s %6s  %s", rel, formatSize(info.Size()), modTime))
			} else {
				results = append(results, fmt.Sprintf("[FILE] %s", rel))
			}
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("list_dir error: %s", err)
	}

	if len(results) == 0 {
		return &tools.GenericToolOutput{
			Success: true,
			Data:    []byte("Directory is empty or all contents are ignored."),
		}, nil
	}

	results = append(results, fmt.Sprintf("\n--- %d file(s), %d dir(s) ---", fileCount, dirCount))
	out := strings.Join(results, "\n")
	return &tools.GenericToolOutput{
		Success: true,
		Data:    []byte(out),
	}, nil
}
