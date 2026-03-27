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
)

type chatMessage struct {
	role      string
	content   string
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
}

func initialModel(agentCtrl *agent.AgentControl) appModel {
	ti := textinput.New()
	ti.Placeholder = "Type your command to Godex..."
	ti.Focus()
	ti.CharLimit = 512
	ti.Width = 60

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

	for _, msg := range messages {
		switch msg.role {
		case openai.ChatMessageRoleUser:
			s.WriteString(wrapStyle.Render(userStyle.Render("You: ") + msg.content) + "\n\n")

		case openai.ChatMessageRoleAssistant:
			if msg.content != "" {
				s.WriteString(wrapStyle.Render(agentStyle.Render("Godex: ") + msg.content) + "\n\n")
			}
			if len(msg.toolCalls) > 0 {
				for _, call := range msg.toolCalls {
					s.WriteString(wrapStyle.Render(systemStyle.Render(fmt.Sprintf("  [Tool] Using ➔ %s", call.Function.Name))) + "\n")
				}
				s.WriteString("\n")
			}

		case openai.ChatMessageRoleTool:
			s.WriteString(wrapStyle.Render(systemStyle.Render("  [Success] System Job Completed")) + "\n\n")

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
		footerHeight := 2                                                                   // Footer lines

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
	if m.isLoading {
		footer.WriteString(agentStyle.Render("Godex Sub-Agent loop running..."))
	} else {
		footer.WriteString(m.ti.View() + "\n")
		footer.WriteString(systemStyle.Render("  [Enter: Send]  [Esc: Quit]  [PgUp/PgDn: Scroll]"))
	}

	return fmt.Sprintf("%s\n\n%s\n%s", header, body, footer.String())
}

// RunTUI is the single exposed entry point. All TUI logic and styling are encapsulated here.
func RunTUI(agentCtrl *agent.AgentControl) error {
	p := tea.NewProgram(initialModel(agentCtrl), tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}
