package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
)

// cancelBody 在关闭响应体时释放请求级 context，避免超时计时器泄漏。
type cancelBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c cancelBody) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

// discardAndClose 读掉少量残余内容后关闭，便于连接复用。
func discardAndClose(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
}

// hashStream 流式计算 SHA-256 与字节数，不把内容整体读入内存。
func hashStream(r io.Reader, limit int64) (string, int64, error) {
	hasher := sha256.New()
	reader := r
	if limit > 0 {
		reader = io.LimitReader(r, limit+1)
	}
	n, err := io.Copy(hasher, reader)
	if err != nil {
		return "", n, err
	}
	return hex.EncodeToString(hasher.Sum(nil)), n, nil
}
