package storage

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// 语义对齐 gaogao src/storage/webdav.ts：PUT 上传、MKCOL 建目录、HEAD 核对大小、
// GET 流式重算 SHA-256。WebDAV 标准没有匿名分享；仅当配置了 public_base_url 时才返回公开链接。
const (
	webdavRequestTimeout = 30 * time.Second
	webdavUploadTimeout  = 5 * time.Minute
	webdavMaxUploadBytes = 512 << 20
	webdavUserAgent      = "paper-collect-storage/1"
	webdavPropfindBody   = `<?xml version="1.0"?><d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/></d:prop></d:propfind>`
)

type webdavAdapter struct {
	base           *url.URL // 末尾带斜杠
	prefix         []string // path_prefix 拆段，可为空
	publicBase     *url.URL // public_base_url，对应 path_prefix 所指公开目录；nil 表示无分享能力
	username       string
	password       string
	client         *http.Client
	requestTimeout time.Duration
	uploadTimeout  time.Duration
	maxUploadBytes int64
}

func newWebDAV(cfg Config, client *http.Client) (Adapter, error) {
	rawBase := strings.TrimSpace(cfg.Settings["base_url"])
	if rawBase == "" {
		return nil, newCloudError(ClassConfig, "webdav_base_url_required", false)
	}
	base, cerr := normalizeHTTPBase(rawBase)
	if cerr != nil {
		return nil, cerr
	}
	prefix, cerr := validatePathPrefix(cfg.Settings["path_prefix"])
	if cerr != nil {
		return nil, cerr
	}
	var publicBase *url.URL
	if raw := strings.TrimSpace(cfg.Settings["public_base_url"]); raw != "" {
		publicBase, cerr = normalizeHTTPBase(raw)
		if cerr != nil {
			return nil, newCloudError(ClassConfig, "webdav_public_base_url_invalid", false)
		}
	}
	username := cfg.Secrets["username"]
	password := cfg.Secrets["password"]
	if username == "" || password == "" {
		return nil, newCloudError(ClassConfig, "webdav_credentials_required", false)
	}
	if client == nil {
		client = &http.Client{}
	}
	return &webdavAdapter{
		base:           base,
		prefix:         prefix,
		publicBase:     publicBase,
		username:       username,
		password:       password,
		client:         client,
		requestTimeout: webdavRequestTimeout,
		uploadTimeout:  webdavUploadTimeout,
		maxUploadBytes: webdavMaxUploadBytes,
	}, nil
}

