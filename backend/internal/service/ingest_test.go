package service

import (
	"crypto/rand"
	"encoding/hex"
	"testing"

	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

func testSHA() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func TestSuggestPartFromFileName(t *testing.T) {
	tests := []struct {
		name, mediaType, want string
	}{
		{"高三数学教师版.pdf", "application/pdf", "teacher"},
		{"函数专题讲义.pdf", "application/pdf", "main"},
		{"高三数学试卷（含答案）.pdf", "application/pdf", "full"},
		{"参考答案.pdf", "application/pdf", "answers"},
		{"数学答题卡.pdf", "application/pdf", "answer_sheet"},
		{"高三数学试卷.pdf", "application/pdf", "questions"},
		{"听力.mp3", "audio/mpeg", "listening"},
		{"无法分类.pdf", "application/pdf", "other"},
	}
	for _, tt := range tests {
		if got := suggestPart(tt.name, tt.mediaType); got != tt.want {
			t.Errorf("suggestPart(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestPairingStemMatchesSetMembers(t *testing.T) {
	a := pairingStem("2025浙江9月联考/数学（试题）.pdf")
	b := pairingStem("2025浙江9月联考/数学-参考答案.docx")
	c := pairingStem("2025浙江9月联考/数学答题卡.pdf")
	if a == "" || a != b || b != c {
		t.Fatalf("set members must share a stem: %q %q %q", a, b, c)
	}
	if pairingStem("物理试题.pdf") == a {
		t.Fatal("different subjects must not share a stem")
	}
}

func TestCanonicalPackTitle(t *testing.T) {
	title := canonicalPackTitle(packFixture("joint", "grade12", 2025, "浙江", "Z20联盟", "2024-09", `["math"]`))
	if title != "2025届 浙江 Z20联盟 9月联考 高三 数学" {
		t.Fatalf("title = %q", title)
	}
	if got := canonicalPackTitle(packFixture("", "", 0, "", "", "", `[]`)); got != "" {
		t.Fatalf("empty catalogue must yield empty title, got %q", got)
	}
}

func packFixture(docType, grade string, cohort int, region, org, examDate, subjects string) model.Pack {
	return model.Pack{DocType: docType, Grade: grade, Cohort: cohort, Region: region, Organization: org, ExamDate: examDate, SubjectsJSON: subjects, TagsJSON: "[]"}
}

// testPackID creates an empty pack so search joins find the material.
func testPackID(t *testing.T, db *gorm.DB) string {
	t.Helper()
	pack := model.Pack{ID: newID(), Title: "测试套卷", SubjectsJSON: "[]", TagsJSON: "[]"}
	if err := db.Create(&pack).Error; err != nil {
		t.Fatal(err)
	}
	return pack.ID
}

func TestPackStemTitleKeepsBalancedBrackets(t *testing.T) {
	for name, want := range map[string]string{
		"英语周测（一）.pdf":  "英语周测（一）",
		"数学（答案）.pdf":    "数学",
		"物理-参考答案.docx": "物理",
	} {
		if got := packStemTitle(name); got != want {
			t.Errorf("packStemTitle(%q) = %q, want %q", name, got, want)
		}
	}
}
