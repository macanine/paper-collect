package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 蓝奏云适配器（仅支持管理员导入的 Cookie 会话，不做账号密码登录）。
// 协议逻辑移植自 gaogao src/ops/cloud/lanzou/client.ts 与 driver.ts：
// html5up.php 上传、doupload.php task=5 列表与 task=22 分享、分享页 downprocess 直链、
// “验证并下载”页、acw_sc__v2 挑战与 mydisk 会话验证。
const (
	lanzouDefaultBaseURL   = "https://pc.woozooo.com"
	lanzouDefaultFolderID  = "-1"
	lanzouUserAgent        = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36 Edg/141.0.0.0"
	lanzouMaxUploadBytes   = 100 << 20 // 免费账号单文件上限；VIP 账号的实际上限由远端决定
	lanzouMaxDownloadBytes = 512 << 20
	lanzouMaxCookieBytes   = 8 << 10
	lanzouMaxTextBytes     = 4 << 20
	lanzouRequestTimeout   = 30 * time.Second
	lanzouUploadTimeout    = 10 * time.Minute
	lanzouDownloadTimeout  = 5 * time.Minute
	lanzouVerifyDelay      = 2 * time.Second // “验证并下载”页需要等待脚本，过快提交会被判失败
	lanzouRetryDelay       = 300 * time.Millisecond
	lanzouBusyDelay        = time.Second
	lanzouHTMLAccept       = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
	lanzouJSONAccept       = "application/json, text/javascript, */*; q=0.01"
	lanzouAcceptLanguage   = "zh-CN,zh;q=0.9,en;q=0.8"
)

var (
	lanzouFolderIDRe    = regexp.MustCompile(`^-?\d+$`)
	lanzouFileIDRe      = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	lanzouHostRe        = regexp.MustCompile(`(?i)(^|\.)lanzou[a-z]*\.com$`)
	lanzouAuthMsgRe     = regexp.MustCompile(`(?i)登录|登陆|账号|帐户|密码|cookie|激活|未登录|login|forbidden|unauthor`)
	lanzouQuotaMsgRe    = regexp.MustCompile(`(?i)空间|容量|不足|已满|超限|quota`)
	lanzouMissingMsgRe  = regexp.MustCompile(`(?i)不存在|已删除|取消|失效|not\s*found`)
	lanzouBusyMsgRe     = regexp.MustCompile(`(?i)频繁|稍后|busy|too\s*many|timeout|超时`)
	lanzouInactiveRe    = regexp.MustCompile(`(?i)账号未激活|not\s*activated|nosucss`)
	lanzouPasswordInfo  = regexp.MustCompile(`(?i)密码|password`)
	lanzouHTMLContentRe = regexp.MustCompile(`text/html`)
)

type lanzouAdapter struct {
	origin           string // scheme://host，无末尾斜杠
	folderID         string
	verifyHash       bool
	client           *http.Client // 带 Cookie Jar：网盘与分享页
	cdn              *http.Client // 无 Jar：直链下载与验证页提交，登录 Cookie 不发往 CDN
	jar              http.CookieJar
	requestTimeout   time.Duration
	uploadTimeout    time.Duration
	downloadTimeout  time.Duration
	verifyDelay      time.Duration
	retryDelay       time.Duration
	busyDelay        time.Duration
	maxUploadBytes   int64
	maxDownloadBytes int64

	mu  sync.Mutex
	uid string
}

// NewLanzou 构造蓝奏适配器。client 为 nil 时使用默认 http.Client（测试可注入 fake RoundTripper）。
// 注入的 client 不会被修改：内部复制一份并挂上 Cookie Jar 与无 Jar 的 CDN 版本。
func NewLanzou(cfg Config, client *http.Client) (Adapter, error) {
	rawBase := strings.TrimSpace(cfg.Settings["base_url"])
	if rawBase == "" {
		rawBase = lanzouDefaultBaseURL
	}
	base, cerr := normalizeHTTPBase(rawBase)
	if cerr != nil {
		return nil, cerr
	}
	if base.Path != "/" {
		return nil, newCloudError(ClassConfig, "lanzou_base_url_path_unsupported", false)
	}
	origin := base.Scheme + "://" + base.Host

	folderID := strings.TrimSpace(cfg.Settings["folder_id"])
	if folderID == "" {
		folderID = lanzouDefaultFolderID
	}
	if !lanzouFolderIDRe.MatchString(folderID) {
		return nil, newCloudError(ClassConfig, "lanzou_folder_id_invalid", false)
	}

	verifyHash := false
	switch strings.ToLower(strings.TrimSpace(cfg.Settings["verify_remote_hash"])) {
	case "", "false":
	case "true":
		verifyHash = true
	default:
		return nil, newCloudError(ClassConfig, "lanzou_verify_flag_invalid", false)
	}

	cookie := strings.TrimSpace(cfg.Secrets["cookie"])
	if cookie == "" {
		return nil, newCloudError(ClassConfig, "lanzou_cookie_required", false)
	}
	if len(cookie) > lanzouMaxCookieBytes {
		return nil, newCloudError(ClassConfig, "lanzou_cookie_too_long", false)
	}
	if strings.ContainsAny(cookie, "\r\n") {
		return nil, newCloudError(ClassConfig, "lanzou_cookie_invalid", false)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, newCloudError(ClassUnknown, "lanzou_jar_unavailable", false)
	}
	seedLanzouCookies(jar, base, cookie)

	if client == nil {
		client = &http.Client{}
	}
	web := *client
	web.Jar = jar
	cdn := *client
	cdn.Jar = nil

	return &lanzouAdapter{
		origin:           origin,
		folderID:         folderID,
		verifyHash:       verifyHash,
		client:           &web,
		cdn:              &cdn,
		jar:              jar,
		requestTimeout:   lanzouRequestTimeout,
		uploadTimeout:    lanzouUploadTimeout,
		downloadTimeout:  lanzouDownloadTimeout,
		verifyDelay:      lanzouVerifyDelay,
		retryDelay:       lanzouRetryDelay,
		busyDelay:        lanzouBusyDelay,
		maxUploadBytes:   lanzouMaxUploadBytes,
		maxDownloadBytes: lanzouMaxDownloadBytes,
	}, nil
}

