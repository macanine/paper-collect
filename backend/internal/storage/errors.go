package storage

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
)

// ErrorClass 是适配器对外暴露的稳定错误分类，后端据此决定重试、标记或提示。
type ErrorClass string

const (
	ClassInvalidInput ErrorClass = "invalid_input"  // 调用参数不合法（Key、路径、大小、哈希格式）
	ClassConfig       ErrorClass = "config_invalid" // Settings/Secrets 缺失或非法
	ClassAuth         ErrorClass = "auth_failed"    // 认证、会话或令牌被拒绝
	ClassFileMissing  ErrorClass = "file_missing"   // 本地或远端对象不存在
	ClassTimeout      ErrorClass = "timeout"        // 请求超时（可重试）
	ClassNetwork      ErrorClass = "network"        // 连接失败、DNS、reset 等传输层错误（可重试）
	ClassUnavailable  ErrorClass = "unavailable"    // 远端 5xx/429 等暂不可用（可重试）
	ClassQuota        ErrorClass = "quota_exceeded" // 远端空间不足
	ClassSizeMismatch ErrorClass = "size_mismatch"  // 字节数与声明不符
	ClassHashMismatch ErrorClass = "hash_mismatch"  // SHA-256 与声明不符
	ClassLinkExpired  ErrorClass = "link_expired"   // 分享或直链失效，或需要提取码
	ClassUnsupported  ErrorClass = "unsupported"    // 适配器或远端不支持该操作
	ClassUnknown      ErrorClass = "unknown"        // 其余未能归类的失败
)

// CloudError 是所有适配器返回的分类错误。Detail 只允许是稳定的机器码，
// 不包含凭据、Cookie、完整 URL 或远端原始消息。
type CloudError struct {
	Class     ErrorClass
	Detail    string
	Retryable bool
}

func (e *CloudError) Error() string {
	return fmt.Sprintf("storage %s: %s", e.Class, e.Detail)
}

// Is 使 errors.Is(err, ErrUnsupported) 对 unsupported 分类成立。
func (e *CloudError) Is(target error) bool {
	return target == ErrUnsupported && e.Class == ClassUnsupported
}

func newCloudError(class ErrorClass, detail string, retryable bool) *CloudError {
	return &CloudError{Class: class, Detail: detail, Retryable: retryable}
}

// AsCloudError 把任意错误归一为 *CloudError；未分类的错误记为 unknown 且不可重试。
func AsCloudError(err error) *CloudError {
	if err == nil {
		return nil
	}
	var ce *CloudError
	if errors.As(err, &ce) {
		return ce
	}
	return newCloudError(ClassUnknown, "unclassified", false)
}

// IsRetryable 报告错误是否值得由后端稍后重试。
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	return AsCloudError(err).Retryable
}

// transportError 把 http.Client.Do 或读取响应体时的传输层错误归类。
// 不使用 err.Error()：net/url 的错误文本会携带完整 URL。
func transportError(ctx context.Context, err error, op string) *CloudError {
	if ctx != nil {
		switch ctx.Err() {
		case context.Canceled:
			return newCloudError(ClassUnknown, op+"_canceled", false)
		case context.DeadlineExceeded:
			return newCloudError(ClassTimeout, op+"_timeout", true)
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return newCloudError(ClassTimeout, op+"_timeout", true)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return newCloudError(ClassTimeout, op+"_timeout", true)
	}
	return newCloudError(ClassNetwork, op+"_network", true)
}

// statusError 把 HTTP 状态码映射为分类错误。op 是稳定前缀，例如 webdav_put。
func statusError(op string, status int) *CloudError {
	detail := fmt.Sprintf("%s_http_%d", op, status)
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return newCloudError(ClassAuth, detail, false)
	case status == http.StatusNotFound || status == http.StatusGone:
		return newCloudError(ClassFileMissing, detail, false)
	case status == http.StatusRequestTimeout:
		return newCloudError(ClassTimeout, detail, true)
	case status == http.StatusTooManyRequests || status >= 500:
		return newCloudError(ClassUnavailable, detail, true)
	case status == http.StatusRequestEntityTooLarge:
		return newCloudError(ClassUnsupported, detail, false)
	case status == http.StatusInsufficientStorage:
		return newCloudError(ClassQuota, detail, false)
	default:
		return newCloudError(ClassUnknown, detail, false)
	}
}
