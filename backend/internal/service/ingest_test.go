package service

import "testing"

func TestSuggestRoleUsesExactlySixRoles(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"高三数学教师版.pdf", "teacher_edition"},
		{"教师讲义.pdf", "lecture"},
		{"函数专题讲义.pdf", "lecture"},
		{"函数专题教案.docx", "lecture"},
		{"高三数学试卷+答案.pdf", "questions_answers"},
		{"参考答案.pdf", "answer"},
		{"高三数学试卷.pdf", "exam"},
		{"无法分类.pdf", "other"},
	}
	for _, tt := range tests {
		if got := suggestRole(tt.name); got != tt.want {
			t.Errorf("suggestRole(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}
