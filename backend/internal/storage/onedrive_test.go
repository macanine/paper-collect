package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fakeResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}
}

func TestOneDriveSharedFolderUploadAndShare(t *testing.T) {
	const size = oneDriveChunkSize + 123
	file := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(file, bytes.Repeat([]byte("x"), size), 0600); err != nil {
		t.Fatal(err)
	}
	var uploads, progress, tokens int
	var rotated bool
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host {
		case "login.microsoftonline.com":
			tokens++
			return fakeResponse(r, 200, `{"access_token":"access-value","refresh_token":"new-refresh","expires_in":3600}`), nil
		case "upload.1drv.com":
			if r.Header.Get("Authorization") != "" {
				t.Error("upload session received bearer token")
			}
			content, _ := io.ReadAll(r.Body)
			uploads++
			if uploads == 1 {
				if len(content) != oneDriveChunkSize || r.Header.Get("Content-Range") != fmt.Sprintf("bytes 0-%d/%d", oneDriveChunkSize-1, size) {
					t.Error("incorrect first chunk")
				}
				return fakeResponse(r, 202, `{}`), nil
			}
			if len(content) != 123 {
				t.Error("incorrect final chunk")
			}
			return fakeResponse(r, 201, fmt.Sprintf(`{"id":"file-id","size":%d}`, size)), nil
		case "graph.microsoft.com":
			if r.Header.Get("Authorization") != "Bearer access-value" {
				t.Error("missing graph authorization")
			}
			switch {
			case strings.HasPrefix(r.URL.Path, "/v1.0/shares/"):
				if r.Header.Get("Prefer") == "" {
					t.Error("share must be redeemed")
				}
				return fakeResponse(r, 200, `{"id":"root","folder":{},"parentReference":{"driveId":"drive"}}`), nil
			case strings.HasSuffix(r.URL.Path, ":/folder"):
				return fakeResponse(r, 404, `{}`), nil
			case strings.HasSuffix(r.URL.Path, "/children") && r.Method == http.MethodPost:
				return fakeResponse(r, 201, `{"id":"folder-id","folder":{}}`), nil
			case strings.HasSuffix(r.URL.Path, ":/createUploadSession"):
				return fakeResponse(r, 200, `{"uploadUrl":"https://upload.1drv.com/session?key=sensitive"}`), nil
			case strings.HasSuffix(r.URL.Path, "/createLink"):
				return fakeResponse(r, 201, `{"link":{"webUrl":"https://1drv.ms/share-example"}}`), nil
			case strings.HasSuffix(r.URL.Path, "/content"):
				return fakeResponse(r, 200, "file contents"), nil
			}
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		return fakeResponse(r, 404, `{}`), nil
	})}
	cfg := Config{Settings: map[string]string{"client_id": "app", "share_scope": "anonymous"}, Secrets: map[string]string{"refresh_token": "old-refresh", "share_url": "https://example.sharepoint.com/folder?secret=hidden"}, SaveRefreshToken: func(_ context.Context, old, next string) error {
		rotated = old == "old-refresh" && next == "new-refresh"
		return nil
	}}
	a, err := NewOneDrive(cfg, client)
	if err != nil {
		t.Fatal(err)
	}
	a.(*oneDriveAdapter).simpleUploadLimit = 0
	stored, err := a.Put(context.Background(), Object{Key: "folder/file.pdf", Path: file, Size: size, Progress: func(done, total int64) {
		if done > 0 && total == size {
			progress++
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !rotated || tokens != 1 || uploads != 2 || progress != 2 || stored.Verification != "size" {
		t.Fatalf("unexpected token, transfer or verification state")
	}
	share, err := a.Share(context.Background(), stored.RemoteID)
	if err != nil || share.Status != "ready" {
		t.Fatalf("share: %v", err)
	}
	reader, err := a.Open(context.Background(), stored.RemoteID)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	content, _ := io.ReadAll(reader)
	if string(content) != "file contents" {
		t.Fatal("download content mismatch")
	}
}

func TestOneDriveSmallFileUsesSimpleUpload(t *testing.T) {
	const content = "small-file"
	file := filepath.Join(t.TempDir(), "source.pdf")
	if err := os.WriteFile(file, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	var uploads, progress int
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.URL.Host == "login.microsoftonline.com":
			return fakeResponse(r, 200, `{"access_token":"access-value","expires_in":3600}`), nil
		case r.URL.Host == "graph.microsoft.com" && r.URL.Path == "/v1.0/me/drive/root":
			return fakeResponse(r, 200, `{"id":"root","folder":{},"parentReference":{"driveId":"drive"}}`), nil
		case r.URL.Host == "graph.microsoft.com" && r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, ":/content"):
			uploads++
			if r.URL.Query().Get("@microsoft.graph.conflictBehavior") != "replace" ||
				r.Header.Get("Authorization") != "Bearer access-value" ||
				r.Header.Get("Content-Type") != "application/octet-stream" ||
				r.ContentLength != int64(len(content)) {
				t.Error("simple upload request is invalid")
			}
			data, _ := io.ReadAll(r.Body)
			if string(data) != content {
				t.Errorf("unexpected simple upload content: %q", data)
			}
			return fakeResponse(r, 201, fmt.Sprintf(`{"id":"simple-id","size":%d}`, len(content))), nil
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		return fakeResponse(r, 404, `{}`), nil
	})}
	a, err := NewOneDrive(Config{Settings: map[string]string{"client_id": "app"}, Secrets: map[string]string{"refresh_token": "refresh"}}, client)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := a.Put(context.Background(), Object{Key: "paper.pdf", Path: file, Size: int64(len(content)), Progress: func(done, total int64) {
		if done > 0 && total == int64(len(content)) {
			progress++
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	drive, item, err := decodeOneDriveID(stored.RemoteID)
	if err != nil || drive != "drive" || item != "simple-id" || uploads != 1 || progress == 0 {
		t.Fatalf("unexpected simple upload result: drive=%q item=%q uploads=%d progress=%d err=%v", drive, item, uploads, progress, err)
	}
}

func TestOneDriveUploadSessionRemovesZeroByteStubOnConflict(t *testing.T) {
	file := filepath.Join(t.TempDir(), "source.pdf")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	var sessions, stubReads, stubDeletes, uploads int
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host {
		case "login.microsoftonline.com":
			return fakeResponse(r, 200, `{"access_token":"access-value","expires_in":3600}`), nil
		case "graph.microsoft.com":
			switch {
			case r.URL.Path == "/v1.0/me/drive/root":
				return fakeResponse(r, 200, `{"id":"root","folder":{},"parentReference":{"driveId":"drive"}}`), nil
			case strings.HasSuffix(r.URL.Path, ":/createUploadSession"):
				sessions++
				if sessions == 1 {
					return fakeResponse(r, http.StatusConflict, `{"error":{"code":"nameAlreadyExists"}}`), nil
				}
				return fakeResponse(r, 201, `{"uploadUrl":"https://upload.1drv.com/session?key=private"}`), nil
			case r.URL.Path == "/v1.0/drives/drive/items/root:/paper.pdf" && r.Method == http.MethodGet:
				stubReads++
				return fakeResponse(r, 200, `{"id":"stub-id","size":0}`), nil
			case r.URL.Path == "/v1.0/drives/drive/items/stub-id" && r.Method == http.MethodDelete:
				stubDeletes++
				return fakeResponse(r, http.StatusNoContent, ""), nil
			}
		case "upload.1drv.com":
			if r.Header.Get("Authorization") != "" {
				t.Error("upload session received bearer token")
			}
			data, _ := io.ReadAll(r.Body)
			uploads++
			if string(data) != "x" {
				t.Errorf("unexpected upload content: %q", data)
			}
			return fakeResponse(r, 201, `{"id":"file-id","size":1}`), nil
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		return fakeResponse(r, 404, `{}`), nil
	})}
	a, err := NewOneDrive(Config{Settings: map[string]string{"client_id": "app"}, Secrets: map[string]string{"refresh_token": "refresh"}}, client)
	if err != nil {
		t.Fatal(err)
	}
	a.(*oneDriveAdapter).simpleUploadLimit = 0
	stored, err := a.Put(context.Background(), Object{Key: "paper.pdf", Path: file, Size: 1})
	if err != nil {
		t.Fatal(err)
	}
	_, item, err := decodeOneDriveID(stored.RemoteID)
	if err != nil || item != "file-id" || sessions != 2 || stubReads != 1 || stubDeletes != 1 || uploads != 1 {
		t.Fatalf("stub cleanup did not recover upload: item=%q sessions=%d reads=%d deletes=%d uploads=%d err=%v", item, sessions, stubReads, stubDeletes, uploads, err)
	}
}

func TestOneDriveRejectsInvalidUploadSessionURLs(t *testing.T) {
	file := filepath.Join(t.TempDir(), "source.pdf")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, response, detail string
	}{
		{name: "missing", response: `{}`, detail: "onedrive_upload_session_url_missing"},
		{name: "malformed", response: `{"uploadUrl":"://private-value"}`, detail: "onedrive_upload_session_url_invalid"},
		{name: "insecure", response: `{"uploadUrl":"http://upload.1drv.com/private-value"}`, detail: "onedrive_upload_session_url_invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				switch {
				case r.URL.Host == "login.microsoftonline.com":
					return fakeResponse(r, 200, `{"access_token":"access-value","expires_in":3600}`), nil
				case r.URL.Path == "/v1.0/me/drive/root":
					return fakeResponse(r, 200, `{"id":"root","folder":{},"parentReference":{"driveId":"drive"}}`), nil
				case strings.HasSuffix(r.URL.Path, ":/createUploadSession"):
					return fakeResponse(r, 201, tt.response), nil
				}
				t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				return fakeResponse(r, 404, `{}`), nil
			})}
			a, err := NewOneDrive(Config{Settings: map[string]string{"client_id": "app"}, Secrets: map[string]string{"refresh_token": "refresh"}}, client)
			if err != nil {
				t.Fatal(err)
			}
			a.(*oneDriveAdapter).simpleUploadLimit = 0
			_, err = a.Put(context.Background(), Object{Key: "paper.pdf", Path: file, Size: 1})
			if err == nil || AsCloudError(err).Detail != tt.detail || strings.Contains(err.Error(), "private-value") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestOneDriveDownloadRedirectDoesNotForwardBearer(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host {
		case "login.microsoftonline.com":
			return fakeResponse(r, 200, `{"access_token":"access","expires_in":3600}`), nil
		case "graph.microsoft.com":
			resp := fakeResponse(r, http.StatusFound, "")
			resp.Header.Set("Location", "https://download.1drv.com/file")
			return resp, nil
		case "download.1drv.com":
			if r.Header.Get("Authorization") != "" {
				t.Error("bearer token forwarded to download host")
			}
			return fakeResponse(r, 200, "content"), nil
		}
		t.Error("unexpected host")
		return fakeResponse(r, 404, ""), nil
	})}
	a, err := NewOneDrive(Config{Settings: map[string]string{"client_id": "app"}, Secrets: map[string]string{"refresh_token": "refresh"}}, client)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := a.Open(context.Background(), "ZHJpdmU.ZmlsZQ")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	content, _ := io.ReadAll(reader)
	if string(content) != "content" {
		t.Fatal("unexpected download")
	}
}

