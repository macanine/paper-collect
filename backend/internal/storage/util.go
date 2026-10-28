package storage

import (
	"context"
	"encoding/hex"
	"io"
	"net/url"
	"strings"
	"unicode/utf8"
)

const maxObjectKeyBytes = 1024

// validateObjectKey 校验 CAS 相对键：非空、正斜杠分隔、不含绝对路径、反斜杠、
// 空段、"." 与 ".."、控制字符。返回按段拆分的结果。
func validateObjectKey(key string) ([]string, *CloudError) {
	if key == "" || len(key) > maxObjectKeyBytes || !utf8.ValidString(key) {
		return nil, newCloudError(ClassInvalidInput, "object_key_invalid", false)
	}
	if strings.HasPrefix(key, "/") || strings.Contains(key, "\\") || hasControl(key) {
		return nil, newCloudError(ClassInvalidInput, "object_key_invalid", false)
	}
	segments := strings.Split(key, "/")
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return nil, newCloudError(ClassInvalidInput, "object_key_invalid", false)
		}
	}
	return segments, nil
}

func hasControl(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// normalizeSHA256 接受 64 位十六进制（大小写不限），返回小写形式。
func normalizeSHA256(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 64 {
		return "", false
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", false
	}
	return value, true
}

// ctxReader 在每次读取前检查 context，使长时间复制可被取消。
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// normalizeHTTPBase 解析 http/https 基地址：禁止内嵌凭据、查询与片段，路径补齐末尾斜杠。
func normalizeHTTPBase(raw string) (*url.URL, *CloudError) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || raw == "" {
		return nil, newCloudError(ClassConfig, "base_url_invalid", false)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, newCloudError(ClassConfig, "base_url_scheme_invalid", false)
	}
	if parsed.Host == "" {
		return nil, newCloudError(ClassConfig, "base_url_host_missing", false)
	}
	if parsed.User != nil {
		return nil, newCloudError(ClassConfig, "base_url_has_credentials", false)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, newCloudError(ClassConfig, "base_url_has_query", false)
	}
	if !strings.HasSuffix(parsed.Path, "/") {
		parsed.Path += "/"
		parsed.RawPath = ""
	}
	return parsed, nil
}

// validatePathPrefix 把 path_prefix 拆成段；允许首尾斜杠，拒绝 "."、".."、反斜杠与控制字符。
func validatePathPrefix(raw string) ([]string, *CloudError) {
	trimmed := strings.Trim(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return nil, nil
	}
	if strings.Contains(trimmed, "\\") || hasControl(trimmed) {
		return nil, newCloudError(ClassConfig, "path_prefix_invalid", false)
	}
	var segments []string
	for _, segment := range strings.Split(trimmed, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return nil, newCloudError(ClassConfig, "path_prefix_invalid", false)
		}
		segments = append(segments, segment)
	}
	return segments, nil
}

// joinURLPath 在基地址后追加已拆段的路径，并正确转义每一段。
func joinURLPath(base *url.URL, segments []string, trailingSlash bool) *url.URL {
	joined := *base
	escaped := make([]string, 0, len(segments))
	for _, segment := range segments {
		escaped = append(escaped, url.PathEscape(segment))
	}
	decodedPath := strings.TrimSuffix(base.Path, "/") + "/" + strings.Join(segments, "/")
	rawPath := strings.TrimSuffix(base.EscapedPath(), "/") + "/" + strings.Join(escaped, "/")
	if trailingSlash {
		decodedPath += "/"
		rawPath += "/"
	}
	joined.Path = decodedPath
	joined.RawPath = rawPath
	return &joined
}

// truncateRunes 按 rune 截断，避免切断 UTF-8 序列。
func truncateRunes(value string, max int) string {
	if utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	return string(runes[:max])
}
