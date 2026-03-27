package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"godex/internal/tools"

	openai "github.com/sashabaranov/go-openai"
	"github.com/sashabaranov/go-openai/jsonschema"
)

type FetchArgs struct {
	URL string `json:"url"`
}

// FetchHandler allows the Agent to retrieve web page content for documentation lookups and API references.
type FetchHandler struct{}

func NewFetchHandler() *FetchHandler {
	return &FetchHandler{}
}

func (h *FetchHandler) Kind() tools.ToolKind {
	return tools.ToolKindCustom
}

func (h *FetchHandler) MatchesKind(payload *tools.ToolPayload) bool {
	return true
}

func (h *FetchHandler) PreToolUsePayload(invocation *tools.ToolInvocation) *tools.PreToolUsePayload {
	return &tools.PreToolUsePayload{Command: "fetch"}
}

func (h *FetchHandler) PostToolUsePayload(callID string, payload *tools.ToolPayload, result tools.ToolOutput) *tools.PostToolUsePayload {
	return &tools.PostToolUsePayload{Command: "fetch", ToolResponse: result.ToJSON()}
}

func (h *FetchHandler) GetToolSpec() openai.Tool {
	return openai.Tool{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "fetch",
			Description: "Fetch the content of a web URL and return its text. Use this tool for reading online documentation, API references, or any publicly accessible web page.",
			Parameters: jsonschema.Definition{
				Type: jsonschema.Object,
				Properties: map[string]jsonschema.Definition{
					"url": {Type: jsonschema.String, Description: "The full URL to fetch (must start with http:// or https://)."},
				},
				Required: []string{"url"},
			},
		},
	}
}

func (h *FetchHandler) IsMutating(_ context.Context, _ *tools.ToolInvocation) bool {
	return false
}

// stripHTML removes HTML tags and extracts readable text content.
func stripHTML(raw string) string {
	// Remove script and style blocks entirely
	reScript := regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</\1>`)
	raw = reScript.ReplaceAllString(raw, "")

	// Remove all HTML tags
	reTag := regexp.MustCompile(`<[^>]+>`)
	raw = reTag.ReplaceAllString(raw, " ")

	// Decode common HTML entities
	replacer := strings.NewReplacer(
		"&amp;", "&", "&lt;", "<", "&gt;", ">",
		"&quot;", "\"", "&#39;", "'", "&nbsp;", " ",
	)
	raw = replacer.Replace(raw)

	// Collapse excessive whitespace
	reSpaces := regexp.MustCompile(`[ \t]+`)
	raw = reSpaces.ReplaceAllString(raw, " ")

	// Collapse excessive newlines
	reLines := regexp.MustCompile(`\n{3,}`)
	raw = reLines.ReplaceAllString(raw, "\n\n")

	return strings.TrimSpace(raw)
}

func (h *FetchHandler) Handle(ctx context.Context, invocation *tools.ToolInvocation) (tools.ToolOutput, error) {
	var args FetchArgs
	if err := json.Unmarshal(invocation.Payload.Arguments, &args); err != nil {
		return nil, fmt.Errorf("failed to decode fetch args: %w", err)
	}

	if !strings.HasPrefix(args.URL, "http://") && !strings.HasPrefix(args.URL, "https://") {
		return nil, fmt.Errorf("invalid URL: must start with http:// or https://")
	}

	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, args.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "Godex/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &tools.GenericToolOutput{
			Success: false,
			Data:    []byte(fmt.Sprintf("HTTP %d: %s", resp.StatusCode, resp.Status)),
		}, nil
	}

	// Read with a 512KB cap to avoid memory bombs
	const maxBody = 512 * 1024
	limitedReader := io.LimitReader(resp.Body, maxBody)
	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	content := string(body)

	// Strip HTML if the response is HTML content
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "text/html") {
		content = stripHTML(content)
	}

	// Truncate to protect LLM context window
	const maxChars = 50000
	if len(content) > maxChars {
		content = content[:maxChars] + "\n\n... [Content truncated at 50000 characters]"
	}

	return &tools.GenericToolOutput{
		Success: true,
		Data:    []byte(content),
	}, nil
}
