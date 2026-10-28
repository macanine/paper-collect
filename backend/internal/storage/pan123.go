package storage

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// 123 云盘开放平台适配器。
//
// 当前只实现已确认的部分：access_token 获取与 Health。上传、下载、分享依赖的接口路径、
// 分片规则与错误码无法从官方文档核对（官方页面为 JS 渲染，本环境无法抓取），因此这些操作
// 明确返回 unsupported 并携带固定错误码，不返回伪造的成功结果。
//
// access_token 的路径与字段参照社区 SDK（gitee 上的 123pan 示例）：
// POST {base}/api/v1/access_token，请求头 Platform: open_platform，表单字段 clientID/clientSecret，
// 响应 {code, message, data:{accessToken, expiredAt}}。该参照未经官方文档或真实服务核对。
const (
	pan123DefaultBaseURL  = "https://open-api.123pan.com"
	pan123DefaultParentID = "0"
	pan123Platform        = "open_platform"
	pan123MaxUploadBytes  = 512 << 20 // 系统单文件上限；123 官方的单文件与分片限制未核实，不以此为准
	pan123RequestTimeout  = 30 * time.Second
	pan123TokenSkew       = time.Minute
)

var pan123ParentRe = regexp.MustCompile(`^\d+$`)

type pan123Adapter struct {
	origin         string
	parentFileID   string
	clientID       string
	clientSecret   string
	client         *http.Client
	requestTimeout time.Duration
	maxUploadBytes int64
	now            func() time.Time

	mu           sync.Mutex
	token        string
	tokenExpires time.Time
}

// New123Pan 构造 123 云盘适配器；client 为 nil 时使用默认 http.Client。
func New123Pan(cfg Config, client *http.Client) (Adapter, error) {
	rawBase := strings.TrimSpace(cfg.Settings["base_url"])
	if rawBase == "" {
		rawBase = pan123DefaultBaseURL
	}
	base, cerr := normalizeHTTPBase(rawBase)
	if cerr != nil {
		return nil, cerr
	}
	if base.Path != "/" {
		return nil, newCloudError(ClassConfig, "pan123_base_url_path_unsupported", false)
	}
	parentID := strings.TrimSpace(cfg.Settings["parent_file_id"])
	if parentID == "" {
		parentID = pan123DefaultParentID
	}
	if !pan123ParentRe.MatchString(parentID) {
		return nil, newCloudError(ClassConfig, "pan123_parent_file_id_invalid", false)
	}
	clientID := strings.TrimSpace(cfg.Secrets["client_id"])
	clientSecret := strings.TrimSpace(cfg.Secrets["client_secret"])
	if clientID == "" || clientSecret == "" {
		return nil, newCloudError(ClassConfig, "pan123_credentials_required", false)
	}
	if client == nil {
		client = &http.Client{}
	}
	return &pan123Adapter{
		origin:         base.Scheme + "://" + base.Host,
		parentFileID:   parentID,
		clientID:       clientID,
		clientSecret:   clientSecret,
		client:         client,
		requestTimeout: pan123RequestTimeout,
		maxUploadBytes: pan123MaxUploadBytes,
		now:            time.Now,
	}, nil
}

type pan123Envelope struct {
	Code int             `json:"code"`
	Data json.RawMessage `json:"data"`
}

type pan123TokenData struct {
	AccessToken string `json:"accessToken"`
	ExpiredAt   string `json:"expiredAt"`
}

// accessToken 返回有效的 access_token；未过期时复用缓存，不重复请求。
// 只有 expiredAt 可解析时才缓存，避免把不确定有效期的令牌长期保留。
func (a *pan123Adapter) accessToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.token != "" && a.now().Add(pan123TokenSkew).Before(a.tokenExpires) {
		return a.token, nil
	}

	reqCtx, cancel := context.WithTimeout(ctx, a.requestTimeout)
	defer cancel()
	form := url.Values{"clientID": {a.clientID}, "clientSecret": {a.clientSecret}}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, a.origin+"/api/v1/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", newCloudError(ClassConfig, "pan123_request_invalid", false)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Platform", pan123Platform)
	req.Header.Set("User-Agent", webdavUserAgent)
	resp, err := a.client.Do(req)
	if err != nil {
		return "", transportError(ctx, err, "pan123_token")
	}
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, lanzouMaxTextBytes))
	_ = resp.Body.Close()
	if readErr != nil {
		return "", transportError(ctx, readErr, "pan123_token")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", statusError("pan123_token", resp.StatusCode)
	}

	var envelope pan123Envelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return "", newCloudError(ClassUnknown, "pan123_token_response_invalid", false)
	}
	switch {
	case envelope.Code == 0:
	case envelope.Code == 401:
		return "", newCloudError(ClassAuth, "pan123_token_rejected", false)
	case envelope.Code == 429:
		return "", newCloudError(ClassUnavailable, "pan123_rate_limited", true)
	default:
		return "", newCloudError(ClassUnknown, "pan123_token_code_error", false)
	}
	var data pan123TokenData
	if err := json.Unmarshal(envelope.Data, &data); err != nil || data.AccessToken == "" {
		return "", newCloudError(ClassUnknown, "pan123_token_response_invalid", false)
	}
	if expires, err := time.Parse(time.RFC3339, data.ExpiredAt); err == nil {
		a.token = data.AccessToken
		a.tokenExpires = expires
	} else {
		a.token = ""
		a.tokenExpires = time.Time{}
	}
	return data.AccessToken, nil
}

// Put 当前不可用：上传接口未经核对，因此在校验输入与大小上限后明确返回 unsupported。
// 不发出任何网络请求，也不返回成功。
func (a *pan123Adapter) Put(ctx context.Context, obj Object) (Stored, error) {
	if _, cerr := validateObjectKey(obj.Key); cerr != nil {
		return Stored{}, cerr
	}
	if obj.Path == "" {
		return Stored{}, newCloudError(ClassInvalidInput, "pan123_source_path_required", false)
	}
	if obj.Size <= 0 {
		return Stored{}, newCloudError(ClassInvalidInput, "pan123_file_empty", false)
	}
	if obj.Size > a.maxUploadBytes {
		return Stored{}, newCloudError(ClassUnsupported, "pan123_file_too_large", false)
	}
	return Stored{}, newCloudError(ClassUnsupported, "123pan_upload_unverified", false)
}

// Open 当前不可用：直链获取接口未经核对。
func (a *pan123Adapter) Open(ctx context.Context, remoteID string) (io.ReadCloser, error) {
	return nil, newCloudError(ClassUnsupported, "123pan_download_unverified", false)
}

// Share 当前不可用：分享接口未经核对。返回 unsupported 状态且不构造 URL。
func (a *pan123Adapter) Share(ctx context.Context, remoteID string) (Share, error) {
	return Share{Status: "unsupported"}, newCloudError(ClassUnsupported, "123pan_share_unverified", false)
}

// Health 通过获取 access_token 验证 client_id/client_secret 与网络连通性。
func (a *pan123Adapter) Health(ctx context.Context) error {
	_, err := a.accessToken(ctx)
	return err
}

// Capabilities 全部为 false：上传、下载、分享均未经核对，不对外声称可用。
func (a *pan123Adapter) Capabilities() Capabilities {
	return Capabilities{Upload: false, Download: false, Share: false}
}
