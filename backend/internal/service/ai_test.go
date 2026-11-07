package service

import (
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
