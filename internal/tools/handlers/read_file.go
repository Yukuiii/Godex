package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"godex/internal/tools"

	openai "github.com/sashabaranov/go-openai"
	"github.com/sashabaranov/go-openai/jsonschema"
)

// 大文件保护：超过此大小使用流式读取，避免 OOM
const maxFastReadSize = 10 * 1024 * 1024 // 10 MB

// 默认最大返回行数，防止撑爆上下文窗口
const maxOutputLines = 2000

// 文件大小上限，超过则拒绝全量读取并引导使用 offset/limit
const maxFileSizeBytes = 256 * 1024 // 256 KB

// 会导致进程挂起的设备文件黑名单
var blockedDevicePaths = map[string]bool{
	"/dev/zero": true, "/dev/random": true, "/dev/urandom": true, "/dev/full": true,
	"/dev/stdin": true, "/dev/tty": true, "/dev/console": true,
	"/dev/stdout": true, "/dev/stderr": true,
	"/dev/fd/0": true, "/dev/fd/1": true, "/dev/fd/2": true,
}

// 无法有意义读取的二进制文件扩展名
var binaryExtensions = map[string]bool{
	".exe": true, ".dll": true, ".so": true, ".dylib": true, ".bin": true,
	".o": true, ".a": true, ".class": true, ".pyc": true, ".pyo": true,
	".zip": true, ".tar": true, ".gz": true, ".bz2": true, ".xz": true, ".7z": true, ".rar": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".bmp": true, ".ico": true, ".webp": true,
	".mp3": true, ".mp4": true, ".avi": true, ".mov": true, ".wav": true, ".flac": true,
	".woff": true, ".woff2": true, ".ttf": true, ".otf": true, ".eot": true,
	".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true, ".ppt": true, ".pptx": true,
	".sqlite": true, ".db": true, ".DS_Store": true,
}

type ReadFileArgs struct {
	AbsolutePath string `json:"absolute_path"`
	Offset       *int   `json:"offset,omitempty"`
	Limit        *int   `json:"limit,omitempty"`
}

type ReadFileHandler struct{}

// NewReadFileHandler creates a handler for reading local files safely.
func NewReadFileHandler() *ReadFileHandler {
	return &ReadFileHandler{}
}

func (h *ReadFileHandler) Kind() tools.ToolKind {
	return tools.ToolKindCustom
}

func (h *ReadFileHandler) MatchesKind(payload *tools.ToolPayload) bool {
	return true
}

func (h *ReadFileHandler) PreToolUsePayload(invocation *tools.ToolInvocation) *tools.PreToolUsePayload {
	var args struct {
		AbsolutePath string `json:"absolute_path"`
	}
	_ = json.Unmarshal(invocation.Payload.Arguments, &args)
	return &tools.PreToolUsePayload{Command: "read_file", FilePath: args.AbsolutePath}
}

func (h *ReadFileHandler) PostToolUsePayload(callID string, payload *tools.ToolPayload, result tools.ToolOutput) *tools.PostToolUsePayload {
	return &tools.PostToolUsePayload{Command: "read_file", ToolResponse: result.ToJSON()}
}

func (h *ReadFileHandler) GetToolSpec() openai.Tool {
	return openai.Tool{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "read_file",
			Description: "Read the content of a specified file on the local filesystem. Outputs are prefixed with line numbers (cat -n format). By default reads up to 2000 lines from the beginning. Use offset and limit for large files.",
			Parameters: jsonschema.Definition{
				Type: jsonschema.Object,
				Properties: map[string]jsonschema.Definition{
					"absolute_path": {Type: jsonschema.String, Description: "The absolute path to the file to read."},
					"offset":        {Type: jsonschema.Integer, Description: "The line number to start reading from (1-indexed). Only provide if the file is too large to read at once."},
					"limit":         {Type: jsonschema.Integer, Description: "The number of lines to read. Only provide if the file is too large to read at once."},
				},
				Required: []string{"absolute_path"},
			},
		},
	}
}

// IsMutating establishes whether executing this tool has side effects.
func (h *ReadFileHandler) IsMutating(_ context.Context, _ *tools.ToolInvocation) bool {
	return false
}

