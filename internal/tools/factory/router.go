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
	
	// 扩展高级代码检索引擎套件：避免 AI 在庞大工程里失去环境视距
	registry.Register("list_dir", "", handlers.NewListDirHandler())
	registry.Register("glob", "", handlers.NewGlobHandler())
	registry.Register("search_code", "", handlers.NewSearchCodeHandler())

	// 最强安全写码核心：允许大模型局部精准打补丁，杜绝全文大段重写造成的灾难
	registry.Register("edit_file", "", handlers.NewEditFileHandler())

	return tools.NewToolRouter(registry)
}
