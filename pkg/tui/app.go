package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	openai "github.com/sashabaranov/go-openai"

	"godex/internal/agent"
	"godex/internal/tools"
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

	promptStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("208")).
			Bold(true)

	sepStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("238"))

	toolErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	mascotStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("208"))

	versionStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))
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

func renderTitle() string {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).Render("── Godex ") +
		versionStyle.Render("v0.1.0 ──")
}

func renderWelcomeBanner() string {
	mascot := mascotStyle.Render(strings.Join([]string{
		`    ██████████████`,
		`  ██                ██`,
		`  ██  ███    ███  ██`,
		`  ██  ███    ███  ██`,
		`  ██                ██`,
		`  ████  ██████  ████`,
		`      ████████████`,
		`    ██████████████`,
	}, "\n"))

	info := lipgloss.JoinVertical(lipgloss.Left,
		userStyle.Render("Welcome to Godex!"),
		"",
		systemStyle.Render("Tips:"),
		systemStyle.Render("  Enter to send message"),
		systemStyle.Render("  Ctrl+C twice to exit"),
		systemStyle.Render("  PgUp/PgDn to scroll"),
	)

	return lipgloss.JoinHorizontal(lipgloss.Center, mascot, "    ", info) + "\n\n"
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

func renderMessages(messages []chatMessage, width int) string {
	var s strings.Builder
	if width < 10 {
		width = 80
	}

	wrapStyle := lipgloss.NewStyle().Width(width - 4)

	toolDotStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	toolNameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	toolFileStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	toolInfoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

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
					isErr := false
					var meta *tools.ToolDisplayMeta
					for j := i + 1; j < len(messages); j++ {
						if messages[j].role == openai.ChatMessageRoleTool && messages[j].name == call.Function.Name {
							isDone = true
							isErr = messages[j].isError
							meta = messages[j].displayMeta
							break
						}
					}

					line := renderToolCallLine(call, isDone, isErr, meta, toolDotStyle, toolNameStyle, toolFileStyle, toolInfoStyle)
					s.WriteString(wrapStyle.Render(line) + "\n")
				}
				s.WriteString("\n")
			}

		case openai.ChatMessageRoleTool:
			if msg.isError && msg.content != "" {
				s.WriteString(wrapStyle.Render(toolErrorStyle.Render(fmt.Sprintf("  ✘ [%s] %s", msg.name, msg.content))) + "\n")
			}

		case openai.ChatMessageRoleSystem:
			s.WriteString(wrapStyle.Render(systemStyle.Render(" > " + msg.content)) + "\n\n")
		}
	}
	return s.String()
}

// renderToolCallLine 为单个 tool call 生成富格式的显示行。
// 有 DisplayMeta 时显示为 "● Read filename — N lines read"。
func renderToolCallLine(
	call openai.ToolCall,
	isDone, isErr bool,
	meta *tools.ToolDisplayMeta,
	dotStyle, nameStyle, fileStyle, infoStyle lipgloss.Style,
) string {
	if isDone && isErr {
		return toolErrorStyle.Render(fmt.Sprintf("  ✘ [%s] error", call.Function.Name))
	}

	// 有 DisplayMeta 时使用富格式
	if meta != nil {
		dot := dotStyle.Render("●")
		label := nameStyle.Render(meta.Label)
		file := fileStyle.Render(filepath.Base(meta.FilePath))

		if !isDone {
			return fmt.Sprintf("  %s %s %s", dot, label, file)
		}
		info := infoStyle.Render("— " + meta.Summary)
		return fmt.Sprintf("  %s %s %s %s", dot, label, file, info)
	}

	// 无 DisplayMeta 时使用默认格式
	if isDone {
		return systemStyle.Render(fmt.Sprintf("  [%s] done", call.Function.Name))
	}
	return systemStyle.Render(fmt.Sprintf("  [%s]...", call.Function.Name))
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
			m.quitting = false // Reset quit state if user types something else
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
				m.vp.SetContent(renderWelcomeBanner() + renderMessages(m.messages, m.vp.Width))
			return m, nil
		}
		if msg.Done {
			m.isLoading = false
				m.vp.SetContent(renderWelcomeBanner() + renderMessages(m.messages, m.vp.Width))
			return m, nil // Stream pushing is complete
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

// RunTUI is the single exposed entry point. All TUI logic and styling are encapsulated here.
func RunTUI(agentCtrl *agent.AgentControl) error {
	p := tea.NewProgram(initialModel(agentCtrl), tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}
