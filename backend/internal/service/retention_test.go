package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nic/paper-collect/backend/internal/model"
)

func writeAged(t *testing.T, path string, age time.Duration) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Retention removes only old files that no row references, and leaves fresh
// files alone so an import between publish and commit is never broken.
func TestRetentionSweepsOnlyOldUnreferencedFiles(t *testing.T) {
	dir := t.TempDir()
	svc, conn := newWorkerTestService(t, dir, "master")
	known := strings.Repeat("a", 64)
	orphan := strings.Repeat("b", 64)
	fresh := strings.Repeat("c", 64)
	if err := conn.Create(&model.Blob{ID: newID(), SHA256: known, SizeBytes: 1, MediaType: "application/pdf", FileName: "a.pdf", CreatedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	for _, sha := range []string{known, orphan} {
		writeAged(t, svc.blobs.Path(sha), 48*time.Hour)
	}
	writeAged(t, svc.blobs.Path(fresh), time.Hour)
	renders := filepath.Join(dir, "renders", "office-v1")
	writeAged(t, filepath.Join(renders, "aa", known+".pdf"), 48*time.Hour)
	writeAged(t, filepath.Join(renders, "bb", orphan+".pdf"), 48*time.Hour)
	writeAged(t, filepath.Join(dir, "tmp", "stage-1.part"), 48*time.Hour)
	writeAged(t, filepath.Join(dir, "tmp", "stage-2.part"), time.Minute)
	writeAged(t, filepath.Join(dir, "tmp", "upload-archives", "x.zip"), 48*time.Hour)

	report, err := svc.RunRetention(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.OrphanBlobs != 1 || report.OrphanRenders != 1 || report.StaleTemp != 1 {
		t.Fatalf("report = %+v", report)
	}
	for path, want := range map[string]bool{
		svc.blobs.Path(known):                                 true,
		svc.blobs.Path(orphan):                                false,
		svc.blobs.Path(fresh):                                 true,
		filepath.Join(renders, "aa", known+".pdf"):            true,
		filepath.Join(renders, "bb", orphan+".pdf"):           false,
		filepath.Join(dir, "tmp", "stage-1.part"):             false,
		filepath.Join(dir, "tmp", "stage-2.part"):             true,
		filepath.Join(dir, "tmp", "upload-archives", "x.zip"): true,
	} {
		if exists(path) != want {
			t.Errorf("%s exists = %v, want %v", strings.TrimPrefix(path, dir), !want, want)
		}
	}
}

// Old succeeded jobs go unless something still links to them; failed jobs stay.
func TestRetentionPrunesOnlyUnreferencedOldSucceededJobs(t *testing.T) {
	svc, conn := newWorkerTestService(t, t.TempDir(), "master")
	old := time.Now().UTC().Add(-40 * 24 * time.Hour)
	recent := time.Now().UTC().Add(-time.Hour)
	add := func(status string, finished time.Time) model.Job {
		job := model.Job{ID: newID(), Kind: model.JobIndex, Status: status, MaterialID: newID(), FinishedAt: &finished, CreatedAt: finished, UpdatedAt: finished}
		if err := conn.Create(&job).Error; err != nil {
			t.Fatal(err)
		}
		return job
	}
	stale := add(model.JobSucceeded, old)
	referenced := add(model.JobSucceeded, old)
	failed := add(model.JobFailed, old)
	fresh := add(model.JobSucceeded, recent)
	if err := conn.Create(&model.MaterialIndexState{MaterialID: referenced.MaterialID, Status: "succeeded", JobID: referenced.ID, UpdatedAt: old}).Error; err != nil {
		t.Fatal(err)
	}
	report, err := svc.RunRetention(context.Background())
	if err != nil || report.Jobs != 1 {
		t.Fatalf("report = %+v, %v", report, err)
	}
	var left []string
	conn.Model(&model.Job{}).Order("id").Pluck("id", &left)
	for _, job := range []model.Job{referenced, failed, fresh} {
		if !contains(left, job.ID) {
			t.Errorf("job %s (%s) was pruned", job.ID, job.Status)
		}
	}
	if contains(left, stale.ID) {
		t.Error("unreferenced old succeeded job was kept")
	}
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}
