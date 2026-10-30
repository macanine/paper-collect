package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

func TestNormalizeGroupTitleRemovesRoleWords(t *testing.T) {
	left := normalizeGroupTitle("2026届高三数学第一次月考（参考答案）.pdf")
	right := normalizeGroupTitle("2026届高三数学第一次月考 教师版.docx")
	if left != "2026届高三数学第一次月考" || right != "2026届高三数学第一次月考" {
		t.Fatalf("unexpected normalized titles: %q, %q", left, right)
	}
}

func TestNormalizeGroupTitleKeepsDecimalSection(t *testing.T) {
	if got := normalizeGroupTitle("3.1 函数与导数 试卷.pdf"); got != "31函数与导数" {
		t.Fatalf("normalized title = %q, want %q", got, "31函数与导数")
	}
	if got := normalizeGroupTitle("Math.Unit 试卷.pdf"); got != "mathunit" {
		t.Fatalf("normalized title = %q, want %q", got, "mathunit")
	}
}

func TestScoreExamCollectionRejectsConflicts(t *testing.T) {
	material := model.Material{
		Title:        "2026届高三数学第一次月考",
		FileName:     "2026届高三数学第一次月考.pdf",
		Role:         model.RoleExam,
		Grade:        "grade12",
		AcademicYear: 2026,
		SHA256:       strings.Repeat("1", 64),
	}
	member := model.Material{
		Title:        "2026届高三数学第一次月考答案",
		Role:         model.RoleAnswer,
		Grade:        "grade12",
		AcademicYear: 2026,
		SHA256:       strings.Repeat("2", 64),
	}
	collection := model.Collection{
		Title:        material.Title,
		Kind:         model.KindSeries,
		SubjectsJSON: model.EncodeStrings([]string{"math"}),
	}
	if score, _, ok := scoreExamCollection(
		material, []string{"math"}, collection, []model.Material{member}, nil,
	); !ok || score < groupAutoScore {
		t.Fatalf("compatible exam was rejected: score=%d ok=%v", score, ok)
	}

	tests := []struct {
		name     string
		subjects []string
		mutate   func(*model.Material, *model.Collection, *model.Material)
	}{
		{
			name: "subject", subjects: []string{"english"},
			mutate: func(*model.Material, *model.Collection, *model.Material) {},
		},
		{
			name: "grade", subjects: []string{"math"},
			mutate: func(material *model.Material, _ *model.Collection, _ *model.Material) {
				material.Grade = "grade11"
			},
		},
		{
			name: "academic year", subjects: []string{"math"},
			mutate: func(material *model.Material, _ *model.Collection, _ *model.Material) {
				material.AcademicYear = 2023
			},
		},
		{
			name: "same content", subjects: []string{"math"},
			mutate: func(_ *model.Material, _ *model.Collection, member *model.Material) {
				member.SHA256 = material.SHA256
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotMaterial, gotCollection, gotMember := material, collection, member
			test.mutate(&gotMaterial, &gotCollection, &gotMember)
			if score, _, ok := scoreExamCollection(
				gotMaterial, test.subjects, gotCollection, []model.Material{gotMember}, nil,
			); ok {
				t.Fatalf("conflicting exam was accepted: score=%d", score)
			}
		})
	}
}

