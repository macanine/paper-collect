package service

import (
	"testing"
	"time"

	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

func seedPack(t *testing.T, db *gorm.DB, pack model.Pack, parts ...string) (model.Pack, []model.Material) {
	t.Helper()
	pack.ID = newID()
	if pack.SubjectsJSON == "" {
		pack.SubjectsJSON = "[]"
	}
	pack.TagsJSON = "[]"
	if err := db.Create(&pack).Error; err != nil {
		t.Fatal(err)
	}
	var out []model.Material
	for i, part := range parts {
		m := model.Material{ID: newID(), PackID: pack.ID, Position: i, Part: part, Stage: model.StageReady,
			ReviewReasonsJSON: "[]", SHA256: testSHA(), OriginalName: part + ".pdf", MediaType: "application/pdf"}
		if err := db.Create(&m).Error; err != nil {
			t.Fatal(err)
		}
		out = append(out, m)
	}
	return pack, out
}

func TestPairingSuggestsMatchesForConfirmation(t *testing.T) {
	svc, db := newWorkerTestService(t, t.TempDir(), "pairing")
	now := time.Now().UTC()
	exam := model.Pack{DocType: "joint", Grade: "grade12", Cohort: 2025, Region: "浙江", Organization: "Z20联盟",
		ExamName: "浙江省Z20名校联盟2025届高三第一次联考", SubjectsJSON: `["math"]`}
	target, _ := seedPack(t, db, exam, model.PartQuestions)
	_, lone := seedPack(t, db, exam, model.PartAnswers)

	if err := db.Transaction(func(tx *gorm.DB) error {
		_, _, _, err := svc.pairMaterialTx(tx, lone[0].ID, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var moved model.Material
	if err := db.Where("id = ?", lone[0].ID).First(&moved).Error; err != nil || moved.PackID == target.ID {
		t.Fatalf("answer key must wait for confirmation: %+v %v", moved, err)
	}
	if moved.Stage != model.StageReview || moved.SuggestedPackID != target.ID ||
		!containsString(model.DecodeStrings(moved.ReviewReasonsJSON), model.ReviewPackSuggestion) {
		t.Fatalf("strong match must produce a confirmation suggestion: %+v", moved)
	}

	// Same cohort and grade but no exam name or organization: only a suggestion.
	weak := model.Pack{DocType: "monthly", Grade: "grade11", Cohort: 2026, SubjectsJSON: `["physics"]`}
	seedPack(t, db, weak, model.PartQuestions)
	_, loneWeak := seedPack(t, db, weak, model.PartAnswers)
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, _, _, err := svc.pairMaterialTx(tx, loneWeak[0].ID, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var suggested model.Material
	if err := db.Where("id = ?", loneWeak[0].ID).First(&suggested).Error; err != nil {
		t.Fatal(err)
	}
	if suggested.Stage != model.StageReview || suggested.SuggestedPackID == "" ||
		!containsString(model.DecodeStrings(suggested.ReviewReasonsJSON), model.ReviewPackSuggestion) {
		t.Fatalf("weak match must wait in the inbox with a suggestion: %+v", suggested)
	}

	// A conflicting field rules a candidate out.
	other := exam
	other.Region = "江苏"
	_, conflict := seedPack(t, db, other, model.PartAnswers)
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, _, _, err := svc.pairMaterialTx(tx, conflict[0].ID, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var stayed model.Material
	db.Where("id = ?", conflict[0].ID).First(&stayed)
	if stayed.PackID == target.ID || stayed.SuggestedPackID == target.ID {
		t.Fatal("a different region must never pair")
	}
}
