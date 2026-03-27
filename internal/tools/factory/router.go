package factory

import (
	"godex/internal/tools"
	"godex/internal/tools/handlers"
)

func BuildDefaultRouter() *tools.ToolRouter {
	registry := tools.NewToolRegistry()

	registry.Register("local_shell", "", handlers.NewShellHandler())
	registry.Register("read_file", "", handlers.NewReadFileHandler())
	registry.Register("write_file", "", handlers.NewWriteFileHandler())

	return tools.NewToolRouter(registry)
}