func TestOneDriveRejectsUntrustedUploadSession(t *testing.T) {
	file := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "login.microsoftonline.com" {
			return fakeResponse(r, 200, `{"access_token":"secret","expires_in":3600}`), nil
		}
		if r.URL.Path == "/v1.0/me/drive/root" {
			return fakeResponse(r, 200, `{"id":"root","folder":{},"parentReference":{"driveId":"drive"}}`), nil
		}
		return fakeResponse(r, 200, `{"uploadUrl":"https://attacker.example/upload?token=private"}`), nil
	})}
	a, err := NewOneDrive(Config{Settings: map[string]string{"client_id": "app"}, Secrets: map[string]string{"refresh_token": "private"}}, client)
	if err != nil {
		t.Fatal(err)
	}
	a.(*oneDriveAdapter).simpleUploadLimit = 0
	_, err = a.Put(context.Background(), Object{Key: "file.pdf", Path: file, Size: 1})
	if err == nil || AsCloudError(err).Detail != "onedrive_upload_session_host_invalid" || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "attacker") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOneDriveOpenListAuthorizationUploadAndShare(t *testing.T) {
	file := filepath.Join(t.TempDir(), "source.pdf")
	if err := os.WriteFile(file, []byte("test-file"), 0600); err != nil {
		t.Fatal(err)
	}
	var rotated bool
	var requests int
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		switch r.URL.Host {
		case "api.oplist.org":
			if r.Method != http.MethodGet || r.URL.Path != "/onedrive/renewapi" || r.URL.Query().Get("server_use") != "true" || r.URL.Query().Get("driver_txt") != "onedrive_pr" || r.URL.Query().Get("refresh_ui") != "test-refresh" || r.Header.Get("Authorization") != "" {
				t.Error("OpenList renewal request does not match the published protocol")
			}
			return fakeResponse(r, 200, `{"access_token":"test-access","refresh_token":"rotated-refresh"}`), nil
		case "graph.microsoft.com":
			if r.Header.Get("Authorization") != "Bearer test-access" {
				t.Error("missing Graph access token")
			}
			switch {
			case r.URL.Path == "/v1.0/me/drive/root":
				return fakeResponse(r, 200, `{"id":"root","folder":{},"parentReference":{"driveId":"drive"}}`), nil
			case strings.HasSuffix(r.URL.Path, ":/createUploadSession"):
				return fakeResponse(r, 200, `{"uploadUrl":"https://upload.1drv.com/session"}`), nil
			case strings.HasSuffix(r.URL.Path, "/createLink"):
				return fakeResponse(r, 201, `{"link":{"webUrl":"https://1drv.ms/example"}}`), nil
			}
		case "upload.1drv.com":
			data, _ := io.ReadAll(r.Body)
			if string(data) != "test-file" || r.Header.Get("Authorization") != "" {
				t.Error("invalid upload content or unexpected bearer token")
			}
			return fakeResponse(r, 201, `{"id":"file-id","size":9}`), nil
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Host)
		return fakeResponse(r, 404, `{}`), nil
	})}
	cfg := Config{Settings: map[string]string{"auth_mode": "openlist"}, Secrets: map[string]string{"refresh_token": "test-refresh"}, SaveRefreshToken: func(_ context.Context, old, next string) error {
		rotated = old == "test-refresh" && next == "rotated-refresh"
		return nil
	}}
	a, err := NewOneDrive(cfg, client)
	if err != nil {
		t.Fatal(err)
	}
	a.(*oneDriveAdapter).simpleUploadLimit = 0
	stored, err := a.Put(context.Background(), Object{Key: "test.pdf", Path: file, Size: 9})
	if err != nil {
		t.Fatal(err)
	}
	share, err := a.Share(context.Background(), stored.RemoteID)
	if err != nil || share.Status != "ready" || !rotated || requests != 5 {
		t.Fatalf("OpenList upload/share did not succeed: %v", err)
	}
}