// seedLanzouCookies 把 Secrets["cookie"] 中的 name=value 对种为 base 主机的 host-only Cookie。
func seedLanzouCookies(jar http.CookieJar, base *url.URL, header string) {
	var cookies []*http.Cookie
	for _, part := range strings.Split(header, ";") {
		part = strings.TrimSpace(part)
		eq := strings.IndexByte(part, '=')
		if eq <= 0 {
			continue
		}
		name := strings.TrimSpace(part[:eq])
		if name == "" {
			continue
		}
		cookies = append(cookies, &http.Cookie{Name: name, Value: strings.TrimSpace(part[eq+1:]), Path: "/"})
	}
	jar.SetCookies(&url.URL{Scheme: base.Scheme, Host: base.Host, Path: "/"}, cookies)
}

func (a *lanzouAdapter) cookieValue(u *url.URL, name string) string {
	for _, c := range a.jar.Cookies(u) {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

func (a *lanzouAdapter) setCookie(u *url.URL, name, value string) {
	a.jar.SetCookies(&url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/"}, []*http.Cookie{{Name: name, Value: value, Path: "/"}})
}

func (a *lanzouAdapter) currentUID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.uid
}

func (a *lanzouAdapter) storeUID(uid string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.uid = uid
}

// lzResponse 是读完响应体后的快照；响应体已关闭。
type lzResponse struct {
	status   int
	finalURL *url.URL
	text     string
}

type lzOptions struct {
	op             string
	timeout        time.Duration
	attempts       int
	retry5xx       bool
	retryTransport bool
}

// send 发送请求并处理 acw_sc__v2 挑战。build 每次尝试都会被重新调用，保证请求体可重放。
func (a *lanzouAdapter) send(ctx context.Context, client *http.Client, build func(context.Context) (*http.Request, error), opts lzOptions) (*lzResponse, error) {
	attempts := max(opts.attempts, 1)
	timeout := opts.timeout
	if timeout <= 0 {
		timeout = a.requestTimeout
	}
	var lastErr error = newCloudError(ClassUnknown, opts.op+"_retry_exhausted", false)
	for attempt := 1; attempt <= attempts; attempt++ {
		reqCtx, cancel := context.WithTimeout(ctx, timeout)
		req, err := build(reqCtx)
		if err != nil {
			cancel()
			return nil, newCloudError(ClassInvalidInput, opts.op+"_request_invalid", false)
		}
		setLanzouHeaders(req)
		resp, err := client.Do(req)
		if err != nil {
			cancel()
			mapped := transportError(ctx, err, opts.op)
			lastErr = mapped
			if attempt < attempts && mapped.Retryable && opts.retryTransport {
				if waitErr := sleepLanzou(ctx, a.retryDelay*time.Duration(attempt)); waitErr != nil {
					return nil, waitErr
				}
				continue
			}
			return nil, mapped
		}
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, lanzouMaxTextBytes))
		_ = resp.Body.Close()
		cancel()
		if readErr != nil {
			mapped := transportError(ctx, readErr, opts.op)
			lastErr = mapped
			if attempt < attempts {
				if waitErr := sleepLanzou(ctx, a.retryDelay*time.Duration(attempt)); waitErr != nil {
					return nil, waitErr
				}
				continue
			}
			return nil, mapped
		}

		final := req.URL
		if resp.Request != nil && resp.Request.URL != nil {
			final = resp.Request.URL
		}
		out := &lzResponse{status: resp.StatusCode, finalURL: final, text: string(raw)}
		if arg1 := extractAcwArg1(out.text); arg1 != "" {
			if solved := computeAcwScV2(arg1); solved != "" && a.cookieValue(req.URL, "acw_sc__v2") != solved {
				a.setCookie(req.URL, "acw_sc__v2", solved)
				if attempt < attempts {
					continue
				}
			}
		}
		if opts.retry5xx && resp.StatusCode >= 500 && attempt < attempts {
			lastErr = statusError(opts.op, resp.StatusCode)
			if waitErr := sleepLanzou(ctx, a.retryDelay*time.Duration(attempt)); waitErr != nil {
				return nil, waitErr
			}
			continue
		}
		return out, nil
	}
	return nil, lastErr
}

