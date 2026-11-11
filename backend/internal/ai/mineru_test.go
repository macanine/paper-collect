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

func TestParseMineruZipUsesDetailedLayoutPages(t *testing.T) {
	layout := `{"pdf_info":[
	 {"page_idx":0,"para_blocks":[{"type":"text","lines":[{"spans":[{"type":"text","content":"高三数学试题卷"}]}]}]},
	 {"page_idx":1,"para_blocks":[{"type":"table","blocks":[{"type":"table_body","lines":[{"spans":[{"type":"table","html":"<table><tr><td>题号</td><td>答案</td></tr></table>"}]}]}]}]},
	 {"page_idx":2,"para_blocks":[]},
	 {"page_idx":3,"para_blocks":[{"type":"title","lines":[{"spans":[{"type":"text","content":"参考答案"}]}]}]}
	]}`
	raw := zipBytes(t, map[string][]byte{
		"full.md":     []byte("高三数学试题卷\n参考答案"),
		"layout.json": []byte(layout),
	})
	result, err := parseMineruZip(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if result.PageCount != 4 {
		t.Fatalf("page count = %d, want 4 (empty pages count too)", result.PageCount)
	}
	if len(result.Pages) != 3 || result.Pages[0].PageNo != 1 || result.Pages[1].PageNo != 2 || result.Pages[2].PageNo != 4 {
		t.Fatalf("pages = %+v", result.Pages)
	}
	if !strings.Contains(result.Pages[1].Text, "题号 答案") || result.Pages[2].Text != "参考答案" {
		t.Fatalf("page text = %+v", result.Pages)
	}
}

func TestParseMineruZipContentListV2Pages(t *testing.T) {
	v2 := `[[{"type":"title","content":{"title_content":[{"type":"text","content":"期中考试"}]}}],[{"type":"paragraph","content":{"paragraph_content":[{"type":"text","content":"参考答案"}]}}]]`
	raw := zipBytes(t, map[string][]byte{"full.md": []byte("期中考试\n参考答案"), "x_content_list_v2.json": []byte(v2)})
	result, err := parseMineruZip(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if result.PageCount != 2 || len(result.Pages) != 2 || !strings.Contains(result.Pages[1].Text, "参考答案") {
		t.Fatalf("result = %+v", result)
	}
}

func TestParseMineruZipNeverInventsPages(t *testing.T) {
	raw := zipBytes(t, map[string][]byte{"full.md": []byte("很长的正文")})
	result, err := parseMineruZip(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(result.Pages) != 0 || result.PageCount != 0 {
		t.Fatalf("without page structure got pages=%v count=%d", result.Pages, result.PageCount)
	}
}
