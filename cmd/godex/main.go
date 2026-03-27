package main

import (
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// 各种赛博霓虹风格样式定义
var (
	// 整个 TUI 外边框：紫粉色渐变圆角
	borderStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")). // 紫色
		Padding(1, 4).
		Margin(1, 2).
		Align(lipgloss.Center)

	// 夸张的主标题
	titleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("205")). // 亮粉色
		MarginBottom(1)

	// 子系统文字提示
	subtitleStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")) // 幽灵灰

	// Agent 动态输出字体的赛博绿
	agentStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("78")). // 荧光绿
		Italic(true)
)

// ------------------------------
// Elm 架构: 1. Model (数据状态)
// ------------------------------
type model struct {
	spinner  spinner.Model
	loading  bool
	messages []string // 伪装的系统加载步骤
	quitting bool
}

// 定义一个自定义的时钟滴答消息
type tickMsg time.Time

func initialModel() model {
	// 初始化一个好看的圆点旋转动画
	s := spinner.New()
	s.Spinner = spinner.Points
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	return model{
		spinner: s,
		loading: true,
		messages: []string{
			"Godex Engine Initializing...",
			"Loading Core Protocols (Op, Event)...",
			"Establishing Sub-Agent Channels...",
			"Connecting to Neural Nexus via sashabaranov/go-openai...",
			"Agent Registry Prepared...",
		},
	}
}

// ------------------------------
// Elm 架构: 2. Init (初始化命令)
// ------------------------------
func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick, // 启动旋转器
		tickCmd(),      // 启动步进计时器
	)
}

// 每隔 800 毫秒跳动一次
func tickCmd() tea.Cmd {
	return tea.Tick(time.Millisecond*800, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// ------------------------------
// Elm 架构: 3. Update (事件更新机)
// ------------------------------
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	// 处理键盘输入事件
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "enter":
			// 为后续接入大模型输入预留扩展点
		}

	// 处理步进倒计时事件
	case tickMsg:
		if len(m.messages) > 1 {
			// 扔掉第一条消息，显示下一条
			m.messages = m.messages[1:]
			return m, tickCmd()
		}
		// 加载完毕
		m.loading = false
		m.messages = []string{"Godex Orchestrator 启动完毕！正在等待指令接入..."}

	// 处理旋转动画帧事件
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

// ------------------------------
// Elm 架构: 4. View (视图渲染)
// ------------------------------
func (m model) View() string {
	if m.quitting {
		return "\n  Godex Engine 已安全下线，再见！✨\n"
	}

	header := titleStyle.Render("🔮 GODEX MULTI-AGENT ENGINE 🔮")

	var status string
	if m.loading {
		// 拼接 Spinner 动画和加载文字
		status = fmt.Sprintf("%s %s", m.spinner.View(), agentStyle.Render(m.messages[0]))
	} else {
		// 稳态显示文字，并引导退出键
		status = agentStyle.Render(m.messages[0]) + "\n\n" + subtitleStyle.Render("(Press 'q' or 'esc' to exit)")
	}

	// 上下垂直居中排版对齐
	content := lipgloss.JoinVertical(
		lipgloss.Center,
		header,
		status,
	)

	// 套上我们设计好的绝美霓虹边框渲染输出
	return borderStyle.Render(content)
}

func main() {
	// 创建程序实例并开启备用屏幕模式 (类似于 vim 的整屏独占)
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("啊呀，引擎启动失败了: %v", err)
		os.Exit(1)
	}
}