// do 发出一次带 Basic 认证的请求。返回的响应体关闭时释放超时 context。
func (a *webdavAdapter) do(
	ctx context.Context,
	method string,
	target *url.URL,
	body io.Reader,
	contentLength int64,
	headers map[string]string,
	timeout time.Duration,
	op string,
) (*http.Response, error) {
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	req, err := http.NewRequestWithContext(reqCtx, method, target.String(), body)
	if err != nil {
		cancel()
		return nil, newCloudError(ClassConfig, op+"_request_invalid", false)
	}
	req.SetBasicAuth(a.username, a.password)
	req.Header.Set("User-Agent", webdavUserAgent)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	if body != nil && contentLength >= 0 {
		req.ContentLength = contentLength
	}
	resp, err := a.client.Do(req)
	if err != nil {
		cancel()
		return nil, transportError(ctx, err, op)
	}
	resp.Body = cancelBody{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

func (a *webdavAdapter) remoteURL(segments []string) *url.URL {
	all := make([]string, 0, len(a.prefix)+len(segments))
	all = append(all, a.prefix...)
	all = append(all, segments...)
	return joinURLPath(a.base, all, false)
}

// ensureCollections 依次 MKCOL 前缀与对象所在的每一级目录。
// 201/200/204 表示创建或已存在，405 表示已是集合；409 说明父级缺失，按未知失败如实返回。
func (a *webdavAdapter) ensureCollections(ctx context.Context, fileSegments []string) error {
	all := make([]string, 0, len(a.prefix)+len(fileSegments))
	all = append(all, a.prefix...)
	all = append(all, fileSegments...)
	for i := 1; i < len(all); i++ {
		target := joinURLPath(a.base, all[:i], true)
		resp, err := a.do(ctx, "MKCOL", target, nil, -1, nil, a.requestTimeout, "webdav_mkcol")
		if err != nil {
			return err
		}
		status := resp.StatusCode
		discardAndClose(resp)
		switch {
		case status >= 200 && status < 300, status == http.StatusMethodNotAllowed:
			continue
		case status == http.StatusConflict:
			return newCloudError(ClassUnknown, "webdav_mkcol_parent_missing", false)
		default:
			return statusError("webdav_mkcol", status)
		}
	}
	return nil
}

func (a *webdavAdapter) Put(ctx context.Context, obj Object) (Stored, error) {
	segments, cerr := validateObjectKey(obj.Key)
	if cerr != nil {
		return Stored{}, cerr
	}
	if obj.Path == "" {
		return Stored{}, newCloudError(ClassInvalidInput, "webdav_source_path_required", false)
	}
	if obj.Size < 0 {
		return Stored{}, newCloudError(ClassInvalidInput, "object_size_negative", false)
	}
	if obj.Size > a.maxUploadBytes {
		return Stored{}, newCloudError(ClassUnsupported, "webdav_file_too_large", false)
	}
	expectedSHA := ""
	if obj.SHA256 != "" {
		normalized, ok := normalizeSHA256(obj.SHA256)
		if !ok {
			return Stored{}, newCloudError(ClassInvalidInput, "sha256_invalid", false)
		}
		expectedSHA = normalized
	}

	src, err := os.Open(obj.Path)
	if err != nil {
		return Stored{}, newCloudError(ClassFileMissing, "webdav_local_source_missing", false)
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return Stored{}, newCloudError(ClassInvalidInput, "webdav_local_source_not_regular", false)
	}
	if info.Size() != obj.Size {
		return Stored{}, newCloudError(ClassSizeMismatch, "webdav_local_size_mismatch", false)
	}

	if err := a.ensureCollections(ctx, segments); err != nil {
		return Stored{}, err
	}
	target := a.remoteURL(segments)
	resp, err := a.do(ctx, http.MethodPut, target, src, obj.Size, map[string]string{
		"Content-Type": "application/octet-stream",
	}, a.uploadTimeout, "webdav_put")
	if err != nil {
		return Stored{}, err
	}
	status := resp.StatusCode
	discardAndClose(resp)
	if status < 200 || status >= 300 {
		return Stored{}, statusError("webdav_put", status)
	}

	verification, err := a.verifyRemote(ctx, target, obj.Size, expectedSHA)
	if err != nil {
		return Stored{}, err
	}
	return Stored{RemoteID: obj.Key, Size: obj.Size, Verification: verification}, nil
}

// verifyRemote 上传后复核远端：HEAD 比对 Content-Length（不支持 HEAD 时跳过）；
// 提供 SHA-256 时 GET 流式重算。只有真正比对过的项才会被报告。
func (a *webdavAdapter) verifyRemote(ctx context.Context, target *url.URL, size int64, expectedSHA string) (string, error) {
	verification := "uploaded"
	head, err := a.do(ctx, http.MethodHead, target, nil, -1, nil, a.requestTimeout, "webdav_verify_head")
	if err != nil {
		return "", err
	}
	status := head.StatusCode
	declared := head.Header.Get("Content-Length")
	discardAndClose(head)
	switch {
	case status >= 200 && status < 300:
		if n, parseErr := strconv.ParseInt(declared, 10, 64); parseErr == nil {
			if n != size {
				return "", newCloudError(ClassSizeMismatch, "webdav_verify_size_mismatch", false)
			}
			verification = "size"
		}
	case status == http.StatusMethodNotAllowed || status == http.StatusNotImplemented:
		// 部分 WebDAV 不支持 HEAD：退回 GET 复核。
	default:
		return "", statusError("webdav_verify_head", status)
	}

	if expectedSHA == "" {
		return verification, nil
	}
	get, err := a.do(ctx, http.MethodGet, target, nil, -1, nil, a.uploadTimeout, "webdav_verify_get")
	if err != nil {
		return "", err
	}
	defer discardAndClose(get)
	if get.StatusCode < 200 || get.StatusCode >= 300 {
		return "", statusError("webdav_verify_get", get.StatusCode)
	}
	sum, n, err := hashStream(get.Body, 0)
	if err != nil {
		return "", transportError(ctx, err, "webdav_verify_read")
	}
	if n != size {
		return "", newCloudError(ClassSizeMismatch, "webdav_verify_size_mismatch", false)
	}
	if sum != expectedSHA {
		return "", newCloudError(ClassHashMismatch, "webdav_verify_hash_mismatch", false)
	}
	return "sha256", nil
}

func (a *webdavAdapter) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	segments, cerr := validateObjectKey(key)
	if cerr != nil {
		return nil, cerr
	}
	resp, err := a.do(ctx, http.MethodGet, a.remoteURL(segments), nil, -1, nil, a.uploadTimeout, "webdav_open")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		status := resp.StatusCode
		discardAndClose(resp)
		return nil, statusError("webdav_open", status)
	}
	return resp.Body, nil
}

// Share 仅在配置了 public_base_url 时返回拼接的公开链接；URL 不代表远端已存在或已校验。
func (a *webdavAdapter) Share(ctx context.Context, remoteID string) (Share, error) {
	segments, cerr := validateObjectKey(remoteID)
	if cerr != nil {
		return Share{}, cerr
	}
	if a.publicBase == nil {
		return Share{Status: "unsupported"}, nil
	}
	return Share{URL: joinURLPath(a.publicBase, segments, false).String(), Status: "ready"}, nil
}

// Health 使用 PROPFIND Depth:0 探测基地址，区分认证失败、路径缺失、方法不支持与网络失败。
func (a *webdavAdapter) Health(ctx context.Context) error {
	resp, err := a.do(ctx, "PROPFIND", a.base, strings.NewReader(webdavPropfindBody), int64(len(webdavPropfindBody)),
		map[string]string{"Depth": "0", "Content-Type": "application/xml; charset=utf-8"},
		a.requestTimeout, "webdav_health")
	if err != nil {
		return err
	}
	status := resp.StatusCode
	discardAndClose(resp)
	switch {
	case status == http.StatusMultiStatus || status == http.StatusOK:
		return nil
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return newCloudError(ClassAuth, "webdav_auth_rejected", false)
	case status == http.StatusNotFound:
		return newCloudError(ClassConfig, "webdav_base_path_missing", false)
	case status == http.StatusMethodNotAllowed || status == http.StatusNotImplemented:
		return newCloudError(ClassUnsupported, "webdav_propfind_unsupported", false)
	default:
		return statusError("webdav_health", status)
	}
}

func (a *webdavAdapter) Capabilities() Capabilities {
	return Capabilities{Upload: true, Download: true, Share: a.publicBase != nil}
}
