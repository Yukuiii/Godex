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
	// 1. Load core environment parameters from local .env
	_ = godotenv.Load()

	// 2. Initialize Model Client network base
	client := llm.NewModelClient()

	// 3. Build local Tool Registry & Router
	registry := tools.NewToolRegistry()
	registry.Register("local_shell", "", handlers.NewShellHandler())
	router := tools.NewToolRouter(registry)

	// 4. Assign gateway and tools to the Agent Controller
	agentCtrl := agent.NewAgentControl(client, router)

	// 5. Start the isolated TUI sandbox engine
	if err := tui.RunTUI(agentCtrl); err != nil {
		fmt.Printf("Godex Engine CLI failed: %v\n", err)
		os.Exit(1)
	}
}
