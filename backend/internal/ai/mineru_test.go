package ai

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func zipBytes(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if _, err := w.Write(content); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return buf.Bytes()
}

func TestParseMineruZip(t *testing.T) {
	raw := zipBytes(t, map[string][]byte{
		"full.md":                 []byte("# 数学\n导数练习"),
		"images/page-1.png":       []byte("\x89PNG\r\n\x1a\nrest"),
		"batch_content_list.json": []byte("{}"),
	})
	result, err := parseMineruZip(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !strings.Contains(result.Markdown, "导数练习") {
		t.Fatalf("markdown: %q", result.Markdown)
	}
	if len(result.Images) != 1 || result.PageCount != 1 {
		t.Fatalf("images=%d pages=%d", len(result.Images), result.PageCount)
	}
}

func TestParseMineruZipErrors(t *testing.T) {
	empty := zipBytes(t, map[string][]byte{"full.md": []byte("   \n")})
	if _, err := parseMineruZip(empty); err == nil || !strings.Contains(err.Error(), "没有文本内容") {
		t.Fatalf("expected empty-text error, got %v", err)
	}
	noMd := zipBytes(t, map[string][]byte{"images/page-1.png": []byte("x")})
	if _, err := parseMineruZip(noMd); err == nil || !strings.Contains(err.Error(), "缺少 Markdown") {
		t.Fatalf("expected missing-markdown error, got %v", err)
	}
}
