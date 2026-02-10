package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// APIClient 封装 OpenAI 兼容 API 调用（支持 Deepseek、OpenAI、本地 Ollama 等）
type APIClient struct {
	BaseURL string
	APIKey  string
	Model   string
	Client  *http.Client
}

// ChatMessage 聊天消息
type ChatMessage struct {
	Role    string `json:"role"` // system, user, assistant
	Content string `json:"content"`
}

// ChatRequest OpenAI Chat Completion 请求
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Temperature float64       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Stream      bool          `json:"stream"`
}

// ChatResponse OpenAI Chat Completion 响应
type ChatResponse struct {
	ID      string `json:"id"`
	Choices []struct {
		Message      ChatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// StreamChunk SSE 流式响应块
type StreamChunk struct {
	ID      string `json:"id"`
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// NewAPIClient 创建 API 客户端
// 自动从环境变量读取配置:
//   - DEEPSEEK_API_KEY / OPENAI_API_KEY
//   - DEEPSEEK_BASE_URL / OPENAI_BASE_URL
//   - DEEPSEEK_MODEL / OPENAI_MODEL
func NewAPIClient() *APIClient {
	client := &APIClient{
		Client: &http.Client{Timeout: 120 * time.Second},
	}

	// 优先 Deepseek
	if key := os.Getenv("DEEPSEEK_API_KEY"); key != "" {
		client.APIKey = key
		client.BaseURL = getEnvOr("DEEPSEEK_BASE_URL", "https://api.deepseek.com/v1")
		client.Model = getEnvOr("DEEPSEEK_MODEL", "deepseek-chat")
		return client
	}

	// 其次 OpenAI
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		client.APIKey = key
		client.BaseURL = getEnvOr("OPENAI_BASE_URL", "https://api.openai.com/v1")
		client.Model = getEnvOr("OPENAI_MODEL", "gpt-4o-mini")
		return client
	}

	// 本地 Ollama 兜底（无需 API Key）
	client.BaseURL = getEnvOr("OLLAMA_BASE_URL", "http://localhost:11434/v1")
	client.Model = getEnvOr("OLLAMA_MODEL", "qwen2.5:7b")

	return client
}

// IsConfigured 检查是否已配置 API
func (c *APIClient) IsConfigured() bool {
	return c.APIKey != "" || c.BaseURL == "http://localhost:11434/v1"
}

// Provider 返回当前使用的提供商名称
func (c *APIClient) Provider() string {
	if key := os.Getenv("DEEPSEEK_API_KEY"); key != "" {
		return "Deepseek"
	}
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		return "OpenAI"
	}
	return "Ollama (local)"
}

// Chat 发送聊天请求（非流式）
func (c *APIClient) Chat(messages []ChatMessage, temperature float64, maxTokens int) (string, error) {
	req := ChatRequest{
		Model:       c.Model,
		Messages:    messages,
		Temperature: temperature,
		MaxTokens:   maxTokens,
		Stream:      false,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.Client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("no response choices")
	}

	return chatResp.Choices[0].Message.Content, nil
}

// ChatStream 发送流式聊天请求，通过 callback 逐块输出
func (c *APIClient) ChatStream(messages []ChatMessage, temperature float64, maxTokens int, onChunk func(string)) (string, error) {
	req := ChatRequest{
		Model:       c.Model,
		Messages:    messages,
		Temperature: temperature,
		MaxTokens:   maxTokens,
		Stream:      true,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	// 流式请求不设超时
	streamClient := &http.Client{}
	resp, err := streamClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var fullContent string
	buf := make([]byte, 4096)
	var remainder string

	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			data := remainder + string(buf[:n])
			remainder = ""

			lines := splitSSELines(data)
			for _, line := range lines {
				if line == "" || line == "data: [DONE]" {
					continue
				}

				if len(line) > 6 && line[:6] == "data: " {
					jsonStr := line[6:]
					var chunk StreamChunk
					if err := json.Unmarshal([]byte(jsonStr), &chunk); err != nil {
						// 可能是不完整的 JSON，保存到 remainder
						remainder = line + "\n"
						continue
					}
					if len(chunk.Choices) > 0 {
						content := chunk.Choices[0].Delta.Content
						if content != "" {
							fullContent += content
							if onChunk != nil {
								onChunk(content)
							}
						}
					}
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return fullContent, nil // 流正常结束
		}
	}

	return fullContent, nil
}

// splitSSELines 分割 SSE 数据行
func splitSSELines(data string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			line := data[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lines = append(lines, line)
			start = i + 1
		}
	}
	// 保留未完成的行
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}

func getEnvOr(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
