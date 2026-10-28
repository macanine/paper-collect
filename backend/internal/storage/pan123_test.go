package storage

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	pan123ID     = "client-id-123"
	pan123Secret = "client-secret-xyz"
)

type pan123Server struct {
	mu       sync.Mutex
	tokens   int
	lastForm string
	lastHdr  string
	handler  func(w http.ResponseWriter, r *http.Request)
}

func (s *pan123Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.lastHdr = r.Header.Get("Platform")
	if r.URL.Path == "/api/v1/access_token" {
		s.tokens++
		_ = r.ParseForm()
		s.lastForm = r.PostForm.Encode()
	}
	handler := s.handler
	s.mu.Unlock()
	if handler != nil {
		handler(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"accessToken":"tok-1","expiredAt":"2099-01-01T00:00:00+08:00"}}`))
}

func newPan123Test(t *testing.T, base string, settings map[string]string) Adapter {
	t.Helper()
	merged := map[string]string{"base_url": base}
	for k, v := range settings {
		merged[k] = v
	}
	adapter, err := New(Config{Kind: "123pan", Settings: merged,
		Secrets: map[string]string{"client_id": pan123ID, "client_secret": pan123Secret}})
	if err != nil {
		t.Fatalf("New(123pan): %v", err)
	}
	return adapter
}

func TestPan123HealthFetchesTokenWithPlatformHeader(t *testing.T) {
	server := &pan123Server{}
	srv := httptest.NewServer(server)
	defer srv.Close()
	adapter := newPan123Test(t, srv.URL, nil)

	if err := adapter.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
	if server.lastHdr != "open_platform" {
		t.Fatalf("Platform header = %q", server.lastHdr)
	}
	if !strings.Contains(server.lastForm, "clientID="+pan123ID) || !strings.Contains(server.lastForm, "clientSecret="+pan123Secret) {
		t.Fatalf("token form = %q", server.lastForm)
	}
}

func TestPan123TokenIsCachedUntilExpiry(t *testing.T) {
	server := &pan123Server{}
	srv := httptest.NewServer(server)
	defer srv.Close()
	adapter := newPan123Test(t, srv.URL, nil)
	for i := 0; i < 3; i++ {
		if err := adapter.Health(context.Background()); err != nil {
			t.Fatalf("Health #%d: %v", i, err)
		}
	}
	if server.tokens != 1 {
		t.Fatalf("token requests = %d, want 1 (cached)", server.tokens)
	}

	// 令牌临近过期时应重新获取。
	a := adapter.(*pan123Adapter)
	a.now = func() time.Time { return time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC) }
	if err := adapter.Health(context.Background()); err != nil {
		t.Fatalf("Health after expiry: %v", err)
	}
	if server.tokens != 2 {
		t.Fatalf("token requests = %d, want 2 after expiry", server.tokens)
	}
}

func TestPan123TokenErrorCodesAreClassified(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		status int
		class  ErrorClass
		retry  bool
	}{
		{"unauthorized code", `{"code":401,"message":"bad secret"}`, 200, ClassAuth, false},
		{"rate limited", `{"code":429,"message":"slow down"}`, 200, ClassUnavailable, true},
		{"other code", `{"code":5000,"message":"internal"}`, 200, ClassUnknown, false},
		{"http 503", `{}`, 503, ClassUnavailable, true},
		{"invalid json", `<html>`, 200, ClassUnknown, false},
		{"missing token", `{"code":0,"data":{"accessToken":""}}`, 200, ClassUnknown, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			err := newPan123Test(t, srv.URL, nil).Health(context.Background())
			requireClass(t, err, tc.class)
			if IsRetryable(err) != tc.retry {
				t.Fatalf("retryable = %v, want %v", IsRetryable(err), tc.retry)
			}
			if strings.Contains(err.Error(), pan123Secret) || strings.Contains(err.Error(), "bad secret") {
				t.Fatalf("error leaks secret or remote message: %v", err)
			}
		})
	}
}

func TestPan123NetworkFailureIsNetworkClass(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	err := newPan123Test(t, url, nil).Health(context.Background())
	requireClass(t, err, ClassNetwork)
	if errors.Is(err, ErrUnsupported) {
		t.Fatal("network failure must not be reported as unsupported")
	}
}

func TestPan123UploadIsExplicitlyUnverifiedWithoutNetwork(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer srv.Close()
	adapter := newPan123Test(t, srv.URL, nil)

	_, err := adapter.Put(context.Background(), Object{Key: "ab/x/p.pdf", Path: "/tmp/p.pdf", Size: 10})
	requireClass(t, err, ClassUnsupported)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatal("upload must satisfy errors.Is(err, ErrUnsupported)")
	}
	if AsCloudError(err).Detail != "123pan_upload_unverified" {
		t.Fatalf("detail = %q", AsCloudError(err).Detail)
	}
	if calls != 0 {
		t.Fatalf("unverified upload must not contact the network")
	}
}

func TestPan123UploadEnforcesSizeCapAndValidation(t *testing.T) {
	adapter := newPan123Test(t, "https://open-api.123pan.com", nil)
	a := adapter.(*pan123Adapter)

	_, err := adapter.Put(context.Background(), Object{Key: "ab/x/p.pdf", Path: "/tmp/p.pdf", Size: pan123MaxUploadBytes + 1})
	requireClass(t, err, ClassUnsupported)
	if AsCloudError(err).Detail != "pan123_file_too_large" {
		t.Fatalf("detail = %q", AsCloudError(err).Detail)
	}
	_, err = adapter.Put(context.Background(), Object{Key: "../p.pdf", Path: "/tmp/p.pdf", Size: 1})
	requireClass(t, err, ClassInvalidInput)
	_, err = adapter.Put(context.Background(), Object{Key: "ab/x/p.pdf", Path: "/tmp/p.pdf", Size: 0})
	requireClass(t, err, ClassInvalidInput)
	if a.maxUploadBytes != pan123MaxUploadBytes {
		t.Fatalf("cap = %d", a.maxUploadBytes)
	}
}

func TestPan123OpenAndShareAreUnsupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	adapter := newPan123Test(t, srv.URL, nil)

	reader, err := adapter.Open(context.Background(), "file-1")
	if reader != nil {
		_ = reader.Close()
		t.Fatal("Open must not return a body")
	}
	requireClass(t, err, ClassUnsupported)
	if AsCloudError(err).Detail != "123pan_download_unverified" {
		t.Fatalf("download detail = %q", AsCloudError(err).Detail)
	}

	share, err := adapter.Share(context.Background(), "file-1")
	requireClass(t, err, ClassUnsupported)
	if share.Status != "unsupported" || share.URL != "" {
		t.Fatalf("share = %+v", share)
	}
	if AsCloudError(err).Detail != "123pan_share_unverified" {
		t.Fatalf("share detail = %q", AsCloudError(err).Detail)
	}
}

func TestPan123CapabilitiesAreAllFalse(t *testing.T) {
	adapter := newPan123Test(t, "https://open-api.123pan.com", nil)
	caps := adapter.Capabilities()
	if caps.Upload || caps.Download || caps.Share {
		t.Fatalf("capabilities must not claim unverified features: %+v", caps)
	}
}

func TestPan123ConfigValidation(t *testing.T) {
	cases := []struct {
		name     string
		settings map[string]string
		secrets  map[string]string
		detail   string
	}{
		{"missing secret", map[string]string{}, map[string]string{"client_id": "a"}, "pan123_credentials_required"},
		{"parent not numeric", map[string]string{"parent_file_id": "root"}, map[string]string{"client_id": "a", "client_secret": "b"}, "pan123_parent_file_id_invalid"},
		{"bad base", map[string]string{"base_url": "ftp://open.example"}, map[string]string{"client_id": "a", "client_secret": "b"}, "base_url_scheme_invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(Config{Kind: "123pan", Settings: tc.settings, Secrets: tc.secrets})
			requireClass(t, err, ClassConfig)
			if AsCloudError(err).Detail != tc.detail {
				t.Fatalf("detail = %q, want %q", AsCloudError(err).Detail, tc.detail)
			}
		})
	}
}

func TestPan123Defaults(t *testing.T) {
	adapter := newPan123Test(t, "", nil)
	a := adapter.(*pan123Adapter)
	if a.origin != "https://open-api.123pan.com" || a.parentFileID != "0" {
		t.Fatalf("defaults origin=%q parent=%q", a.origin, a.parentFileID)
	}
}
