package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	openai "github.com/sashabaranov/go-openai"

	"godex/internal/tools"
)

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

func renderMessages(messages []chatMessage, width int) string {
	var s strings.Builder
	if width < 10 {
		width = 80
	}

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

					line := renderToolCallLine(call, isDone, isErr, meta)
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
) string {
	if isDone && isErr {
		return toolErrorStyle.Render(fmt.Sprintf("  ✘ [%s] error", call.Function.Name))
	}

	if meta != nil {
		dot := toolDotStyle.Render("●")
		label := toolNameStyle.Render(meta.Label)
		file := toolFileStyle.Render(filepath.Base(meta.FilePath))

		if !isDone {
			return fmt.Sprintf("  %s %s %s", dot, label, file)
		}
		info := toolInfoStyle.Render("— " + meta.Summary)
		return fmt.Sprintf("  %s %s %s %s", dot, label, file, info)
	}

	if isDone {
		return systemStyle.Render(fmt.Sprintf("  [%s] done", call.Function.Name))
	}
	return systemStyle.Render(fmt.Sprintf("  [%s]...", call.Function.Name))
}
