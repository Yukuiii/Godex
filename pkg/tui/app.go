package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	openai "github.com/sashabaranov/go-openai"

	"godex/internal/agent"
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

	// 输入区域样式 — 模仿 Claude Code 的深色输入条
	promptStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("208")).
			Bold(true)

	inputBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("235")).
			PaddingLeft(0)

	hintStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Background(lipgloss.Color("235"))
)

type chatMessage struct {
	role      string
	content   string
	name      string
	toolCalls []openai.ToolCall
}

type appModel struct {
	vp         viewport.Model
	ti         textinput.Model
	agentCtrl  *agent.AgentControl
	messages   []chatMessage
	isLoading  bool
	streamChan chan agent.AgentEvent
	ready      bool
	quitting   bool
}

func initialModel(agentCtrl *agent.AgentControl) appModel {
	ti := textinput.New()
	ti.Prompt = ""       // 不使用 textinput 自带的 prompt，我们自己渲染 ❯
	ti.Placeholder = ""  // Claude Code 风格：无 placeholder 文字
	ti.Focus()
	ti.CharLimit = 4096
	ti.Width = 80        // 初始值，会在 WindowSizeMsg 中动态调整

	return appModel{
		ti:        ti,
		agentCtrl: agentCtrl,
		messages: []chatMessage{
			{role: openai.ChatMessageRoleSystem, content: "[System] Godex OS Activated! Modular Architecture Loaded."},
		},
	}
}

func (m appModel) Init() tea.Cmd {
	return textinput.Blink
}