func TestOneDriveOpenListDoesNotLeakTokenOnFailureOrRedirect(t *testing.T) {
	var calls int
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		response := fakeResponse(r, http.StatusTemporaryRedirect, "")
		response.Header.Set("Location", "https://attacker.invalid/receive")
		return response, nil
	})}
	a, err := NewOneDrive(Config{Settings: map[string]string{"auth_mode": "openlist"}, Secrets: map[string]string{"refresh_token": "private-test-token"}}, client)
	if err != nil {
		t.Fatal(err)
	}
	err = a.Health(context.Background())
	if err == nil || calls != 1 || strings.Contains(err.Error(), "private-test-token") {
		t.Fatalf("redirect must be rejected without exposing the token: %v", err)
	}
	client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("failed for %s", r.URL.String())
	})
	a, err = NewOneDrive(Config{Settings: map[string]string{"auth_mode": "openlist"}, Secrets: map[string]string{"refresh_token": "private-test-token"}}, client)
	if err != nil {
		t.Fatal(err)
	}
	err = a.Health(context.Background())
	if err == nil || strings.Contains(err.Error(), "private-test-token") {
		t.Fatalf("transport failure must not expose the token: %v", err)
	}
}

func TestOneDriveOpenListExpiredTokenIsNotRetriedOrExposed(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return fakeResponse(r, 500, `{"text":"AADSTS700082: expired private-test-token"}`), nil
	})}
	a, err := NewOneDrive(Config{Settings: map[string]string{"auth_mode": "openlist"}, Secrets: map[string]string{"refresh_token": "private-test-token"}}, client)
	if err != nil {
		t.Fatal(err)
	}
	err = a.Health(context.Background())
	if err == nil || AsCloudError(err).Class != ClassAuth || IsRetryable(err) || strings.Contains(err.Error(), "private-test-token") {
		t.Fatalf("expired token should be a redacted authentication error: %v", err)
	}
}