func sleepLanzou(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		if err := ctx.Err(); err != nil {
			return transportError(ctx, err, "lanzou_wait")
		}
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return transportError(ctx, ctx.Err(), "lanzou_wait")
	case <-timer.C:
		return nil
	}
}

func setLanzouHeaders(req *http.Request) {
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", lanzouUserAgent)
	}
	if req.Header.Get("Accept-Language") == "" {
		req.Header.Set("Accept-Language", lanzouAcceptLanguage)
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "*/*")
	}
}

func lanzouGET(target string, headers map[string]string) func(context.Context) (*http.Request, error) {
	return func(ctx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return nil, err
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		return req, nil
	}
}

func lanzouPOSTForm(target string, fields map[string]string, headers map[string]string) func(context.Context) (*http.Request, error) {
	encoded := valuesOf(fields).Encode()
	return func(ctx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(encoded))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		return req, nil
	}
}

func valuesOf(fields map[string]string) url.Values {
	values := url.Values{}
	for key, value := range fields {
		values.Set(key, value)
	}
	return values
}

func lanzouOK(status int) bool {
	return status >= 200 && status < 300
}

func parseLanzouJSON(text string) (map[string]any, bool) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil || payload == nil {
		return nil, false
	}
	return payload, true
}

// lanzouZT 返回响应的 zt；无法解析时为 -1。
func lanzouZT(text string) int {
	payload, ok := parseLanzouJSON(text)
	if !ok {
		return -1
	}
	zt, ok := jsonInt(payload["zt"])
	if !ok {
		return -1
	}
	return zt
}

func jsonInt(value any) (int, bool) {
	switch v := value.(type) {
	case float64:
		if v == math.Trunc(v) {
			return int(v), true
		}
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		return n, err == nil
	}
	return 0, false
}

func jsonString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}

// lanzouPayloadError 按 zt/info 等文本归类。Detail 仅使用固定机器码，不回显远端消息。
func lanzouPayloadError(payload map[string]any) *CloudError {
	message := jsonString(payload["info"])
	if message == "" {
		message = jsonString(payload["inf"])
	}
	if message == "" {
		message = jsonString(payload["msgs"])
	}
	if message == "" {
		message = jsonString(payload["text"])
	}
	switch {
	case lanzouAuthMsgRe.MatchString(message):
		return newCloudError(ClassAuth, "lanzou_api_auth", false)
	case lanzouQuotaMsgRe.MatchString(message):
		return newCloudError(ClassQuota, "lanzou_api_quota", false)
	case lanzouMissingMsgRe.MatchString(message):
		return newCloudError(ClassFileMissing, "lanzou_api_missing", false)
	case lanzouBusyMsgRe.MatchString(message):
		return newCloudError(ClassUnavailable, "lanzou_api_busy", true)
	default:
		return newCloudError(ClassUnknown, "lanzou_api_unknown", false)
	}
}

// isLanzouHost 判断是否为蓝奏域名（woozooo.com 及 lanzou*.com）。
func isLanzouHost(host string) bool {
	h := strings.ToLower(host)
	return h == "woozooo.com" || strings.HasSuffix(h, ".woozooo.com") || lanzouHostRe.MatchString(h)
}

