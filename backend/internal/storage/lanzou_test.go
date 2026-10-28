package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const (
	lzBase      = "https://pc.woozooo.com"
	lzAccounts  = "https://accounts.woozooo.com"
	lzShare     = "https://wwx.lanzouj.com"
	lzDownload  = "https://download.example/file/abc"
	lzCookieVal = "disk-cookie-value"
	acwArg1Lz   = "696E38613C462C5728B190D4E08DE2ADF4928C88"
	acwValueLz  = "69e4e00cc33fe80be4918942f9d3fb6e04ed4b17"
)

type lzCall struct {
	method string
	url    string
	cookie string
	header http.Header
	body   []byte
	req    *http.Request
}

type lzRoute struct {
	match  func(method, rawURL string) bool
	handle func(req *http.Request, body []byte) *http.Response
}

// lzFake 是离线 RoundTripper：按路由返回响应，并记录每次请求。未匹配的请求会使测试失败。
type lzFake struct {
	t      *testing.T
	routes []lzRoute
	mu     sync.Mutex
	calls  []lzCall
}

func (f *lzFake) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
		_ = req.Body.Close()
	}
	rawURL := req.URL.String()
	f.mu.Lock()
	f.calls = append(f.calls, lzCall{
		method: req.Method, url: rawURL, cookie: req.Header.Get("Cookie"),
		header: req.Header.Clone(), body: body, req: req,
	})
	f.mu.Unlock()
	for _, r := range f.routes {
		if r.match(req.Method, rawURL) {
			resp := r.handle(req, body)
			resp.Request = req
			return resp, nil
		}
	}
	f.t.Errorf("unexpected request: %s %s", req.Method, rawURL)
	return nil, errors.New("unexpected request")
}

func (f *lzFake) count(rawURL string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c.url == rawURL {
			n++
		}
	}
	return n
}

func (f *lzFake) find(rawURL string) (lzCall, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c.url == rawURL {
			return c, true
		}
	}
	return lzCall{}, false
}

func lzResp(status int, contentType, body string, headers http.Header) *http.Response {
	h := http.Header{}
	if contentType != "" {
		h.Set("Content-Type", contentType)
	}
	for key, values := range headers {
		h[key] = values
	}
	return &http.Response{
		StatusCode:    status,
		Status:        fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:        h,
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
	}
}

func lzHTML(body string) *http.Response {
	return lzResp(http.StatusOK, "text/html; charset=utf-8", body, nil)
}

func lzJSON(body string) *http.Response {
	return lzResp(http.StatusOK, "application/json", body, nil)
}

func lzBytes(body []byte) *http.Response {
	return lzResp(http.StatusOK, "application/pdf", string(body), nil)
}

func lzExact(method, target string) func(string, string) bool {
	return func(m, u string) bool { return m == method && u == target }
}

func lzPrefix(method, prefix string) func(string, string) bool {
	return func(m, u string) bool { return m == method && strings.HasPrefix(u, prefix) }
}

func newLanzouTest(t *testing.T, fake *lzFake, settings map[string]string, cookie string) *lanzouAdapter {
	t.Helper()
	if cookie == "" {
		cookie = "phpdisk_info=" + lzCookieVal
	}
	adapter, err := NewLanzou(Config{Kind: "lanzou", Settings: settings, Secrets: map[string]string{"cookie": cookie}},
		&http.Client{Transport: fake})
	if err != nil {
		t.Fatalf("NewLanzou: %v", err)
	}
	a := adapter.(*lanzouAdapter)
	a.verifyDelay = 0
	a.retryDelay = 0
	a.busyDelay = 0
	return a
}

type lzMultipart struct {
	fields   map[string]string
	file     []byte
	fileName string
}

func parseLzMultipart(t *testing.T, req *http.Request, body []byte) lzMultipart {
	t.Helper()
	if req.ContentLength != int64(len(body)) {
		t.Fatalf("Content-Length = %d, body = %d", req.ContentLength, len(body))
	}
	mediaType, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		t.Fatalf("content type = %q, %v", req.Header.Get("Content-Type"), err)
	}
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	out := lzMultipart{fields: map[string]string{}}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("multipart: %v", err)
		}
		data, _ := io.ReadAll(part)
		if part.FileName() != "" {
			out.file = data
			out.fileName = part.FileName()
			out.fields[part.FormName()] = string(data)
			continue
		}
		out.fields[part.FormName()] = string(data)
	}
	return out
}

