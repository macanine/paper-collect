package storage

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestS3UploadDownloadAndPresignedShare(t *testing.T) {
	var saved []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
			t.Error("request was not signed")
		}
		switch {
		case r.Method == http.MethodHead && r.URL.Path == "/bucket":
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/bucket/archive/a/file.pdf" && r.Method == http.MethodPut:
			saved, _ = io.ReadAll(r.Body)
			w.Header().Set("ETag", `"etag"`)
		case r.URL.Path == "/bucket/archive/a/file.pdf" && r.Method == http.MethodHead:
			w.Header().Set("Content-Length", strconv.Itoa(len(saved)))
		case r.URL.Path == "/bucket/archive/a/file.pdf" && r.Method == http.MethodGet:
			_, _ = w.Write(saved)
		default:
			t.Errorf("unexpected operation %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	cfg := Config{Settings: map[string]string{"endpoint": server.URL, "region": "us-east-1", "bucket": "bucket", "path_prefix": "archive", "path_style": "true", "share_expires_seconds": "900"}, Secrets: map[string]string{"access_key_id": "access", "secret_access_key": "secret"}}
	a, err := NewS3(cfg, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}
	stored, err := a.Put(context.Background(), Object{Key: "a/file.pdf", Path: file, Size: 7})
	if err != nil {
		t.Fatal(err)
	}
	if stored.Verification != "size" || string(saved) != "content" {
		t.Fatal("incorrect uploaded content or verification")
	}
	reader, err := a.Open(context.Background(), stored.RemoteID)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	got, _ := io.ReadAll(reader)
	if string(got) != "content" {
		t.Fatal("incorrect download")
	}
	share, err := a.Share(context.Background(), stored.RemoteID)
	if err != nil || share.Status != "ready" || share.ExpiresAt == nil || !strings.Contains(share.URL, "X-Amz-Expires=900") {
		t.Fatalf("invalid presigned share: %v", err)
	}
	if _, err := a.Open(context.Background(), "../outside"); err == nil {
		t.Fatal("unsafe object key accepted")
	}
}

func TestS3MultipartUploadAndAbort(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(strconv.FormatBool(fail), func(t *testing.T) {
			var mu sync.Mutex
			parts := map[string]int{}
			aborted, completed := false, false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				query := r.URL.Query()
				switch {
				case r.Method == http.MethodPost && query.Has("uploads"):
					_, _ = io.WriteString(w, `<InitiateMultipartUploadResult><Bucket>bucket</Bucket><Key>file.pdf</Key><UploadId>upload-1</UploadId></InitiateMultipartUploadResult>`)
				case r.Method == http.MethodPut && query.Get("partNumber") != "":
					if fail {
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					body, _ := io.ReadAll(r.Body)
					parts[query.Get("partNumber")] = len(body)
					w.Header().Set("ETag", `"part"`)
				case r.Method == http.MethodPost && query.Get("uploadId") != "":
					completed = true
					_, _ = io.WriteString(w, `<CompleteMultipartUploadResult><Bucket>bucket</Bucket><Key>file.pdf</Key><ETag>"done"</ETag></CompleteMultipartUploadResult>`)
				case r.Method == http.MethodDelete && query.Get("uploadId") != "":
					aborted = true
					w.WriteHeader(http.StatusNoContent)
				case r.Method == http.MethodHead:
					w.Header().Set("Content-Length", strconv.Itoa(17<<20))
				default:
					t.Errorf("unexpected multipart operation %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			a, err := NewS3(Config{Settings: map[string]string{"endpoint": server.URL, "region": "us-east-1", "bucket": "bucket", "path_style": "true"}, Secrets: map[string]string{"access_key_id": "a", "secret_access_key": "b"}}, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(t.TempDir(), "file")
			if err := os.WriteFile(file, bytes.Repeat([]byte("a"), 17<<20), 0600); err != nil {
				t.Fatal(err)
			}
			_, err = a.Put(context.Background(), Object{Key: "file.pdf", Path: file, Size: 17 << 20})
			mu.Lock()
			defer mu.Unlock()
			if fail {
				if err == nil || !aborted || completed {
					t.Fatalf("multipart failure was not aborted: %v", err)
				}
			} else if err != nil || aborted || !completed || len(parts) < 2 {
				t.Fatalf("multipart upload failed: err=%v parts=%d", err, len(parts))
			}
		})
	}
}
