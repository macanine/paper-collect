package storage

import "net/http"

// New 按 cfg.Kind 构造适配器：local、webdav、lanzou、123pan。
// 未知 kind 返回 unsupported 分类错误，该错误同时满足 errors.Is(err, ErrUnsupported)。
func New(cfg Config) (Adapter, error) {
	switch cfg.Kind {
	case "local":
		return newLocal(cfg)
	case "webdav":
		return NewWebDAV(cfg, nil)
	case "lanzou":
		return NewLanzou(cfg, nil)
	case "123pan":
		return New123Pan(cfg, nil)
	default:
		return nil, newCloudError(ClassUnsupported, "kind_unsupported", false)
	}
}

// NewWebDAV 构造 WebDAV 适配器；client 为 nil 时使用默认 http.Client（测试可注入）。
func NewWebDAV(cfg Config, client *http.Client) (Adapter, error) {
	return newWebDAV(cfg, client)
}
