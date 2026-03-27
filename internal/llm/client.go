package llm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	openai "github.com/sashabaranov/go-openai"
	"github.com/sashabaranov/go-openai/jsonschema"
)

// ModelClient represents the core long-lived environment gateway (Session-Scoped).
// It contains stable environment configurations (Auth, endpoint, proxy, etc.) living alongside the Godex engine.
type ModelClient struct {
	APIClient *openai.Client
	ModelName string
}

// NewModelClient aggregates dependencies from the external environment and returns a stable communication gateway.
func NewModelClient() *ModelClient {
	apiKey := strings.TrimSpace(os.Getenv("API_KEY"))
	baseURL := strings.TrimSpace(os.Getenv("BASE_URL"))
	modelName := strings.TrimSpace(os.Getenv("MODEL"))
	proxyURLStr := strings.TrimSpace(os.Getenv("PROXY_URL"))

	if modelName == "" {
		modelName = "openai/gpt-3.5-turbo"
	}

	var client *openai.Client
	if apiKey != "" {
		config := openai.DefaultConfig(apiKey)
		if baseURL != "" {
			config.BaseURL = baseURL
		}
		config.APIType = openai.APITypeOpenAI

		if proxyURLStr == "" {
			proxyURLStr = "http://127.0.0.1:7890"
		}
		if parsedURL, err := url.Parse(proxyURLStr); err == nil {
			transport := &http.Transport{
				Proxy: http.ProxyURL(parsedURL),
			}
			config.HTTPClient = &http.Client{
				Transport: transport,
			}
		}

		client = openai.NewClientWithConfig(config)
	}

	return &ModelClient{
		APIClient: client,
		ModelName: modelName,
	}
}

// StreamEvent is a pure, unpolluted underlying network exposure model.
type StreamEvent struct {
	DeltaContent    string
	ToolCallCreated *openai.ToolCall
	Done            bool
	Err             error
}

// Stream initiates an unpolluted isolated underlying request engine channel.
// It handles the underlying reconstruction of JSON fragments per LLM dialogue, without any business logic scheduling.
func (c *ModelClient) Stream(ctx context.Context, apiMessages []openai.ChatCompletionMessage) (<-chan StreamEvent, error) {
	out := make(chan StreamEvent, 100)

	if c.APIClient == nil {
		return nil, errors.New("Missing API_KEY in Godex Environment")
	}

	// Initialize the structure containing Tool Calling abstraction capabilities
	req := openai.ChatCompletionRequest{
		Model:    c.ModelName,
		Messages: apiMessages,
		Stream:   true,
		Tools: []openai.Tool{
			{
				Type: openai.ToolTypeFunction,
				Function: &openai.FunctionDefinition{
					Name:        "local_shell",
					Description: "Execute a strictly un-interactive command on the host OS shell.",
					Parameters: jsonschema.Definition{
						Type: jsonschema.Object,
						Properties: map[string]jsonschema.Definition{
							"command":    {Type: jsonschema.String, Description: "shell command"},
							"workdir":    {Type: jsonschema.String},
							"timeout_ms": {Type: jsonschema.Integer},
						},
						Required: []string{"command"},
					},
				},
			},
		},
	}

	stream, err := c.APIClient.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return nil, err
	}

	// Reconstruct model network slices together via a Goroutine and return
	go func() {
		defer close(out)
		defer stream.Close()

		toolCallMap := make(map[int]openai.ToolCall)

		for {
			resp, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				out <- StreamEvent{Err: err}
				return
			}

			if len(resp.Choices) > 0 {
				delta := resp.Choices[0].Delta

				if delta.Content != "" {
					out <- StreamEvent{DeltaContent: delta.Content}
				}

				for _, tChunk := range delta.ToolCalls {
					idx := 0
					if tChunk.Index != nil {
						idx = *tChunk.Index
					}

					existing, ok := toolCallMap[idx]
					if !ok {
						existing = tChunk
					} else {
						if tChunk.ID != "" {
							existing.ID = tChunk.ID
						}
						if tChunk.Function.Name != "" {
							existing.Function.Name = tChunk.Function.Name
						}
						existing.Function.Arguments += tChunk.Function.Arguments
					}
					toolCallMap[idx] = existing
				}
			}
		}

		// Methodically yield all fully assembled tool entities
		if len(toolCallMap) > 0 {
			maxIdx := -1
			for idx := range toolCallMap {
				if idx > maxIdx {
					maxIdx = idx
				}
			}
			for i := 0; i <= maxIdx; i++ {
				if tc, ok := toolCallMap[i]; ok {
					tcClone := tc
					out <- StreamEvent{ToolCallCreated: &tcClone}
				}
			}
		}

		out <- StreamEvent{Done: true}
	}()

	return out, nil
}
