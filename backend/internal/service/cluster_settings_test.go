package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/model"
)

func TestClusterSettingsPersistApplyAndReachWorkers(t *testing.T) {
	svc, conn := newWorkerTestService(t, t.TempDir(), "master")
	ctx := context.Background()
	initial, err := svc.EnsureClusterSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if initial.Settings.Revision != 1 || len(initial.Kinds) != 3 {
		t.Fatalf("initial settings = %+v", initial)
	}

	now := time.Now().UTC()
	worker := model.Node{
		ID: "worker", Name: "家里 NAS", TokenHash: "worker-token", Enabled: true,
		KindsJSON: "[]", SlotsJSON: "{}",
		ConfiguredSlotsJSON: `{"replicate":2}`,
		ConfiguredCacheMax:  8 << 30,
		OCRProviderIDsJSON:  `["paddle","mineru"]`,
		ConfigRevision:      1, CreatedAt: now, UpdatedAt: now,
	}
	if err := conn.Create(&worker).Error; err != nil {
		t.Fatal(err)
	}
	masterNode := model.Node{
		ID: "master", Name: "主端", TokenHash: "master-token", IsMaster: true, Enabled: true,
		KindsJSON: "[]", SlotsJSON: "{}", ConfiguredSlotsJSON: "{}",
		OCRProviderIDsJSON: "[]", ConfigRevision: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := conn.Create(&masterNode).Error; err != nil {
		t.Fatal(err)
	}

	kinds := []string{model.JobAnalyze, model.JobReplicate, model.JobAnalyze}
	ingest, replicate, analyze, index := 3, 4, 5, 6
	lease, heartbeat, claimWait, poll := 180, 15, 10, 7
	updated, err := svc.UpdateClusterSettings(ctx, auth.Principal{UserID: "admin"}, ClusterSettingsPatch{
		MasterKinds:                &kinds,
		MasterIngestConcurrency:    &ingest,
		MasterReplicateConcurrency: &replicate,
		MasterAnalyzeConcurrency:   &analyze,
		MasterIndexConcurrency:     &index,
		LeaseSeconds:               &lease,
		HeartbeatSeconds:           &heartbeat,
		ClaimWaitSeconds:           &claimWait,
		PollSeconds:                &poll,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Settings.Revision != 2 ||
		len(updated.Kinds) != 2 || updated.Kinds[0] != model.JobReplicate || updated.Kinds[1] != model.JobAnalyze {
		t.Fatalf("updated settings = %+v", updated)
	}
	gotWorker := svc.workerSettings()
	if gotWorker.IngestConcurrency != ingest || gotWorker.IndexConcurrency != index ||
		gotWorker.LeaseDuration != 3*time.Minute || gotWorker.HeartbeatInterval != 15*time.Second ||
		gotWorker.ClaimWait != 10*time.Second || gotWorker.PollInterval != 7*time.Second {
		t.Fatalf("runtime settings = %+v", gotWorker)
	}
	if err := conn.First(&worker, "id = ?", worker.ID).Error; err != nil {
		t.Fatal(err)
	}
	if worker.ConfigRevision != 2 {
		t.Fatalf("worker revision = %d, want 2", worker.ConfigRevision)
	}
	if err := conn.First(&masterNode, "id = ?", masterNode.ID).Error; err != nil {
		t.Fatal(err)
	}
	masterSlots := decodeSlots(masterNode.SlotsJSON)
	if len(masterSlots) != 2 || masterSlots[model.JobReplicate] != replicate || masterSlots[model.JobAnalyze] != analyze {
		t.Fatalf("master slots = %v, want only configured remote kinds", masterSlots)
	}

	reply, err := svc.NodeHeartbeat(ctx, worker, Heartbeat{
		Version: "test", Protocol: 1, Revision: 2, CacheUsed: 1024,
	}, "192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	if reply.Config == nil || reply.Config.Revision != 2 ||
		reply.Config.HeartbeatMS != 15_000 || reply.Config.ClaimWaitMS != 10_000 ||
		reply.Config.CacheMaxBytes != 8<<30 || reply.Config.Slots[model.JobReplicate] != 2 ||
		len(reply.Config.OCRProviderIDs) != 2 {
		t.Fatalf("heartbeat config = %+v", reply.Config)
	}
	if err := conn.First(&worker, "id = ?", worker.ID).Error; err != nil {
		t.Fatal(err)
	}
	if worker.AppliedConfigRevision != 2 || worker.LastIP != "192.0.2.10" {
		t.Fatalf("heartbeat state = %+v", worker)
	}
}

func TestClusterSettingsRejectInvalidValuesWithoutChangingState(t *testing.T) {
	svc, conn := newWorkerTestService(t, t.TempDir(), "master")
	ctx := context.Background()
	initial, err := svc.EnsureClusterSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	badKinds := []string{model.JobIngest}
	lease, heartbeat := 60, 60
	for label, patch := range map[string]ClusterSettingsPatch{
		"unsupported master kind": {MasterKinds: &badKinds},
		"heartbeat equals lease":  {LeaseSeconds: &lease, HeartbeatSeconds: &heartbeat},
	} {
		t.Run(label, func(t *testing.T) {
			if _, err := svc.UpdateClusterSettings(ctx, auth.Principal{UserID: "admin"}, patch); err == nil {
				t.Fatal("invalid settings were accepted")
			} else {
				var target *apperr.Error
				if !errors.As(err, &target) || target.Status != 400 {
					t.Fatalf("error = %v, want validation error", err)
				}
			}
		})
	}
	var row model.ClusterSettings
	if err := conn.Where("id = ?", clusterSettingsID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Revision != initial.Settings.Revision {
		t.Fatalf("revision changed after rejected patches: %d -> %d", initial.Settings.Revision, row.Revision)
	}
}

func TestNodeOCRProviderUsesGlobalPriority(t *testing.T) {
	svc, conn := newWorkerTestService(t, t.TempDir(), "master")
	now := time.Now().UTC()
	for _, provider := range []model.OCRProvider{
		{ID: "slow", Name: "MinerU", Kind: model.OCRKindMinerU, BaseURL: "https://mineru.example.com", Enabled: true, Priority: 20, CreatedAt: now, UpdatedAt: now},
		{ID: "fast", Name: "PaddleOCR", Kind: model.OCRKindPaddleOCR, BaseURL: "https://ocr.example.com", Enabled: true, Priority: 10, CreatedAt: now.Add(time.Second), UpdatedAt: now},
	} {
		if err := conn.Create(&provider).Error; err != nil {
			t.Fatal(err)
		}
	}
	row, available, err := svc.nodeOCRProvider(context.Background(), model.AISettings{}, []string{"slow", "fast"})
	if err != nil {
		t.Fatal(err)
	}
	if !available || row.ID != "fast" {
		t.Fatalf("provider = %q, available=%v, want global-priority provider fast", row.ID, available)
	}
}
