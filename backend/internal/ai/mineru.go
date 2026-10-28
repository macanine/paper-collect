package ai

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"
)

const (
	mineruMaxZipBytes   = 256 << 20
	mineruMaxMarkdown   = 8 << 20
	mineruMaxImageBytes = 12 << 20
	mineruMaxImages     = 20
)

// MinerU parses PDF/DOC/DOCX/PPT/PPTX/images through the official hosted API.
// The exact protocol is exercised only against a fake server in tests; real
// network behaviour is unverified.
type MinerU struct {
	BaseURL      string
	Token        string
	HTTP         *http.Client
	PollInterval time.Duration
	Timeout      time.Duration
}

// NewMinerU validates the endpoint and applies defaults.
func NewMinerU(baseURL, token string, client *http.Client) (*MinerU, error) {
	normalized, err := ValidateBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	if client == nil {
		// Extract and Health impose operation-wide context deadlines. A client
		// timeout here would also abort large uploads before the job deadline.
		client = &http.Client{}
	}
	return &MinerU{BaseURL: normalized, Token: strings.TrimSpace(token), HTTP: client, PollInterval: 3 * time.Second, Timeout: 15 * time.Minute}, nil
}

// PageText is one parsed page with its 1-based page number.
type PageText struct {
	PageNo int
	Text   string
}

// Result is the parsed document plus page images and page texts.
type Result struct {
	Markdown  string
	Images    [][]byte
	Pages     []PageText
	PageCount int
}