func lzForm(t *testing.T, body []byte) map[string]string {
	t.Helper()
	parsed, err := url.ParseQuery(string(body))
	if err != nil {
		t.Fatalf("form body: %v", err)
	}
	values := map[string]string{}
	for key, list := range parsed {
		values[key] = list[0]
	}
	return values
}

func sha256Lz(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func writeLzSource(t *testing.T, data []byte, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLanzouAcwChallengeVector(t *testing.T) {
	if got := extractAcwArg1(`<script>var arg1='` + acwArg1Lz + `';</script>`); got != acwArg1Lz {
		t.Fatalf("extractAcwArg1 = %q", got)
	}
	if got := computeAcwScV2(acwArg1Lz); got != acwValueLz {
		t.Fatalf("computeAcwScV2 = %q, want %q", got, acwValueLz)
	}
	if computeAcwScV2("not-hex") != "" || computeAcwScV2("0") != "" || computeAcwScV2("") != "" {
		t.Fatal("invalid arg1 must return empty")
	}
}

func TestLanzouCookieSeedingStaysOnBaseHost(t *testing.T) {
	fake := &lzFake{t: t}
	fake.routes = []lzRoute{
		{match: lzExact("GET", lzBase+"/mydisk.php?item=files&action=index"), handle: func(*http.Request, []byte) *http.Response {
			return lzHTML(`<script>var uid=12345;</script>`)
		}},
		{match: lzPrefix("POST", lzBase+"/doupload.php"), handle: func(*http.Request, []byte) *http.Response {
			return lzJSON(`{"zt":1,"text":[]}`)
		}},
	}
	a := newLanzouTest(t, fake, map[string]string{"base_url": lzBase}, "")
	if err := a.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
	mydisk, _ := fake.find(lzBase + "/mydisk.php?item=files&action=index")
	if !strings.Contains(mydisk.cookie, "phpdisk_info="+lzCookieVal) {
		t.Fatalf("session cookie not sent to base host: %q", mydisk.cookie)
	}
	if a.currentUID() != "12345" {
		t.Fatalf("uid = %q", a.currentUID())
	}
}

func TestLanzouHealthRetriesAcwChallengeAndLists(t *testing.T) {
	fake := &lzFake{t: t}
	fake.routes = []lzRoute{
		{match: lzExact("GET", lzBase+"/mydisk.php?item=files&action=index"), handle: func(req *http.Request, _ []byte) *http.Response {
			if !strings.Contains(req.Header.Get("Cookie"), "acw_sc__v2="+acwValueLz) {
				return lzHTML(`<html><script>var arg1='` + acwArg1Lz + `';</script></html>`)
			}
			return lzHTML(`<html><script>var uid=12345;</script></html>`)
		}},
		{match: lzPrefix("POST", lzBase+"/doupload.php"), handle: func(req *http.Request, body []byte) *http.Response {
			form := lzForm(t, body)
			if form["task"] != "5" || form["folder_id"] != "-1" {
				t.Errorf("list form = %v", form)
			}
			return lzJSON(`{"zt":1,"text":[]}`)
		}},
	}
	a := newLanzouTest(t, fake, map[string]string{}, "")
	if err := a.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
	if fake.count(lzBase+"/mydisk.php?item=files&action=index") != 2 {
		t.Fatalf("mydisk should be requested twice (challenge + retry)")
	}
	list, _ := fake.find(lzBase + "/doupload.php?uid=12345")
	if list.method != "POST" {
		t.Fatalf("doupload with uid not called")
	}
}

func TestLanzouHealthSessionRejectedWhenRedirectedToLogin(t *testing.T) {
	fake := &lzFake{t: t}
	fake.routes = []lzRoute{
		{match: lzExact("GET", lzBase+"/mydisk.php?item=files&action=index"), handle: func(*http.Request, []byte) *http.Response {
			return lzResp(http.StatusFound, "", "", http.Header{"Location": {lzAccounts + "/accounts.php?action=login"}})
		}},
		{match: lzExact("GET", lzAccounts+"/accounts.php?action=login"), handle: func(*http.Request, []byte) *http.Response {
			return lzHTML(`<div>网盘用户登录</div>`)
		}},
	}
	a := newLanzouTest(t, fake, map[string]string{}, "")
	err := a.Health(context.Background())
	requireClass(t, err, ClassAuth)
	if IsRetryable(err) {
		t.Fatal("expired session must not be retryable")
	}
	if strings.Contains(err.Error(), lzCookieVal) {
		t.Fatalf("error leaks cookie: %v", err)
	}
}

func TestLanzouUploadDefaultReportsUploadedWithoutDownload(t *testing.T) {
	data := []byte("no-verify-by-default")
	src := writeLzSource(t, data, "n.pdf")
	fake := &lzFake{t: t}
	fake.routes = []lzRoute{
		{match: lzExact("POST", lzBase+"/html5up.php"), handle: func(req *http.Request, body []byte) *http.Response {
			part := parseLzMultipart(t, req, body)
			if part.fields["task"] != "1" || part.fields["vie"] != "2" || part.fields["ve"] != "2" ||
				part.fields["id"] != "WU_FILE_0" || part.fields["name"] != "n.pdf" || part.fields["folder_id_bb_n"] != "-1" {
				t.Errorf("upload fields = %v", part.fields)
			}
			if !bytes.Equal(part.file, data) || part.fileName != "n.pdf" {
				t.Errorf("uploaded file mismatch: name=%q", part.fileName)
			}
			if !strings.Contains(req.Header.Get("Cookie"), "phpdisk_info="+lzCookieVal) {
				t.Errorf("upload must carry the session cookie")
			}
			return lzJSON(`{"zt":1,"text":[{"id":"file-n","name_all":"n.pdf","size":"1.0 K"}]}`)
		}},
	}
	a := newLanzouTest(t, fake, map[string]string{}, "")
	stored, err := a.Put(context.Background(), Object{Key: "ab/" + sha256Lz(data) + "/n.pdf", Name: "n.pdf", Path: src, Size: int64(len(data))})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if stored.RemoteID != "file-n" || stored.Verification != "uploaded" || stored.Size != int64(len(data)) {
		t.Fatalf("stored = %+v", stored)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("default mode must make exactly one request, made %d", len(fake.calls))
	}
}

// lzStrongRoutes 构造完整的分享→fn→downprocess→下载链路。download 决定下载返回的内容。
func lzStrongRoutes(t *testing.T, fake *lzFake, fileID, shareID, ajaxFile string, download []byte) []lzRoute {
	t.Helper()
	sharePage := lzShare + "/" + shareID
	fnPage := lzShare + "/fn?xyz=1"
	ajaxm := lzShare + "/ajaxm.php?file=" + ajaxFile
	return []lzRoute{
		{match: lzExact("POST", lzBase+"/doupload.php"), handle: func(req *http.Request, body []byte) *http.Response {
			form := lzForm(t, body)
			if form["task"] != "22" || form["file_id"] != fileID {
				t.Errorf("share form = %v", form)
			}
			return lzJSON(`{"zt":1,"info":{"is_newd":"` + lzShare + `","f_id":"` + shareID + `","pwd":"","onof":"0"}}`)
		}},
		{match: lzExact("GET", sharePage), handle: func(*http.Request, []byte) *http.Response {
			return lzHTML(`<html><iframe src="/fn?xyz=1"></iframe><script>var down_1='?telecom';</script></html>`)
		}},
		{match: lzExact("GET", fnPage), handle: func(*http.Request, []byte) *http.Response {
			return lzHTML(`<html><script>
        var wp_sign = 'sig';
        var ajaxdata = 'data';
        var kdns = 1;
        var down_1 = '?telecom';
        var down_2 = '?unicom';
        var down_3 = '?normal';
        $.ajax({url: '/ajaxm.php?file=` + ajaxFile + `', data: {'action':'downprocess','sign':wp_sign,'websignkey':ajaxdata,'kd':kdns}});
      </script></html>`)
		}},
		{match: lzExact("POST", ajaxm), handle: func(req *http.Request, body []byte) *http.Response {
			form := lzForm(t, body)
			if form["action"] != "downprocess" || form["sign"] != "sig" || form["websignkey"] != "data" || form["kd"] != "1" {
				t.Errorf("downprocess form = %v", form)
			}
			return lzJSON(`{"zt":1,"dom":"https://download.example","url":"abc"}`)
		}},
		{match: lzExact("GET", lzDownload), handle: func(*http.Request, []byte) *http.Response {
			return lzBytes(download)
		}},
		// 备用直链（down_1..3）：明确返回 404，使候选遍历可被断言。
		{match: func(m, u string) bool {
			return m == "GET" && strings.HasPrefix(u, lzDownload+"?")
		}, handle: func(*http.Request, []byte) *http.Response {
			return lzResp(http.StatusNotFound, "text/plain", "", nil)
		}},
	}
}

func TestLanzouStrongVerifyReportsSHA256AndKeepsCDNCookieFree(t *testing.T) {
	data := []byte("%PDF-1.4 lanzou adapter fixture")
	digest := sha256Lz(data)
	src := writeLzSource(t, data, "paper.pdf")
	fake := &lzFake{t: t}
	fake.routes = append([]lzRoute{
		{match: lzExact("POST", lzBase+"/html5up.php"), handle: func(req *http.Request, body []byte) *http.Response {
			part := parseLzMultipart(t, req, body)
			if part.fields["name"] != "paper.pdf" {
				t.Errorf("remote name = %q, want sanitized paper.pdf", part.fields["name"])
			}
			if !bytes.Equal(part.file, data) {
				t.Errorf("uploaded bytes mismatch")
			}
			return lzJSON(`{"zt":1,"text":[{"id":"file-7788","name_all":"paper.pdf","size":"1.0 M"}]}`)
		}},
	}, lzStrongRoutes(t, fake, "file-7788", "abc123", "7788", data)...)

	a := newLanzouTest(t, fake, map[string]string{"verify_remote_hash": "true"}, "")
	stored, err := a.Put(context.Background(), Object{
		Key: "ab/" + digest + "/paper.pdf", Name: "../evil/paper.pdf", Path: src, SHA256: digest, Size: int64(len(data)),
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if stored.RemoteID != "file-7788" || stored.Verification != "sha256" || stored.Size != int64(len(data)) {
		t.Fatalf("stored = %+v", stored)
	}
	if fake.count(lzDownload) != 1 {
		t.Fatalf("download should be hashed exactly once, got %d", fake.count(lzDownload))
	}
	download, _ := fake.find(lzDownload)
	if download.cookie != "" {
		t.Fatalf("CDN request must not carry login cookies: %q", download.cookie)
	}
}

func TestLanzouStrongVerifyRejectsRemoteHashMismatchWithoutRetry(t *testing.T) {
	local := []byte("local-bytes")
	remote := []byte("remote-bytes-different")
	src := writeLzSource(t, local, "a.pdf")
	fake := &lzFake{t: t}
	fake.routes = append([]lzRoute{
		{match: lzExact("POST", lzBase+"/html5up.php"), handle: func(*http.Request, []byte) *http.Response {
			return lzJSON(`{"zt":1,"text":[{"id":"fid","name_all":"a.pdf","size":"1.0 M"}]}`)
		}},
	}, lzStrongRoutes(t, fake, "fid", "share1", "1", remote)...)

	a := newLanzouTest(t, fake, map[string]string{"verify_remote_hash": "true"}, "")
	_, err := a.Put(context.Background(), Object{Key: "k/a.pdf", Name: "a.pdf", Path: src, SHA256: sha256Lz(local), Size: int64(len(local))})
	requireClass(t, err, ClassHashMismatch)
	if fake.count(lzDownload) != 1 {
		t.Fatalf("hash mismatch is deterministic and must not be retried; downloads = %d", fake.count(lzDownload))
	}
}

func TestLanzouShareReturnsURLAndPassword(t *testing.T) {
	fake := &lzFake{t: t}
	fake.routes = []lzRoute{
		{match: lzPrefix("POST", lzBase+"/doupload.php"), handle: func(req *http.Request, body []byte) *http.Response {
			form := lzForm(t, body)
			if form["task"] != "22" || form["file_id"] != "file-9" {
				t.Errorf("share form = %v", form)
			}
			return lzJSON(`{"zt":1,"info":{"is_newd":"http://wwx.lanzouj.com","f_id":"/abc123","pwd":"pw9","onof":"1"}}`)
		}},
	}
	a := newLanzouTest(t, fake, map[string]string{}, "")
	share, err := a.Share(context.Background(), "file-9")
	if err != nil {
		t.Fatalf("Share: %v", err)
	}
	if share.Status != "ready" || share.URL != lzShare+"/abc123" || share.Password != "pw9" {
		t.Fatalf("share = %+v", share)
	}
	if !a.Capabilities().Share {
		t.Fatal("lanzou must advertise share capability")
	}
}

func TestLanzouShareRejectsNonLanzouHost(t *testing.T) {
	fake := &lzFake{t: t}
	fake.routes = []lzRoute{
		{match: lzPrefix("POST", lzBase+"/doupload.php"), handle: func(*http.Request, []byte) *http.Response {
			return lzJSON(`{"zt":1,"info":{"is_newd":"https://evil.example","f_id":"x","pwd":""}}`)
		}},
	}
	a := newLanzouTest(t, fake, map[string]string{}, "")
	_, err := a.Share(context.Background(), "file-9")
	requireClass(t, err, ClassLinkExpired)
}

func TestLanzouOpenStreamsWithoutSaving(t *testing.T) {
	data := []byte("%PDF-1.4 lanzou cloud read fixture")
	fake := &lzFake{t: t}
	fake.routes = []lzRoute{
		{match: lzPrefix("POST", lzBase+"/doupload.php"), handle: func(req *http.Request, body []byte) *http.Response {
			form := lzForm(t, body)
			if form["task"] != "22" || form["file_id"] != "file-read" {
				t.Errorf("share form = %v", form)
			}
			return lzJSON(`{"zt":1,"info":{"is_newd":"` + lzShare + `","f_id":"read123","pwd":"","onof":"0"}}`)
		}},
		{match: lzExact("GET", lzShare+"/read123"), handle: func(*http.Request, []byte) *http.Response {
			return lzHTML(`<html><iframe src="/fn?read=1"></iframe></html>`)
		}},
		{match: lzExact("GET", lzShare+"/fn?read=1"), handle: func(*http.Request, []byte) *http.Response {
			return lzHTML(`<html><script>
        var wp_sign = 'sig';
        var ajaxdata = 'data';
        var kdns = 1;
        $.ajax({url: '/ajaxm.php?file=99', data: {'action':'downprocess','sign':wp_sign,'websignkey':ajaxdata,'kd':kdns}});
      </script></html>`)
		}},
		{match: lzExact("POST", lzShare+"/ajaxm.php?file=99"), handle: func(*http.Request, []byte) *http.Response {
			return lzJSON(`{"zt":1,"dom":"https://download.example","url":"read"}`)
		}},
		{match: lzExact("GET", "https://download.example/file/read"), handle: func(*http.Request, []byte) *http.Response {
			return lzResp(http.StatusOK, "application/pdf", string(data), http.Header{"Content-Length": {fmt.Sprint(len(data))}})
		}},
	}
	a := newLanzouTest(t, fake, map[string]string{}, "")
	reader, err := a.Open(context.Background(), "file-read")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("streamed bytes mismatch: %v", err)
	}
	if fake.count("https://download.example/file/read") != 1 {
		t.Fatal("open should fetch the direct link once")
	}
	if _, err := a.Open(context.Background(), "../bad"); err == nil {
		t.Fatal("invalid remote id must be rejected")
	}
}

func TestLanzouPasswordSharePageUsesDataBlockAfterCommentStrip(t *testing.T) {
	shareURL := lzShare + "/pw1"
	ajaxURL := "https://apifile.woozooo.com/ajaxfile.php?file=42"
	page := `<html>
    <div id="passwddiv"><input id="pwd" /></div>
    <script>
      var kdns = 1;
      var isngis = '';
      var isngis = 'REAL_SIGN';
      var pwd = document.getElementById('pwd').value;
      //data : { 'action':'downprocess','sign':'commented','kd':1,'p':pwd, },
      $.ajax({
        type: 'post',
        url : 'https://apifile.woozooo.com/ajaxfile.php?file=42',
        data : { 'action':'downprocess','sign':isngis,'kd':kdns,'p':pwd, },
      });
    </script></html>`
	fake := &lzFake{t: t}
	fake.routes = []lzRoute{
		{match: lzExact("GET", shareURL), handle: func(*http.Request, []byte) *http.Response { return lzHTML(page) }},
		{match: lzExact("POST", ajaxURL), handle: func(req *http.Request, body []byte) *http.Response {
			form := lzForm(t, body)
			if form["action"] != "downprocess" || form["sign"] != "REAL_SIGN" || form["kd"] != "1" || form["p"] != "pass123" {
				t.Errorf("password form = %v", form)
			}
			return lzJSON(`{"zt":1,"dom":"https://download.example","url":"z"}`)
		}},
	}
	a := newLanzouTest(t, fake, map[string]string{}, "")
	direct, err := a.getDirectLink(context.Background(), shareURL, "pass123")
	if err != nil {
		t.Fatalf("getDirectLink: %v", err)
	}
	if len(direct.candidates) == 0 || direct.candidates[0] != "https://download.example/file/z" {
		t.Fatalf("candidates = %v", direct.candidates)
	}
	if fake.count(ajaxURL) != 1 {
		t.Fatalf("ajaxfile should be called once")
	}

	_, err = a.getDirectLink(context.Background(), shareURL, "")
	requireClass(t, err, ClassLinkExpired)
	if AsCloudError(err).Detail != "lanzou_share_password_required" {
		t.Fatalf("detail = %q", AsCloudError(err).Detail)
	}
}

func TestLanzouRejectsObsoleteSharePageLayout(t *testing.T) {
	shareURL := lzShare + "/old"
	fake := &lzFake{t: t, routes: []lzRoute{
		{match: lzExact("GET", shareURL), handle: func(*http.Request, []byte) *http.Response {
			return lzHTML(`<html><iframe src="/fn?old=1"></iframe></html>`)
		}},
		{match: lzExact("GET", lzShare+"/fn?old=1"), handle: func(*http.Request, []byte) *http.Response {
			return lzHTML(`<script>url:'/ajaxm.php?file=1'; var wp_sign='sig'; var ajaxdata='data';</script>`)
		}},
	}}
	a := newLanzouTest(t, fake, map[string]string{}, "")
	_, err := a.getDirectLink(context.Background(), shareURL, "")
	if err == nil || AsCloudError(err).Detail != "lanzou_fn_layout_unknown" {
		t.Fatalf("expected unsupported old layout, got %v", err)
	}
}

func TestLanzouVerifyPageFlowSubmitsFormThenHashes(t *testing.T) {
	data := []byte("real file bytes")
	direct := "https://download.example/file/?token"
	verifyURL := "https://download.example/file/ajax.php"
	finalURL := "https://cdn.example/real"
	verifyPage := `<html><script>
    function down_r(el){
      $.ajax({ type:'post', url:'ajax.php',
        data:{ 'file':'FILE_TOKEN','el':el,'sign':'SIGN_TOKEN' } });
    }
  </script></html>`
	fake := &lzFake{t: t}
	fake.routes = []lzRoute{
		{match: lzExact("GET", direct), handle: func(*http.Request, []byte) *http.Response { return lzHTML(verifyPage) }},
		{match: lzExact("POST", verifyURL), handle: func(req *http.Request, body []byte) *http.Response {
			form := lzForm(t, body)
			if form["file"] != "FILE_TOKEN" || form["sign"] != "SIGN_TOKEN" || form["el"] != "2" {
				t.Errorf("verify form = %v", form)
			}
			return lzJSON(`{"zt":1,"url":"` + finalURL + `"}`)
		}},
		{match: lzExact("GET", finalURL), handle: func(*http.Request, []byte) *http.Response {
			return lzResp(http.StatusOK, "application/octet-stream", string(data), nil)
		}},
	}
	a := newLanzouTest(t, fake, map[string]string{}, "")
	sum, n, err := a.downloadHash(context.Background(), direct, lzShare+"/abc")
	if err != nil {
		t.Fatalf("downloadHash: %v", err)
	}
	if n != int64(len(data)) || sum != sha256Lz(data) {
		t.Fatalf("digest mismatch: %s %d", sum, n)
	}
	if len(fake.calls) != 3 {
		t.Fatalf("expected direct, verify POST, final GET; got %d calls", len(fake.calls))
	}
}

func TestLanzouSizeOnlyVerificationDetectsTruncation(t *testing.T) {
	data := []byte("12345")
	src := writeLzSource(t, data, "s.pdf")
	fake := &lzFake{t: t}
	fake.routes = append([]lzRoute{
		{match: lzExact("POST", lzBase+"/html5up.php"), handle: func(*http.Request, []byte) *http.Response {
			return lzJSON(`{"zt":1,"text":[{"id":"fid-s","name_all":"s.pdf","size":"1.0 K"}]}`)
		}},
	}, lzStrongRoutes(t, fake, "fid-s", "sh-s", "5", []byte("123"))...)
	a := newLanzouTest(t, fake, map[string]string{"verify_remote_hash": "true"}, "")
	_, err := a.Put(context.Background(), Object{Key: "k/s.pdf", Name: "s.pdf", Path: src, Size: int64(len(data))})
	requireClass(t, err, ClassSizeMismatch)
}

func TestLanzouUploadSessionExpiredIsAuthAndNotRetried(t *testing.T) {
	data := []byte("x")
	src := writeLzSource(t, data, "x.pdf")
	fake := &lzFake{t: t}
	fake.routes = []lzRoute{
		{match: lzExact("POST", lzBase+"/html5up.php"), handle: func(*http.Request, []byte) *http.Response {
			return lzJSON(`{"zt":9,"info":"login not"}`)
		}},
	}
	a := newLanzouTest(t, fake, map[string]string{}, "")
	_, err := a.Put(context.Background(), Object{Key: "k/x.pdf", Name: "x.pdf", Path: src, Size: 1})
	requireClass(t, err, ClassAuth)
	if fake.count(lzBase+"/html5up.php") != 1 {
		t.Fatalf("zt=9 must not be retried")
	}
	if strings.Contains(err.Error(), lzCookieVal) {
		t.Fatalf("error leaks cookie: %v", err)
	}
}

func TestLanzouUploadSizeLimitRejectedBeforeNetwork(t *testing.T) {
	fake := &lzFake{t: t}
	a := newLanzouTest(t, fake, map[string]string{}, "")
	a.maxUploadBytes = 10
	_, err := a.Put(context.Background(), Object{Key: "k/big.pdf", Name: "big.pdf", Path: "/nonexistent", Size: 11})
	requireClass(t, err, ClassUnsupported)
	if len(fake.calls) != 0 {
		t.Fatalf("size limit must fail before any request")
	}
}

func TestLanzouConfigValidation(t *testing.T) {
	cases := []struct {
		name     string
		settings map[string]string
		cookie   string
		detail   string
	}{
		{"cookie missing", map[string]string{}, "", "lanzou_cookie_required"},
		{"cookie newline", map[string]string{}, "a=b\r\nX: y", "lanzou_cookie_invalid"},
		{"base with path", map[string]string{"base_url": "https://pc.woozooo.com/x"}, "a=b", "lanzou_base_url_path_unsupported"},
		{"folder not numeric", map[string]string{"folder_id": "abc"}, "a=b", "lanzou_folder_id_invalid"},
		{"verify flag invalid", map[string]string{"verify_remote_hash": "yes"}, "a=b", "lanzou_verify_flag_invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewLanzou(Config{Kind: "lanzou", Settings: tc.settings, Secrets: map[string]string{"cookie": tc.cookie}}, nil)
			requireClass(t, err, ClassConfig)
			if AsCloudError(err).Detail != tc.detail {
				t.Fatalf("detail = %q, want %q", AsCloudError(err).Detail, tc.detail)
			}
		})
	}
}

func TestLanzouSanitizeRemoteName(t *testing.T) {
	if got := sanitizeRemoteName("../../etc/passwd", "fallback"); got != "passwd" {
		t.Fatalf("passwd sanitize = %q", got)
	}
	if got := sanitizeRemoteName("   ", "fallback.pdf"); got != "fallback.pdf" {
		t.Fatalf("blank fallback = %q", got)
	}
	if got := sanitizeRemoteName("..", "fallback"); got != "fallback" {
		t.Fatalf("dotdot fallback = %q", got)
	}
	long := strings.Repeat("试", 200)
	if got := sanitizeRemoteName(long, "f"); len([]rune(got)) != 120 {
		t.Fatalf("truncation rune length = %d", len([]rune(got)))
	}
	if !isLanzouHost("pc.woozooo.com") || !isLanzouHost("wwd.lanzouj.com") {
		t.Fatal("lanzou hosts must be accepted")
	}
	if isLanzouHost("evilwoozooo.com") || isLanzouHost("lanrar.com") {
		t.Fatal("look-alike hosts must be rejected")
	}
}

func TestLanzouCapabilitiesAdvertiseAll(t *testing.T) {
	a := newLanzouTest(t, &lzFake{t: t}, map[string]string{}, "")
	caps := a.Capabilities()
	if !caps.Upload || !caps.Download || !caps.Share {
		t.Fatalf("capabilities = %+v", caps)
	}
}
