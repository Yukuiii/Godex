package llm

import (
	"context"
	"errors"
	"io"

	openai "github.com/sashabaranov/go-openai"
	"github.com/sashabaranov/go-openai/jsonschema"
)

// ModelClientSession 对应 Codex 中极短的回合制生存周期隔离 (Turn-Scoped)
// 负责处理大模型每次对话的底层碎片拼接，不掺杂任何业务逻辑调度
type ModelClientSession struct {
	client *ModelClient
}

// StreamEvent 是一套纯粹、无污染的网络底层暴露模型
type StreamEvent struct {
	DeltaContent    string
	ToolCallCreated *openai.ToolCall
	Done            bool
	Err             error
}

// Stream 发起纯净剥离的底层请求引擎通道
func (s *ModelClientSession) Stream(ctx context.Context, apiMessages []openai.ChatCompletionMessage) (<-chan StreamEvent, error) {
	out := make(chan StreamEvent, 100)

	if s.client.APIClient == nil {
		return nil, errors.New("Missing API_KEY in Godex Environment")
	}

	// 初始化具有 Tool Calling 抽象能力的发包结构
	req := openai.ChatCompletionRequest{
		Model:    s.client.ModelName,
		Messages: apiMessages,
		Stream:   true,
		Tools: []openai.Tool{
			{
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
			},
		},
	}

	stream, err := s.client.APIClient.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return nil, err
	}

	// 通过 Goroutine 将模型网络层切片粘在一起返回
	go func() {
		defer close(out)
		defer stream.Close()

		toolCallMap := make(map[int]openai.ToolCall)

		for {
			resp, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				out <- StreamEvent{Err: err}
				return
			}

			if len(resp.Choices) > 0 {
				delta := resp.Choices[0].Delta

				if delta.Content != "" {
					out <- StreamEvent{DeltaContent: delta.Content}
				}

				for _, tChunk := range delta.ToolCalls {
					idx := 0
					if tChunk.Index != nil {
						idx = *tChunk.Index
					}

					existing, ok := toolCallMap[idx]
					if !ok {
						existing = tChunk
					} else {
						if tChunk.ID != "" {
							existing.ID = tChunk.ID
						}
						if tChunk.Function.Name != "" {
							existing.Function.Name = tChunk.Function.Name
						}
						existing.Function.Arguments += tChunk.Function.Arguments
					}
					toolCallMap[idx] = existing
				}
			}
		}

		// 有序结算抛出所有的组装完毕的工具实体
		if len(toolCallMap) > 0 {
			maxIdx := -1
			for idx := range toolCallMap {
				if idx > maxIdx {
					maxIdx = idx
				}
			}
			for i := 0; i <= maxIdx; i++ {
				if tc, ok := toolCallMap[i]; ok {
					tcClone := tc
					out <- StreamEvent{ToolCallCreated: &tcClone}
				}
			}
		}

		out <- StreamEvent{Done: true}
	}()

	return out, nil
}
