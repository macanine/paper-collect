package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nic/paper-collect/backend/internal/model"
)

func TestJobProgressIsLeaseFencedAndRedactsFailure(t *testing.T) {
	svc, conn := newWorkerTestService(t, t.TempDir(), "progress-worker")
	job := model.Job{ID: newID(), Kind: model.JobIngest, Status: model.JobPending, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := conn.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	claimed, err := svc.claimJob(context.Background(), model.JobIngest)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.StartedAt == nil {
		t.Fatal("claimed job should record a start time")
	}
	svc.jobProgressWithMetrics(context.Background(), claimed, "已检查 2 / 3 个上传文件", 2, 3, map[string]any{
		"file_count": 3,
		"size_bytes": int64(1024),
	})
	stale := claimed
	stale.LeaseOwner = "other-worker"
	svc.jobProgress(context.Background(), stale, "不应出现", 3, 3)
	if !svc.failJob(claimed.ID, claimed.LeaseOwner, "访问 https://example.com/secret 失败") {
		t.Fatal("failed to finish job")
	}
	got, err := svc.GetJob(context.Background(), claimed.ID)
	if err != nil {
		t.Fatal(err)
	}
	var logs []model.JobLog
	if err := json.Unmarshal([]byte(got.LogJSON), &logs); err != nil {
		t.Fatal(err)
	}
	if got.ProgressCurrent != 2 || got.ProgressTotal != 3 || len(logs) != 3 || logs[2].Level != "error" {
		t.Fatalf("unexpected progress: %#v %#v", got, logs)
	}
	if got.FinishedAt == nil || got.DurationMS < 0 || logs[1].Metrics["file_count"] != float64(3) {
		t.Fatalf("unexpected timing metrics: %#v", got)
	}
	if strings.Contains(got.LogJSON, "example.com") || strings.Contains(got.LogJSON, "不应出现") {
		t.Fatal("logs leaked a URL or accepted a stale worker")
	}
}

func TestJobProgressHistoryIsBounded(t *testing.T) {
	raw := ""
	for i := 0; i < 250; i++ {
		raw = appendJobLog(raw, model.JobLog{Message: "step"})
	}
	var logs []model.JobLog
	if err := json.Unmarshal([]byte(raw), &logs); err != nil || len(logs) != 200 {
		t.Fatalf("unexpected history length: %d", len(logs))
	}
}
