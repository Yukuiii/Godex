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
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/joho/godotenv"
	openai "github.com/sashabaranov/go-openai"
	"github.com/sashabaranov/go-openai/jsonschema"

	"godex/internal/tools"
	"godex/internal/tools/handlers"
)

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

type streamMsg struct {
	DeltaContent    string
	ToolCallCreated *openai.ToolCall
	ToolCallResult  *chatMessage
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
	vp         viewport.Model
	ti         textinput.Model
	client     *openai.Client
	registry   *tools.ToolRegistry
	messages   []chatMessage
	isLoading  bool
	streamChan chan streamMsg
	ready      bool
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

// 帮助函数：复用历史对话渲染的逻辑推送到隔离的 Viewport 中
func renderMessages(messages []chatMessage) string {
	var s strings.Builder
	for _, msg := range messages {
		switch msg.role {
		case openai.ChatMessageRoleUser:
			s.WriteString(userStyle.Render("You: ") + msg.content + "\n\n")

		case openai.ChatMessageRoleAssistant:
			if msg.content != "" {
				s.WriteString(agentStyle.Render("Godex: ") + msg.content + "\n\n")
			}
			if len(msg.toolCalls) > 0 {
				for _, call := range msg.toolCalls {
					s.WriteString(systemStyle.Render(fmt.Sprintf("  [Tool] Using ➔ %s", call.Function.Name)) + "\n")
				}
				s.WriteString("\n")
			}

		case openai.ChatMessageRoleTool:
			s.WriteString(systemStyle.Render("  [Success] System Job Completed") + "\n\n")

		case openai.ChatMessageRoleSystem:
			s.WriteString(systemStyle.Render(" > " + msg.content) + "\n\n")
		}
	}
	return s.String()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// 当命令行尺寸变动时自适应 Viewport 的宽高边界
		headerHeight := lipgloss.Height(titleStyle.Render("╭── GODEX CHAT ENGINE ──╮")) + 2 // 包含下划线换行
		footerHeight := 2                                                                   // 给底部留两行的操作空间

		if !m.ready {
			m.vp = viewport.New(msg.Width, msg.Height-headerHeight-footerHeight)
			m.vp.YPosition = headerHeight
			m.vp.SetContent(renderMessages(m.messages))
			m.ready = true
		} else {
			m.vp.Width = msg.Width
			m.vp.Height = msg.Height - headerHeight - footerHeight
		}

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyEnter:
			v := strings.TrimSpace(m.ti.Value())
			if v == "" || m.isLoading {
				break
			}

			m.ti.SetValue("")
			m.messages = append(m.messages, chatMessage{role: openai.ChatMessageRoleUser, content: v})
			m.isLoading = true
			
			// 触发交互时，自动将视野卷动到页面最底部
			m.vp.SetContent(renderMessages(m.messages))
			m.vp.GotoBottom()

			if m.client == nil {
				m.messages = append(m.messages, chatMessage{role: openai.ChatMessageRoleSystem, content: "Error: Missing API_KEY"})
				m.isLoading = false
				m.vp.SetContent(renderMessages(m.messages))
				return m, nil
			}

			m.streamChan = make(chan streamMsg, 100)
			return m, tea.Batch(
				m.runStreamWorker(),
				m.waitForStream(),
			)
		}

	case streamMsg:
		if msg.Err != nil {
			m.isLoading = false
			m.messages = append(m.messages, chatMessage{role: openai.ChatMessageRoleSystem, content: "Error: " + msg.Err.Error()})
			m.vp.SetContent(renderMessages(m.messages))
			return m, nil
		}
		if msg.Done {
			m.isLoading = false
			return m, nil
		}

		if msg.ToolCallCreated != nil {
			m.messages = append(m.messages, chatMessage{
				role:      openai.ChatMessageRoleAssistant,
				toolCalls: []openai.ToolCall{*msg.ToolCallCreated},
			})
		} else if msg.ToolCallResult != nil {
			m.messages = append(m.messages, *msg.ToolCallResult)
		} else if msg.DeltaContent != "" {
			lastIdx := len(m.messages) - 1
			if lastIdx >= 0 && m.messages[lastIdx].role == openai.ChatMessageRoleAssistant && len(m.messages[lastIdx].toolCalls) == 0 {
				m.messages[lastIdx].content += msg.DeltaContent
			} else {
				m.messages = append(m.messages, chatMessage{role: openai.ChatMessageRoleAssistant, content: msg.DeltaContent})
			}
		}

		// 有字打过来的时候一边更新界面一边锁定底部
		m.vp.SetContent(renderMessages(m.messages))
		m.vp.GotoBottom()
		return m, m.waitForStream()
	}

	m.ti, cmd = m.ti.Update(msg)
	cmds = append(cmds, cmd)

	m.vp, cmd = m.vp.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m model) waitForStream() tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-m.streamChan
		if !ok {
			return streamMsg{Done: true}
		}
		return msg
	}
}

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

		for {
			req := openai.ChatCompletionRequest{
				Model:    modelName,
				Messages: apiMessages,
				Stream:   true,
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

					if delta.Content != "" {
						totalContent.WriteString(delta.Content)
						m.streamChan <- streamMsg{DeltaContent: delta.Content}
					}

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
				break
			}

			var finalToolCalls []openai.ToolCall
			for _, tc := range toolCallMap {
				finalToolCalls = append(finalToolCalls, tc)
				tcClone := tc
				m.streamChan <- streamMsg{ToolCallCreated: &tcClone}
			}

			apiMessages = append(apiMessages, openai.ChatCompletionMessage{
				Role:      openai.ChatMessageRoleAssistant,
				Content:   totalContent.String(),
				ToolCalls: finalToolCalls,
			})

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

				toolChatMsg := &chatMessage{role: openai.ChatMessageRoleTool, content: c, toolCallID: tc.ID}
				m.streamChan <- streamMsg{ToolCallResult: toolChatMsg}

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

func (m model) View() string {
	if !m.ready {
		return "\n  Initializing Godex OS..."
	}

	header := titleStyle.Render("╭── GODEX CHAT ENGINE ──╮")
	body := m.vp.View()

	var footer strings.Builder
	if m.isLoading {
		footer.WriteString(agentStyle.Render("Godex is thinking & interacting with OS..."))
	} else {
		footer.WriteString(m.ti.View() + "\n")
		footer.WriteString(systemStyle.Render("  [Enter: Send]  [Esc: Quit]  [PgUp/PgDn: Scroll]"))
	}

	return fmt.Sprintf("%s\n\n%s\n%s", header, body, footer.String())
}

func main() {
	_ = godotenv.Load()
	// 加载 Alt Screen 以支持沉浸式滚动视口和对旧窗口日志的无痕清理
	p := tea.NewProgram(initialModel(), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Godex Engine failed to start: %v\n", err)
		os.Exit(1)
	}
}
