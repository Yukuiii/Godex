package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/joho/godotenv"
	openai "github.com/sashabaranov/go-openai"
	"github.com/sashabaranov/go-openai/jsonschema"

	"godex/internal/tools"
	"godex/internal/tools/handlers"
)

// ------------------------------
// 赛博朋克霓虹主题色盘
// ------------------------------
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")).
			MarginBottom(1)

	userStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")).
			Bold(true)

	agentStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("78")).
			Italic(true)

	systemStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))
)

// streamMsg 承载后台异步协程往 TUI 的 UI 线程持续不断的事件注入
type streamMsg struct {
	DeltaContent    string             // AI 正在逐字说话
	ToolCallCreated *openai.ToolCall   // 拦截到了新的工具使用企图
	ToolCallResult  *chatMessage       // 刚刚执行完了一个本地指令
	Done            bool
	Err             error
}

type chatMessage struct {
	role       string
	content    string
	toolCalls  []openai.ToolCall
	toolCallID string
}

type model struct {
	ti         textinput.Model
	client     *openai.Client
	registry   *tools.ToolRegistry
	messages   []chatMessage
	isLoading  bool
	streamChan chan streamMsg
}

func initialModel() model {
	ti := textinput.New()
	ti.Placeholder = "Type your command to Godex..."
	ti.Focus()
	ti.CharLimit = 512
	ti.Width = 60

	apiKey := strings.TrimSpace(os.Getenv("API_KEY"))
	baseURL := strings.TrimSpace(os.Getenv("BASE_URL"))
	proxyURLStr := strings.TrimSpace(os.Getenv("PROXY_URL"))

	var client *openai.Client
	if apiKey != "" {
		config := openai.DefaultConfig(apiKey)
		if baseURL != "" {
			config.BaseURL = baseURL
		}
		config.APIType = openai.APITypeOpenAI

		if proxyURLStr == "" {
			proxyURLStr = "http://127.0.0.1:7890"
		}
		parsedURL, err := url.Parse(proxyURLStr)
		if err == nil {
			transport := &http.Transport{
				Proxy: http.ProxyURL(parsedURL),
			}
			config.HTTPClient = &http.Client{
				Transport: transport,
			}
		}

		client = openai.NewClientWithConfig(config)
	}

	registry := tools.NewToolRegistry()
	registry.Register("local_shell", "", handlers.NewShellHandler())

	return model{
		ti:       ti,
		client:   client,
		registry: registry,
		messages: []chatMessage{
			{role: openai.ChatMessageRoleSystem, content: "[System] Godex OS Activated! Local Shell is ready. Stream Mode Enabled."},
		},
	}
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyEnter:
			v := strings.TrimSpace(m.ti.Value())
			if v == "" || m.isLoading {
				return m, nil
			}

			m.ti.SetValue("")
			m.messages = append(m.messages, chatMessage{role: openai.ChatMessageRoleUser, content: v})
			m.isLoading = true

			if m.client == nil {
				m.messages = append(m.messages, chatMessage{role: openai.ChatMessageRoleSystem, content: "Error: Missing API_KEY"})
				m.isLoading = false
				return m, nil
			}

			// 为本次聊天开启专属的新流水线通道
			m.streamChan = make(chan streamMsg, 100)
			
			// 开启背景循环执行与实时投递，并同时监听通道
			return m, tea.Batch(
				m.runStreamWorker(),
				m.waitForStream(),
			)
		}

	// 接住管道里源源不断流过来的增量消息
	case streamMsg:
		if msg.Err != nil {
			m.isLoading = false
			m.messages = append(m.messages, chatMessage{role: openai.ChatMessageRoleSystem, content: "Error: " + msg.Err.Error()})
			return m, nil
		}
		if msg.Done {
			m.isLoading = false
			return m, nil
		}

		// 根据异步协程传来的四种状态类型，对屏幕文字做不同处理
		if msg.ToolCallCreated != nil {
			m.messages = append(m.messages, chatMessage{
				role:      openai.ChatMessageRoleAssistant,
				toolCalls: []openai.ToolCall{*msg.ToolCallCreated},
			})
		} else if msg.ToolCallResult != nil {
			m.messages = append(m.messages, *msg.ToolCallResult)
		} else if msg.DeltaContent != "" {
			// 如果连续说话，就续在上一条神明对话的尾巴上，形成打字机效果
			lastIdx := len(m.messages) - 1
			if lastIdx >= 0 && m.messages[lastIdx].role == openai.ChatMessageRoleAssistant && len(m.messages[lastIdx].toolCalls) == 0 {
				m.messages[lastIdx].content += msg.DeltaContent
			} else {
				m.messages = append(m.messages, chatMessage{role: openai.ChatMessageRoleAssistant, content: msg.DeltaContent})
			}
		}

		// 循环等待下一粒打字的字发过来
		return m, m.waitForStream()
	}

	m.ti, cmd = m.ti.Update(msg)
	return m, cmd
}

