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
	"strings"
	"time"
)

const (
	rapidOCRMaxResultBytes = 64 << 20
	rapidOCRMaxPages       = 1000
)

// RapidOCR talks to the self-hosted document OCR service in services/rapidocr.
// The service renders PDF pages and exposes the same asynchronous job shape as
// PaddleOCR, but requires no cloud token.
type RapidOCR struct {
	BaseURL      string
	Model        string
	HTTP         *http.Client
	PollInterval time.Duration
	Timeout      time.Duration
}

// NewRapidOCR validates the self-hosted endpoint and applies defaults.
func NewRapidOCR(baseURL, modelName string, client *http.Client) (*RapidOCR, error) {
	normalized, err := ValidateBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		modelName = "PP-OCRv5"
	}
	if client == nil {
		client = &http.Client{}
	}
	return &RapidOCR{
		BaseURL: normalized, Model: modelName, HTTP: client,
		PollInterval: 2 * time.Second, Timeout: 15 * time.Minute,
	}, nil
}

// Health verifies that the self-hosted service and OCR models are ready.
func (r *RapidOCR) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.BaseURL+"/healthz", nil)
	if err != nil {
		return &APIError{Class: "config", Detail: "无法构造 RapidOCR 请求"}
	}
	res, err := r.HTTP.Do(req)
	if err != nil {
		return rapidOCRTransportError(err, "健康检查")
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		return rapidOCRStatusError(res.StatusCode, body, "健康检查")
	}
	var reply struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &reply); err != nil || reply.Status != "ok" {
		return &APIError{Class: "invalid_response", Detail: "RapidOCR 服务未就绪"}
	}
	return nil
}

// Extract uploads one PDF/image, polls the asynchronous job, and parses JSONL.
func (r *RapidOCR) Extract(ctx context.Context, filename string, data []byte) (Result, error) {
	if len(data) == 0 {
		return Result{}, &APIError{Class: "config", Detail: "文件内容为空"}
	}
	if r.PollInterval <= 0 {
		r.PollInterval = 2 * time.Second
	}
	if r.Timeout <= 0 {
		r.Timeout = 15 * time.Minute
	}
	deadline := time.Now().Add(r.Timeout)
	jobID, err := r.submit(ctx, filename, data)
	if err != nil {
		return Result{}, err
	}
	var jsonURL string
	for {
		if time.Now().After(deadline) {
			return Result{}, &APIError{Class: "timeout", Detail: "RapidOCR 解析超时", Retryable: true}
		}
		state, resultURL, errMsg, err := r.poll(ctx, jobID)
		if err != nil {
			return Result{}, err
		}
		switch state {
		case "done":
			if resultURL == "" {
				return Result{}, &APIError{Class: "invalid_response", Detail: "RapidOCR 完成但未返回结果地址"}
			}
			jsonURL = resultURL
		case "failed":
			if errMsg == "" {
				errMsg = "RapidOCR 解析失败"
			}
			return Result{}, &APIError{Class: "unavailable", Detail: Sanitize(errMsg)}
		case "pending", "running", "":
		default:
		}
		if jsonURL != "" {
			break
		}
		select {
		case <-ctx.Done():
			return Result{}, &APIError{Class: "timeout", Detail: "RapidOCR 解析被取消", Retryable: true}
		case <-time.After(r.PollInterval):
		}
	}
	return r.downloadResult(ctx, jsonURL)
}