// Health checks reachability and token acceptance without uploading a file.
func (m *MinerU) Health(ctx context.Context) error {
	if m.Token == "" {
		return &APIError{Class: "config", Detail: "未配置 MinerU 令牌"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		m.BaseURL+"/api/v4/extract-results/batch/00000000-0000-0000-0000-000000000000", nil)
	if err != nil {
		return &APIError{Class: "config", Detail: "无法构造 MinerU 请求"}
	}
	req.Header.Set("Authorization", "Bearer "+m.Token)
	res, err := m.HTTP.Do(req)
	if err != nil {
		return transportError(err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	switch {
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		return &APIError{Class: "auth", Detail: "MinerU 令牌被拒绝"}
	case res.StatusCode >= 500:
		return &APIError{Class: "unavailable", Detail: fmt.Sprintf("MinerU 返回 HTTP %d", res.StatusCode), Retryable: true}
	default:
		// 404/400 means the endpoint is reachable and the token was accepted.
		return nil
	}
}

// Extract uploads one document, polls until completion, downloads the result
// ZIP and returns Markdown plus decoded page images.
func (m *MinerU) Extract(ctx context.Context, filename string, data []byte) (Result, error) {
	if m.Token == "" {
		return Result{}, &APIError{Class: "config", Detail: "未配置 MinerU 令牌"}
	}
	if len(data) == 0 {
		return Result{}, &APIError{Class: "config", Detail: "文件内容为空"}
	}
	if m.PollInterval <= 0 {
		m.PollInterval = 3 * time.Second
	}
	deadline := time.Now().Add(m.Timeout)
	batchID, uploadURL, err := m.submit(ctx, filename, int64(len(data)))
	if err != nil {
		return Result{}, err
	}
	if err := m.upload(ctx, uploadURL, data); err != nil {
		return Result{}, err
	}

	var zipURL string
	for {
		if time.Now().After(deadline) {
			return Result{}, &APIError{Class: "timeout", Detail: "MinerU 解析超时", Retryable: true}
		}
		state, resultURL, errMsg, err := m.poll(ctx, batchID)
		if err != nil {
			return Result{}, err
		}
		switch state {
		case "done":
			if resultURL == "" {
				return Result{}, &APIError{Class: "invalid_response", Detail: "MinerU 完成但未返回结果地址"}
			}
			zipURL = resultURL
		case "failed":
			if errMsg == "" {
				errMsg = "MinerU 解析失败"
			}
			return Result{}, &APIError{Class: "unavailable", Detail: Sanitize(errMsg)}
		case "pending", "running", "converting", "waiting-file", "":
			// keep polling
		default:
			// Unknown states are treated as still running; the deadline bounds the wait.
		}
		if zipURL != "" {
			break
		}
		select {
		case <-ctx.Done():
			return Result{}, &APIError{Class: "timeout", Detail: "解析被取消", Retryable: true}
		case <-time.After(m.PollInterval):
		}
	}
	return m.downloadResult(ctx, zipURL)
}

func (m *MinerU) submit(ctx context.Context, filename string, size int64) (batchID, uploadURL string, err error) {
	payload := map[string]any{
		"enable_formula": true,
		"language":       "ch",
		"files": []map[string]any{{
			"name":   filename,
			"is_ocr": true,
			"size":   size,
		}},
	}
	raw, _ := json.Marshal(payload)
	req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, m.BaseURL+"/api/v4/file-urls/batch", bytes.NewReader(raw))
	if reqErr != nil {
		return "", "", &APIError{Class: "config", Detail: "无法构造 MinerU 上传请求"}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.Token)
	res, doErr := m.HTTP.Do(req)
	if doErr != nil {
		return "", "", transportError(doErr)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode != http.StatusOK {
		return "", "", statusError(res.StatusCode, body, "mineru submit")
	}
	var reply struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			BatchID  string            `json:"batch_id"`
			FileURLs []json.RawMessage `json:"file_urls"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &reply); err != nil {
		return "", "", &APIError{Class: "invalid_response", Detail: "MinerU 上传响应不是合法 JSON"}
	}
	if reply.Code != 0 && reply.Code != 200 {
		return "", "", &APIError{Class: "unavailable", Detail: Sanitize(reply.Msg)}
	}
	if reply.Data.BatchID == "" || len(reply.Data.FileURLs) == 0 {
		return "", "", &APIError{Class: "invalid_response", Detail: "MinerU 未返回 batch_id 或上传地址"}
	}
	var url string
	if json.Unmarshal(reply.Data.FileURLs[0], &url) != nil || url == "" {
		var object struct {
			URL string `json:"url"`
		}
		if json.Unmarshal(reply.Data.FileURLs[0], &object) != nil || object.URL == "" {
			return "", "", &APIError{Class: "invalid_response", Detail: "MinerU 上传地址无法解析"}
		}
		url = object.URL
	}
	return reply.Data.BatchID, url, nil
}

func (m *MinerU) upload(ctx context.Context, uploadURL string, data []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, bytes.NewReader(data))
	if err != nil {
		return &APIError{Class: "config", Detail: "无法构造文件上传请求"}
	}
	// The presigned URL signature may cover the absence of Content-Type; do not set it.
	res, err := m.HTTP.Do(req)
	if err != nil {
		return transportError(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		detail := fmt.Sprintf("MinerU 文件上传返回 HTTP %d", res.StatusCode)
		if snippet := Sanitize(string(body)); snippet != "" && snippet != "<nil>" {
			detail += ": " + snippet
		}
		return &APIError{Class: "unavailable", Detail: detail, Retryable: res.StatusCode >= 500}
	}
	return nil
}

func (m *MinerU) poll(ctx context.Context, batchID string) (state, zipURL, errMsg string, err error) {
	req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet,
		m.BaseURL+"/api/v4/extract-results/batch/"+batchID, nil)
	if reqErr != nil {
		return "", "", "", &APIError{Class: "config", Detail: "无法构造 MinerU 轮询请求"}
	}
	req.Header.Set("Authorization", "Bearer "+m.Token)
	res, doErr := m.HTTP.Do(req)
	if doErr != nil {
		return "", "", "", transportError(doErr)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode != http.StatusOK {
		return "", "", "", statusError(res.StatusCode, body, "mineru poll")
	}
	var reply struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			BatchID       string `json:"batch_id"`
			ExtractResult []struct {
				FileName   string `json:"file_name"`
				State      string `json:"state"`
				FullZipURL string `json:"full_zip_url"`
				ErrMsg     string `json:"err_msg"`
			} `json:"extract_result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &reply); err != nil {
		return "", "", "", &APIError{Class: "invalid_response", Detail: "MinerU 轮询响应不是合法 JSON"}
	}
	if reply.Code != 0 && reply.Code != 200 {
		return "", "", "", &APIError{Class: "unavailable", Detail: Sanitize(reply.Msg)}
	}
	if len(reply.Data.ExtractResult) == 0 {
		return "pending", "", "", nil
	}
	first := reply.Data.ExtractResult[0]
	return strings.ToLower(strings.TrimSpace(first.State)), first.FullZipURL, first.ErrMsg, nil
}

func (m *MinerU) downloadResult(ctx context.Context, zipURL string) (Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, zipURL, nil)
	if err != nil {
		return Result{}, &APIError{Class: "invalid_response", Detail: "结果地址无效"}
	}
	res, err := m.HTTP.Do(req)
	if err != nil {
		return Result{}, transportError(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Result{}, &APIError{Class: "unavailable", Detail: fmt.Sprintf("下载 MinerU 结果返回 HTTP %d", res.StatusCode), Retryable: res.StatusCode >= 500}
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, mineruMaxZipBytes+1))
	if err != nil {
		return Result{}, &APIError{Class: "unavailable", Detail: "读取 MinerU 结果失败", Retryable: true}
	}
	if len(raw) > mineruMaxZipBytes {
		return Result{}, &APIError{Class: "unsupported", Detail: "MinerU 结果 ZIP 超过 256 MiB"}
	}
	return parseMineruZip(raw)
}

func parseMineruZip(raw []byte) (Result, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return Result{}, &APIError{Class: "invalid_response", Detail: "MinerU 结果不是合法 ZIP"}
	}
	var markdown string
	var markdownSize int
	markdownBytes := 0
	var contentList []byte
	var images [][]byte
	var imageNames []string
	type imageEntry struct {
		name string
		data []byte
	}
	var found []imageEntry
	allPages := 0
	names := make([]string, 0, len(zr.File))

	for _, file := range zr.File {
		if len(names) < 30 {
			names = append(names, file.Name)
		}
		name := file.Name
		lower := strings.ToLower(name)
		if file.FileInfo().IsDir() {
			continue
		}
		if strings.HasSuffix(lower, "content_list.json") || strings.HasSuffix(lower, "content_list_v2.json") {
			if contentList == nil {
				if data, err := readZipEntry(file, mineruMaxMarkdown); err == nil {
					contentList = data
				}
			}
			continue
		}
		if strings.HasSuffix(lower, ".md") {
			data, err := readZipEntry(file, mineruMaxMarkdown)
			if err != nil {
				continue
			}
			// Prefer full.md; otherwise keep the largest Markdown file.
			if markdown == "" || path.Base(lower) == "full.md" || len(data) > markdownSize {
				markdown = string(data)
				markdownSize = len(data)
				markdownBytes = len(data)
			}
			continue
		}
		if strings.HasSuffix(lower, ".jpg") || strings.HasSuffix(lower, ".jpeg") ||
			strings.HasSuffix(lower, ".png") || strings.HasSuffix(lower, ".webp") {
			allPages++
			if !strings.Contains(lower, "images/") {
				continue
			}
			if len(found) >= mineruMaxImages {
				continue
			}
			data, err := readZipEntry(file, mineruMaxImageBytes)
			if err != nil {
				continue
			}
			found = append(found, imageEntry{name: name, data: data})
		}
	}
	if markdownBytes == 0 {
		return Result{}, &APIError{Class: "invalid_response", Detail: "MinerU 结果缺少 Markdown: " + Sanitize(strings.Join(names, ", "))}
	}
	if strings.TrimSpace(markdown) == "" {
		return Result{}, &APIError{Class: "invalid_response", Detail: "MinerU 解析结果没有文本内容（可能是空文档或纯图片）"}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].name < found[j].name })
	for _, entry := range found {
		images = append(images, entry.data)
		imageNames = append(imageNames, entry.name)
	}
	if allPages == 0 {
		allPages = len(images)
	}
	pages := parseContentListPages(contentList)
	if len(pages) == 0 {
		pages = []PageText{{PageNo: 1, Text: markdown}}
		allPages = 1
	} else if len(pages) > allPages {
		allPages = len(pages)
	}
	return Result{Markdown: markdown, Images: images, Pages: pages, PageCount: allPages}, nil
}

// parseContentListPages groups MinerU content_list blocks by page_idx. Images
// and blocks without text are ignored; table text is kept as plain text.
func parseContentListPages(raw []byte) []PageText {
	if len(raw) == 0 {
		return nil
	}
	var blocks []struct {
		Type    string `json:"type"`
		Text    string `json:"text"`
		PageIdx *int   `json:"page_idx"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil
	}
	pageTexts := map[int][]string{}
	for _, block := range blocks {
		if block.PageIdx == nil || *block.PageIdx < 0 {
			continue
		}
		text := strings.TrimSpace(block.Text)
		if text == "" {
			continue
		}
		pageTexts[*block.PageIdx+1] = append(pageTexts[*block.PageIdx+1], text)
	}
	pages := make([]PageText, 0, len(pageTexts))
	for pageNo, texts := range pageTexts {
		pages = append(pages, PageText{PageNo: pageNo, Text: strings.Join(texts, "\n")})
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].PageNo < pages[j].PageNo })
	return pages
}

func readZipEntry(file *zip.File, limit int64) ([]byte, error) {
	rc, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("entry too large")
	}
	return data, nil
}