func renderMessages(messages []chatMessage, width int) string {
	var s strings.Builder
	if width < 10 {
		width = 80 // Fallback width protection
	}
	
	// Use lipgloss to limit max width for natural word wrapping
	wrapStyle := lipgloss.NewStyle().Width(width - 4)

	for i, msg := range messages {
		switch msg.role {
		case openai.ChatMessageRoleUser:
			s.WriteString(wrapStyle.Render(userStyle.Render("You: ") + msg.content) + "\n\n")

		case openai.ChatMessageRoleAssistant:
			if msg.content != "" {
				s.WriteString(wrapStyle.Render(agentStyle.Render("Godex: ") + msg.content) + "\n\n")
			}
			if len(msg.toolCalls) > 0 {
				for _, call := range msg.toolCalls {
					isDone := false
					for j := i + 1; j < len(messages); j++ {
						if messages[j].role == openai.ChatMessageRoleTool && messages[j].name == call.Function.Name {
							isDone = true
							break
						}
					}

					if isDone {
						s.WriteString(wrapStyle.Render(systemStyle.Render(fmt.Sprintf("  [%s] done", call.Function.Name))) + "\n")
					} else {
						s.WriteString(wrapStyle.Render(systemStyle.Render(fmt.Sprintf("  [%s]...", call.Function.Name))) + "\n")
					}
				}
				s.WriteString("\n")
			}

		case openai.ChatMessageRoleTool:
			// Hidden: Result explicitly folded into the Assistant's UI above as `done`

		case openai.ChatMessageRoleSystem:
			s.WriteString(wrapStyle.Render(systemStyle.Render(" > " + msg.content)) + "\n\n")
		}
	}
	return s.String()
}

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		headerHeight := lipgloss.Height(titleStyle.Render("╭── GODEX CHAT ENGINE ──╮")) + 2 // Includes bottom margin
		footerHeight := 3                                                                   // 输入提示符行 + 分隔线 + hint 行

		// 让输入框跟随终端宽度
		m.ti.Width = msg.Width - 4 // 留出 "❯ " 提示符宽度

		if !m.ready {
			m.vp = viewport.New(msg.Width, msg.Height-headerHeight-footerHeight)
			m.vp.YPosition = headerHeight
				m.vp.SetContent(renderMessages(m.messages, m.vp.Width))
			m.ready = true
		} else {
			m.vp.Width = msg.Width
			m.vp.Height = msg.Height - headerHeight - footerHeight
		}

	case tea.KeyMsg:
		if msg.Type != tea.KeyCtrlC && m.quitting {
			m.quitting = false // Reset quit state if user types something else
		}

		switch msg.Type {
		case tea.KeyCtrlC:
			if m.quitting {
				return m, tea.Quit
			}
			m.quitting = true
			m.messages = append(m.messages, chatMessage{
				role:    openai.ChatMessageRoleSystem,
				content: "Press Ctrl+C again to exit Godex.",
			})
			m.vp.SetContent(renderMessages(m.messages, m.vp.Width))
			m.vp.GotoBottom()
			return m, nil

		case tea.KeyEnter:
			v := strings.TrimSpace(m.ti.Value())
			if v == "" || m.isLoading {
				break
			}

			m.ti.SetValue("")
			m.messages = append(m.messages, chatMessage{role: openai.ChatMessageRoleUser, content: v})
			m.isLoading = true

				m.vp.SetContent(renderMessages(m.messages, m.vp.Width))
			m.vp.GotoBottom()

			// ====== Trigger the underlying Agent upon user input ======
			m.agentCtrl.AddUserMessage(v)
			m.streamChan = make(chan agent.AgentEvent, 100)

			return m, tea.Batch(
				func() tea.Msg {
					m.agentCtrl.RunTurn(context.Background(), m.streamChan)
					return nil
				},
				m.waitForStream(),
			)
		}

	// Capture pure Agent Stream events only
	case agent.AgentEvent:
		if msg.Err != nil {
			m.isLoading = false
			m.messages = append(m.messages, chatMessage{role: openai.ChatMessageRoleSystem, content: "Error: " + msg.Err.Error()})
				m.vp.SetContent(renderMessages(m.messages, m.vp.Width))
			return m, nil
		}
		if msg.Done {
			m.isLoading = false
				m.vp.SetContent(renderMessages(m.messages, m.vp.Width))
			return m, nil // Stream pushing is complete
		}

		if msg.ToolCallCreated != nil {
			m.messages = append(m.messages, chatMessage{
				role:      openai.ChatMessageRoleAssistant,
				toolCalls: []openai.ToolCall{*msg.ToolCallCreated},
			})
		} else if msg.ToolCallResult != nil {
			m.messages = append(m.messages, chatMessage{
				role:    openai.ChatMessageRoleTool,
				content: msg.ToolCallResult.Content,
				name:    msg.ToolCallResult.Name,
			})
		} else if msg.DeltaContent != "" {
			lastIdx := len(m.messages) - 1
			if lastIdx >= 0 && m.messages[lastIdx].role == openai.ChatMessageRoleAssistant && len(m.messages[lastIdx].toolCalls) == 0 {
				m.messages[lastIdx].content += msg.DeltaContent
			} else {
				m.messages = append(m.messages, chatMessage{role: openai.ChatMessageRoleAssistant, content: msg.DeltaContent})
			}
		}

			m.vp.SetContent(renderMessages(m.messages, m.vp.Width))
		m.vp.GotoBottom()
		return m, m.waitForStream()
	}

	m.ti, cmd = m.ti.Update(msg)
	cmds = append(cmds, cmd)

	m.vp, cmd = m.vp.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m appModel) waitForStream() tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-m.streamChan
		if !ok {
			return agent.AgentEvent{Done: true}
		}
		return msg
	}
}

func (m appModel) View() string {
	if !m.ready {
		return "\n  Initializing Godex OS..."
	}

	header := titleStyle.Render("╭── GODEX CHAT ENGINE ──╮")
	body := m.vp.View()

	var footer strings.Builder

	// 输入区域顶部分隔线
	sepWidth := m.ti.Width + 4
	if sepWidth < 10 {
		sepWidth = 80
	}
	footer.WriteString(inputBarStyle.Render(strings.Repeat("─", sepWidth)) + "\n")

	if m.isLoading {
		// 加载态：显示 spinner 风格提示
		spinnerLine := promptStyle.Render("❯ ") + agentStyle.Render("Thinking...")
		footer.WriteString(inputBarStyle.Width(sepWidth).Render(spinnerLine))
	} else {
		// 正常态：❯ 提示符 + 输入框，暗色背景条
		inputLine := promptStyle.Render("❯ ") + m.ti.View()
		footer.WriteString(inputBarStyle.Width(sepWidth).Render(inputLine))
	}

	return fmt.Sprintf("%s\n\n%s\n%s", header, body, footer.String())
}

// RunTUI is the single exposed entry point. All TUI logic and styling are encapsulated here.
func RunTUI(agentCtrl *agent.AgentControl) error {
	p := tea.NewProgram(initialModel(agentCtrl), tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}