func (r *RapidOCR) submit(ctx context.Context, filename string, data []byte) (string, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return "", &APIError{Class: "config", Detail: "无法构造 RapidOCR 上传请求"}
	}
	if _, err := part.Write(data); err != nil {
		return "", &APIError{Class: "config", Detail: "无法读取上传文件"}
	}
	if err := mw.WriteField("model", r.Model); err != nil {
		return "", &APIError{Class: "config", Detail: "无法构造 RapidOCR 模型参数"}
	}
	if err := mw.Close(); err != nil {
		return "", &APIError{Class: "config", Detail: "无法完成 RapidOCR 上传请求"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.BaseURL+"/api/v1/ocr/jobs", &body)
	if err != nil {
		return "", &APIError{Class: "config", Detail: "无法构造 RapidOCR 上传请求"}
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err := r.HTTP.Do(req)
	if err != nil {
		return "", rapidOCRTransportError(err, "上传")
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusAccepted {
		return "", rapidOCRStatusError(res.StatusCode, raw, "上传")
	}
	var reply struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil || strings.TrimSpace(reply.JobID) == "" {
		return "", &APIError{Class: "invalid_response", Detail: "RapidOCR 未返回 job_id"}
	}
	return strings.TrimSpace(reply.JobID), nil
}

func (r *RapidOCR) poll(ctx context.Context, jobID string) (state, jsonURL, errMsg string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		r.BaseURL+"/api/v1/ocr/jobs/"+jobID, nil)
	if err != nil {
		return "", "", "", &APIError{Class: "config", Detail: "无法构造 RapidOCR 轮询请求"}
	}
	res, err := r.HTTP.Do(req)
	if err != nil {
		return "", "", "", rapidOCRTransportError(err, "轮询")
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode != http.StatusOK {
		return "", "", "", rapidOCRStatusError(res.StatusCode, raw, "轮询")
	}
	var reply struct {
		State     string `json:"state"`
		Error     string `json:"error"`
		ResultURL string `json:"result_url"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return "", "", "", &APIError{Class: "invalid_response", Detail: "RapidOCR 轮询响应不是合法 JSON"}
	}
	return strings.ToLower(strings.TrimSpace(reply.State)),
		strings.TrimSpace(reply.ResultURL), reply.Error, nil
}

func (r *RapidOCR) downloadResult(ctx context.Context, jsonURL string) (Result, error) {
	if strings.HasPrefix(jsonURL, "/") {
		jsonURL = r.BaseURL + jsonURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jsonURL, nil)
	if err != nil {
		return Result{}, &APIError{Class: "invalid_response", Detail: "RapidOCR 结果地址无效"}
	}
	res, err := r.HTTP.Do(req)
	if err != nil {
		return Result{}, rapidOCRTransportError(err, "下载结果")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Result{}, &APIError{Class: "unavailable", Detail: fmt.Sprintf("下载 RapidOCR 结果返回 HTTP %d", res.StatusCode)}
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, rapidOCRMaxResultBytes+1))
	if err != nil {
		return Result{}, &APIError{Class: "unavailable", Detail: "读取 RapidOCR 结果失败", Retryable: true}
	}
	if len(raw) > rapidOCRMaxResultBytes {
		return Result{}, &APIError{Class: "unsupported", Detail: "RapidOCR 结果超过 64 MiB"}
	}
	return ParseRapidOCRJSONL(raw)
}

// ParseRapidOCRJSONL converts one JSON object per page into a page-oriented
// result. Each page object has {"texts":[...]}.
func ParseRapidOCRJSONL(raw []byte) (Result, error) {
	lines := bytes.Split(raw, []byte{'\n'})
	pages := make([]PageText, 0, len(lines))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var page struct {
			PageNo int      `json:"page_no"`
			Texts  []string `json:"texts"`
		}
		if err := json.Unmarshal(line, &page); err != nil {
			return Result{}, &APIError{Class: "invalid_response", Detail: "RapidOCR JSONL 格式无效"}
		}
		pageNo := page.PageNo
		if pageNo <= 0 {
			pageNo = len(pages) + 1
		}
		texts := make([]string, 0, len(page.Texts))
		for _, text := range page.Texts {
			if strings.TrimSpace(text) != "" {
				texts = append(texts, strings.TrimSpace(text))
			}
		}
		pages = append(pages, PageText{PageNo: pageNo, Text: strings.Join(texts, "\n")})
		if len(pages) >= rapidOCRMaxPages {
			break
		}
	}
	if len(pages) == 0 {
		return Result{}, &APIError{Class: "invalid_response", Detail: "RapidOCR 结果为空"}
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
		return Result{}, &APIError{Class: "invalid_response", Detail: "RapidOCR 解析结果没有文本内容"}
	}
	return Result{Markdown: markdown.String(), Pages: pages, PageCount: len(pages)}, nil
}

func rapidOCRTransportError(err error, action string) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &APIError{Class: "timeout", Detail: "RapidOCR " + action + "请求超时", Retryable: true}
	}
	return &APIError{Class: "unavailable", Detail: "无法连接 RapidOCR 服务", Retryable: true}
}

func rapidOCRStatusError(status int, body []byte, action string) error {
	var payload struct {
		Detail string `json:"detail"`
	}
	_ = json.Unmarshal(body, &payload)
	detail := Sanitize(payload.Detail)
	if detail == "" {
		detail = fmt.Sprintf("RapidOCR %s返回 HTTP %d", action, status)
	}
	class := "unavailable"
	retryable := status >= 500 || status == http.StatusTooManyRequests
	switch status {
	case http.StatusBadRequest, http.StatusUnsupportedMediaType:
		class = "unsupported"
	case http.StatusNotFound:
		class = "config"
	}
	return &APIError{Class: class, Detail: detail, Retryable: retryable}
}