func TestGroupAssignsSingleExamToUniqueCollection(t *testing.T) {
	svc, db := newWorkerTestService(t, t.TempDir(), "group-success")
	exam := createGroupMaterial(t, db, "2026届高三数学第一次月考", "exam", "math", "grade12", 2026, strings.Repeat("a", 64))
	answer := createGroupMaterial(t, db, "2026届高三数学第一次月考答案", "answer", "math", "grade12", 2026, strings.Repeat("b", 64))
	collection := createGroupCollection(t, db, "2026届高三数学第一次月考", model.KindSeries, "math", answer, 0)
	upload := createSingleFileUpload(t, db, exam)
	groupJob := createGroupJob(t, db, exam, upload)

	if got := svc.ProcessAvailable(context.Background(), 10); got != 1 {
		t.Fatalf("processed jobs = %d, want 1", got)
	}

	assertJobStatus(t, db, groupJob.ID, model.JobSucceeded)
	var items []model.CollectionItem
	if err := db.Where("collection_id = ?", collection.ID).Order("position ASC").Find(&items).Error; err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[1].MaterialID != exam.ID || items[1].Role != model.RoleExam || items[1].Position != 1 {
		t.Fatalf("exam was not appended to collection: %+v", items)
	}
	var gotUpload model.Upload
	if err := db.Where("id = ?", upload.ID).First(&gotUpload).Error; err != nil {
		t.Fatal(err)
	}
	if gotUpload.CollectionID != collection.ID {
		t.Fatalf("upload collection_id = %q, want %q", gotUpload.CollectionID, collection.ID)
	}
	var auditCount int64
	if err := db.Model(&model.Audit{}).
		Where("action = ? AND target_id = ?", "collection.auto_assign", collection.ID).
		Count(&auditCount).Error; err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("auto assignment audits = %d, want 1", auditCount)
	}
	var job model.Job
	if err := db.Where("id = ?", groupJob.ID).First(&job).Error; err != nil {
		t.Fatal(err)
	}
	if job.Error != "" {
		t.Fatalf("succeeded job error = %q, want empty", job.Error)
	}
	if !strings.Contains(job.ProgressMessage, "已将试卷归入合集") {
		t.Fatalf("unexpected progress message %q", job.ProgressMessage)
	}
}

func TestGroupDoesNotAssignWhenCandidatesAreAmbiguous(t *testing.T) {
	svc, db := newWorkerTestService(t, t.TempDir(), "group-ambiguous")
	exam := createGroupMaterial(t, db, "2026届高三数学第一次月考", "exam", "math", "grade12", 2026, strings.Repeat("c", 64))
	answer1 := createGroupMaterial(t, db, "第一次月考答案", "answer", "math", "grade12", 2026, strings.Repeat("d", 64))
	answer2 := createGroupMaterial(t, db, "第一次月考答案", "answer", "math", "grade12", 2026, strings.Repeat("e", 64))
	first := createGroupCollection(t, db, "2026届高三数学第一次月考", model.KindSeries, "math", answer1, 0)
	second := createGroupCollection(t, db, "2026届高三数学第一次月考", model.KindSeries, "math", answer2, 0)
	upload := createSingleFileUpload(t, db, exam)
	groupJob := createGroupJob(t, db, exam, upload)

	svc.ProcessAvailable(context.Background(), 10)

	assertJobStatus(t, db, groupJob.ID, model.JobSucceeded)
	var count int64
	if err := db.Model(&model.CollectionItem{}).Where("material_id = ?", exam.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("ambiguous exam was assigned %d times", count)
	}
	var job model.Job
	if err := db.Where("id = ?", groupJob.ID).First(&job).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(job.ProgressMessage, "未自动归类") {
		t.Fatalf("unexpected progress message %q", job.ProgressMessage)
	}
	for _, collectionID := range []string{first.ID, second.ID} {
		if count := collectionItemCount(t, db, collectionID); count != 1 {
			t.Fatalf("collection %s items = %d, want 1", collectionID, count)
		}
	}
}

func TestGroupIgnoresAnswerAndMultiFileUpload(t *testing.T) {
	svc, db := newWorkerTestService(t, t.TempDir(), "group-skips")
	answer := createGroupMaterial(t, db, "2026届高三数学第一次月考答案", "answer", "math", "grade12", 2026, strings.Repeat("f", 64))
	answerUpload := createSingleFileUpload(t, db, answer)
	answerJob := createGroupJob(t, db, answer, answerUpload)
	svc.ProcessAvailable(context.Background(), 10)
	assertJobStatus(t, db, answerJob.ID, model.JobSucceeded)

	exam := createGroupMaterial(t, db, "2026届高三数学第二次月考", "exam", "math", "grade12", 2026, strings.Repeat("1", 64))
	answer2 := createGroupMaterial(t, db, "2026届高三数学第二次月考答案", "answer", "math", "grade12", 2026, strings.Repeat("2", 64))
	collection := createGroupCollection(t, db, "2026届高三数学第二次月考", model.KindSeries, "math", answer2, 0)
	upload := createMultiFileUpload(t, db, exam)
	examJob := createGroupJob(t, db, exam, upload)
	svc.ProcessAvailable(context.Background(), 10)
	assertJobStatus(t, db, examJob.ID, model.JobSucceeded)

	if count := collectionItemCount(t, db, collection.ID); count != 1 {
		t.Fatalf("multi-file upload was assigned: items=%d", count)
	}
}

