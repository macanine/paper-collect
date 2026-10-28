package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	paddleMaxResultBytes = 64 << 20
	paddleMaxJSONLLines  = 1000
)

// PaddleOCR uses Baidu AI Studio's asynchronous PaddleOCR job API.
// PP-OCRv6 currently shares the documented v5-compatible job protocol.
type PaddleOCR struct {
	BaseURL      string
	Token        string
	Model        string
	HTTP         *http.Client
	PollInterval time.Duration
	Timeout      time.Duration
}

// NewPaddleOCR validates the endpoint and applies defaults.
func NewPaddleOCR(baseURL, token, model string, client *http.Client) (*PaddleOCR, error) {
	normalized, err := ValidateBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = "PP-OCRv6"
	}
	if client == nil {
		// Extract and Health impose operation-wide context deadlines. A client
		// timeout here would also abort large uploads before the job deadline.
		client = &http.Client{}
	}
	return &PaddleOCR{
		BaseURL: normalized, Token: strings.TrimSpace(token), Model: model,
		HTTP: client, PollInterval: 3 * time.Second, Timeout: 15 * time.Minute,
	}, nil
}

// Health verifies that the token is accepted without uploading a document.
func (p *PaddleOCR) Health(ctx context.Context) error {
	if p.Token == "" {
		return &APIError{Class: "config", Detail: "未配置 PaddleOCR 令牌"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		p.BaseURL+"/api/v2/ocr/jobs/00000000-0000-0000-0000-000000000000", nil)
	if err != nil {
		return &APIError{Class: "config", Detail: "无法构造 PaddleOCR 请求"}
	}
	req.Header.Set("Authorization", "Bearer "+p.Token)
	res, err := p.HTTP.Do(req)
	if err != nil {
		return transportError(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return &APIError{Class: "auth", Detail: "PaddleOCR 令牌被拒绝"}
	}
	if res.StatusCode >= 500 {
		return &APIError{Class: "unavailable", Detail: fmt.Sprintf("PaddleOCR 返回 HTTP %d", res.StatusCode), Retryable: true}
	}
	// A 404/10001 response for the synthetic job id proves reachability and
	// token acceptance. A 200 is also accepted for compatible implementations.
	var reply struct {
		Code int `json:"code"`
	}
	_ = json.Unmarshal(body, &reply)
	if reply.Code == 401 {
		return &APIError{Class: "auth", Detail: "PaddleOCR 令牌被拒绝"}
	}
	return nil
}

// Extract uploads one local file, polls the asynchronous job and converts the
// JSONL OCR result into the application's page-oriented result.
func (p *PaddleOCR) Extract(ctx context.Context, filename string, data []byte) (Result, error) {
	if p.Token == "" {
		return Result{}, &APIError{Class: "config", Detail: "未配置 PaddleOCR 令牌"}
	}
	if len(data) == 0 {
		return Result{}, &APIError{Class: "config", Detail: "文件内容为空"}
	}
	if p.PollInterval <= 0 {
		p.PollInterval = 3 * time.Second
	}
	if p.Timeout <= 0 {
		p.Timeout = 15 * time.Minute
	}
	deadline := time.Now().Add(p.Timeout)
	jobID, err := p.submit(ctx, filename, data)
	if err != nil {
		return Result{}, err
	}
	var jsonURL string
	for {
		if time.Now().After(deadline) {
			return Result{}, &APIError{Class: "timeout", Detail: "PaddleOCR 解析超时", Retryable: true}
		}
		state, resultURL, errMsg, err := p.poll(ctx, jobID)
		if err != nil {
			return Result{}, err
		}
		switch state {
		case "done":
			if resultURL == "" {
				return Result{}, &APIError{Class: "invalid_response", Detail: "PaddleOCR 完成但未返回结果地址"}
			}
			jsonURL = resultURL
		case "failed":
			if errMsg == "" {
				errMsg = "PaddleOCR 解析失败"
			}
			return Result{}, &APIError{Class: "unavailable", Detail: Sanitize(errMsg)}
		case "pending", "running", "":
			// Keep polling until done/failed or the local deadline.
		default:
			// Unknown states are treated as running; the deadline limits the wait.
		}
		if jsonURL != "" {
			break
		}
		select {
		case <-ctx.Done():
			return Result{}, &APIError{Class: "timeout", Detail: "PaddleOCR 解析被取消", Retryable: true}
		case <-time.After(p.PollInterval):
		}
	}
	return p.downloadResult(ctx, jsonURL)
}

func (p *PaddleOCR) submit(ctx context.Context, filename string, data []byte) (string, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return "", &APIError{Class: "config", Detail: "无法构造 PaddleOCR 上传请求"}
	}
	if _, err := part.Write(data); err != nil {
		return "", &APIError{Class: "config", Detail: "无法读取上传文件"}
	}
	if err := mw.WriteField("model", p.Model); err != nil {
		return "", &APIError{Class: "config", Detail: "无法构造 PaddleOCR 模型参数"}
	}
	payload, _ := json.Marshal(map[string]any{
		"useDocOrientationClassify": false,
		"useDocUnwarping":           false,
		"useTextlineOrientation":    false,
	})
	if err := mw.WriteField("optionalPayload", string(payload)); err != nil {
		return "", &APIError{Class: "config", Detail: "无法构造 PaddleOCR 解析参数"}
	}
	if err := mw.Close(); err != nil {
		return "", &APIError{Class: "config", Detail: "无法完成 PaddleOCR 上传请求"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/api/v2/ocr/jobs", &body)
	if err != nil {
		return "", &APIError{Class: "config", Detail: "无法构造 PaddleOCR 上传请求"}
	}
	req.Header.Set("Authorization", "Bearer "+p.Token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err := p.HTTP.Do(req)
	if err != nil {
		return "", paddleTransportError(err, "上传")
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode != http.StatusOK {
		return "", paddleStatusError(res.StatusCode, raw, "paddleocr submit")
	}
	var reply struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			JobID string `json:"jobId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return "", &APIError{Class: "invalid_response", Detail: "PaddleOCR 上传响应不是合法 JSON"}
	}
	if reply.Code != 0 {
		return "", paddleAPIError(reply.Code, reply.Msg)
	}
	if strings.TrimSpace(reply.Data.JobID) == "" {
		return "", &APIError{Class: "invalid_response", Detail: "PaddleOCR 未返回 jobId"}
	}
	return reply.Data.JobID, nil
}

func (p *PaddleOCR) poll(ctx context.Context, jobID string) (state, jsonURL, errMsg string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		p.BaseURL+"/api/v2/ocr/jobs/"+jobID, nil)
	if err != nil {
		return "", "", "", &APIError{Class: "config", Detail: "无法构造 PaddleOCR 轮询请求"}
	}
	req.Header.Set("Authorization", "Bearer "+p.Token)
	res, doErr := p.HTTP.Do(req)
	if doErr != nil {
		return "", "", "", paddleTransportError(doErr, "轮询")
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode != http.StatusOK {
		return "", "", "", paddleStatusError(res.StatusCode, body, "paddleocr poll")
	}
	var reply struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			State     string `json:"state"`
			ErrorMsg  string `json:"errorMsg"`
			ResultURL struct {
				JSONURL string `json:"jsonUrl"`
			} `json:"resultUrl"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &reply); err != nil {
		return "", "", "", &APIError{Class: "invalid_response", Detail: "PaddleOCR 轮询响应不是合法 JSON"}
	}
	if reply.Code != 0 {
		return "", "", "", paddleAPIError(reply.Code, reply.Msg)
	}
	return strings.ToLower(strings.TrimSpace(reply.Data.State)),
		strings.TrimSpace(reply.Data.ResultURL.JSONURL), reply.Data.ErrorMsg, nil
}

func (p *PaddleOCR) downloadResult(ctx context.Context, jsonURL string) (Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jsonURL, nil)
	if err != nil {
		return Result{}, &APIError{Class: "invalid_response", Detail: "PaddleOCR 结果地址无效"}
	}
	res, err := p.HTTP.Do(req)
	if err != nil {
		return Result{}, paddleTransportError(err, "下载结果")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Result{}, &APIError{Class: "unavailable", Detail: fmt.Sprintf("下载 PaddleOCR 结果返回 HTTP %d", res.StatusCode), Retryable: res.StatusCode >= 500}
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, paddleMaxResultBytes+1))
	if err != nil {
		return Result{}, &APIError{Class: "unavailable", Detail: "读取 PaddleOCR 结果失败", Retryable: true}
	}
	if len(raw) > paddleMaxResultBytes {
		return Result{}, &APIError{Class: "unsupported", Detail: "PaddleOCR 结果超过 64 MiB"}
	}
	return ParsePaddleOCRJSONL(raw)
}

// ParsePaddleOCRJSONL converts one JSON object per page into Markdown and page
// text. It accepts both the documented {"result": ...} wrapper and a bare result.
func ParsePaddleOCRJSONL(raw []byte) (Result, error) {
	lines := bytes.Split(raw, []byte{'\n'})
	pages := make([]PageText, 0, len(lines))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var envelope struct {
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(line, &envelope); err != nil {
			return Result{}, &APIError{Class: "invalid_response", Detail: "PaddleOCR JSONL 格式无效"}
		}
		result := envelope.Result
		if len(result) == 0 {
			result = line
		}
		texts, err := paddlePageTexts(result)
		if err != nil {
			return Result{}, err
		}
		pageNo := len(pages) + 1
		pages = append(pages, PageText{PageNo: pageNo, Text: texts})
		if len(pages) >= paddleMaxJSONLLines {
			break
		}
	}
	if len(pages) == 0 {
		return Result{}, &APIError{Class: "invalid_response", Detail: "PaddleOCR 结果为空"}
	}
	var markdown strings.Builder
	for _, page := range pages {
		if strings.TrimSpace(page.Text) == "" {
			continue
		}
		if markdown.Len() > 0 {
			markdown.WriteString("\n\n")
		}
		fmt.Fprintf(&markdown, "## 第 %d 页\n\n%s", page.PageNo, page.Text)
	}
	if strings.TrimSpace(markdown.String()) == "" {
		return Result{}, &APIError{Class: "invalid_response", Detail: "PaddleOCR 解析结果没有文本内容"}
	}
	return Result{Markdown: markdown.String(), Pages: pages, PageCount: len(pages)}, nil
}

func paddlePageTexts(raw json.RawMessage) (string, error) {
	var result map[string]json.RawMessage
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", &APIError{Class: "invalid_response", Detail: "PaddleOCR 页结果不是对象"}
	}
	for _, key := range []string{"ocrResults", "res", "result"} {
		if nested, ok := result[key]; ok {
			if texts := extractPaddleTexts(nested); len(texts) > 0 {
				return strings.Join(texts, "\n"), nil
			}
		}
	}
	if texts := extractPaddleTexts(raw); len(texts) > 0 {
		return strings.Join(texts, "\n"), nil
	}
	return "", nil
}

// extractPaddleTexts recursively finds rec_texts arrays. It intentionally
// ignores bbox/polygon data because the app only stores portable text.
func extractPaddleTexts(raw json.RawMessage) []string {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	var out []string
	var walk func(any)
	walk = func(node any) {
		switch v := node.(type) {
		case map[string]any:
			for key, child := range v {
				if key == "rec_texts" {
					switch texts := child.(type) {
					case []any:
						for _, item := range texts {
							if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
								out = append(out, strings.TrimSpace(text))
							}
						}
					}
					continue
				}
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(value)
	return out
}

func paddleStatusError(status int, body []byte, context string) error {
	var payload struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	_ = json.Unmarshal(body, &payload)
	if payload.Code != 0 {
		return paddleAPIError(payload.Code, payload.Msg)
	}
	return statusError(status, body, context)
}

func paddleTransportError(err error, action string) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &APIError{Class: "timeout", Detail: "PaddleOCR " + action + "请求超时", Retryable: true}
	}
	return &APIError{Class: "unavailable", Detail: "无法连接 PaddleOCR 服务", Retryable: true}
}

func paddleAPIError(code int, message string) error {
	detail := Sanitize(message)
	if detail == "" {
		detail = "PaddleOCR 返回错误码 " + strconv.Itoa(code)
	}
	class := "unavailable"
	retryable := false
	switch code {
	case 401:
		class = "auth"
	case 500, 10010:
		retryable = true
	case 12001:
		class = "quota"
	case 12002:
		class = "rate_limited"
		retryable = true
	case 11001, 11002, 11003:
		class = "invalid_response"
	case 10000, 10001, 10002, 10003, 10004, 10005, 10006, 10007, 10008:
		class = "config"
	}
	return &APIError{Class: class, Detail: detail, Retryable: retryable}
}
