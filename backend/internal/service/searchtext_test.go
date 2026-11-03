package service

import (
	"encoding/json"
	"errors"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/nic/paper-collect/backend/internal/ai"
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

func TestHighlightSnippetFindsTheBestPassage(t *testing.T) {
	text := "这是一道题。" + strings.Repeat("甲", 220) + "目标函数的最大值为多少？" + strings.Repeat("乙", 220)
	snippet, highlights := highlightSnippet(text, normalizeSearchText("目标函数的最大值"), 50)
	if !strings.HasPrefix(snippet, "…") || !strings.HasSuffix(snippet, "…") {
		t.Fatalf("truncated excerpt should have ellipses: %q", snippet)
	}
	if len(highlights) != 1 || string([]rune(snippet)[highlights[0][0]:highlights[0][1]]) != "目标函数的最大值" {
		t.Fatalf("did not highlight the complete matching passage: %q %v", snippet, highlights)
	}
	if strings.Contains(snippet, "这是一道题") {
		t.Fatalf("excerpt was anchored on an early common character: %q", snippet)
	}
}

func TestHighlightSnippetMatchesWidthCaseAndPunctuation(t *testing.T) {
	text := "说明🚀：ＡＢ， 中间结果"
	snippet, highlights := highlightSnippet(text, normalizeSearchText("ab中间"), 80)
	if len(highlights) != 1 || string([]rune(snippet)[highlights[0][0]:highlights[0][1]]) != "ＡＢ， 中间" {
		t.Fatalf("normalized match lost source offsets: %q %v", snippet, highlights)
	}
}

func TestHighlightSnippetPrefersLongestPartialMatch(t *testing.T) {
	text := "最先是一段无关说明。" + strings.Repeat("甲", 80) + "求函数的最小值"
	snippet, highlights := highlightSnippet(text, normalizeSearchText("求函数的最大值"), 40)
	if len(highlights) != 1 || string([]rune(snippet)[highlights[0][0]:highlights[0][1]]) != "求函数的最" {
		t.Fatalf("partial excerpt should prefer the longest shared passage: %q %v", snippet, highlights)
	}
}

func TestHighlightSnippetFindsOverlappingMatches(t *testing.T) {
	for _, tt := range []struct{ query, text, want string }{
		{"banana", "ananas", "anana"},
		{"abcbc", "zzbcb", "bcb"},
		{"abcabc", "cabca", "cabc"},
	} {
		snippet, highlights := highlightSnippet(tt.text, normalizeSearchText(tt.query), 40)
		if len(highlights) != 1 || string([]rune(snippet)[highlights[0][0]:highlights[0][1]]) != tt.want {
			t.Errorf("query %q in %q: %q %v; want %q", tt.query, tt.text, snippet, highlights, tt.want)
		}
	}
}

func TestHighlightSnippetAgainstLongestSubstring(t *testing.T) {
	random := rand.New(rand.NewSource(42))
	alphabet := []rune("abc甲乙")
	makeText := func(length int) []rune {
		out := make([]rune, length)
		for i := range out {
			out[i] = alphabet[random.Intn(len(alphabet))]
		}
		return out
	}
	for range 300 {
		query, text := makeText(1+random.Intn(12)), makeText(1+random.Intn(20))
		want := 0
		for i := range query {
			for j := range text {
				length := 0
				for i+length < len(query) && j+length < len(text) && query[i+length] == text[j+length] {
					length++
				}
				if length > want {
					want = length
				}
			}
		}
		_, highlights := highlightSnippet(string(text), string(query), 100)
		got := 0
		if len(highlights) > 0 {
			got = highlights[0][1] - highlights[0][0]
		}
		if got != want {
			t.Fatalf("query %q, text %q: matched length %d, want %d", string(query), string(text), got, want)
		}
	}
}

func TestHighlightSnippetWithoutMatchDoesNotInventHighlights(t *testing.T) {
	snippet, highlights := highlightSnippet("这是不相干的长文本", normalizeSearchText("abc"), 5)
	if snippet != "这是不相干…" || len(highlights) != 0 {
		t.Fatalf("unmatched excerpt = %q %v", snippet, highlights)
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

func TestSplitIndexBatchForRetryAlwaysShrinks(t *testing.T) {
	for pages := 2; pages <= 20; pages++ {
		part := make([]pageText, pages)
		for i := range part {
			part[i] = pageText{PageNo: i + 1, Text: "题目正文"}
		}
		split, ok := splitIndexBatchForRetry(part)
		if !ok {
			t.Fatalf("%d pages: expected a split", pages)
		}
		if len(split[0]) >= len(part) || len(split[1]) >= len(part) {
			t.Fatalf("%d pages: split did not shrink: left=%d right=%d", pages, len(split[0]), len(split[1]))
		}
	}
}

func TestBuildIndexBatchesUsesLimitsAndOverlap(t *testing.T) {
	pages := []pageText{
		{PageNo: 1, Text: "one"},
		{PageNo: 2, Text: "two"},
		{PageNo: 3, Text: "three"},
		{PageNo: 4, Text: "four"},
	}
	batches := buildIndexBatches(pages, 2, 100)
	if len(batches) != 3 {
		t.Fatalf("batches = %d, want 3", len(batches))
	}
	if len(batches[0]) != 2 || batches[1][0].PageNo != 2 || batches[2][0].PageNo != 3 {
		t.Fatalf("unexpected overlap: %#v", batches)
	}
}

func TestShouldSplitIndexErrorOnlyForMalformedOutput(t *testing.T) {
	if !shouldSplitIndexError(&ai.APIError{Class: "invalid_response", Detail: "模型返回空内容"}) {
		t.Fatal("empty response should be retried with a smaller batch")
	}
	if !shouldSplitIndexError(errors.New("切题模型输出不是合法 JSON")) {
		t.Fatal("malformed JSON should be retried with a smaller batch")
	}
	if shouldSplitIndexError(&ai.APIError{Class: "timeout", Detail: "上游请求超时"}) {
		t.Fatal("timeout should abort instead of multiplying calls")
	}
}

func TestShouldRetryIndexTransientOnlyForShortLivedFailures(t *testing.T) {
	if !shouldRetryIndexTransient(&ai.APIError{Class: "unavailable", Detail: "上游返回 HTTP 504", Retryable: true}) {
		t.Fatal("HTTP 504 should get one bounded retry")
	}
	if !shouldRetryIndexTransient(&ai.APIError{Class: "rate_limited", Detail: "上游限流", Retryable: true}) {
		t.Fatal("rate limiting should get one bounded retry")
	}
	if shouldRetryIndexTransient(&ai.APIError{Class: "timeout", Detail: "上游请求超时", Retryable: true}) {
		t.Fatal("timeout should not multiply a long-running request")
	}
	if shouldRetryIndexTransient(&ai.APIError{Class: "auth", Detail: "鉴权失败"}) {
		t.Fatal("auth errors are not transient")
	}
}

func TestSearchLLMTimeoutCoversDeepReasoning(t *testing.T) {
	if searchLLMTimeout < 5*time.Minute {
		t.Fatalf("search timeout = %s, want at least 5m", searchLLMTimeout)
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
