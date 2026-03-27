package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	"godex/internal/tools"

	openai "github.com/sashabaranov/go-openai"
	"github.com/sashabaranov/go-openai/jsonschema"
)

// ShellPayload defines the parameter structure expected from the LLM when acting as a shell.
type ShellPayload struct {
	Command   string `json:"command"`
	Workdir   string `json:"workdir,omitempty"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
}

// ShellOutput encapsulates the detailed response to be returned to the model context.
type ShellOutput struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	Error    string `json:"error,omitempty"`
}

// ShellHandler performs local OS shell command execution matching the ToolHandler pattern.
type ShellHandler struct{}

var _ tools.ToolHandler = (*ShellHandler)(nil) // Static interface validation

func NewShellHandler() *ShellHandler {
	return &ShellHandler{}
}

func (h *ShellHandler) Kind() tools.ToolKind {
	return tools.ToolKindShell
}

func (h *ShellHandler) MatchesKind(payload *tools.ToolPayload) bool {
	return payload != nil && payload.Kind == tools.ToolKindShell
}

func (h *ShellHandler) IsMutating(ctx context.Context, invocation *tools.ToolInvocation) bool {
	// Conservative security assumption:
	// A raw shell explicitly requests root mutation gate locking by default.
	return true
}

func (h *ShellHandler) PreToolUsePayload(invocation *tools.ToolInvocation) *tools.PreToolUsePayload {
	var sp ShellPayload
	if err := json.Unmarshal(invocation.Payload.Arguments, &sp); err != nil {
		return &tools.PreToolUsePayload{Command: "parse_error"}
	}
	return &tools.PreToolUsePayload{Command: sp.Command}
}

func (h *ShellHandler) PostToolUsePayload(callID string, payload *tools.ToolPayload, result tools.ToolOutput) *tools.PostToolUsePayload {
	return &tools.PostToolUsePayload{Command: "shell", ToolResponse: result.ToJSON()}
}

func (h *ShellHandler) GetToolSpec() openai.Tool {
	return openai.Tool{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "local_shell",
			Description: "Execute a strictly un-interactive command on the host OS shell.",
			Parameters: jsonschema.Definition{
				Type: jsonschema.Object,
				Properties: map[string]jsonschema.Definition{
					"command":    {Type: jsonschema.String, Description: "shell command"},
					"workdir":    {Type: jsonschema.String},
					"timeout_ms": {Type: jsonschema.Integer},
				},
				Required: []string{"command"},
			},
		},
	}
}

func (h *ShellHandler) Handle(ctx context.Context, invocation *tools.ToolInvocation) (tools.ToolOutput, error) {
	var sp ShellPayload
	if err := json.Unmarshal(invocation.Payload.Arguments, &sp); err != nil {
		return nil, fmt.Errorf("failed to decode shell payload: %w", err)
	}

	timeout := time.Duration(sp.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second // Default fallback timeout
	}

	// Utilizing Go's native OS cancellation context to bound executions
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Cross-platform shell detection
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		shell := os.Getenv("COMSPEC")
		if shell == "" {
			shell = "cmd.exe"
		}
		cmd = exec.CommandContext(cmdCtx, shell, "/C", sp.Command)
	} else {
		// POSIX (Linux / macOS) fallback to user's assigned shell or sh
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "sh"
		}
		cmd = exec.CommandContext(cmdCtx, shell, "-c", sp.Command)
	}
	if sp.Workdir != "" {
		cmd.Dir = sp.Workdir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	
	// Ensure we safely map termination status
	exitCode := -1
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	errStr := ""
	if err != nil {
		if cmdCtx.Err() == context.DeadlineExceeded {
			errStr = fmt.Sprintf("command timed out after %d milliseconds", timeout.Milliseconds())
		} else {
			errStr = err.Error()
		}
	}

	outputModel := &ShellOutput{
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Error:    errStr,
	}

	outputBytes, err := json.Marshal(outputModel)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize tool output: %w", err)
	}

	return &tools.GenericToolOutput{
		Success: exitCode == 0,
		Data:    outputBytes,
	}, nil
}
