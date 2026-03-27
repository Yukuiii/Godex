package main

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"

	"godex/internal/agent"
	"godex/internal/llm"
	"godex/internal/tools"
	"godex/internal/tools/handlers"
	"godex/pkg/tui"
)

func main() {
	// 1. 加载本级目录下的核心机密参数（.env）
	_ = godotenv.Load()

	// 2. 初始化网络大本营（Model Client）
	client := llm.NewModelClient()

	// 3. 构建本地底层的火力库（Tool Registry & Router）
	registry := tools.NewToolRegistry()
	registry.Register("local_shell", "", handlers.NewShellHandler())
	router := tools.NewToolRouter(registry)

	// 4. 将网关和武器库赋予顶层 AI 大使（Agent Controller）
	agentCtrl := agent.NewAgentControl(client, router)

	// 5. 组装完毕启动纯粹隔离的绘制沙箱引擎 (TUI View)
	if err := tui.RunTUI(agentCtrl); err != nil {
		fmt.Printf("Godex Engine CLI failed: %v\n", err)
		os.Exit(1)
	}
}
