package service

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

func testSHA() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
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

// 高考真题 has one organizer, so recording it only lengthens the title.
func TestGaokaoRealTitleOmitsOrganization(t *testing.T) {
	title := canonicalPackTitle(packFixture("gaokao_real", "grade12", 2024, "新高考I卷", "教育部教育考试院", "2024-06", `["chinese"]`))
	if strings.Contains(title, "教育部") {
		t.Fatalf("gaokao real title carries the organizer: %q", title)
	}
	if title != "2024届 新高考I卷 6月高考真题 高三 语文" {
		t.Fatalf("title = %q", title)
	}
}

// 专题卷、讲义这类资料用模型自己写的名字，考试卷仍按编目字段组合。
func TestPackTitleKeepsTheNameOfADocumentThatIsNotAnExam(t *testing.T) {
	topic := packFixture("topic", "grade12", 0, "", "", "", `["chemistry"]`)
	topic.ExamName = "十年（2012-2021）高考化学真题分项汇编 专题44 水的电离与溶液的pH"
	if got := packTitleFromCatalogue(topic); got != topic.ExamName {
		t.Fatalf("topic title = %q", got)
	}

	exam := packFixture("joint", "grade12", 2025, "浙江", "Z20联盟", "2024-09", `["math"]`)
	exam.ExamName = "2025届浙江Z20名校联盟高三第一次联考"
	if got := packTitleFromCatalogue(exam); got != "2025届 浙江 Z20联盟 9月联考 高三 数学" {
		t.Fatalf("exam title = %q", got)
	}

	// Without a name of its own a 专题 still gets the field-built title rather
	// than "未命名资料".
	nameless := packFixture("topic", "grade12", 0, "", "", "", `["chemistry"]`)
	if got := packTitleFromCatalogue(nameless); got != "专题练习 高三 化学" {
		t.Fatalf("nameless topic title = %q", got)
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
