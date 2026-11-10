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