func TestGroupIgnoresUnsupportedCollectionKinds(t *testing.T) {
	svc, db := newWorkerTestService(t, t.TempDir(), "group-kinds")
	exam := createGroupMaterial(t, db, "2026届高三数学第四次月考", "exam", "math", "grade12", 2026, strings.Repeat("4", 64))
	answer := createGroupMaterial(t, db, "2026届高三数学第四次月考答案", "answer", "math", "grade12", 2026, strings.Repeat("5", 64))
	custom := createGroupCollection(t, db, exam.Title, model.KindCustom, "math", answer, 0)
	zip := createGroupCollection(t, db, exam.Title, model.KindZip, "math", answer, 0)
	upload := createSingleFileUpload(t, db, exam)
	groupJob := createGroupJob(t, db, exam, upload)

	svc.ProcessAvailable(context.Background(), 10)

	assertJobStatus(t, db, groupJob.ID, model.JobSucceeded)
	for _, collection := range []model.Collection{custom, zip} {
		if count := collectionItemCount(t, db, collection.ID); count != 1 {
			t.Fatalf("unsupported collection %s items = %d, want 1", collection.Kind, count)
		}
	}
}

func TestAutoEnqueueExamAssignmentIsIdempotent(t *testing.T) {
	svc, db := newWorkerTestService(t, t.TempDir(), "group-enqueue")
	exam := createGroupMaterial(t, db, "2026届高三数学第三次月考", "exam", "math", "grade12", 2026, strings.Repeat("3", 64))
	createSingleFileUpload(t, db, exam)
	for i := 0; i < 2; i++ {
		if err := db.Transaction(func(tx *gorm.DB) error {
			return svc.autoEnqueueExamAssignmentTx(tx, exam.ID)
		}); err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	if err := db.Model(&model.Job{}).Where("material_id = ? AND kind = ?", exam.ID, model.JobGroup).
		Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("group jobs = %d, want 1", count)
	}
}

