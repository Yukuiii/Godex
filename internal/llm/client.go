package llm

import (
	"net/http"
	"net/url"
	"os"
	"strings"

	openai "github.com/sashabaranov/go-openai"
)

// ModelClient corresponds to the core long-lived environment gateway (Session-Scoped).
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

// NewSession creates an isolated session boundary for an upcoming business request (Turn-Scoped).
func (c *ModelClient) NewSession() *ModelClientSession {
	return &ModelClientSession{
		client: c,
	}
}
