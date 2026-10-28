package service

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeAndTerms(t *testing.T) {
	normalized := normalizeSearchText("已知函数 f(x)＝x²＋2x，求最小值！")
	if strings.ContainsAny(normalized, "，！（）") || strings.Contains(normalized, " ") {
		t.Fatalf("punctuation/space not removed: %q", normalized)
	}
	terms := searchTerms("已知函数")
	for _, want := range []string{"已", "知", "函", "数", "已知", "知函", "函数"} {
		if terms[want] == 0 {
			t.Fatalf("missing term %q in %v", want, terms)
		}
	}
	if len(searchTerms("abc123")) != 1 {
		t.Fatalf("latin/digit run should be one term: %v", searchTerms("abc123"))
	}
}

func TestHighlightSnippet(t *testing.T) {
	text := "1. 已知函数 f(x)=x^2+2x，求函数的最小值。"
	snippet, highlights := highlightSnippet(text, normalizeSearchText("已知函数"), 40)
	if !strings.Contains(snippet, "已知函数") || len(highlights) == 0 {
		t.Fatalf("snippet=%q highlights=%v", snippet, highlights)
	}
	for _, h := range highlights {
		if h[0] < 0 || h[1] > len([]rune(snippet)) || h[0] >= h[1] {
			t.Fatalf("bad range %v for %q", h, snippet)
		}
	}
}

func TestSplitLongPagesKeepsPageNumber(t *testing.T) {
	long := strings.Repeat("第一段内容。\n", 400)
	chunks := splitLongPages([]pageText{{PageNo: 7, Text: long}}, 2000)
	if len(chunks) < 2 {
		t.Fatalf("expected chunks, got %d", len(chunks))
	}
	for _, chunk := range chunks {
		if chunk.PageNo != 7 || chunk.Text == "" {
			t.Fatalf("bad chunk: %+v", chunk)
		}
	}
}

func TestSplitIndexBatchForRetrySplitsDenseSinglePage(t *testing.T) {
	page := pageText{PageNo: 9, Text: strings.Repeat("题目正文。\n", 400)}
	split, ok := splitIndexBatchForRetry([]pageText{page})
	if !ok || len(split[0]) != 1 || len(split[1]) != 1 {
		t.Fatalf("split = %#v, ok = %v", split, ok)
	}
	if split[0][0].PageNo != page.PageNo || split[1][0].PageNo != page.PageNo {
		t.Fatalf("page number changed: %#v", split)
	}
	if split[0][0].Text == "" || split[1][0].Text == "" {
		t.Fatalf("empty split text: %#v", split)
	}
}

func TestSplitIndexBatchForRetryKeepsSmallSinglePage(t *testing.T) {
	if _, ok := splitIndexBatchForRetry([]pageText{{PageNo: 1, Text: "短题目"}}); ok {
		t.Fatal("small single page should be retried without splitting")
	}
}

func TestRepairJSONStrings(t *testing.T) {
	raw := "{\"questions\":[{\"text\":\"第一行\n第二行\",\"page_no\":1}]}"
	var parsed struct {
		Questions []struct {
			Text string `json:"text"`
		} `json:"questions"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
		t.Fatal("expected raw newline to be invalid JSON")
	}
	if err := json.Unmarshal([]byte(repairJSONStrings(raw)), &parsed); err != nil {
		t.Fatalf("repair failed: %v", err)
	}
	if !strings.Contains(parsed.Questions[0].Text, "\n") {
		t.Fatalf("newline lost: %q", parsed.Questions[0].Text)
	}
}
