package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/jobexec"
	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

func addReplicateJob(t *testing.T, conn *gorm.DB, driveKind string) model.Job {
	t.Helper()
	now := time.Now().UTC()
	drive := model.Drive{ID: newID(), Name: driveKind + newID(), Kind: driveKind, Enabled: true, AllowedExtsJSON: "[]", OnPathChange: model.OnPathChangeMove, ConfigJSON: "{}", CreatedAt: now, UpdatedAt: now}
	if err := conn.Create(&drive).Error; err != nil {
		t.Fatal(err)
	}
	replica := model.Replica{ID: newID(), BlobID: newID(), DriveID: drive.ID, Status: model.ReplicaPending, CreatedAt: now, UpdatedAt: now}
	if err := conn.Create(&replica).Error; err != nil {
		t.Fatal(err)
	}
	job := model.Job{ID: newID(), Kind: model.JobReplicate, Status: model.JobPending, ReplicaID: replica.ID, CreatedAt: now, UpdatedAt: now}
	if err := conn.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	return job
}

func addWorkerNode(t *testing.T, conn *gorm.DB, draining bool) model.Node {
	t.Helper()
	now := time.Now().UTC()
	n := model.Node{ID: newID(), Name: "从端", TokenHash: newID(), Enabled: true, Draining: draining, KindsJSON: "[]", SlotsJSON: "{}", CreatedAt: now, UpdatedAt: now}
	if err := conn.Create(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func addLiveNode(t *testing.T, conn *gorm.DB, id, name string, master bool) model.Node {
	t.Helper()
	now := time.Now().UTC()
	n := model.Node{ID: id, Name: name, IsMaster: master, TokenHash: newID(), Enabled: true, KindsJSON: "[\"replicate\"]", SlotsJSON: "{\"replicate\":1}", LastSeenAt: &now, CreatedAt: now, UpdatedAt: now}
	if err := conn.Create(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func addRoutedReplicateJob(t *testing.T, conn *gorm.DB, driveID string) model.Job {
	t.Helper()
	now := time.Now().UTC()
	replica := model.Replica{ID: newID(), BlobID: newID(), DriveID: driveID, Status: model.ReplicaPending, CreatedAt: now, UpdatedAt: now}
	if err := conn.Create(&replica).Error; err != nil {
		t.Fatal(err)
	}
	job := model.Job{ID: newID(), Kind: model.JobReplicate, Status: model.JobPending, ReplicaID: replica.ID, CreatedAt: now, UpdatedAt: now}
	if err := conn.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	return job
}

// A local drive's root is inside the master's DATA_DIR. A worker claiming it
// would write to its own disk and the master would record a replica it lacks.
func TestWorkerCannotClaimLocalDriveReplica(t *testing.T) {
	svc, conn := newWorkerTestService(t, t.TempDir(), "master")
	local := addReplicateJob(t, conn, model.KindLocal)
	remote := addReplicateJob(t, conn, model.KindWebDAV)
	node := claimer{ID: "node", Name: "从端", RemoteOnly: true}

	job, err := svc.claimJobFor(context.Background(), node, []string{model.JobReplicate})
	if err != nil || job.ID != remote.ID {
		t.Fatalf("node claimed %q (%v), want the remote drive job", job.ID, err)
	}
	if _, err := svc.claimJobFor(context.Background(), node, []string{model.JobReplicate}); !errors.Is(err, errNoJob) {
		t.Fatalf("node must not claim the local drive job: %v", err)
	}
	job, err = svc.claimJob(context.Background(), model.JobReplicate)
	if err != nil || job.ID != local.ID {
		t.Fatalf("master claimed %q (%v), want the local drive job", job.ID, err)
	}
}

func TestDriveRoutePriorityAndFailover(t *testing.T) {
	svc, conn := newWorkerTestService(t, t.TempDir(), "master")
	primary := addLiveNode(t, conn, "primary", "主上传从端", false)
	backup := addLiveNode(t, conn, "backup", "备用上传从端", false)
	drive := model.Drive{ID: newID(), Name: "路由网盘", Kind: model.KindWebDAV, Enabled: true, RouteMode: model.DriveRouteExclusive, AllowedExtsJSON: "[]", OnPathChange: model.OnPathChangeMove, ConfigJSON: "{}", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := conn.Create(&drive).Error; err != nil {
		t.Fatal(err)
	}
	if err := conn.Create(&model.DriveNodeRoute{ID: newID(), DriveID: drive.ID, NodeID: primary.ID, Priority: 1, Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := conn.Create(&model.DriveNodeRoute{ID: newID(), DriveID: drive.ID, NodeID: backup.ID, Priority: 2, Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	first := addRoutedReplicateJob(t, conn, drive.ID)
	claimed, err := svc.claimJobFor(context.Background(), claimer{ID: primary.ID, Name: primary.Name, RemoteOnly: true}, []string{model.JobReplicate})
	if err != nil || claimed.ID != first.ID {
		t.Fatalf("primary claim = %q (%v), want %q", claimed.ID, err, first.ID)
	}
	if _, err := svc.claimJobFor(context.Background(), claimer{ID: backup.ID, Name: backup.Name, RemoteOnly: true}, []string{model.JobReplicate}); !errors.Is(err, errNoJob) {
		t.Fatalf("backup claimed while primary was available: %v", err)
	}

	if err := conn.Model(&model.Node{}).Where("id = ?", primary.ID).Updates(map[string]any{"enabled": false}).Error; err != nil {
		t.Fatal(err)
	}
	second := addRoutedReplicateJob(t, conn, drive.ID)
	claimed, err = svc.claimJobFor(context.Background(), claimer{ID: backup.ID, Name: backup.Name, RemoteOnly: true}, []string{model.JobReplicate})
	if err != nil || claimed.ID != second.ID {
		t.Fatalf("backup failover claim = %q (%v), want %q", claimed.ID, err, second.ID)
	}
}

func TestPreferredDriveRouteFallsBackToPool(t *testing.T) {
	svc, conn := newWorkerTestService(t, t.TempDir(), "master")
	preferred := addLiveNode(t, conn, "preferred", "首选从端", false)
	other := addLiveNode(t, conn, "other", "兜底从端", false)
	drive := model.Drive{ID: newID(), Name: "首选网盘", Kind: model.KindWebDAV, Enabled: true, RouteMode: model.DriveRoutePreferred, AllowedExtsJSON: "[]", OnPathChange: model.OnPathChangeMove, ConfigJSON: "{}", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := conn.Create(&drive).Error; err != nil {
		t.Fatal(err)
	}
	if err := conn.Create(&model.DriveNodeRoute{ID: newID(), DriveID: drive.ID, NodeID: preferred.ID, Priority: 1, Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := conn.Model(&model.Node{}).Where("id = ?", preferred.ID).Updates(map[string]any{"enabled": false}).Error; err != nil {
		t.Fatal(err)
	}
	job := addRoutedReplicateJob(t, conn, drive.ID)
	claimed, err := svc.claimJobFor(context.Background(), claimer{ID: other.ID, Name: other.Name, RemoteOnly: true}, []string{model.JobReplicate})
	if err != nil || claimed.ID != job.ID {
		t.Fatalf("preferred fallback claim = %q (%v), want %q", claimed.ID, err, job.ID)
	}
}

func TestMasterCanClaimExplicitRouteWhenRemotePoolIsDisabled(t *testing.T) {
	svc, conn := newWorkerTestService(t, t.TempDir(), "master")
	svc.worker.Kinds = []string{}
	master := addLiveNode(t, conn, "master", "主端", true)
	svc.worker.InstanceID, svc.worker.NodeName = master.ID, master.Name
	drive := model.Drive{ID: newID(), Name: "主端网盘", Kind: model.KindWebDAV, Enabled: true, RouteMode: model.DriveRouteExclusive, AllowedExtsJSON: "[]", OnPathChange: model.OnPathChangeMove, ConfigJSON: "{}", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := conn.Create(&drive).Error; err != nil {
		t.Fatal(err)
	}
	if err := conn.Create(&model.DriveNodeRoute{ID: newID(), DriveID: drive.ID, NodeID: master.ID, Priority: 1, Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	job := addRoutedReplicateJob(t, conn, drive.ID)
	claimed, err := svc.claimJob(context.Background(), model.JobReplicate)
	if err != nil || claimed.ID != job.ID {
		t.Fatalf("explicit master route claim = %q (%v), want %q", claimed.ID, err, job.ID)
	}
}

// Leaving replicate to nodes, or draining the master, must not strand jobs that
// only the master can run, and must stop everything else.
func TestMasterKeepsOnlyMasterOnlyJobsWhenNotClaiming(t *testing.T) {
	cases := map[string]func(*Service, *gorm.DB){
		"left to nodes": func(s *Service, _ *gorm.DB) { s.worker.Kinds = []string{} },
		"draining": func(s *Service, conn *gorm.DB) {
			if err := conn.Create(&model.Node{ID: newID(), Name: "主端", TokenHash: newID(), IsMaster: true, Enabled: true, Draining: true, KindsJSON: "[]", SlotsJSON: "{}"}).Error; err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			svc, conn := newWorkerTestService(t, t.TempDir(), "master")
			setup(svc, conn)
			addReplicateJob(t, conn, model.KindWebDAV)
			local := addReplicateJob(t, conn, model.KindLocal)
			analyze := model.Job{ID: newID(), Kind: model.JobAnalyze, Status: model.JobPending, MaterialID: newID()}
			if err := conn.Create(&analyze).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := svc.claimJob(context.Background(), model.JobAnalyze); !errors.Is(err, errNoJob) {
				t.Fatalf("analyze must stay pending: %v", err)
			}
			job, err := svc.claimJob(context.Background(), model.JobReplicate)
			if err != nil || job.ID != local.ID {
				t.Fatalf("claimed %q (%v), want only the local drive job", job.ID, err)
			}
			if _, err := svc.claimJob(context.Background(), model.JobReplicate); !errors.Is(err, errNoJob) {
				t.Fatalf("remote drive job must stay pending: %v", err)
			}
		})
	}
}

// An immediate empty reply would make every slot of a draining node spin.
func TestDrainingNodeWaitsThenResumesOnWake(t *testing.T) {
	svc, conn := newWorkerTestService(t, t.TempDir(), "master")
	job := addReplicateJob(t, conn, model.KindWebDAV)
	node := addWorkerNode(t, conn, true)
	req := NodeClaim{Kinds: []string{model.JobReplicate}, Wait: 80 * time.Millisecond, Protocol: jobexec.Protocol}

	started := time.Now()
	in, err := svc.ClaimForNode(context.Background(), node, req)
	if err != nil || in != nil {
		t.Fatalf("draining claim = %+v, %v", in, err)
	}
	if elapsed := time.Since(started); elapsed < 80*time.Millisecond {
		t.Fatalf("draining node answered after %v, want the full wait", elapsed)
	}

	// Draining is re-read while waiting, so resuming takes effect during a long poll.
	go func() {
		time.Sleep(30 * time.Millisecond)
		conn.Model(&model.Node{}).Where("id = ?", node.ID).Update("draining", false)
		svc.notify()
	}()
	req.Wait = 5 * time.Second
	started = time.Now()
	// The replica has no blob, so preparation fails after the claim. What matters
	// is that the job left pending well before the wait ran out.
	_, _ = svc.ClaimForNode(ctxWithTimeout(t, 500*time.Millisecond), node, req)
	var got model.Job
	if err := conn.Where("id = ?", job.ID).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.Attempts == 0 {
		t.Fatalf("job was not claimed after resuming: %+v", got)
	}
}

func ctxWithTimeout(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	t.Cleanup(cancel)
	return ctx
}

func TestClaimRejectsOtherProtocolAndDisabledNode(t *testing.T) {
	svc, conn := newWorkerTestService(t, t.TempDir(), "master")
	job := addReplicateJob(t, conn, model.KindWebDAV)
	node := addWorkerNode(t, conn, false)
	var ae *apperr.Error
	if _, err := svc.ClaimForNode(context.Background(), node, NodeClaim{Kinds: []string{model.JobReplicate}, Protocol: jobexec.Protocol + 1}); !errors.As(err, &ae) || ae.Status != 409 {
		t.Fatalf("other protocol = %v, want 409", err)
	}
	if err := conn.Model(&model.Node{}).Where("id = ?", node.ID).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ClaimForNode(context.Background(), node, NodeClaim{Kinds: []string{model.JobReplicate}, Protocol: jobexec.Protocol}); !errors.As(err, &ae) || ae.Status != 401 {
		t.Fatalf("disabled node = %v, want 401", err)
	}
	var got model.Job
	if err := conn.Where("id = ?", job.ID).First(&got).Error; err != nil || got.Status != model.JobPending {
		t.Fatalf("job must stay pending: %+v %v", got, err)
	}
}

func TestReachableKindsSkipsMasterInternalServices(t *testing.T) {
	svc, conn := newWorkerTestService(t, t.TempDir(), "master")
	ctx := context.Background()
	all := []string{model.JobReplicate, model.JobAnalyze, model.JobIndex}
	check := func(label string, providerIDs []string, want ...string) {
		t.Helper()
		got, _, err := svc.reachableKinds(ctx, all, providerIDs)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(want) {
			t.Fatalf("%s: kinds = %v, want %v", label, got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("%s: kinds = %v, want %v", label, got, want)
			}
		}
	}
	if err := conn.Where("1 = 1").Delete(&model.OCRProvider{}).Error; err != nil {
		t.Fatal(err)
	}
	ocr := model.OCRProvider{ID: newID(), Name: "RapidOCR", Kind: model.OCRKindRapidOCR, BaseURL: "http://rapidocr:9003", Model: "PP-OCRv5", Enabled: true}
	if err := conn.Create(&ocr).Error; err != nil {
		t.Fatal(err)
	}
	if err := conn.Model(&model.AISettings{}).Where("1 = 1").Update("ocr_provider_id", ocr.ID).Error; err != nil {
		t.Fatal(err)
	}
	check("compose OCR, no node provider", nil, model.JobReplicate)
	check("compose OCR selected by node but no model", []string{ocr.ID}, model.JobReplicate)

	addModel := func(id, baseURL string, priority int) {
		t.Helper()
		if err := conn.Create(&model.AIProvider{ID: id, Name: id, BaseURL: baseURL, Enabled: true, Priority: priority, DefaultModel: "m"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	addModel("gateway", "http://new-api:3000/v1", 1)
	check("internal gateway only", []string{ocr.ID}, model.JobReplicate)
	addModel("hosted", "https://api.example.com/v1", 2)
	// Analysis fails over to the hosted endpoint; indexing uses only the first.
	check("internal gateway first", []string{ocr.ID}, model.JobReplicate, model.JobAnalyze)
}

func TestIsMasterInternalURL(t *testing.T) {
	for raw, want := range map[string]bool{
		"http://rapidocr:9003":        true,
		"http://localhost:9003":       true,
		"http://127.0.0.1:9003":       true,
		"http://[::1]:9003":           true,
		"http://192.168.1.5:9003":     false,
		"https://mineru.net":          false,
		"https://ocr.example.com/api": false,
		"":                            false,
	} {
		if got := isMasterInternalURL(raw); got != want {
			t.Errorf("isMasterInternalURL(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestListNodesReportsJobsPerNode(t *testing.T) {
	svc, conn := newWorkerTestService(t, t.TempDir(), "master")
	node := addWorkerNode(t, conn, false)
	now := time.Now().UTC()
	old := now.Add(-2 * time.Hour)
	for _, j := range []model.Job{
		{ID: newID(), Kind: model.JobReplicate, Status: model.JobRunning, LeaseOwner: node.ID, NodeID: node.ID},
		{ID: newID(), Kind: model.JobReplicate, Status: model.JobSucceeded, NodeID: node.ID, FinishedAt: &now},
		{ID: newID(), Kind: model.JobIndex, Status: model.JobFailed, NodeID: node.ID, FinishedAt: &now},
		{ID: newID(), Kind: model.JobIndex, Status: model.JobFailed, NodeID: node.ID, FinishedAt: &old},
	} {
		if err := conn.Create(&j).Error; err != nil {
			t.Fatal(err)
		}
	}
	views, err := svc.ListNodes(context.Background())
	if err != nil || len(views) != 1 {
		t.Fatalf("list nodes = %+v, %v", views, err)
	}
	v := views[0]
	if v.RunningKinds[model.JobReplicate] != 1 || v.Succeeded != 1 || v.Failed != 1 {
		t.Fatalf("job stats = %+v", v)
	}
}
