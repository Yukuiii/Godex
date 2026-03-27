package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	openai "github.com/sashabaranov/go-openai"

	"godex/internal/llm"
	"godex/internal/tools"
)

// AgentEvent encapsulates clean context abstraction communication packages for one-way reception by the top-level UI.
type AgentEvent struct {
	DeltaContent    string
	ToolCallCreated *openai.ToolCall
	ToolCallResult  *AgentToolResult
	Done            bool
	Err             error
}

// AgentToolResult represents the entity after underlying tool execution completes.
type AgentToolResult struct {
	Role       string
	Content    string
	ToolCallID string
}

// AgentControl orchestrates the execution flow of tools and stream coordination.
// It seamlessly connects the boundary between the LLM stream and the underlying tool execution package forming a multi-step inner loop, preventing the UI from bearing this burden.
type AgentControl struct {
	client      *llm.ModelClient
	router      *tools.ToolRouter
	apiMessages []openai.ChatCompletionMessage
}

func NewAgentControl(client *llm.ModelClient, router *tools.ToolRouter) *AgentControl {
	a := &AgentControl{
		client: client,
		router: router,
	}
	
	a.apiMessages = append(a.apiMessages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleSystem,
		Content: "You are Godex, an advanced macOS coding engine. Employ 'read_file', 'write_file', or 'local_shell' extensively.",
	})
	return a
}

func (a *AgentControl) AddUserMessage(content string) {
	a.apiMessages = append(a.apiMessages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: content,
	})
}

// RunTurn creates a closed-loop iteration for a single chat turn until the model signals it no longer needs tools and finishes thinking.
func (a *AgentControl) RunTurn(ctx context.Context, outChan chan<- AgentEvent) {
	defer close(outChan)

	for {
		streamChan, err := a.client.Stream(ctx, a.apiMessages, a.router.GetAllToolSpecs())
		if err != nil {
			outChan <- AgentEvent{Err: err}
			return
		}

		var totalContent strings.Builder
		var finalToolCalls []openai.ToolCall

		// Relay: Pass the underlying Stream to the UI to draw progress, and retain it in the Agent control layer for history feedback.
		for rawEvent := range streamChan {
			if rawEvent.Err != nil {
				outChan <- AgentEvent{Err: rawEvent.Err}
				return
			}
			if rawEvent.DeltaContent != "" {
				totalContent.WriteString(rawEvent.DeltaContent)
				outChan <- AgentEvent{DeltaContent: rawEvent.DeltaContent}
			}
			if rawEvent.ToolCallCreated != nil {
				finalToolCalls = append(finalToolCalls, *rawEvent.ToolCallCreated)
				tcClone := *rawEvent.ToolCallCreated
				outChan <- AgentEvent{ToolCallCreated: &tcClone}
			}
		}

		// Consolidate scattered LLM reasoning and generated tool calls back into core memory.
		if totalContent.Len() > 0 || len(finalToolCalls) > 0 {
			a.apiMessages = append(a.apiMessages, openai.ChatCompletionMessage{
				Role:      openai.ChatMessageRoleAssistant,
				Content:   totalContent.String(),
				ToolCalls: finalToolCalls,
			})
		}

		// If there are no tools to execute (e.g., greeted user and finished reporting), safely exit the main loop!
		if len(finalToolCalls) == 0 {
			outChan <- AgentEvent{Done: true}
			return
		}

		// Mediate the execution of the Agent's systemic control requests.
		for _, tc := range finalToolCalls {
			payload := &tools.ToolPayload{
				Arguments: json.RawMessage(tc.Function.Arguments),
			}
			call := &tools.ToolCall{
				CallID:   tc.ID,
				ToolName: tc.Function.Name,
				Payload:  payload,
			}

			// Delegate scheduling power strictly to the system-level Router to execute commands concurrently under a real OS environment.
			res, err := a.router.BuildAndDispatch(ctx, call)

			var c string
			if err != nil {
				c = fmt.Sprintf(`{"error": "%s"}`, err.Error())
			} else {
				c = string(res.ToJSON())
			}

			// Record the execution artifact into memory, sending it to the LLM during the next loop for result analysis and summarization.
			a.apiMessages = append(a.apiMessages, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    c,
				ToolCallID: tc.ID,
			})

			outChan <- AgentEvent{
				ToolCallResult: &AgentToolResult{
					Role:       openai.ChatMessageRoleTool,
					Content:    c,
					ToolCallID: tc.ID,
				},
			}
		}
	}
}
