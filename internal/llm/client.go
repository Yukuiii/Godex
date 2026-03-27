package llm

import (
	"net/http"
	"net/url"
	"os"
	"strings"

	openai "github.com/sashabaranov/go-openai"
)

// ModelClient 对应 Codex 中的核心长链接/环境网关 (Session-Scoped)
// 包含稳定的环境配置（Auth, 端点，代理隧道等），伴随 Godex 引擎整个生命周期
type ModelClient struct {
	APIClient *openai.Client
	ModelName string
}

// NewModelClient 从外部环境中聚合依赖，返回一个稳定的通信网关
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

// NewSession 为即将到来的一次业务请求（Turn-Scoped）创建一个独立的会话边界
func (c *ModelClient) NewSession() *ModelClientSession {
	return &ModelClientSession{
		client: c,
	}
}
