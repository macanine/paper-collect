package service

import (
	"strings"
	"testing"
)

func TestParseSuggestionsKeepsOnlyWhitelistedValues(t *testing.T) {
	result, confidence, err := parseSuggestions(`{"exam_name":"浙江省Z20联盟2025届高三第一次联考","doc_type":"joint","subject_codes":["math"],
		"grade":"grade12","term":"first","cohort":2025,"region":"浙江","organization":"Z20联盟","exam_date":"2024-09",
		"part":"full","answer_start_page":5,"uncertain":["organization","bogus"],"confidence":0.92}`)
	if err != nil {
		t.Fatal(err)
	}
	if result.DocType != "joint" || result.Grade != "grade12" || result.Cohort != 2025 || result.Part != "full" ||
		result.AnswerStartPage != 5 || result.ExamDate != "2024-09" || confidence != 0.92 {
		t.Fatalf("valid fields lost: %+v", result)
	}
	if len(result.Uncertain) != 1 || result.Uncertain[0] != "organization" {
		t.Fatalf("uncertain must keep known fields only: %v", result.Uncertain)
	}
	result, _, err = parseSuggestions(`{"doc_type":"novel","grade":"grade13","part":"teacher_edition","exam_date":"2024-13","cohort":12,"answer_start_page":3}`)
	if err != nil {
		t.Fatal(err)
	}
	if result.DocType != "" || result.Grade != "" || result.Part != "" || result.ExamDate != "" || result.Cohort != 0 || result.AnswerStartPage != 0 {
		t.Fatalf("invalid values must be dropped: %+v", result)
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

func TestAnalysisMessagesRequireDocumentAndTreatMetadataAsAuxiliary(t *testing.T) {
	messages := buildAnalysisMessages("正文第一页\n正文最后一页答案", "文件名：高三数学试卷.pdf", nil)
	if len(messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(messages))
	}
	system, _ := messages[0].Content.(string)
	user, _ := messages[1].Content.(string)
	if !strings.Contains(system, "正文与它们冲突时以正文为准") || !strings.Contains(system, "末尾是否有集中答案区") {
		t.Fatalf("prompt does not put document content first: %s", system)
	}
	if !strings.Contains(user, "正文第一页") || !strings.Contains(user, "高三数学试卷.pdf") {
		t.Fatalf("user prompt misses document or metadata: %s", user)
	}
}

func TestAnalysisMessagesKeepCompleteShortDocument(t *testing.T) {
	document := "前面是完整试卷题目\n\n参考答案\n1. A\n2. B"
	sampled := sampleAnalysisDocument(document, 12000)
	if sampled != document {
		t.Fatalf("short document was changed before classification: %q", sampled)
	}
}