// Handle performs the logic of parsing and reading the target file payload.
func (h *ReadFileHandler) Handle(ctx context.Context, invocation *tools.ToolInvocation) (tools.ToolOutput, error) {
	var args ReadFileArgs
	if err := json.Unmarshal(invocation.Payload.Arguments, &args); err != nil {
		return nil, fmt.Errorf("failed to parse read_file arguments: %w", err)
	}

	if args.AbsolutePath == "" {
		return nil, fmt.Errorf("argument 'absolute_path' is required")
	}

	filePath := args.AbsolutePath

	// 安全检查：阻断会导致挂起的设备文件
	if blockedDevicePaths[filePath] {
		return nil, fmt.Errorf("cannot read '%s': this device file would block or produce infinite output", filePath)
	}

	// 安全检查：拒绝二进制文件
	ext := strings.ToLower(filepath.Ext(filePath))
	if binaryExtensions[ext] {
		return nil, fmt.Errorf("cannot read binary file '%s' (%s). Use appropriate tools for binary file analysis", filepath.Base(filePath), ext)
	}

	// 获取文件信息
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file '%s': %w", filePath, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("'%s' is a directory, not a file. Use local_shell with 'ls' to list directory contents", filePath)
	}

	// 解析 offset/limit 参数
	offset := 1
	if args.Offset != nil && *args.Offset >= 1 {
		offset = *args.Offset
	}

	limit := maxOutputLines // 默认最多读取 maxOutputLines 行
	hasExplicitLimit := false
	if args.Limit != nil && *args.Limit > 0 {
		limit = *args.Limit
		hasExplicitLimit = true
	}

	// 大文件保护：无显式 offset/limit 时，检查文件大小
	if !hasExplicitLimit && offset == 1 && info.Size() > maxFileSizeBytes {
		return nil, fmt.Errorf(
			"file '%s' (%s) exceeds maximum allowed size (%s). Use offset and limit parameters to read specific portions of the file, or use local_shell with grep to search for specific content",
			filepath.Base(filePath), formatSize(info.Size()), formatSize(maxFileSizeBytes),
		)
	}

	// 截断 limit 到上限
	truncated := false
	if limit > maxOutputLines {
		limit = maxOutputLines
		truncated = true
	}

	// 根据文件大小选择读取策略
	var lines []string
	var totalLines int

	if info.Size() < maxFastReadSize {
		// Fast path: 小文件一次性读入内存
		lines, totalLines, err = readFileFast(filePath)
	} else {
		// Streaming path: 大文件流式读取
		lines, totalLines, err = readFileStreaming(filePath, offset, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read file '%s': %w", filePath, err)
	}

	// 计算实际输出范围 (offset 是 1-indexed)
	startIdx := offset - 1 // 转为 0-indexed
	if startIdx >= totalLines {
		return nil, fmt.Errorf("offset %d exceeds the file's total line count (%d). The file has %d lines", offset, totalLines, totalLines)
	}
	if startIdx < 0 {
		startIdx = 0
	}

	endIdx := startIdx + limit
	if endIdx > totalLines {
		endIdx = totalLines
	}

	// 流式读取已经只返回目标范围，快速读取需要切片
	var selectedLines []string
	if info.Size() < maxFastReadSize {
		if endIdx > len(lines) {
			endIdx = len(lines)
		}
		selectedLines = lines[startIdx:endIdx]
	} else {
		selectedLines = lines
	}

	// 如果切片后的行数仍超限，截断
	if len(selectedLines) > maxOutputLines {
		selectedLines = selectedLines[:maxOutputLines]
		truncated = true
	}

	// 构建输出 (cat -n 格式)
	var builder strings.Builder
	for i, line := range selectedLines {
		lineNum := offset + i
		builder.WriteString(fmt.Sprintf("%d\t%s\n", lineNum, line))
	}

	// 追加状态提示
	outputLineCount := len(selectedLines)
	if truncated {
		builder.WriteString(fmt.Sprintf(
			"\n[Notice: Output truncated at %d lines. Use 'offset' and 'limit' parameters to read other portions of the file. Total lines in file: %d]\n",
			maxOutputLines, totalLines,
		))
	} else if offset > 1 || endIdx < totalLines {
		builder.WriteString(fmt.Sprintf(
			"\n[Showing lines %d-%d of %d total lines in '%s']\n",
			offset, offset+outputLineCount-1, totalLines, filepath.Base(filePath),
		))
	}

	return &tools.GenericToolOutput{
		Success: true,
		Data:    []byte(builder.String()),
	}, nil
}

// readFileFast 一次性读入文件，适用于 < 10MB 的文件。
// 自动处理 UTF-8 BOM 和 CRLF 换行符。
func readFileFast(filePath string) (lines []string, totalLines int, err error) {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return nil, 0, err
	}

	text := string(raw)

	// 去除 UTF-8 BOM
	if strings.HasPrefix(text, "\xEF\xBB\xBF") {
		text = text[3:]
	}

	// CRLF → LF
	text = strings.ReplaceAll(text, "\r\n", "\n")
	// 处理单独的 \r (旧 Mac 风格)
	text = strings.ReplaceAll(text, "\r", "\n")

	lines = strings.Split(text, "\n")
	return lines, len(lines), nil
}

// readFileStreaming 流式读取大文件，只保留目标范围内的行，
// 范围外的行只计数不存储，防止 OOM。
func readFileStreaming(filePath string, offset, limit int) (lines []string, totalLines int, err error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// 增大 buffer 以处理超长行
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	startIdx := offset - 1 // 0-indexed
	endIdx := startIdx + limit
	lineIdx := 0
	selectedLines := make([]string, 0, limit)

	for scanner.Scan() {
		if lineIdx >= startIdx && lineIdx < endIdx {
			line := scanner.Text()
			// 去除 BOM (仅首行)
			if lineIdx == 0 {
				line = stripBOM(line)
			}
			selectedLines = append(selectedLines, line)
		}
		lineIdx++
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}

	return selectedLines, lineIdx, nil
}

// stripBOM 去除字符串开头的 UTF-8 BOM。
func stripBOM(s string) string {
	if r, size := utf8.DecodeRuneInString(s); r == '\uFEFF' {
		return s[size:]
	}
	return s
}



