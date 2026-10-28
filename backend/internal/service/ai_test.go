package service

import (
	"strings"
	"testing"
)

func TestParseSuggestionsAcceptsCurrentRolesOnly(t *testing.T) {
	result, _, err := parseSuggestions(`{"title":"测试","role":"teacher_edition","confidence":0.9}`)
	if err != nil || result.Role != "teacher_edition" {
		t.Fatalf("current role: %+v, %v", result, err)
	}
	result, _, err = parseSuggestions(`{"title":"测试","role":"lecture","confidence":0.9}`)
	if err != nil || result.Role != "lecture" {
		t.Fatalf("lecture role should be accepted: %+v, %v", result, err)
	}
	result, _, err = parseSuggestions(`{"title":"测试","role":"teacher","confidence":0.9}`)
	if err != nil || result.Role != "" {
		t.Fatalf("unknown role should be discarded: %+v, %v", result, err)
	}
}

func TestSampleAnalysisDocumentKeepsHeadAndAnswerTail(t *testing.T) {
	document := strings.Repeat("题", 20) + "中间内容" + strings.Repeat("答", 20)
	sample := sampleAnalysisDocument(document, 26)
	if !strings.Contains(sample, "题题题题") {
		t.Fatalf("sample dropped document head: %q", sample)
	}
	if !strings.Contains(sample, "答答答答") {
		t.Fatalf("sample dropped answer tail: %q", sample)
	}
	if !strings.Contains(sample, "正文中间省略") {
		t.Fatalf("sample did not mark omitted content: %q", sample)
	}
}

func TestAnalysisMessagesRequireDocumentAndTreatFilenameAsAuxiliary(t *testing.T) {
	messages := buildAnalysisMessages("正文第一页\n正文最后一页答案", "高三数学试卷.pdf", "试卷.pdf", nil)
	if len(messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(messages))
	}
	system, _ := messages[0].Content.(string)
	user, _ := messages[1].Content.(string)
	if !strings.Contains(system, "不得仅凭文件名归类") ||
		!strings.Contains(system, "正文末尾") {
		t.Fatalf("prompt does not forbid filename-only classification: %s", system)
	}
	if !strings.Contains(user, "正文第一页") || !strings.Contains(user, "正文最后一页答案") {
		t.Fatalf("user prompt does not include document content: %s", user)
	}
}

func TestAnalysisMessagesKeepCompleteShortDocument(t *testing.T) {
	document := "前面是完整试卷题目\n\n参考答案\n1. A\n2. B"
	sampled := sampleAnalysisDocument(document, 12000)
	if sampled != document {
		t.Fatalf("short document was changed before classification: %q", sampled)
	}
}