// sanitizeRemoteName 只保留最后一段文件名，去掉控制字符并规范空白，截断到 120 个字符。
func sanitizeRemoteName(name, fallback string) string {
	clean := func(value string) string {
		if idx := strings.LastIndexAny(value, `/\`); idx >= 0 {
			value = value[idx+1:]
		}
		stripped := strings.Map(func(r rune) rune {
			if r < 0x20 || r == 0x7f {
				return -1
			}
			return r
		}, value)
		return strings.Join(strings.Fields(stripped), " ")
	}
	cleaned := clean(name)
	if cleaned == "" || cleaned == "." || cleaned == ".." {
		cleaned = clean(fallback)
	}
	if cleaned == "" || cleaned == "." || cleaned == ".." {
		cleaned = "未命名资料"
	}
	return truncateRunes(cleaned, 120)
}

// buildLanzouMultipart 生成 multipart 的前缀与后缀字节，文件内容由调用方以 SectionReader 串接，
// 这样可以在不把整个文件读入内存的情况下得到确定的 Content-Length，并支持重试时重放。
func buildLanzouMultipart(fields [][2]string, fileField, fileName string) (prefix, suffix []byte, boundary string, err error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for _, field := range fields {
		if err := writer.WriteField(field[0], field[1]); err != nil {
			return nil, nil, "", err
		}
	}
	if _, err := writer.CreateFormFile(fileField, fileName); err != nil {
		return nil, nil, "", err
	}
	prefix = append([]byte(nil), buf.Bytes()...)
	buf.Reset()
	if err := writer.Close(); err != nil {
		return nil, nil, "", err
	}
	suffix = append([]byte(nil), buf.Bytes()...)
	return prefix, suffix, writer.Boundary(), nil
}

type lanzouShare struct {
	shareURL string
	password string
}

type lanzouDirectLink struct {
	candidates []string
}

// syncUID 读取 mydisk 页面：验证会话未被踢出，并取得 uid（doupload 的 uid 参数）。
func (a *lanzouAdapter) syncUID(ctx context.Context) error {
	target := a.origin + "/mydisk.php?item=files&action=index"
	resp, err := a.send(ctx, a.client, lanzouGET(target, map[string]string{"Accept": lanzouHTMLAccept}), lzOptions{
		op: "lanzou_mydisk", attempts: 2, retry5xx: true, retryTransport: true,
	})
	if err != nil {
		return err
	}
	if !lanzouOK(resp.status) {
		return statusError("lanzou_mydisk", resp.status)
	}
	html := trimHTML(resp.text)
	if strings.Contains(strings.ToLower(resp.finalURL.Host), "accounts.woozooo.com") || looksLikeLoginPage(html) {
		return newCloudError(ClassAuth, "lanzou_session_rejected", false)
	}
	if lanzouInactiveRe.MatchString(html) {
		return newCloudError(ClassAuth, "lanzou_account_not_activated", false)
	}
	if uid := extractUID(html); uid != "" {
		a.storeUID(uid)
	}
	return nil
}

// doUpload 调用 doupload.php（列表、分享等）。zt=4 表示服务端忙，有限次退避重试；zt=9 为会话失效。
func (a *lanzouAdapter) doUpload(ctx context.Context, fields map[string]string) (map[string]any, error) {
	target := a.origin + "/doupload.php"
	if uid := a.currentUID(); uid != "" {
		target += "?uid=" + url.QueryEscape(uid)
	}
	headers := map[string]string{
		"Accept":  lanzouJSONAccept,
		"Origin":  a.origin,
		"Referer": a.origin + "/mydisk.php?item=files&action=index",
	}
	busy := 0
	for {
		resp, err := a.send(ctx, a.client, lanzouPOSTForm(target, fields, headers), lzOptions{
			op: "lanzou_doupload", attempts: 2, retry5xx: true, retryTransport: true,
		})
		if err != nil {
			return nil, err
		}
		payload, ok := parseLanzouJSON(resp.text)
		if !ok {
			if !lanzouOK(resp.status) {
				return nil, statusError("lanzou_doupload", resp.status)
			}
			return nil, newCloudError(ClassUnknown, "lanzou_invalid_json", false)
		}
		zt, _ := jsonInt(payload["zt"])
		switch zt {
		case 1, 2:
			return payload, nil
		case 9:
			return nil, newCloudError(ClassAuth, "lanzou_session_expired", false)
		case 4:
			if busy < 2 {
				busy++
				if waitErr := sleepLanzou(ctx, a.busyDelay*time.Duration(busy)); waitErr != nil {
					return nil, waitErr
				}
				continue
			}
		}
		return nil, lanzouPayloadError(payload)
	}
}

// listFirstPage 请求根目录第一页，用于健康检查确认 doupload 接口与会话可用。
func (a *lanzouAdapter) listFirstPage(ctx context.Context) error {
	_, err := a.doUpload(ctx, map[string]string{"task": "5", "folder_id": a.folderID, "pg": "1"})
	return err
}

// getFileShare 调用 task=22 获取分享页地址。分享页域名必须是蓝奏域名并强制 https。
func (a *lanzouAdapter) getFileShare(ctx context.Context, fileID string) (lanzouShare, error) {
	if strings.TrimSpace(fileID) == "" {
		return lanzouShare{}, newCloudError(ClassFileMissing, "lanzou_file_id_missing", false)
	}
	payload, err := a.doUpload(ctx, map[string]string{"task": "22", "file_id": fileID})
	if err != nil {
		return lanzouShare{}, err
	}
	info, ok := payload["info"].(map[string]any)
	if !ok {
		return lanzouShare{}, lanzouPayloadError(payload)
	}
	isNewd := jsonString(info["is_newd"])
	fID := jsonString(info["f_id"])
	if isNewd == "" || fID == "" {
		return lanzouShare{}, newCloudError(ClassFileMissing, "lanzou_share_info_missing", false)
	}
	origin, err := url.Parse(isNewd)
	if err != nil || origin.Host == "" {
		return lanzouShare{}, newCloudError(ClassUnknown, "lanzou_share_origin_invalid", false)
	}
	if !isLanzouHost(origin.Hostname()) {
		return lanzouShare{}, newCloudError(ClassLinkExpired, "lanzou_share_host_unexpected", false)
	}
	origin.Scheme = "https"
	origin.Path = ""
	origin.RawQuery = ""
	origin.Fragment = ""
	ref, err := url.Parse(strings.TrimLeft(fID, "/"))
	if err != nil {
		return lanzouShare{}, newCloudError(ClassUnknown, "lanzou_share_id_invalid", false)
	}
	return lanzouShare{shareURL: origin.ResolveReference(ref).String(), password: jsonString(info["pwd"])}, nil
}

// uploadFile 通过 html5up.php 上传。成功只代表远端返回了 fileId；校验由调用方决定。
func (a *lanzouAdapter) uploadFile(ctx context.Context, sourcePath, remoteName string, size int64, progress func(int64, int64)) (string, error) {
	if a.maxUploadBytes > 0 && size > a.maxUploadBytes {
		return "", newCloudError(ClassUnsupported, "lanzou_file_too_large", false)
	}
	if size <= 0 {
		return "", newCloudError(ClassInvalidInput, "lanzou_file_empty", false)
	}
	src, err := os.Open(sourcePath)
	if err != nil {
		return "", newCloudError(ClassFileMissing, "lanzou_local_file_missing", false)
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", newCloudError(ClassInvalidInput, "lanzou_local_file_not_regular", false)
	}
	if info.Size() != size {
		return "", newCloudError(ClassSizeMismatch, "lanzou_local_size_mismatch", false)
	}

	prefix, suffix, boundary, err := buildLanzouMultipart([][2]string{
		{"task", "1"},
		{"vie", "2"},
		{"ve", "2"},
		{"id", "WU_FILE_0"},
		{"name", remoteName},
		{"folder_id_bb_n", a.folderID},
	}, "upload_file", remoteName)
	if err != nil {
		return "", newCloudError(ClassUnknown, "lanzou_multipart_failed", false)
	}
	total := int64(len(prefix)) + size + int64(len(suffix))
	target := a.origin + "/html5up.php"
	referer := a.origin + "/mydisk.php?item=files&action=index"
	build := func(reqCtx context.Context) (*http.Request, error) {
		body := io.MultiReader(bytes.NewReader(prefix), &progressReader{r: io.NewSectionReader(src, 0, size), total: size, report: progress}, bytes.NewReader(suffix))
		req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, target, body)
		if err != nil {
			return nil, err
		}
		req.ContentLength = total
		req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
		req.Header.Set("Origin", a.origin)
		req.Header.Set("Referer", referer)
		req.Header.Set("Accept", lanzouJSONAccept)
		return req, nil
	}

	var resp *lzResponse
	for attempt := 1; attempt <= 3; attempt++ {
		current, err := a.send(ctx, a.client, build, lzOptions{
			op: "lanzou_upload", timeout: a.uploadTimeout, attempts: 3, retry5xx: false, retryTransport: false,
		})
		if err != nil {
			return "", err
		}
		resp = current
		// zt=4：服务端忙，上传未落库，退避后重发是安全的。
		if lanzouZT(resp.text) == 4 && attempt < 3 {
			if waitErr := sleepLanzou(ctx, a.busyDelay*time.Duration(attempt)); waitErr != nil {
				return "", waitErr
			}
			continue
		}
		break
	}

	payload, ok := parseLanzouJSON(resp.text)
	if !ok {
		if !lanzouOK(resp.status) {
			return "", statusError("lanzou_upload", resp.status)
		}
		return "", newCloudError(ClassUnknown, "lanzou_upload_invalid_json", false)
	}
	zt, _ := jsonInt(payload["zt"])
	if zt == 9 {
		return "", newCloudError(ClassAuth, "lanzou_session_expired", false)
	}
	if zt != 1 && zt != 2 {
		return "", lanzouPayloadError(payload)
	}
	items, _ := payload["text"].([]any)
	for _, item := range items {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if id := jsonString(record["id"]); id != "" {
			return id, nil
		}
	}
	return "", newCloudError(ClassUnknown, "lanzou_upload_missing_file_id", false)
}

func (a *lanzouAdapter) postDownprocess(ctx context.Context, ajaxURL string, fields map[string]string, origin, referer string) (*lzResponse, error) {
	resp, err := a.send(ctx, a.client, lanzouPOSTForm(ajaxURL, fields, map[string]string{
		"Origin":  origin,
		"Referer": referer,
		"Accept":  lanzouJSONAccept,
	}), lzOptions{op: "lanzou_downprocess", attempts: 2, retry5xx: true, retryTransport: true})
	if err != nil {
		return nil, err
	}
	if !lanzouOK(resp.status) {
		return nil, statusError("lanzou_downprocess", resp.status)
	}
	return resp, nil
}

// getDirectLink 解析分享页得到直链候选。无密码页走 iframe /fn；有密码页需要提取码。
func (a *lanzouAdapter) getDirectLink(ctx context.Context, shareURL, password string) (lanzouDirectLink, error) {
	parsed, err := url.Parse(shareURL)
	if err != nil || parsed.Host == "" {
		return lanzouDirectLink{}, newCloudError(ClassLinkExpired, "lanzou_share_url_invalid", false)
	}
	shareOrigin := parsed.Scheme + "://" + parsed.Host

	page, err := a.send(ctx, a.client, lanzouGET(shareURL, map[string]string{
		"Referer": shareURL,
		"Accept":  lanzouHTMLAccept,
	}), lzOptions{op: "lanzou_share_page", attempts: 3, retry5xx: true, retryTransport: true})
	if err != nil {
		return lanzouDirectLink{}, err
	}
	if !lanzouOK(page.status) {
		return lanzouDirectLink{}, statusError("lanzou_share_page", page.status)
	}
	shareHTML := stripJsComments(trimHTML(page.text))
	requiresPassword := passwordDivRe.MatchString(shareHTML)

	fnURL, hasFn := buildAbsoluteURL(shareURL, extractIframeFnPath(shareHTML))
	if hasFn && !requiresPassword {
		fnPage, err := a.send(ctx, a.client, lanzouGET(fnURL, map[string]string{
			"Referer": shareURL,
			"Accept":  lanzouHTMLAccept,
		}), lzOptions{op: "lanzou_fn_page", attempts: 3, retry5xx: true, retryTransport: true})
		if err != nil {
			return lanzouDirectLink{}, err
		}
		if !lanzouOK(fnPage.status) {
			return lanzouDirectLink{}, statusError("lanzou_fn_page", fnPage.status)
		}
		fnHTML := stripJsComments(trimHTML(fnPage.text))
		params, ok := extractDownprocessParams(fnHTML, fnURL)
		if !ok {
			return lanzouDirectLink{}, newCloudError(ClassLinkExpired, "lanzou_fn_layout_unknown", false)
		}
		ajax, err := a.postDownprocess(ctx, params.ajaxURL, params.fields, shareOrigin, fnURL)
		if err != nil {
			return lanzouDirectLink{}, err
		}
		return buildLanzouDirectLink(fnHTML, ajax.text)
	}

	if !requiresPassword {
		return lanzouDirectLink{}, newCloudError(ClassLinkExpired, "lanzou_share_layout_unknown", false)
	}
	if strings.TrimSpace(password) == "" {
		return lanzouDirectLink{}, newCloudError(ClassLinkExpired, "lanzou_share_password_required", false)
	}
	params, ok := extractDownprocessParams(shareHTML, shareURL)
	if !ok {
		return lanzouDirectLink{}, newCloudError(ClassLinkExpired, "lanzou_share_layout_unknown", false)
	}
	withPassword := make(map[string]string, len(params.fields)+1)
	for key, value := range params.fields {
		withPassword[key] = value
	}
	withPassword["p"] = password
	ajax, err := a.postDownprocess(ctx, params.ajaxURL, withPassword, shareOrigin, shareURL)
	if err != nil {
		return lanzouDirectLink{}, err
	}
	return buildLanzouDirectLink(shareHTML, ajax.text)
}

func buildLanzouDirectLink(html, body string) (lanzouDirectLink, error) {
	payload, ok := parseLanzouJSON(body)
	if !ok {
		return lanzouDirectLink{}, newCloudError(ClassUnknown, "lanzou_downprocess_invalid_json", false)
	}
	zt, _ := jsonInt(payload["zt"])
	if zt != 1 && zt != 2 {
		info := jsonString(payload["inf"])
		if info == "" {
			info = jsonString(payload["info"])
		}
		if lanzouPasswordInfo.MatchString(info) {
			return lanzouDirectLink{}, newCloudError(ClassLinkExpired, "lanzou_share_password_invalid", false)
		}
		return lanzouDirectLink{}, newCloudError(ClassLinkExpired, "lanzou_downprocess_failed", false)
	}
	dom := jsonString(payload["dom"])
	fileURL := jsonString(payload["url"])

	var candidates []string
	add := func(candidate string) {
		if candidate == "" {
			return
		}
		for _, existing := range candidates {
			if existing == candidate {
				return
			}
		}
		candidates = append(candidates, candidate)
	}
	if absoluteHTTPRe.MatchString(fileURL) {
		add(fileURL)
	} else {
		suffixes := []string{""}
		for _, name := range []string{"down_1", "down_2", "down_3"} {
			value, _ := extractJSStringVar(html, name)
			suffixes = append(suffixes, value)
		}
		for _, suffix := range suffixes {
			if candidate, ok := buildDirectDownloadURL(dom, fileURL, suffix); ok {
				add(candidate)
			}
		}
	}
	if len(candidates) == 0 {
		return lanzouDirectLink{}, newCloudError(ClassLinkExpired, "lanzou_direct_url_missing", false)
	}
	return lanzouDirectLink{candidates: candidates}, nil
}

// getCDN 请求直链 CDN：不带登录 Cookie（除非显式传入 acw 挑战所需的 Cookie）。
// 返回的响应体关闭时释放超时 context。
func (a *lanzouAdapter) getCDN(ctx context.Context, target, referer, cookie string) (*http.Response, error) {
	reqCtx, cancel := context.WithTimeout(ctx, a.downloadTimeout)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, target, nil)
	if err != nil {
		cancel()
		return nil, newCloudError(ClassInvalidInput, "lanzou_download_url_invalid", false)
	}
	req.Header.Set("User-Agent", lanzouUserAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Referer", referer)
	req.Header.Set("Accept-Language", lanzouAcceptLanguage)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := a.cdn.Do(req)
	if err != nil {
		cancel()
		return nil, transportError(ctx, err, "lanzou_download")
	}
	resp.Body = cancelBody{ReadCloser: resp.Body, cancel: cancel}
	if !lanzouOK(resp.StatusCode) {
		status := resp.StatusCode
		discardAndClose(resp)
		return nil, statusError("lanzou_download", status)
	}
	return resp, nil
}

// resolveDownloadResponse 逐层解开直链：acw 挑战、“验证并下载”页，最终返回真正的文件响应。
// 返回的响应体必须由调用方关闭。
func (a *lanzouAdapter) resolveDownloadResponse(ctx context.Context, target, referer, cookie string, depth int) (*http.Response, error) {
	resp, err := a.getCDN(ctx, target, referer, cookie)
	if err != nil {
		return nil, err
	}
	if !lanzouHTMLContentRe.MatchString(strings.ToLower(resp.Header.Get("Content-Type"))) {
		return resp, nil
	}
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, lanzouMaxTextBytes))
	_ = resp.Body.Close()
	if readErr != nil {
		return nil, transportError(ctx, readErr, "lanzou_download")
	}
	html := string(raw)
	finalURL := target
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}

	if arg1 := extractAcwArg1(html); arg1 != "" {
		if solved := computeAcwScV2(arg1); solved != "" {
			if depth < 3 {
				return a.resolveDownloadResponse(ctx, target, referer, "acw_sc__v2="+solved, depth+1)
			}
			return nil, newCloudError(ClassLinkExpired, "lanzou_download_challenge_loop", false)
		}
	}

	verify, ok := extractDownloadVerifyParams(html, finalURL)
	if !ok {
		return nil, newCloudError(ClassLinkExpired, "lanzou_download_got_html", false)
	}
	if waitErr := sleepLanzou(ctx, a.verifyDelay); waitErr != nil {
		return nil, waitErr
	}
	ajaxText, status, err := a.cdnPostForm(ctx, verify.ajaxURL, verify.fields, originOf(target), target)
	if err != nil {
		return nil, err
	}
	if !lanzouOK(status) {
		return nil, statusError("lanzou_download_verify", status)
	}
	payload, ok := parseLanzouJSON(ajaxText)
	if !ok {
		return nil, newCloudError(ClassLinkExpired, "lanzou_download_verify_failed", false)
	}
	nextRaw := jsonString(payload["url"])
	if nextRaw == "" {
		return nil, newCloudError(ClassLinkExpired, "lanzou_download_verify_failed", false)
	}
	next, ok := buildAbsoluteURL(verify.ajaxURL, nextRaw)
	if !ok {
		return nil, newCloudError(ClassLinkExpired, "lanzou_download_url_invalid", false)
	}
	final, err := a.getCDN(ctx, next, target, "")
	if err != nil {
		return nil, err
	}
	if lanzouHTMLContentRe.MatchString(strings.ToLower(final.Header.Get("Content-Type"))) {
		discardAndClose(final)
		return nil, newCloudError(ClassLinkExpired, "lanzou_download_got_html", false)
	}
	return final, nil
}

// cdnPostForm 向直链域名提交“验证并下载”表单，不带登录 Cookie。
func (a *lanzouAdapter) cdnPostForm(ctx context.Context, target string, fields map[string]string, origin, referer string) (string, int, error) {
	reqCtx, cancel := context.WithTimeout(ctx, a.requestTimeout)
	defer cancel()
	req, err := lanzouPOSTForm(target, fields, map[string]string{
		"Origin":  origin,
		"Referer": referer,
		"Accept":  lanzouJSONAccept,
	})(reqCtx)
	if err != nil {
		return "", 0, newCloudError(ClassInvalidInput, "lanzou_download_verify_request_invalid", false)
	}
	setLanzouHeaders(req)
	resp, err := a.cdn.Do(req)
	if err != nil {
		return "", 0, transportError(ctx, err, "lanzou_download_verify")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, lanzouMaxTextBytes))
	if err != nil {
		return "", 0, transportError(ctx, err, "lanzou_download_verify")
	}
	return string(raw), resp.StatusCode, nil
}

func originOf(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

// downloadHash 完整下载一份直链内容并流式计算 SHA-256 与字节数。
func (a *lanzouAdapter) downloadHash(ctx context.Context, target, referer string) (string, int64, error) {
	resp, err := a.resolveDownloadResponse(ctx, target, referer, "", 0)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	sum, n, err := hashStream(resp.Body, a.maxDownloadBytes)
	if err != nil {
		return "", 0, transportError(ctx, err, "lanzou_download_read")
	}
	if a.maxDownloadBytes > 0 && n > a.maxDownloadBytes {
		return "", 0, newCloudError(ClassUnsupported, "lanzou_download_too_large", false)
	}
	return sum, n, nil
}

func (a *lanzouAdapter) resolveFile(ctx context.Context, fileID string) (lanzouShare, lanzouDirectLink, error) {
	share, err := a.getFileShare(ctx, fileID)
	if err != nil {
		return lanzouShare{}, lanzouDirectLink{}, err
	}
	direct, err := a.getDirectLink(ctx, share.shareURL, share.password)
	if err != nil {
		return lanzouShare{}, lanzouDirectLink{}, err
	}
	return share, direct, nil
}

// verifyFile 下载重算远端哈希并与期望比对（仅在 verify_remote_hash=true 时使用）。
func (a *lanzouAdapter) verifyFile(ctx context.Context, fileID, expectedSHA string, expectedSize int64) error {
	share, direct, err := a.resolveFile(ctx, fileID)
	if err != nil {
		return err
	}
	var lastErr error
	sawMismatch := false
	for _, candidate := range direct.candidates {
		sum, n, err := a.downloadHash(ctx, candidate, share.shareURL)
		if err != nil {
			lastErr = err
			continue
		}
		if n == expectedSize && (expectedSHA == "" || sum == expectedSHA) {
			return nil
		}
		sawMismatch = true
	}
	if sawMismatch {
		if expectedSHA != "" {
			return newCloudError(ClassHashMismatch, "lanzou_remote_hash_mismatch", false)
		}
		return newCloudError(ClassSizeMismatch, "lanzou_remote_size_mismatch", false)
	}
	if lastErr != nil {
		return lastErr
	}
	return newCloudError(ClassLinkExpired, "lanzou_download_failed", false)
}

// verifyWithRetry 对新上传的文件重试校验：刚落库的文件分享页可能短暂 404。
// 哈希/大小不符、认证、超限等确定性失败不重试。
func (a *lanzouAdapter) verifyWithRetry(ctx context.Context, fileID, expectedSHA string, expectedSize int64) error {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		err := a.verifyFile(ctx, fileID, expectedSHA, expectedSize)
		if err == nil {
			return nil
		}
		lastErr = err
		switch AsCloudError(err).Class {
		case ClassHashMismatch, ClassSizeMismatch, ClassAuth, ClassUnsupported, ClassQuota, ClassConfig, ClassInvalidInput:
			return err
		}
		if attempt < 3 {
			if waitErr := sleepLanzou(ctx, a.retryDelay*time.Duration(attempt)); waitErr != nil {
				return waitErr
			}
		}
	}
	return lastErr
}

// Put 上传到蓝奏云。Verification 只在真正比对过远端内容时才报告 sha256 或 size。
func (a *lanzouAdapter) Put(ctx context.Context, obj Object) (Stored, error) {
	if _, cerr := validateObjectKey(obj.Key); cerr != nil {
		return Stored{}, cerr
	}
	if obj.Path == "" {
		return Stored{}, newCloudError(ClassInvalidInput, "lanzou_source_path_required", false)
	}
	if obj.Size <= 0 {
		return Stored{}, newCloudError(ClassInvalidInput, "lanzou_file_empty", false)
	}
	if obj.Size > a.maxUploadBytes {
		return Stored{}, newCloudError(ClassUnsupported, "lanzou_file_too_large", false)
	}
	expectedSHA := ""
	if obj.SHA256 != "" {
		normalized, ok := normalizeSHA256(obj.SHA256)
		if !ok {
			return Stored{}, newCloudError(ClassInvalidInput, "sha256_invalid", false)
		}
		expectedSHA = normalized
	}
	name := sanitizeRemoteName(obj.Name, path.Base(obj.Key))
	fileID, err := a.uploadFile(ctx, obj.Path, name, obj.Size, obj.Progress)
	if err != nil {
		return Stored{}, err
	}
	verification := "uploaded"
	if a.verifyHash {
		if err := a.verifyWithRetry(ctx, fileID, expectedSHA, obj.Size); err != nil {
			return Stored{}, err
		}
		verification = "size"
		if expectedSHA != "" {
			verification = "sha256"
		}
	}
	return Stored{RemoteID: fileID, Size: obj.Size, Verification: verification}, nil
}

// Open 通过分享页与直链流式返回文件内容，不落盘。
func (a *lanzouAdapter) Open(ctx context.Context, remoteID string) (io.ReadCloser, error) {
	if !lanzouFileIDRe.MatchString(remoteID) {
		return nil, newCloudError(ClassInvalidInput, "lanzou_remote_id_invalid", false)
	}
	share, direct, err := a.resolveFile(ctx, remoteID)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, candidate := range direct.candidates {
		resp, err := a.resolveDownloadResponse(ctx, candidate, share.shareURL, "", 0)
		if err != nil {
			lastErr = err
			continue
		}
		return resp.Body, nil
	}
	if lastErr == nil {
		lastErr = newCloudError(ClassLinkExpired, "lanzou_open_failed", false)
	}
	return nil, lastErr
}

// Share 返回蓝奏分享页地址；提取码仅在权限允许的详情响应中使用，不写入日志。
func (a *lanzouAdapter) Share(ctx context.Context, remoteID string) (Share, error) {
	if !lanzouFileIDRe.MatchString(remoteID) {
		return Share{}, newCloudError(ClassInvalidInput, "lanzou_remote_id_invalid", false)
	}
	share, err := a.getFileShare(ctx, remoteID)
	if err != nil {
		return Share{}, err
	}
	return Share{URL: share.shareURL, Password: share.password, Status: "ready"}, nil
}

// Health 验证 Cookie 会话（mydisk）并请求根目录第一页（doupload task=5）。
func (a *lanzouAdapter) Health(ctx context.Context) error {
	if err := a.syncUID(ctx); err != nil {
		return err
	}
	return a.listFirstPage(ctx)
}

func (a *lanzouAdapter) Capabilities() Capabilities {
	return Capabilities{Upload: true, Download: true, Share: true}
}
