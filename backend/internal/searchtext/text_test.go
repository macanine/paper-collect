package searchtext

import (
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNormalizeAndTerms(t *testing.T) {
	normalized := Normalize("已知函数 f(x)＝x²＋2x，求最小值！")
	if strings.ContainsAny(normalized, "，！（）") || strings.Contains(normalized, " ") {
		t.Fatalf("punctuation/space not removed: %q", normalized)
	}
	terms := Terms("已知函数")
	for _, want := range []string{"已", "知", "函", "数", "已知", "知函", "函数"} {
		if terms[want] == 0 {
			t.Fatalf("missing term %q in %v", want, terms)
		}
	}
	if len(Terms("abc123")) != 1 {
		t.Fatalf("latin/digit run should be one term: %v", Terms("abc123"))
	}
}

func TestSearchTermsBoundsLongLatinRuns(t *testing.T) {
	long := strings.Repeat("A1", 80)
	got := Terms(Normalize(long))
	if len(got) != 1 {
		t.Fatalf("long Latin run should produce one bounded term: %v", got)
	}
	for term, count := range got {
		if count != 1 {
			t.Fatalf("term %q count = %d, want 1", term, count)
		}
		if utf8.RuneCountInString(term) > maxSearchTermRunes {
			t.Fatalf("term has %d runes, want at most %d", utf8.RuneCountInString(term), maxSearchTermRunes)
		}
		if term != boundedSearchTerm(Normalize(long)) {
			t.Fatalf("term %q is not deterministic", term)
		}
	}
	other := Terms(Normalize(long + "B"))
	if len(other) != 1 {
		t.Fatalf("different long run should also produce one term: %v", other)
	}
	for term := range got {
		if other[term] != 0 {
			t.Fatalf("different long runs mapped to the same term %q", term)
		}
	}
}

func TestHighlightSnippet(t *testing.T) {
	text := "1. 已知函数 f(x)=x^2+2x，求函数的最小值。"
	snippet, highlights := highlightSnippet(text, Normalize("已知函数"), 40)
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
	snippet, highlights := highlightSnippet(text, Normalize("目标函数的最大值"), 50)
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
	snippet, highlights := highlightSnippet(text, Normalize("ab中间"), 80)
	if len(highlights) != 1 || string([]rune(snippet)[highlights[0][0]:highlights[0][1]]) != "ＡＢ， 中间" {
		t.Fatalf("normalized match lost source offsets: %q %v", snippet, highlights)
	}
}

func TestHighlightSnippetPrefersLongestPartialMatch(t *testing.T) {
	text := "最先是一段无关说明。" + strings.Repeat("甲", 80) + "求函数的最小值"
	snippet, highlights := highlightSnippet(text, Normalize("求函数的最大值"), 40)
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
		snippet, highlights := highlightSnippet(tt.text, Normalize(tt.query), 40)
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
	snippet, highlights := highlightSnippet("这是不相干的长文本", Normalize("abc"), 5)
	if snippet != "这是不相干…" || len(highlights) != 0 {
		t.Fatalf("unmatched excerpt = %q %v", snippet, highlights)
	}
}

func highlightSnippet(text, normalizedQuery string, limit int) (string, [][2]int) {
	return NewSnippetMatcher(normalizedQuery).Highlight(text, limit)
}
