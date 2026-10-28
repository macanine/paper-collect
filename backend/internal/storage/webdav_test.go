package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	davUser = "alice"
	davPass = "s3cret-pass-value"
)

// davStore 是 httptest 内的内存 WebDAV：只实现适配器用到的方法。
type davStore struct {
	mu      sync.Mutex
	objects map[string][]byte // 解码后的路径 -> 内容
	dirs    map[string]bool
	mkcol   []string
	// 可选覆盖：用于构造失败场景。
	putStatus  int
	headStatus int
	getBody    map[string][]byte
	noHead     bool
	mkcolCode  int
}

func newDavStore() *davStore {
	return &davStore{objects: map[string][]byte{}, dirs: map[string]bool{}, getBody: map[string][]byte{}}
}

func (s *davStore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	user, pass, ok := r.BasicAuth()
	if !ok || user != davUser || pass != davPass {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := r.URL.Path
	switch r.Method {
	case "PROPFIND":
		w.WriteHeader(http.StatusMultiStatus)
	case "MKCOL":
		s.mkcol = append(s.mkcol, path)
		if s.mkcolCode != 0 {
			w.WriteHeader(s.mkcolCode)
			return
		}
		if s.dirs[path] {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		s.dirs[path] = true
		w.WriteHeader(http.StatusCreated)
	case "PUT":
		body, _ := io.ReadAll(r.Body)
		if s.putStatus != 0 {
			w.WriteHeader(s.putStatus)
			return
		}
		s.objects[path] = body
		w.WriteHeader(http.StatusCreated)
	case "HEAD":
		if s.noHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, exists := s.objects[path]
		if override, ok := s.getBody[path]; ok {
			body, exists = override, true
		}
		if s.headStatus != 0 {
			w.WriteHeader(s.headStatus)
			return
		}
		if !exists {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
	case "GET":
		body, exists := s.objects[path]
		if override, ok := s.getBody[path]; ok {
			body, exists = override, true
		}
		if !exists {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write(body)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func davAdapter(t *testing.T, srvURL string, settings map[string]string) Adapter {
	t.Helper()
	merged := map[string]string{"base_url": srvURL + "/dav"}
	for k, v := range settings {
		merged[k] = v
	}
	adapter, err := New(Config{
		ID:       "dav-1",
		Kind:     "webdav",
		Settings: merged,
		Secrets:  map[string]string{"username": davUser, "password": davPass},
	})
	if err != nil {
		t.Fatalf("New(webdav): %v", err)
	}
	return adapter
}

func writeTempSource(t *testing.T, data []byte) (string, string) {
	t.Helper()
	sum := sha256.Sum256(data)
	path := filepath.Join(t.TempDir(), "paper.pdf")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, hex.EncodeToString(sum[:])
}

func TestWebDAVPutCreatesCollectionsAndVerifiesSHA256(t *testing.T) {
	store := newDavStore()
	srv := httptest.NewServer(store)
	defer srv.Close()
	adapter := davAdapter(t, srv.URL, map[string]string{"path_prefix": "/paper-collect/"})

	data := []byte("%PDF-1.4 webdav fixture")
	src, digest := writeTempSource(t, data)
	key := "ab/" + digest + "/paper.pdf"

	stored, err := adapter.Put(context.Background(), Object{Key: key, Name: "paper.pdf", Path: src, SHA256: digest, Size: int64(len(data))})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if stored.RemoteID != key || stored.Size != int64(len(data)) || stored.Verification != "sha256" {
		t.Fatalf("stored = %+v", stored)
	}
	remote := "/dav/paper-collect/" + key
	if got := string(store.objects[remote]); got != string(data) {
		t.Fatalf("remote content = %q", got)
	}
	want := []string{"/dav/paper-collect/", "/dav/paper-collect/ab/", "/dav/paper-collect/ab/" + digest + "/"}
	if strings.Join(store.mkcol, "|") != strings.Join(want, "|") {
		t.Fatalf("MKCOL calls = %v, want %v", store.mkcol, want)
	}
}

func TestWebDAVPutWithoutSHA256ReportsSize(t *testing.T) {
	store := newDavStore()
	srv := httptest.NewServer(store)
	defer srv.Close()
	adapter := davAdapter(t, srv.URL, nil)
	data := []byte("size only")
	src, _ := writeTempSource(t, data)

	stored, err := adapter.Put(context.Background(), Object{Key: "x/y.pdf", Path: src, Size: int64(len(data))})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if stored.Verification != "size" {
		t.Fatalf("verification = %q, want size", stored.Verification)
	}
}

func TestWebDAVPutReportsUploadedWhenHEADAndGETUnavailable(t *testing.T) {
	store := newDavStore()
	store.noHead = true
	srv := httptest.NewServer(store)
	defer srv.Close()
	adapter := davAdapter(t, srv.URL, nil)
	data := []byte("no head support")
	src, _ := writeTempSource(t, data)

	stored, err := adapter.Put(context.Background(), Object{Key: "x/n.pdf", Path: src, Size: int64(len(data))})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if stored.Verification != "uploaded" {
		t.Fatalf("verification = %q, want uploaded (nothing was compared)", stored.Verification)
	}
}

func TestWebDAVPutDetectsRemoteSizeMismatch(t *testing.T) {
	store := newDavStore()
	srv := httptest.NewServer(store)
	defer srv.Close()
	adapter := davAdapter(t, srv.URL, nil)
	data := []byte("local bytes")
	src, _ := writeTempSource(t, data)
	key := "s/size.pdf"
	// 远端在 PUT 之后变成另一长度的内容（覆盖 HEAD/GET 返回值），HEAD 应发现大小不符。
	store.getBody["/dav/"+key] = []byte("tiny")

	_, err := adapter.Put(context.Background(), Object{Key: key, Path: src, SHA256: sha256Hex(data), Size: int64(len(data))})
	requireClass(t, err, ClassSizeMismatch)
}

func TestWebDAVPutDetectsRemoteHashMismatch(t *testing.T) {
	store := newDavStore()
	srv := httptest.NewServer(store)
	defer srv.Close()
	adapter := davAdapter(t, srv.URL, nil)
	data := []byte("correct bytes!")
	src, digest := writeTempSource(t, data)
	key := "h/hash.pdf"
	// 远端内容长度相同但字节不同：HEAD 大小一致，GET 重算哈希失败。
	store.getBody["/dav/"+key] = []byte("corrupt bytes!")

	_, err := adapter.Put(context.Background(), Object{Key: key, Path: src, SHA256: digest, Size: int64(len(data))})
	requireClass(t, err, ClassHashMismatch)
}

func TestWebDAVPutAuthFailureIsNotRetryable(t *testing.T) {
	store := newDavStore()
	srv := httptest.NewServer(store)
	defer srv.Close()
	// 使用错误密码构造适配器。
	bad, err := New(Config{Kind: "webdav", Settings: map[string]string{"base_url": srv.URL + "/dav"},
		Secrets: map[string]string{"username": davUser, "password": "wrong"}})
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("auth")
	src, digest := writeTempSource(t, data)

	_, err = bad.Put(context.Background(), Object{Key: "a/auth.pdf", Path: src, SHA256: digest, Size: int64(len(data))})
	requireClass(t, err, ClassAuth)
	if IsRetryable(err) {
		t.Fatal("auth failure must not be retryable")
	}
	if strings.Contains(err.Error(), "wrong") || strings.Contains(err.Error(), davUser) {
		t.Fatalf("error leaks credentials: %v", err)
	}
}

func TestWebDAVPutServerErrorIsRetryable(t *testing.T) {
	store := newDavStore()
	store.putStatus = http.StatusServiceUnavailable
	srv := httptest.NewServer(store)
	defer srv.Close()
	adapter := davAdapter(t, srv.URL, nil)
	data := []byte("busy")
	src, digest := writeTempSource(t, data)

	_, err := adapter.Put(context.Background(), Object{Key: "b/busy.pdf", Path: src, SHA256: digest, Size: int64(len(data))})
	requireClass(t, err, ClassUnavailable)
	if !IsRetryable(err) {
		t.Fatal("503 must be retryable")
	}
}

func TestWebDAVPutTimeoutIsClassified(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-block:
		}
	}))
	defer srv.Close()
	defer close(block)
	adapter := davAdapter(t, srv.URL, nil)
	a := adapter.(*webdavAdapter)
	a.requestTimeout = 50 * time.Millisecond
	a.uploadTimeout = 50 * time.Millisecond
	data := []byte("slow")
	src, digest := writeTempSource(t, data)

	_, err := adapter.Put(context.Background(), Object{Key: "t/slow.pdf", Path: src, SHA256: digest, Size: int64(len(data))})
	requireClass(t, err, ClassTimeout)
	if !IsRetryable(err) {
		t.Fatal("timeout must be retryable")
	}
}

func TestWebDAVConnectionFailureIsNetworkClass(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	adapter := davAdapter(t, url, nil)
	data := []byte("offline")
	src, digest := writeTempSource(t, data)

	_, err := adapter.Put(context.Background(), Object{Key: "n/off.pdf", Path: src, SHA256: digest, Size: int64(len(data))})
	requireClass(t, err, ClassNetwork)
	if !IsRetryable(err) {
		t.Fatal("network failure must be retryable")
	}
	if strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("error leaks host: %v", err)
	}
}

func TestWebDAVMKCOLParentMissingIsUnknown(t *testing.T) {
	store := newDavStore()
	store.mkcolCode = http.StatusConflict
	srv := httptest.NewServer(store)
	defer srv.Close()
	adapter := davAdapter(t, srv.URL, nil)
	data := []byte("x")
	src, digest := writeTempSource(t, data)
	_, err := adapter.Put(context.Background(), Object{Key: "c/x.pdf", Path: src, SHA256: digest, Size: 1})
	requireClass(t, err, ClassUnknown)
}

func TestWebDAVPutRejectsBadInputBeforeNetwork(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer srv.Close()
	adapter := davAdapter(t, srv.URL, nil)
	data := []byte("abc")
	src, _ := writeTempSource(t, data)

	_, err := adapter.Put(context.Background(), Object{Key: "../evil.pdf", Path: src, Size: 3})
	requireClass(t, err, ClassInvalidInput)
	_, err = adapter.Put(context.Background(), Object{Key: "ok/a.pdf", Path: src, Size: 99})
	requireClass(t, err, ClassSizeMismatch)
	_, err = adapter.Put(context.Background(), Object{Key: "ok/b.pdf", Path: filepath.Join(t.TempDir(), "missing"), Size: 3})
	requireClass(t, err, ClassFileMissing)
	if calls != 0 {
		t.Fatalf("rejected inputs reached the network %d times", calls)
	}
}

func TestWebDAVPutEscapesPathSegments(t *testing.T) {
	store := newDavStore()
	srv := httptest.NewServer(store)
	defer srv.Close()
	adapter := davAdapter(t, srv.URL, nil)
	data := []byte("escape")
	src, digest := writeTempSource(t, data)
	key := "e/" + digest + "/高三 数学 #1?.pdf"

	if _, err := adapter.Put(context.Background(), Object{Key: key, Path: src, SHA256: digest, Size: int64(len(data))}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if got := string(store.objects["/dav/"+key]); got != "escape" {
		t.Fatalf("decoded path not stored correctly: %q", got)
	}
}

func TestWebDAVOpenReturnsContentAndMissingIsFileMissing(t *testing.T) {
	store := newDavStore()
	store.objects["/dav/o/file.txt"] = []byte("download me")
	srv := httptest.NewServer(store)
	defer srv.Close()
	adapter := davAdapter(t, srv.URL, nil)

	reader, err := adapter.Open(context.Background(), "o/file.txt")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(got) != "download me" {
		t.Fatalf("read = %q, %v", got, err)
	}
	_, err = adapter.Open(context.Background(), "o/missing.txt")
	requireClass(t, err, ClassFileMissing)
}

func TestWebDAVShareRequiresPublicBaseURL(t *testing.T) {
	srv := httptest.NewServer(newDavStore())
	defer srv.Close()

	plain := davAdapter(t, srv.URL, nil)
	if plain.Capabilities().Share {
		t.Fatal("share capability must be false without public_base_url")
	}
	share, err := plain.Share(context.Background(), "ab/x/p.pdf")
	if err != nil {
		t.Fatalf("Share: %v", err)
	}
	if share.Status != "unsupported" || share.URL != "" {
		t.Fatalf("share = %+v", share)
	}

	public := davAdapter(t, srv.URL, map[string]string{
		"path_prefix":     "paper-collect",
		"public_base_url": "https://files.example.test/public/paper-collect",
	})
	if !public.Capabilities().Share {
		t.Fatal("share capability must be true with public_base_url")
	}
	share, err = public.Share(context.Background(), "ab/x/p 1.pdf")
	if err != nil {
		t.Fatalf("Share: %v", err)
	}
	if share.Status != "ready" || share.URL != "https://files.example.test/public/paper-collect/ab/x/p%201.pdf" {
		t.Fatalf("share = %+v", share)
	}
}

func TestWebDAVHealthDistinguishesAuthAndNetwork(t *testing.T) {
	store := newDavStore()
	srv := httptest.NewServer(store)
	defer srv.Close()
	if err := davAdapter(t, srv.URL, nil).Health(context.Background()); err != nil {
		t.Fatalf("Health healthy server: %v", err)
	}

	bad, err := New(Config{Kind: "webdav", Settings: map[string]string{"base_url": srv.URL + "/dav"},
		Secrets: map[string]string{"username": davUser, "password": "nope"}})
	if err != nil {
		t.Fatal(err)
	}
	authErr := bad.Health(context.Background())
	requireClass(t, authErr, ClassAuth)

	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()
	netErr := davAdapter(t, deadURL, nil).Health(context.Background())
	requireClass(t, netErr, ClassNetwork)
	if errors.Is(netErr, ErrUnsupported) {
		t.Fatal("network failure must not look unsupported")
	}
}

func TestWebDAVHealthPropfindUnsupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PROPFIND" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	err := davAdapter(t, srv.URL, nil).Health(context.Background())
	requireClass(t, err, ClassUnsupported)
}

func TestWebDAVConfigValidation(t *testing.T) {
	cases := []struct {
		name     string
		settings map[string]string
		secrets  map[string]string
		detail   string
	}{
		{"missing base", map[string]string{}, creds(), "webdav_base_url_required"},
		{"ftp scheme", map[string]string{"base_url": "ftp://x/dav"}, creds(), "base_url_scheme_invalid"},
		{"embedded credentials", map[string]string{"base_url": "https://bob:hunter2@dav.example/dav"}, creds(), "base_url_has_credentials"},
		{"query in base", map[string]string{"base_url": "https://dav.example/dav?x=1"}, creds(), "base_url_has_query"},
		{"dotdot prefix", map[string]string{"base_url": "https://dav.example/dav", "path_prefix": "a/../b"}, creds(), "path_prefix_invalid"},
		{"bad public", map[string]string{"base_url": "https://dav.example/dav", "public_base_url": "ftp://x"}, creds(), "webdav_public_base_url_invalid"},
		{"no username", map[string]string{"base_url": "https://dav.example/dav"}, map[string]string{"password": "p"}, "webdav_credentials_required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(Config{Kind: "webdav", Settings: tc.settings, Secrets: tc.secrets})
			requireClass(t, err, ClassConfig)
			if got := AsCloudError(err).Detail; got != tc.detail {
				t.Fatalf("detail = %q, want %q", got, tc.detail)
			}
			if strings.Contains(err.Error(), "hunter2") {
				t.Fatalf("config error leaks secret: %v", err)
			}
		})
	}
}

func creds() map[string]string {
	return map[string]string{"username": "u", "password": "p"}
}

func TestWebDAVCapabilitiesDefault(t *testing.T) {
	adapter, err := New(Config{Kind: "webdav", Settings: map[string]string{"base_url": "https://dav.example/dav"}, Secrets: creds()})
	if err != nil {
		t.Fatal(err)
	}
	caps := adapter.Capabilities()
	if !caps.Upload || !caps.Download || caps.Share {
		t.Fatalf("capabilities = %+v", caps)
	}
}
