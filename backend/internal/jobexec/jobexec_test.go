package jobexec

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nic/paper-collect/backend/internal/ai"
	"github.com/nic/paper-collect/backend/internal/catalogai"
)

func TestSampleAnalysisDocumentKeepsHeadAndAnswerTail(t *testing.T) {
	document := strings.Repeat("题", 20) + "中间内容" + strings.Repeat("答", 20)
	sample := SampleAnalysisDocument(document, 26)
	if !strings.Contains(sample, "题题题题") || !strings.Contains(sample, "答答答答") || !strings.Contains(sample, "正文中间省略") {
		t.Fatalf("sample lost head, tail or omission marker: %q", sample)
	}
	short := "前面是完整试卷题目\n\n参考答案\n1. A\n2. B"
	if SampleAnalysisDocument(short, 12000) != short {
		t.Fatal("short document was changed before classification")
	}
}

func TestCardMessagesRequireDocumentAndTreatMetadataAsAuxiliary(t *testing.T) {
	in := CardMeta{FileName: "高三数学试卷.pdf", MediaType: "application/pdf"}.CardInput("正文第一页\n正文最后一页答案", 2, false)
	messages := catalogai.CardMessages(in, nil)
	if len(messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(messages))
	}
	system, _ := messages[0].Content.(string)
	user, _ := messages[1].Content.(string)
	if !strings.Contains(system, "以正文为准") || !strings.Contains(system, "末尾是否有集中答案区") {
		t.Fatalf("prompt does not put document content first: %s", system)
	}
	if !strings.Contains(user, "正文第一页") || !strings.Contains(user, "高三数学试卷.pdf") {
		t.Fatalf("user prompt misses document or metadata: %s", user)
	}
}

func TestSplitLongPagesKeepsPageNumber(t *testing.T) {
	chunks := SplitLongPages([]Page{{PageNo: 7, Text: strings.Repeat("第一段内容。\n", 400)}}, 2000)
	if len(chunks) < 2 {
		t.Fatalf("expected chunks, got %d", len(chunks))
	}
	for _, chunk := range chunks {
		if chunk.PageNo != 7 || chunk.Text == "" {
			t.Fatalf("bad chunk: %+v", chunk)
		}
	}
}

func TestSplitIndexBatchForRetry(t *testing.T) {
	page := Page{PageNo: 9, Text: strings.Repeat("题目正文。\n", 400)}
	split, ok := SplitIndexBatchForRetry([]Page{page})
	if !ok || split[0][0].PageNo != 9 || split[1][0].PageNo != 9 || split[0][0].Text == "" || split[1][0].Text == "" {
		t.Fatalf("dense page split = %#v, ok = %v", split, ok)
	}
	if _, ok := SplitIndexBatchForRetry([]Page{{PageNo: 1, Text: "短题目"}}); ok {
		t.Fatal("small single page should be retried without splitting")
	}
	for pages := 2; pages <= 20; pages++ {
		part := make([]Page, pages)
		for i := range part {
			part[i] = Page{PageNo: i + 1, Text: "题目正文"}
		}
		split, ok := SplitIndexBatchForRetry(part)
		if !ok || len(split[0]) >= len(part) || len(split[1]) >= len(part) {
			t.Fatalf("%d pages: split did not shrink", pages)
		}
	}
}

func TestBuildIndexBatchesUsesLimitsAndOverlap(t *testing.T) {
	pages := []Page{{PageNo: 1, Text: "one"}, {PageNo: 2, Text: "two"}, {PageNo: 3, Text: "three"}, {PageNo: 4, Text: "four"}}
	batches := BuildIndexBatches(pages, 2, 100)
	if len(batches) != 3 || len(batches[0]) != 2 || batches[1][0].PageNo != 2 || batches[2][0].PageNo != 3 {
		t.Fatalf("unexpected batches: %#v", batches)
	}
}

func TestIndexRetryClassification(t *testing.T) {
	if !ShouldSplitIndexError(&ai.APIError{Class: "invalid_response"}) || !ShouldSplitIndexError(errors.New("切题模型输出不是合法 JSON")) {
		t.Fatal("malformed output should be retried with a smaller batch")
	}
	if ShouldSplitIndexError(&ai.APIError{Class: "timeout"}) {
		t.Fatal("timeout should abort instead of multiplying calls")
	}
	if !ShouldRetryIndexTransient(&ai.APIError{Class: "unavailable", Retryable: true}) || !ShouldRetryIndexTransient(&ai.APIError{Class: "rate_limited", Retryable: true}) {
		t.Fatal("short-lived upstream failures get one bounded retry")
	}
	if ShouldRetryIndexTransient(&ai.APIError{Class: "timeout", Retryable: true}) || ShouldRetryIndexTransient(&ai.APIError{Class: "auth"}) {
		t.Fatal("timeouts and auth errors are not retried")
	}
	if SearchLLMTimeout < 5*time.Minute {
		t.Fatalf("search timeout = %s, want at least 5m", SearchLLMTimeout)
	}
}

func TestRepairJSONStringsAndFormulaEscapes(t *testing.T) {
	var parsed struct {
		Questions []struct {
			Text string `json:"text"`
		} `json:"questions"`
	}
	raw := "{\"questions\":[{\"text\":\"第一行\n第二行\",\"page_no\":1}]}"
	if err := json.Unmarshal([]byte(RepairJSONStrings(raw)), &parsed); err != nil || !strings.Contains(parsed.Questions[0].Text, "\n") {
		t.Fatalf("repair failed: %v", err)
	}
	source := `原文包含 \frac{x}{2}、\theta 和 \neq。`
	var output struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(repairJSONMathCommands(`{"text":"求 \frac{x}{2}+\theta，且 x\neq 0\n下一行"}`, source)), &output); err != nil {
		t.Fatal(err)
	}
	if output.Text != "求 \\frac{x}{2}+\\theta，且 x\\neq 0\n下一行" {
		t.Fatalf("formula or newline was changed: %q", output.Text)
	}
	if proper := `{"text":"\\frac{x}{2}"}`; repairJSONMathCommands(proper, source) != proper {
		t.Fatal("valid JSON was changed")
	}
	if !hasInvalidFormulaControls("错误的\frac", "") || hasInvalidFormulaControls("第一行\nequation follows", "") {
		t.Fatal("formula control detection is wrong")
	}
}

func TestVerifyReaderRejectsTamperedContent(t *testing.T) {
	good := "hello"
	sha := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if _, err := readAllLimit(VerifyReader(nopCloser{strings.NewReader(good)}, sha, 5), 100); err != nil {
		t.Fatalf("valid content rejected: %v", err)
	}
	if _, err := readAllLimit(VerifyReader(nopCloser{strings.NewReader("hellx")}, sha, 5), 100); !errors.Is(err, ErrContentMismatch) {
		t.Fatalf("tampered content accepted: %v", err)
	}
	if _, err := readAllLimit(VerifyReader(nopCloser{strings.NewReader("hello!")}, sha, 5), 100); !errors.Is(err, ErrContentMismatch) {
		t.Fatalf("oversized content accepted: %v", err)
	}
}

type nopCloser struct{ *strings.Reader }

func (nopCloser) Close() error { return nil }