func TestAutoEnqueueRequiresStandaloneExam(t *testing.T) {
	svc, db := newWorkerTestService(t, t.TempDir(), "group-gates")
	enqueue := func(materialID string) {
		t.Helper()
		if err := db.Transaction(func(tx *gorm.DB) error {
			return svc.autoEnqueueExamAssignmentTx(tx, materialID)
		}); err != nil {
			t.Fatal(err)
		}
	}
	jobCount := func(materialID string) int64 {
		t.Helper()
		var count int64
		if err := db.Model(&model.Job{}).
			Where("material_id = ? AND kind = ?", materialID, model.JobGroup).
			Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		return count
	}

	answer := createGroupMaterial(t, db, "2026届高三数学第六次月考答案", "answer", "math", "grade12", 2026, strings.Repeat("7", 64))
	createSingleFileUpload(t, db, answer)
	enqueue(answer.ID)
	if count := jobCount(answer.ID); count != 0 {
		t.Fatalf("answer enqueued %d group jobs", count)
	}

	multi := createGroupMaterial(t, db, "2026届高三数学第七次月考", "exam", "math", "grade12", 2026, strings.Repeat("8", 64))
	createMultiFileUpload(t, db, multi)
	enqueue(multi.ID)
	if count := jobCount(multi.ID); count != 0 {
		t.Fatalf("multi-file exam enqueued %d group jobs", count)
	}

	inCollection := createGroupMaterial(t, db, "2026届高三数学第八次月考", "exam", "math", "grade12", 2026, strings.Repeat("9", 64))
	createSingleFileUpload(t, db, inCollection)
	createGroupCollection(t, db, "已有合集", model.KindSeries, "math", inCollection, 0)
	enqueue(inCollection.ID)
	if count := jobCount(inCollection.ID); count != 0 {
		t.Fatalf("collected exam enqueued %d group jobs", count)
	}

	zipExam := createGroupMaterial(t, db, "2026届高三数学第九次月考", "exam", "math", "grade12", 2026, strings.Repeat("a", 64))
	zipUpload := createUploadWithEntries(t, db, zipExam, []model.UploadEntry{{
		RelativePath: zipExam.FileName + ".zip", MediaType: "application/zip",
	}})
	if zipUpload.ID == "" {
		t.Fatal("ZIP upload fixture was not created")
	}
	enqueue(zipExam.ID)
	if count := jobCount(zipExam.ID); count != 0 {
		t.Fatalf("ZIP upload enqueued %d group jobs", count)
	}

	folderExam := createGroupMaterial(t, db, "2026届高三数学第九次月考", "exam", "math", "grade12", 2026, strings.Repeat("c", 64))
	folderUpload := createUploadWithEntries(t, db, folderExam, []model.UploadEntry{{
		RelativePath: "试卷/" + folderExam.FileName, MediaType: folderExam.MediaType,
	}})
	if folderUpload.ID == "" {
		t.Fatal("folder upload fixture was not created")
	}
	enqueue(folderExam.ID)
	if count := jobCount(folderExam.ID); count != 0 {
		t.Fatalf("single-entry folder upload enqueued %d group jobs", count)
	}

	standalone := createGroupMaterial(t, db, "2026届高三数学第十次月考", "exam", "math", "grade12", 2026, strings.Repeat("b", 64))
	standaloneUpload := createSingleFileUpload(t, db, standalone)
	enqueue(standalone.ID)
	var job model.Job
	if err := db.Where("material_id = ? AND kind = ?", standalone.ID, model.JobGroup).First(&job).Error; err != nil {
		t.Fatal(err)
	}
	if job.Status != model.JobPending || job.UploadID != standaloneUpload.ID {
		t.Fatalf("unexpected queued job: status=%q upload=%q", job.Status, job.UploadID)
	}
}

func TestAutoEnqueueRequeuesFinishedGroupJob(t *testing.T) {
	svc, db := newWorkerTestService(t, t.TempDir(), "group-requeue")
	exam := createGroupMaterial(t, db, "2026届高三数学第五次月考", "exam", "math", "grade12", 2026, strings.Repeat("6", 64))
	upload := createSingleFileUpload(t, db, exam)
	if err := db.Transaction(func(tx *gorm.DB) error {
		return svc.autoEnqueueExamAssignmentTx(tx, exam.ID)
	}); err != nil {
		t.Fatal(err)
	}
	var job model.Job
	if err := db.Where("material_id = ? AND kind = ?", exam.ID, model.JobGroup).First(&job).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Job{}).Where("id = ?", job.ID).Updates(map[string]any{
		"status": model.JobSucceeded, "error": "", "progress_message": "没有找到匹配的系列或试卷合集",
		"upload_id": upload.ID, "finished_at": time.Now().UTC(),
	}).Error; err != nil {
		t.Fatal(err)
	}

	if err := db.Transaction(func(tx *gorm.DB) error {
		return svc.autoEnqueueExamAssignmentTx(tx, exam.ID)
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("id = ?", job.ID).First(&job).Error; err != nil {
		t.Fatal(err)
	}
	if job.Status != model.JobPending || job.ProgressMessage != "重新排队归类" || job.FinishedAt != nil {
		t.Fatalf("job was not queued again: status=%q progress=%q finished=%v", job.Status, job.ProgressMessage, job.FinishedAt)
	}
	var count int64
	if err := db.Model(&model.Job{}).Where("material_id = ? AND kind = ?", exam.ID, model.JobGroup).
		Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("group jobs = %d, want 1", count)
	}
}

