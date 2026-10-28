package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ChatMessage is one OpenAI-compatible message. Content is either a string or
// an array of content parts for vision requests.
type ChatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// TextMessage builds a plain text message.
func TextMessage(role, text string) ChatMessage { return ChatMessage{Role: role, Content: text} }

// UserWithImages builds a user message containing text plus inline page images.
// Supported image bytes are JPEG, PNG and WebP; unknown bytes are skipped.
func UserWithImages(text string, images [][]byte) ChatMessage {
	parts := []map[string]any{{"type": "text", "text": text}}
	for _, raw := range images {
		mime := detectImageMIME(raw)
		if mime == "" {
			continue
		}
		parts = append(parts, map[string]any{
			"type": "image_url",
			"image_url": map[string]string{
				"url": "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw),
			},
		})
	}
	return ChatMessage{Role: "user", Content: parts}
}

func detectImageMIME(raw []byte) string {
	switch {
	case len(raw) > 3 && raw[0] == 0xFF && raw[1] == 0xD8 && raw[2] == 0xFF:
		return "image/jpeg"
	case len(raw) > 8 && string(raw[:8]) == "\x89PNG\r\n\x1a\n":
		return "image/png"
	case len(raw) > 12 && string(raw[:4]) == "RIFF" && string(raw[8:12]) == "WEBP":
		return "image/webp"
	default:
		return ""
	}
}

// OpenAI is a minimal OpenAI-compatible client. The base URL is the root that
// contains /models and /chat/completions (usually ending in /v1).
type OpenAI struct {
	BaseURL   string
	APIKey    string
	HTTP      *http.Client
	MaxTokens int // 0 leaves the upstream default
}

// NewOpenAI validates the endpoint and returns a client with sane timeouts.
func NewOpenAI(baseURL, apiKey string, client *http.Client) (*OpenAI, error) {
	normalized, err := ValidateBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}
	return &OpenAI{BaseURL: normalized, APIKey: apiKey, HTTP: client}, nil
}

// ValidateBaseURL accepts an absolute http(s) URL without embedded credentials.
func ValidateBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "", errors.New("地址必须是完整的 http(s) URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("地址必须以 http:// 或 https:// 开头")
	}
	if parsed.User != nil {
		return "", errors.New("地址中不能包含内嵌用户名或密码")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("地址不能包含查询参数或片段")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

// Models probes GET {base}/models and returns model ids in upstream order.
func (c *OpenAI) Models(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/models", nil)
	if err != nil {
		return nil, &APIError{Class: "config", Detail: "无法构造模型探测请求"}
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, transportError(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode != http.StatusOK {
		return nil, statusError(res.StatusCode, body, "models")
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, &APIError{Class: "invalid_response", Detail: "模型列表不是合法 JSON"}
	}
	models := make([]string, 0, len(payload.Data))
	seen := map[string]bool{}
	for _, item := range payload.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		models = append(models, id)
	}
	return models, nil
}

// Chat performs one non-streaming chat completion and returns message content.
func (c *OpenAI) Chat(ctx context.Context, model string, messages []ChatMessage) (string, error) {
	if strings.TrimSpace(model) == "" {
		return "", &APIError{Class: "config", Detail: "未选择模型"}
	}
	payload := map[string]any{
		"model":       model,
		"messages":    messages,
		"temperature": 0.1,
	}
	if c.MaxTokens > 0 {
		payload["max_tokens"] = c.MaxTokens
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", &APIError{Class: "config", Detail: "无法构造模型请求"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return "", &APIError{Class: "config", Detail: "无法构造模型请求"}
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return "", transportError(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if res.StatusCode != http.StatusOK {
		return "", statusError(res.StatusCode, body, "chat")
	}
	var completion struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &completion); err != nil || len(completion.Choices) == 0 {
		return "", &APIError{Class: "invalid_response", Detail: "模型响应缺少 choices"}
	}
	content := strings.TrimSpace(string(completion.Choices[0].Message.Content))
	if strings.HasPrefix(content, "\"") {
		var text string
		if err := json.Unmarshal(completion.Choices[0].Message.Content, &text); err == nil {
			content = text
		}
	}
	if content == "" {
		detail := "模型返回空内容"
		if completion.Choices[0].FinishReason == "length" {
			detail += "（输出达到上限）"
		}
		return "", &APIError{Class: "invalid_response", Detail: detail}
	}
	return content, nil
}

func transportError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &APIError{Class: "timeout", Detail: "上游请求超时", Retryable: true}
	}
	return &APIError{Class: "unavailable", Detail: "无法连接上游服务", Retryable: true}
}

func statusError(status int, body []byte, context string) error {
	message := ""
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &payload) == nil {
		message = Sanitize(payload.Error.Message)
	}
	if message == "" {
		message = fmt.Sprintf("上游返回 HTTP %d", status)
	}
	class := "unavailable"
	retryable := status >= 500 || status == http.StatusTooManyRequests
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		class = "auth"
	case http.StatusTooManyRequests:
		class = "rate_limited"
	case http.StatusNotFound, http.StatusBadRequest:
		class = "config"
	}
	return &APIError{Class: class, Detail: context + ": " + message, Retryable: retryable}
}
