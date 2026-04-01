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
	"godex/internal/tools"
)

type chatMessage struct {
	role        string
	content     string
	name        string
	isError     bool
	toolCalls   []openai.ToolCall
	displayMeta *tools.ToolDisplayMeta
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
	ti.Prompt = ""
	ti.Placeholder = ""
	ti.Focus()
	ti.CharLimit = 4096
	ti.Width = 80

	return appModel{
		ti:        ti,
		agentCtrl: agentCtrl,
	}
}

func (m appModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		headerHeight := lipgloss.Height(renderTitle()) + 2
		footerHeight := 4

		m.ti.Width = msg.Width - 4

		if !m.ready {
			m.vp = viewport.New(msg.Width, msg.Height-headerHeight-footerHeight)
			m.vp.YPosition = headerHeight
			m.vp.SetContent(renderWelcomeBanner() + renderMessages(m.messages, m.vp.Width))
			m.ready = true
		} else {
			m.vp.Width = msg.Width
			m.vp.Height = msg.Height - headerHeight - footerHeight
		}

	case tea.KeyMsg:
		if msg.Type != tea.KeyCtrlC && m.quitting {
			m.quitting = false
		}

		switch msg.Type {
		case tea.KeyCtrlC:
			if m.quitting {
				return m, tea.Quit
			}
			m.quitting = true
			return m, nil

		case tea.KeyEnter:
			v := strings.TrimSpace(m.ti.Value())
			if v == "" || m.isLoading {
				break
			}

			m.ti.SetValue("")
			m.messages = append(m.messages, chatMessage{role: openai.ChatMessageRoleUser, content: v})
			m.isLoading = true

			m.vp.SetContent(renderWelcomeBanner() + renderMessages(m.messages, m.vp.Width))
			m.vp.GotoBottom()

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

	case agent.AgentEvent:
		if msg.Err != nil {
			m.isLoading = false
			m.messages = append(m.messages, chatMessage{role: openai.ChatMessageRoleSystem, content: "Error: " + msg.Err.Error()})
			m.vp.SetContent(renderWelcomeBanner() + renderMessages(m.messages, m.vp.Width))
			return m, nil
		}
		if msg.Done {
			m.isLoading = false
			m.vp.SetContent(renderWelcomeBanner() + renderMessages(m.messages, m.vp.Width))
			return m, nil
		}

		if msg.ToolCallCreated != nil {
			m.messages = append(m.messages, chatMessage{
				role:      openai.ChatMessageRoleAssistant,
				toolCalls: []openai.ToolCall{*msg.ToolCallCreated},
			})
		} else if msg.ToolCallResult != nil {
			m.messages = append(m.messages, chatMessage{
				role:        openai.ChatMessageRoleTool,
				content:     msg.ToolCallResult.Content,
				name:        msg.ToolCallResult.Name,
				isError:     msg.ToolCallResult.IsError,
				displayMeta: msg.ToolCallResult.DisplayMeta,
			})
		} else if msg.DeltaContent != "" {
			lastIdx := len(m.messages) - 1
			if lastIdx >= 0 && m.messages[lastIdx].role == openai.ChatMessageRoleAssistant && len(m.messages[lastIdx].toolCalls) == 0 {
				m.messages[lastIdx].content += msg.DeltaContent
			} else {
				m.messages = append(m.messages, chatMessage{role: openai.ChatMessageRoleAssistant, content: msg.DeltaContent})
			}
		}

		m.vp.SetContent(renderWelcomeBanner() + renderMessages(m.messages, m.vp.Width))
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

	header := renderTitle()
	body := m.vp.View()

	var footer strings.Builder

	sepWidth := m.ti.Width + 4
	if sepWidth < 10 {
		sepWidth = 80
	}
	footer.WriteString(sepStyle.Render(strings.Repeat("─", sepWidth)) + "\n")

	if m.isLoading {
		footer.WriteString(promptStyle.Render("❯ Thinking..."))
	} else {
		footer.WriteString(promptStyle.Render("❯ ") + m.ti.View())
	}

	if m.quitting {
		footer.WriteString("\n" + systemStyle.Render("  Press Ctrl+C again to exit"))
	}

	return fmt.Sprintf("%s\n\n%s\n%s", header, body, footer.String())
}

// RunTUI is the single exposed entry point.
func RunTUI(agentCtrl *agent.AgentControl) error {
	p := tea.NewProgram(initialModel(agentCtrl), tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}
