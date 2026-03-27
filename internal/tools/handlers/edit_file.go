package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"godex/internal/tools"

	openai "github.com/sashabaranov/go-openai"
	"github.com/sashabaranov/go-openai/jsonschema"
)

type EditBlock struct {
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
}

type EditFileArgs struct {
	AbsolutePath string      `json:"absolute_path"`
	Edits        []EditBlock `json:"edits"`
}

// EditFileHandler implements the Claude-style Search and Replace Patch tool mechanism.
type EditFileHandler struct{}

func NewEditFileHandler() *EditFileHandler {
	return &EditFileHandler{}
}

func (h *EditFileHandler) Kind() tools.ToolKind {
	return tools.ToolKindCustom
}

func (h *EditFileHandler) MatchesKind(payload *tools.ToolPayload) bool {
	return true
}

func (h *EditFileHandler) PreToolUsePayload(invocation *tools.ToolInvocation) *tools.PreToolUsePayload {
	return &tools.PreToolUsePayload{Command: "edit_file"}
}

func (h *EditFileHandler) PostToolUsePayload(callID string, payload *tools.ToolPayload, result tools.ToolOutput) *tools.PostToolUsePayload {
	return &tools.PostToolUsePayload{Command: "edit_file", ToolResponse: result.ToJSON()}
}

func (h *EditFileHandler) GetToolSpec() openai.Tool {
	return openai.Tool{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "edit_file",
			Description: "Edit an existing file cleanly. Use this tool heavily for code modification instead of writing the whole file over. You provide an array of search/replace blocks. The old_text MUST match the file contents EXACTLY, including all spaces and indentation. Include lines above and below the change to ensure the match is unique.",
			Parameters: jsonschema.Definition{
				Type: jsonschema.Object,
				Properties: map[string]jsonschema.Definition{
					"absolute_path": {Type: jsonschema.String, Description: "Absolute or relative path to the target file."},
					"edits": {
						Type:        jsonschema.Array,
						Description: "A list of search-and-replace edit blocks to apply sequentially. The edits are atomic: if any search block fails, none are written.",
						Items: &jsonschema.Definition{
							Type: jsonschema.Object,
							Properties: map[string]jsonschema.Definition{
								"old_text": {Type: jsonschema.String, Description: "The exact block of code to replace. Must be absolutely identical to the file's current text, including all whitespace, indentation, and newlines! Provide a few lines of context above and below the change to ensure uniqueness in large files."},
								"new_text": {Type: jsonschema.String, Description: "The new text that will replace the old_text entirely."},
							},
							Required: []string{"old_text", "new_text"},
						},
					},
				},
				Required: []string{"absolute_path", "edits"},
			},
		},
	}
}

// IsMutating defines that this operation writes to disk and requires mutex gates on Registry execution (to avoid parallel writes).
func (h *EditFileHandler) IsMutating(_ context.Context, _ *tools.ToolInvocation) bool {
	return true
}

func (h *EditFileHandler) Handle(ctx context.Context, invocation *tools.ToolInvocation) (tools.ToolOutput, error) {
	var args EditFileArgs
	if err := json.Unmarshal(invocation.Payload.Arguments, &args); err != nil {
		return nil, fmt.Errorf("failed to unmarshal edit_file arguments: %w", err)
	}

	contentBytes, err := os.ReadFile(args.AbsolutePath)
	if err != nil {
		return nil, fmt.Errorf("could not read target file '%s': %w", args.AbsolutePath, err)
	}
	
	content := string(contentBytes)

	// Step 1: Validations and modifications done purely in memory to maintain atomicity
	for i, edit := range args.Edits {
		if edit.OldText == "" {
			return nil, fmt.Errorf("edit block %d failed: 'old_text' cannot be completely empty", i+1)
		}

		count := strings.Count(content, edit.OldText)
		
		if count == 0 {
			errStr := fmt.Sprintf("Edit block %d failed: 'old_text' snippet NOT FOUND in file. It must match exactly including whitespace, spaces, and indentation.", i+1)
			return &tools.GenericToolOutput{
				Success: false,
				Data:    []byte(errStr),
			}, nil
		}
		if count > 1 {
			errStr := fmt.Sprintf("Edit block %d failed: 'old_text' snippet found %d times in the file. It is deeply dangerous to replace an ambiguous block! Please add a few more lines of context to 'old_text' so it becomes perfectly unique.", i+1, count)
			return &tools.GenericToolOutput{
				Success: false,
				Data:    []byte(errStr),
			}, nil
		}

		// Exact single match found, safely swap in memory
		content = strings.Replace(content, edit.OldText, edit.NewText, 1)
	}

	// Step 2: Flush atomic changes forcefully to disk
	if err := os.WriteFile(args.AbsolutePath, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("failed to flush patched file back to disk: %w", err)
	}

	msg := fmt.Sprintf("Successfully applied %d flawless atomic edit(s) to %s.", len(args.Edits), args.AbsolutePath)
	return &tools.GenericToolOutput{
		Success: true,
		Data:    []byte(msg),
	}, nil
}