// waitForStream 监听来自底层工作协程的流式字并包裹成 tea.Msg 供 Update() 消费刷新
func (m model) waitForStream() tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-m.streamChan
		if !ok {
			return streamMsg{Done: true}
		}
		return msg
	}
}

// runStreamWorker 是最核心的异步大脑。不再返回结果后统一包起，而是将任何过程通过 chan 推送到外部
func (m model) runStreamWorker() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		var apiMessages []openai.ChatCompletionMessage

		apiMessages = append(apiMessages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: "You are Godex, an advanced macOS coding engine. You MUST use 'local_shell' extensively.",
		})

		for _, msg := range m.messages {
			aiMsg := openai.ChatCompletionMessage{Role: msg.role, Content: msg.content}
			if msg.toolCallID != "" {
				aiMsg.ToolCallID = msg.toolCallID
			}
			if len(msg.toolCalls) > 0 {
				aiMsg.ToolCalls = msg.toolCalls
			}
			apiMessages = append(apiMessages, aiMsg)
		}

		modelName := strings.TrimSpace(os.Getenv("MODEL"))
		if modelName == "" {
			modelName = "openai/gpt-3.5-turbo"
		}

		// 模型网络循环：由于模型有连发思考跟行动的需求
		for {
			req := openai.ChatCompletionRequest{
				Model:    modelName,
				Messages: apiMessages,
				Stream:   true, // 👉 开启 Stream 魔法模式！
				Tools: []openai.Tool{
					{
						Type: openai.ToolTypeFunction,
						Function: &openai.FunctionDefinition{
							Name:        "local_shell",
							Description: "Execute a command on the host OS shell.",
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

			stream, err := m.client.CreateChatCompletionStream(ctx, req)
			if err != nil {
				m.streamChan <- streamMsg{Err: err}
				break
			}

			// 我们需要一种结构来拼装大模型把 JSON 打碎传过来的块（Tool Call Aggregation）
			toolCallMap := make(map[int]openai.ToolCall)
			var totalContent strings.Builder

			for {
				resp, err := stream.Recv()
				if errors.Is(err, io.EOF) {
					stream.Close()
					break
				}
				if err != nil {
					stream.Close()
					m.streamChan <- streamMsg{Err: err}
					return nil
				}

				if len(resp.Choices) > 0 {
					delta := resp.Choices[0].Delta

					// 1. 如果大模型在说话，直接实时打字推到控制台界面
					if delta.Content != "" {
						totalContent.WriteString(delta.Content)
						m.streamChan <- streamMsg{DeltaContent: delta.Content}
					}

					// 2. 如果大模型决定要下发系统指令了（也是掰碎一波波传过来的，我们要手动将其拼接）
					for _, tcChunk := range delta.ToolCalls {
						idx := 0
						if tcChunk.Index != nil {
							idx = *tcChunk.Index
						}

						existing, ok := toolCallMap[idx]
						if !ok {
							existing = tcChunk
						} else {
							if tcChunk.ID != "" {
								existing.ID = tcChunk.ID
							}
							if tcChunk.Function.Name != "" {
								existing.Function.Name = tcChunk.Function.Name
							}
							existing.Function.Arguments += tcChunk.Function.Arguments
						}
						toolCallMap[idx] = existing
					}
				}
			}

			if len(toolCallMap) == 0 {
				break // 如果本回合只说了话而没用工具，意味着此轮大循环思考彻底完毕
			}

			// == 准备把本回合组装好的大结构正式推发 ==
			var finalToolCalls []openai.ToolCall
			for _, tc := range toolCallMap {
				finalToolCalls = append(finalToolCalls, tc)
				tcClone := tc
				m.streamChan <- streamMsg{ToolCallCreated: &tcClone} // 👉 流入提示界面：“我又要跑一次本地系统命令了”
			}

			apiMessages = append(apiMessages, openai.ChatCompletionMessage{
				Role:      openai.ChatMessageRoleAssistant,
				Content:   totalContent.String(),
				ToolCalls: finalToolCalls,
			})

			// 拦截接驳我们的 Registry，开始真正穿透操作系统做事
			for _, tc := range finalToolCalls {
				payload := &tools.ToolPayload{
					Kind:      tools.ToolKindShell,
					Arguments: json.RawMessage(tc.Function.Arguments),
				}
				invocation := &tools.ToolInvocation{
					CallID:   tc.ID,
					ToolName: tc.Function.Name,
					Payload:  payload,
				}
				
				res, err := m.registry.DispatchAny(ctx, invocation)
				var c string
				if err != nil {
					c = fmt.Sprintf(`{"error": "%s"}`, err.Error())
				} else {
					c = string(res.ToJSON())
				}

				// 将执行完毕的结果实时汇报到终端界面...
				toolChatMsg := &chatMessage{role: openai.ChatMessageRoleTool, content: c, toolCallID: tc.ID}
				m.streamChan <- streamMsg{ToolCallResult: toolChatMsg}

				// ...同时塞到队列尾部重新向网络抛回给大模型分析
				apiMessages = append(apiMessages, openai.ChatCompletionMessage{
					Role:       openai.ChatMessageRoleTool,
					Content:    c,
					ToolCallID: tc.ID,
				})
			}
		}

		m.streamChan <- streamMsg{Done: true}
		return nil
	}
}

// ------------------------------
// Elm 架构: 4. View
// ------------------------------
func (m model) View() string {
	var s strings.Builder
	s.WriteString(titleStyle.Render("╭── GODEX CHAT ENGINE ──╮") + "\n\n")

	for _, msg := range m.messages {
		switch msg.role {
		case openai.ChatMessageRoleUser:
			s.WriteString(userStyle.Render("You: ") + msg.content + "\n\n")

		case openai.ChatMessageRoleAssistant:
			if msg.content != "" {
				s.WriteString(agentStyle.Render("Godex: ") + msg.content + "\n\n")
			}
			if len(msg.toolCalls) > 0 {
				for _, call := range msg.toolCalls {
					// 极简状态：仅提示由于需要正在使用什么工具，屏蔽参数
					s.WriteString(systemStyle.Render(fmt.Sprintf("  [Tool] Using ➔ %s", call.Function.Name)) + "\n")
				}
			}

		case openai.ChatMessageRoleTool:
			// 极简状态：屏蔽超长返回数据，仅给一个打钩标志
			s.WriteString(systemStyle.Render("  [Success] System Job Completed") + "\n\n")
			
		case openai.ChatMessageRoleSystem:
			s.WriteString(systemStyle.Render(" > " + msg.content) + "\n\n")
		}
	}

	if m.isLoading {
		s.WriteString(agentStyle.Render("Godex is thinking & interacting with OS...") + "\n")
	} else {
		s.WriteString(m.ti.View() + "\n")
		s.WriteString(systemStyle.Render("  [Enter: Send] [Esc: Quit]") + "\n")
	}

	return s.String()
}

func main() {
	_ = godotenv.Load()
	p := tea.NewProgram(initialModel())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Godex Engine failed to start: %v\n", err)
		os.Exit(1)
	}
}