func createGroupMaterial(
	t *testing.T,
	db *gorm.DB,
	title, role, subject, grade string,
	year int,
	sha string,
) model.Material {
	t.Helper()
	now := time.Now().UTC()
	material := model.Material{
		ID: newID(), Title: title, Description: "", Grade: grade, AcademicYear: year,
		TagsJSON: "[]", Role: role, Status: model.StatusArchived, BlobID: newID(),
		FileName: title + ".pdf", MediaType: "application/pdf", SizeBytes: 10,
		SHA256: sha, SourcePath: title + ".pdf", CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&material).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.MaterialSubject{MaterialID: material.ID, Code: subject}).Error; err != nil {
		t.Fatal(err)
	}
	return material
}

func createGroupCollection(
	t *testing.T,
	db *gorm.DB,
	title, kind, subject string,
	member model.Material,
	position int,
) model.Collection {
	t.Helper()
	now := time.Now().UTC()
	collection := model.Collection{
		ID: newID(), Title: title, Description: "", Kind: kind,
		SubjectsJSON: model.EncodeStrings([]string{subject}), CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&collection).Error; err != nil {
		t.Fatal(err)
	}
	item := model.CollectionItem{
		ID: newID(), CollectionID: collection.ID, MaterialID: member.ID,
		Position: position, Role: member.Role, RelativePath: member.FileName, CreatedAt: now,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	return collection
}

func createSingleFileUpload(t *testing.T, db *gorm.DB, material model.Material) model.Upload {
	t.Helper()
	return createUploadWithEntries(t, db, material, []model.UploadEntry{{
		RelativePath: material.FileName, MediaType: material.MediaType,
	}})
}

func createMultiFileUpload(t *testing.T, db *gorm.DB, material model.Material) model.Upload {
	t.Helper()
	return createUploadWithEntries(t, db, material, []model.UploadEntry{
		{RelativePath: material.FileName, MediaType: material.MediaType},
		{RelativePath: "另一份答案.pdf", MediaType: "application/pdf"},
	})
}

func createUploadWithEntries(t *testing.T, db *gorm.DB, material model.Material, defs []model.UploadEntry) model.Upload {
	t.Helper()
	now := time.Now().UTC()
	upload := model.Upload{
		ID: newID(), UserID: "user", Title: material.Title, CollectionKind: model.KindCustom,
		SubjectsJSON: "[]", Status: model.UploadCompleted, MaterialID: material.ID,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&upload).Error; err != nil {
		t.Fatal(err)
	}
	for i := range defs {
		defs[i].ID = newID()
		defs[i].UploadID = upload.ID
		defs[i].Position = i
		defs[i].SizeBytes = material.SizeBytes
		defs[i].SHA256 = material.SHA256
		defs[i].Status = model.EntryReceived
		defs[i].CreatedAt = now
		defs[i].UpdatedAt = now
	}
	if err := db.Create(&defs).Error; err != nil {
		t.Fatal(err)
	}
	return upload
}

func createGroupJob(t *testing.T, db *gorm.DB, material model.Material, upload model.Upload) model.Job {
	t.Helper()
	now := time.Now().UTC()
	job := model.Job{
		ID: newID(), Kind: model.JobGroup, Status: model.JobPending,
		UploadID: upload.ID, MaterialID: material.ID, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	return job
}

func assertJobStatus(t *testing.T, db *gorm.DB, jobID, status string) {
	t.Helper()
	var job model.Job
	if err := db.Where("id = ?", jobID).First(&job).Error; err != nil {
		t.Fatal(err)
	}
	if job.Status != status {
		t.Fatalf("job status = %q, want %q; error=%q", job.Status, status, job.Error)
	}
}

func collectionItemCount(t *testing.T, db *gorm.DB, collectionID string) int64 {
	t.Helper()
	var count int64
	if err := db.Model(&model.CollectionItem{}).Where("collection_id = ?", collectionID).
		Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}
