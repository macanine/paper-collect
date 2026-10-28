package service

import "testing"

func TestParseSuggestionsAcceptsCurrentRolesOnly(t *testing.T) {
	result, _, err := parseSuggestions(`{"title":"测试","role":"teacher_edition","confidence":0.9}`)
	if err != nil || result.Role != "teacher_edition" {
		t.Fatalf("current role: %+v, %v", result, err)
	}
	result, _, err = parseSuggestions(`{"title":"测试","role":"teacher","confidence":0.9}`)
	if err != nil || result.Role != "" {
		t.Fatalf("unknown role should be discarded: %+v, %v", result, err)
	}
}
