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

// AgentEvent 代理层封装的干净上下文抽象通信包，供最上层 UI 单向接收
type AgentEvent struct {
	DeltaContent    string
	ToolCallCreated *openai.ToolCall
	ToolCallResult  *AgentToolResult
	Done            bool
	Err             error
}

// AgentToolResult 是底层工具执行结束的实体
type AgentToolResult struct {
	Role       string
	Content    string
	ToolCallID string
}

// AgentControl 全面对齐 Codex Agent Registry / Controller 层级
// 它负责从内部打通大模型流与底层操作执行工具包的边界，形成多步网络内循环，而绝不允许 UI 操心
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
		Content: "You are Godex, an advanced macOS coding engine. You MUST use 'local_shell' extensively.",
	})
	return a
}

func (a *AgentControl) AddUserMessage(content string) {
	a.apiMessages = append(a.apiMessages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: content,
	})
}

// RunTurn 将会为一次聊天创建闭环迭代，直至模型表示无需再利用工作跳出思考
func (a *AgentControl) RunTurn(ctx context.Context, outChan chan<- AgentEvent) {
	defer close(outChan)

	for {
		session := a.client.NewSession()
		streamChan, err := session.Stream(ctx, a.apiMessages)
		if err != nil {
			outChan <- AgentEvent{Err: err}
			return
		}

		var totalContent strings.Builder
		var finalToolCalls []openai.ToolCall

		// 中转站：将底层的 Stream 传递给 UI 画进度，并在 Agent 控制层留存用作回传历史
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

		// 把零散的大模型思维和产生的工具调用整合后重新加入大脑记忆
		if totalContent.Len() > 0 || len(finalToolCalls) > 0 {
			a.apiMessages = append(a.apiMessages, openai.ChatCompletionMessage{
				Role:      openai.ChatMessageRoleAssistant,
				Content:   totalContent.String(),
				ToolCalls: finalToolCalls,
			})
		}

		// 如果没有需要执行的工具（比如已经向用户打完招呼和报告完毕），自动安全退出大循环回合！
		if len(finalToolCalls) == 0 {
			outChan <- AgentEvent{Done: true}
			return
		}

		// 对 Agent 发出的系统控制欲进行执行调停
		for _, tc := range finalToolCalls {
			payload := &tools.ToolPayload{
				Kind:      tools.ToolKindShell, // TODO: 未来按需拓展为 mapping
				Arguments: json.RawMessage(tc.Function.Arguments),
			}
			call := &tools.ToolCall{
				CallID:   tc.ID,
				ToolName: tc.Function.Name,
				Payload:  payload,
			}

			// 直接将调度权下放至系统级 Router 并在真实 OS 环境下锁并发执行指令
			res, err := a.router.BuildAndDispatch(ctx, call)

			var c string
			if err != nil {
				c = fmt.Sprintf(`{"error": "%s"}`, err.Error())
			} else {
				c = string(res.ToJSON())
			}

			// 将执行产物记入思维脑区，下一次循环将发送给大模型进行结果分析总结
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
