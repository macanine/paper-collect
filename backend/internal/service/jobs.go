package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/jobexec"
	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// JobFilter narrows a job list. Empty fields are ignored.
type JobFilter struct {
	Status     string
	Kind       string
	MaterialID string
	UploadID   string
	ReplicaID  string
	NodeID     string
}

// ListJobs returns jobs newest first.
func (s *Service) ListJobs(ctx context.Context, f JobFilter, pg PageQuery) (Page[model.Job], error) {
	if f.Status != "" && !isOneOf(f.Status, model.JobPending, model.JobRunning, model.JobSucceeded, model.JobFailed) {
		return Page[model.Job]{}, apperr.Validation("status 不在允许范围内")
	}
	if f.Kind != "" && !isOneOf(f.Kind, model.JobIngest, model.JobReplicate, model.JobAnalyze, model.JobIndex, model.JobGroup) {
		return Page[model.Job]{}, apperr.Validation("kind 不在允许范围内")
	}
	db := s.db.WithContext(ctx)
	build := func() *gorm.DB {
		tx := db.Model(&model.Job{})
		if f.Status != "" {
			tx = tx.Where("status = ?", f.Status)
		}
		if f.Kind != "" {
			tx = tx.Where("kind = ?", f.Kind)
		}
		if f.MaterialID != "" {
			tx = tx.Where("material_id = ?", f.MaterialID)
		}
		if f.UploadID != "" {
			tx = tx.Where("upload_id = ?", f.UploadID)
		}
		if f.ReplicaID != "" {
			tx = tx.Where("replica_id = ?", f.ReplicaID)
		}
		if f.NodeID != "" {
			tx = tx.Where("node_id = ?", f.NodeID)
		}
		return tx
	}
	var total int64
	if err := build().Count(&total).Error; err != nil {
		return Page[model.Job]{}, apperr.Internal(err)
	}
	items := []model.Job{}
	if err := build().Omit("log_json").Order("created_at DESC").Order("id DESC").
		Limit(pg.PageSize).Offset(pg.offset()).Find(&items).Error; err != nil {
		return Page[model.Job]{}, apperr.Internal(err)
	}
	return newPage(items, total, pg), nil
}

// GetJob returns one job.
func (s *Service) GetJob(ctx context.Context, id string) (model.Job, error) {
	var job model.Job
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&job).Error; err != nil {
		return model.Job{}, apperr.NotFound("任务不存在")
	}
	return job, nil
}

// RetryJob returns a failed job to pending with the same job id. Replicate jobs
// keep their frozen target provider; they are never re-planned here.
func (s *Service) RetryJob(ctx context.Context, p auth.Principal, id string) (model.Job, error) {
	db := s.db.WithContext(ctx)
	var result model.Job
	err := db.Transaction(func(tx *gorm.DB) error {
		var job model.Job
		if err := tx.Where("id = ?", id).First(&job).Error; err != nil {
			return apperr.NotFound("任务不存在")
		}
		if job.Status != model.JobFailed {
			return apperr.Conflict("只有失败的任务可以重试")
		}
		now := s.now()
		switch job.Kind {
		case model.JobIngest:
			if job.UploadID == "" {
				return apperr.Conflict("归档任务缺少上传会话")
			}
			var up model.Upload
			if err := tx.Where("id = ?", job.UploadID).First(&up).Error; err != nil {
				return apperr.Conflict("上传会话不存在")
			}
			if up.JobID != "" && up.JobID != job.ID {
				return apperr.Conflict("上传会话已绑定其他任务")
			}
			if err := tx.Model(&model.Upload{}).Where("id = ?", up.ID).
				Updates(map[string]any{"status": model.UploadProcessing, "updated_at": now}).Error; err != nil {
				return err
			}
		case model.JobAnalyze:
			if job.MaterialID == "" {
				return apperr.Conflict("分析任务缺少资料")
			}
			var analysis model.MaterialAnalysis
			if err := tx.Where("material_id = ?", job.MaterialID).First(&analysis).Error; err != nil {
				return apperr.Conflict("分析记录不存在")
			}
			if err := tx.Model(&model.MaterialAnalysis{}).Where("id = ?", analysis.ID).
				Updates(map[string]any{"status": model.AnalysisPending, "error": "", "job_id": job.ID, "updated_at": now}).Error; err != nil {
				return err
			}
		case model.JobIndex:
			if job.MaterialID == "" {
				return apperr.Conflict("索引任务缺少资料")
			}
			if err := tx.Model(&model.MaterialIndexState{}).Where("material_id = ?", job.MaterialID).
				Updates(map[string]any{"status": "pending", "error": "", "job_id": job.ID, "updated_at": now}).Error; err != nil {
				return err
			}
		case model.JobGroup:
			if job.MaterialID == "" {
				return apperr.Conflict("归类任务缺少资料")
			}
		case model.JobReplicate:
			if job.ReplicaID == "" {
				return apperr.Conflict("复制任务缺少副本")
			}
			var replica model.Replica
			if err := tx.Where("id = ?", job.ReplicaID).First(&replica).Error; err != nil {
				return apperr.Conflict("副本不存在")
			}
			if replica.Status != model.ReplicaReady {
				if err := tx.Model(&model.Replica{}).Where("id = ?", replica.ID).
					Updates(map[string]any{"status": model.ReplicaPending, "error": "", "updated_at": now}).Error; err != nil {
					return err
				}
			}
		default:
			return apperr.Conflict("未知任务类型")
		}
		if err := tx.Model(&model.Job{}).Where("id = ?", job.ID).
			Updates(map[string]any{"status": model.JobPending, "error": "", "progress_message": "已重新排队，等待执行", "progress_current": 0, "progress_total": 0,
				"started_at": nil, "finished_at": nil, "duration_ms": 0,
				"log_json": appendJobLog(job.LogJSON, model.JobLog{Time: now, Level: "info", Message: "已重新排队，等待执行"}), "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Where("id = ?", job.ID).First(&result).Error; err != nil {
			return err
		}
		return s.audit(tx, p, "job.retry", job.ID)
	})
	if err != nil {
		if _, ok := err.(*apperr.Error); ok {
			return model.Job{}, err
		}
		return model.Job{}, apperr.Internal(err)
	}
	s.notify()
	return result, nil
}

var errNoJob = errors.New("no pending job")

const claimAttempts = 64

// StartWorker launches bounded local runners. Ingest, group and replicas on a
// local drive only ever run on the master. Other replicate, analyze and index
// jobs run here for the kinds the master keeps for itself; worker nodes claim
// them over HTTP.
func (s *Service) StartWorker(ctx context.Context) {
	go s.reaperLoop(ctx)
	limits := map[string]int{
		model.JobIngest:    s.worker.IngestConcurrency,
		model.JobReplicate: s.worker.ReplicateConcurrency,
		model.JobAnalyze:   s.worker.AnalyzeConcurrency,
		model.JobIndex:     s.worker.IndexConcurrency,
		model.JobGroup:     s.worker.AnalyzeConcurrency,
	}
	kinds := []string{model.JobIngest, model.JobGroup, model.JobReplicate}
	for _, kind := range s.worker.Kinds {
		if kind != model.JobReplicate {
			kinds = append(kinds, kind)
		}
	}
	for _, kind := range kinds {
		for i := 0; i < limits[kind]; i++ {
			go s.workerLoop(ctx, kind)
		}
	}
}

func (s *Service) reaperLoop(ctx context.Context) {
	ticker := time.NewTicker(s.worker.HeartbeatInterval)
	defer ticker.Stop()
	lastPurge := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if time.Since(lastPurge) > time.Hour {
				lastPurge = time.Now()
				if n, err := s.PurgeExpiredTrash(ctx); err != nil {
					s.logf("purge expired trash: %v", err)
				} else if n > 0 {
					s.logf("purged %d expired trashed packs", n)
				}
			}
			recovered, err := s.RecoverExpiredLeases(ctx)
			if err != nil {
				s.logf("recover expired leases: %v", err)
				continue
			}
			if recovered > 0 {
				s.notify()
			}
		}
	}
}

func (s *Service) workerLoop(ctx context.Context, kind string) {
	for {
		if ctx.Err() != nil {
			return
		}
		if s.processNextKind(ctx, kind) {
			continue
		}
		timer := time.NewTimer(s.worker.PollInterval)
		wake := s.wakeSignal()
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

// ProcessNext claims and runs at most one pending job of any kind. It is kept
// for operational smoke checks and tests.
func (s *Service) ProcessNext(ctx context.Context) bool {
	kinds := append([]string{model.JobIngest, model.JobGroup}, s.worker.Kinds...)
	job, err := s.claimJobFor(ctx, claimer{ID: s.worker.InstanceID, Name: s.worker.NodeName}, kinds)
	if errors.Is(err, errNoJob) && !slices.Contains(s.worker.Kinds, model.JobReplicate) {
		job, err = s.claimJob(ctx, model.JobReplicate)
	}
	if err != nil {
		if errors.Is(err, errNoJob) || ctx.Err() != nil {
			return false
		}
		s.logf("claim job: %v", err)
		return false
	}
	s.executeJob(ctx, job)
	return true
}

func (s *Service) processNextKind(ctx context.Context, kind string) bool {
	job, err := s.claimJob(ctx, kind)
	if err != nil {
		if errors.Is(err, errNoJob) || ctx.Err() != nil {
			return false
		}
		s.logf("claim %s job: %v", kind, err)
		return false
	}
	s.executeJob(ctx, job)
	return true
}

// ProcessAvailable drains currently pending jobs up to limit. A limit <= 0 means
// no explicit bound. It is used by tests and by operational smoke checks.
func (s *Service) ProcessAvailable(ctx context.Context, limit int) int {
	done := 0
	for limit <= 0 || done < limit {
		if ctx.Err() != nil {
			break
		}
		if !s.ProcessNext(ctx) {
			break
		}
		done++
	}
	return done
}

// claimJob leases a job to the master's own runners. A draining master, or one
// that leaves a kind to worker nodes, still runs what no node can: ingest, group
// and replicas on a local drive.
func (s *Service) claimJob(ctx context.Context, kind string) (model.Job, error) {
	owner := claimer{ID: s.worker.InstanceID, Name: s.worker.NodeName}
	var kinds []string
	if kind != "" {
		kinds = []string{kind}
	}
	if jobexec.IsRemoteKind(kind) {
		draining, err := s.masterDraining(ctx)
		if err != nil {
			return model.Job{}, err
		}
		if draining || !slices.Contains(s.worker.Kinds, kind) {
			if kind != model.JobReplicate {
				return model.Job{}, errNoJob
			}
			owner.LocalDriveOnly = true
			owner.AllowRoutedRemote = !draining
		}
	}
	return s.claimJobFor(ctx, owner, kinds)
}

func (s *Service) masterDraining(ctx context.Context) (bool, error) {
	var rows []model.Node
	if err := s.db.WithContext(ctx).Select("draining").Where("is_master = ?", true).Limit(1).Find(&rows).Error; err != nil {
		return false, err
	}
	return len(rows) == 1 && rows[0].Draining, nil
}

// claimer is the node a claimed job is leased to.
type claimer struct {
	ID, Name string
	// RemoteOnly skips replicas on a local drive: its root is inside the master's
	// DATA_DIR, so a worker would write to its own disk and report success.
	RemoteOnly bool
	// LocalDriveOnly keeps only those replicas.
	LocalDriveOnly bool
	// AllowRoutedRemote lets the master run a remote drive explicitly assigned
	// to it even when WORKER_KINDS leaves ordinary remote jobs to nodes.
	AllowRoutedRemote bool
	// RoutedManagedIDs are drives with an explicit node policy. RoutedAllowedIDs
	// are the policy drives this node may claim in the current snapshot.
	RoutedManagedIDs []string
	RoutedAllowedIDs []string
}

const localDriveJobSQL = "EXISTS (SELECT 1 FROM replicas JOIN storage_drives ON storage_drives.id = replicas.drive_id WHERE replicas.id = jobs.replica_id AND storage_drives.kind = ?)"

func (c claimer) scope(query *gorm.DB) *gorm.DB {
	if c.RemoteOnly {
		query = query.Where("NOT "+localDriveJobSQL, model.KindLocal)
	} else if c.LocalDriveOnly {
		if c.AllowRoutedRemote && len(c.RoutedAllowedIDs) > 0 {
			query = query.Where("("+localDriveJobSQL+" OR replica_id IN (SELECT id FROM replicas WHERE drive_id IN ?))", model.KindLocal, c.RoutedAllowedIDs)
		} else {
			query = query.Where(localDriveJobSQL, model.KindLocal)
		}
	}
	if len(c.RoutedManagedIDs) > 0 {
		parts := []string{
			"kind <> ?",
			"replica_id IN (SELECT id FROM replicas WHERE drive_id NOT IN ?)",
		}
		args := []any{model.JobReplicate, c.RoutedManagedIDs}
		if len(c.RoutedAllowedIDs) > 0 {
			parts = append(parts, "replica_id IN (SELECT id FROM replicas WHERE drive_id IN ?)")
			args = append(args, c.RoutedAllowedIDs)
		}
		query = query.Where("("+strings.Join(parts, " OR ")+")", args...)
	}
	return query
}

// applyRouteScope snapshots drive routing for one claim transaction. A
// preferred route fails over only when every configured node is unavailable;
// an exclusive route remains pending until one of its configured nodes returns.
func (s *Service) applyRouteScope(tx *gorm.DB, owner claimer) (claimer, error) {
	if owner.ID == "" {
		return owner, nil
	}
	var drives []model.Drive
	if err := tx.Select("id", "kind", "route_mode").Where("kind <> ?", model.KindLocal).Find(&drives).Error; err != nil {
		return owner, err
	}
	if len(drives) == 0 {
		return owner, nil
	}
	var routes []model.DriveNodeRoute
	if err := tx.Where("enabled = ?", true).Order("priority ASC").Order("node_id ASC").Find(&routes).Error; err != nil {
		return owner, err
	}
	var nodes []model.Node
	if err := tx.Find(&nodes).Error; err != nil {
		return owner, err
	}
	nodeByID := make(map[string]model.Node, len(nodes))
	for _, node := range nodes {
		nodeByID[node.ID] = node
	}
	routesByDrive := make(map[string][]model.DriveNodeRoute)
	for _, route := range routes {
		routesByDrive[route.DriveID] = append(routesByDrive[route.DriveID], route)
	}
	now := s.now()
	available := func(node model.Node) bool {
		return node.Enabled && !node.Draining && node.LastSeenAt != nil && now.Sub(*node.LastSeenAt) <= s.worker.LeaseDuration
	}
	managed := make([]string, 0)
	allowed := make([]string, 0)
	for _, drive := range drives {
		mode := drive.RouteMode
		if mode == "" || mode == model.DriveRoutePool {
			continue
		}
		managed = append(managed, drive.ID)
		driveRoutes := routesByDrive[drive.ID]
		if len(driveRoutes) == 0 {
			if mode == model.DriveRoutePreferred {
				allowed = append(allowed, drive.ID)
			}
			continue
		}
		minPriority := 0
		ownerPreferred := false
		active := false
		for _, route := range driveRoutes {
			node, ok := nodeByID[route.NodeID]
			if !ok || !available(node) {
				continue
			}
			if !active || route.Priority < minPriority {
				active, minPriority, ownerPreferred = true, route.Priority, route.NodeID == owner.ID
			} else if route.Priority == minPriority && route.NodeID == owner.ID {
				ownerPreferred = true
			}
		}
		if !active {
			if mode == model.DriveRoutePreferred {
				allowed = append(allowed, drive.ID)
			}
		} else if ownerPreferred {
			allowed = append(allowed, drive.ID)
		}
	}
	owner.RoutedManagedIDs, owner.RoutedAllowedIDs = managed, allowed
	return owner, nil
}

// claimJobFor leases the oldest pending job of the given kinds (any kind when
// empty) to one node.
func (s *Service) claimJobFor(ctx context.Context, owner claimer, kinds []string) (model.Job, error) {
	db := s.db.WithContext(ctx)
	var claimed model.Job
	err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		owner, err = s.applyRouteScope(tx, owner)
		if err != nil {
			return err
		}
		if s.db.Dialector.Name() == "sqlite" {
			return s.claimJobSQLite(tx, owner, kinds, &claimed)
		}
		return s.claimJobPostgres(tx, owner, kinds, &claimed)
	})
	if err != nil {
		return model.Job{}, err
	}
	return claimed, nil
}

func (s *Service) claimJobPostgres(tx *gorm.DB, owner claimer, kinds []string, claimed *model.Job) error {
	tried := make([]string, 0, claimAttempts)
	for attempts := 0; attempts < claimAttempts; attempts++ {
		var job model.Job
		query := owner.scope(tx.Where("status = ?", model.JobPending))
		if len(kinds) > 0 {
			query = query.Where("kind IN ?", kinds)
		}
		if len(tried) > 0 {
			query = query.Where("id NOT IN ?", tried)
		}
		err := query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Order("created_at ASC").Order("id ASC").First(&job).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errNoJob
			}
			return err
		}
		tried = append(tried, job.ID)
		if err := s.tryClaim(tx, owner, job, claimed); err != nil {
			if errors.Is(err, errJobLocked) {
				continue
			}
			if errors.Is(err, errJobClaimed) {
				continue
			}
			return err
		}
		return nil
	}
	return errNoJob
}

// claimJobSQLite uses the database write lock from BEGIN IMMEDIATE as the
// cross-process exclusion primitive. A read-then-update would otherwise race
// between separate server processes.
func (s *Service) claimJobSQLite(tx *gorm.DB, owner claimer, kinds []string, claimed *model.Job) error {
	tried := make([]string, 0, claimAttempts)
	for attempts := 0; attempts < claimAttempts; attempts++ {
		var job model.Job
		query := owner.scope(tx.Where("status = ?", model.JobPending))
		if len(kinds) > 0 {
			query = query.Where("kind IN ?", kinds)
		}
		if len(tried) > 0 {
			query = query.Where("id NOT IN ?", tried)
		}
		if err := query.Order("created_at ASC").Order("id ASC").First(&job).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errNoJob
			}
			return err
		}
		tried = append(tried, job.ID)
		if err := s.tryClaim(tx, owner, job, claimed); err != nil {
			if errors.Is(err, errJobLocked) {
				continue
			}
			if errors.Is(err, errJobClaimed) {
				continue
			}
			return err
		}
		return nil
	}
	return errNoJob
}

func (s *Service) tryClaim(tx *gorm.DB, owner claimer, job model.Job, claimed *model.Job) error {
	now := s.now()
	until := now.Add(s.worker.LeaseDuration)
	if key := jobLockKey(job); key != "" {
		acquired, err := acquireJobLock(tx, key, job.ID, now, until)
		if err != nil {
			return err
		}
		if !acquired {
			return errJobLocked
		}
	}
	res := tx.Model(&model.Job{}).
		Where("id = ? AND status = ?", job.ID, model.JobPending).
		Updates(map[string]any{
			"status":           model.JobRunning,
			"attempts":         gorm.Expr("attempts + 1"),
			"progress_message": "任务已开始，正在准备处理",
			"progress_current": 0,
			"progress_total":   0,
			"log_json":         appendJobLog(job.LogJSON, model.JobLog{Time: now, Level: "info", Message: "任务已开始，正在准备处理"}),
			"lease_owner":      owner.ID,
			"node_id":          owner.ID,
			"node_name":        owner.Name,
			"lease_until":      until,
			"heartbeat_at":     now,
			"started_at":       now,
			"finished_at":      nil,
			"duration_ms":      0,
			"updated_at":       now,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return errJobClaimed
	}
	job.Status = model.JobRunning
	job.Attempts++
	job.LeaseOwner = owner.ID
	job.NodeID, job.NodeName = owner.ID, owner.Name
	job.LeaseUntil = &until
	job.HeartbeatAt = &now
	job.StartedAt = &now
	job.FinishedAt = nil
	job.DurationMS = 0
	job.UpdatedAt = now
	*claimed = job
	return nil
}

var (
	errJobLocked  = errors.New("job resource locked")
	errJobClaimed = errors.New("job claimed by another worker")
)

func jobLockKey(job model.Job) string {
	if job.MaterialID == "" || (job.Kind != model.JobAnalyze && job.Kind != model.JobIndex && job.Kind != model.JobGroup) {
		return ""
	}
	return "material:" + job.MaterialID
}

func acquireJobLock(tx *gorm.DB, key, jobID string, now, until time.Time) (bool, error) {
	lock := model.JobLock{Key: key, JobID: jobID, LeaseUntil: until, UpdatedAt: now}
	insert := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&lock)
	if insert.Error != nil {
		return false, insert.Error
	}
	if insert.RowsAffected == 1 {
		return true, nil
	}
	res := tx.Model(&model.JobLock{}).
		Where("key = ? AND lease_until < ?", key, now).
		Updates(map[string]any{"job_id": jobID, "lease_until": until, "updated_at": now})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

func (s *Service) executeJob(ctx context.Context, job model.Job) {
	s.running.Add(1)
	defer s.running.Add(-1)
	runCtx, cancel := context.WithCancelCause(ctx)
	stopHeartbeat := s.heartbeat(runCtx, cancel, job)
	defer func() {
		stopHeartbeat()
		cancel(nil)
		if r := recover(); r != nil {
			s.logf("job %s panicked", job.ID)
			s.failJob(job.ID, job.LeaseOwner, "任务执行发生内部错误")
			if job.UploadID != "" {
				_ = s.db.Model(&model.Upload{}).Where("id = ? AND job_id = ?", job.UploadID, job.ID).
					Updates(map[string]any{"status": model.UploadFailed, "updated_at": s.now()}).Error
			}
			if job.ReplicaID != "" {
				_ = s.db.Model(&model.Replica{}).Where("id = ? AND status = ?", job.ReplicaID, model.ReplicaUploading).
					Updates(map[string]any{"status": model.ReplicaFailed, "error": "任务执行发生内部错误", "updated_at": s.now()}).Error
			}
		}
	}()
	switch job.Kind {
	case model.JobIngest:
		s.RunIngest(runCtx, job.ID)
	case model.JobReplicate, model.JobAnalyze, model.JobIndex:
		s.runLocal(runCtx, job)
	case model.JobGroup:
		s.RunGroup(runCtx, job.ID)
	default:
		s.failJob(job.ID, job.LeaseOwner, "未知任务类型")
	}
}

// runLocal executes a prepared job on the master with the same executor that
// worker nodes use, then commits or records the outcome.
func (s *Service) runLocal(ctx context.Context, job model.Job) {
	in, err := s.PrepareJob(ctx, job)
	if err != nil {
		s.handlePrepareError(ctx, job, err)
		return
	}
	out, err := jobexec.Run(ctx, s.localEnv(job), in)
	if err != nil {
		if ctx.Err() != nil {
			if !errors.Is(context.Cause(ctx), errLeaseLost) {
				s.interruptJob(job)
			}
			return
		}
		message, extra := jobexec.Failure(job.Kind, err)
		s.logf("%s job failed: %s", job.Kind, message)
		s.recordFailure(job, message, extra)
		return
	}
	if err := s.CommitJob(ctx, job, out); err != nil && !errors.Is(err, errLeaseLost) {
		s.logf("commit %s job: %v", job.Kind, err)
		s.recordFailure(job, "写入任务结果失败", nil)
	}
}

// PrepareJob builds the executor input for a leased job. The input contains
// decrypted credentials and must only travel over an authenticated channel.
func (s *Service) PrepareJob(ctx context.Context, job model.Job) (jobexec.Input, error) {
	in := jobexec.Input{JobID: job.ID, Kind: job.Kind}
	if job.StartedAt != nil {
		in.StartedAt = *job.StartedAt
	}
	var err error
	switch job.Kind {
	case model.JobReplicate:
		in.Replicate, err = s.prepareReplicate(ctx, job)
		if err == nil && in.Replicate == nil {
			// Nothing to transfer: the drive is disabled or the target was withdrawn.
			if err := s.commitReplicate(job, nil); err != nil {
				return in, err
			}
			return in, errJobDone
		}
	case model.JobAnalyze:
		in.Analyze, err = s.prepareAnalyze(ctx, job)
	case model.JobIndex:
		in.Index, err = s.prepareIndex(ctx, job)
	default:
		err = fmt.Errorf("任务类型 %s 不能交给节点执行", job.Kind)
	}
	return in, err
}

// CommitJob stores a node's result under the job's lease.
func (s *Service) CommitJob(ctx context.Context, job model.Job, out jobexec.Result) error {
	switch job.Kind {
	case model.JobReplicate:
		return s.commitReplicate(job, out.Replicate)
	case model.JobAnalyze:
		return s.commitAnalyze(ctx, job, out.Analyze)
	case model.JobIndex:
		return s.commitIndex(ctx, job, out.Index)
	}
	return fmt.Errorf("未知任务类型 %s", job.Kind)
}

// recordFailure marks a job failed together with its kind's projection.
func (s *Service) recordFailure(job model.Job, message string, extra map[string]int) {
	message = redact(message)
	switch job.Kind {
	case model.JobReplicate:
		s.failReplicaJob(job.ID, job.LeaseOwner, job.ReplicaID, message)
	case model.JobAnalyze:
		s.failJob(job.ID, job.LeaseOwner, message, func(tx *gorm.DB) error { return s.failAnalyzeTx(tx, job, message) })
	case model.JobIndex:
		values := map[string]any{}
		for k, v := range extra {
			values[k] = v
		}
		s.failIndex(job.ID, job.LeaseOwner, job.MaterialID, message, values)
	default:
		s.failJob(job.ID, job.LeaseOwner, message)
	}
}

// interruptJob returns a job whose executor stopped early to the queue.
func (s *Service) interruptJob(job model.Job) {
	s.requeueJob(job.ID, job.LeaseOwner, func(tx *gorm.DB) error { return s.resetProjectionTx(tx, []string{job.ID}, s.now()) })
}

func (s *Service) heartbeat(parent context.Context, cancel context.CancelCauseFunc, job model.Job) func() {
	ctx, stop := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(s.worker.HeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				now := s.now()
				until := now.Add(s.worker.LeaseDuration)
				err := s.db.Transaction(func(tx *gorm.DB) error {
					res := tx.Model(&model.Job{}).
						Where("id = ? AND status = ? AND lease_owner = ? AND lease_until IS NOT NULL AND lease_until >= ?",
							job.ID, model.JobRunning, job.LeaseOwner, now).
						Updates(map[string]any{"lease_until": until, "heartbeat_at": now, "updated_at": now})
					if res.Error != nil {
						return res.Error
					}
					if res.RowsAffected != 1 {
						return errLeaseLost
					}
					if key := jobLockKey(job); key != "" {
						res = tx.Model(&model.JobLock{}).Where("key = ? AND job_id = ?", key, job.ID).
							Updates(map[string]any{"lease_until": until, "updated_at": now})
						if res.Error != nil {
							return res.Error
						}
						if res.RowsAffected != 1 {
							return errLeaseLost
						}
					}
					return nil
				})
				if err != nil {
					if errors.Is(err, errLeaseLost) {
						s.logf("job %s lost its lease; stopping worker", job.ID)
						cancel(errLeaseLost)
						return
					}
					s.logf("heartbeat job %s: %v", job.ID, err)
				}
			}
		}
	}()
	return func() {
		stop()
		<-done
	}
}

var errLeaseLost = errors.New("job lease lost")

// lockJobLease takes a row lock and rejects an expired lease before the caller
// performs business writes. finishJobTx checks the same fence again.
func (s *Service) lockJobLease(tx *gorm.DB, jobID, owner string) error {
	now := s.now()
	query := tx.Model(&model.Job{}).
		Select("id").
		Where("id = ? AND status = ? AND lease_owner = ? AND lease_until IS NOT NULL AND lease_until >= ?",
			jobID, model.JobRunning, owner, now)
	if tx.Dialector.Name() != "sqlite" {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var row model.Job
	if err := query.First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errLeaseLost
		}
		return err
	}
	return nil
}

// finishJobTx is the lease fence for business writes. Callers put their result
// rows and this update in one transaction, so a reclaimed worker cannot commit
// either the result or a stale terminal state.
func (s *Service) finishJobTx(tx *gorm.DB, jobID, owner, status, message string) error {
	return s.finishJobTxWithMetrics(tx, jobID, owner, status, message, nil)
}

func (s *Service) finishJobTxWithMetrics(tx *gorm.DB, jobID, owner, status, message string, metrics map[string]any) error {
	now := s.now()
	message = redact(message)
	var job model.Job
	if err := tx.Select("id", "log_json", "started_at").Where("id = ?", jobID).First(&job).Error; err != nil {
		return err
	}
	durationMS := int64(0)
	if job.StartedAt != nil && !job.StartedAt.IsZero() {
		durationMS = now.Sub(*job.StartedAt).Milliseconds()
		if durationMS < 0 {
			durationMS = 0
		}
	}
	var finishedAt any = now
	if status == model.JobPending {
		finishedAt = nil
		durationMS = 0
	}
	progress, level := "处理完成", "info"
	if status == model.JobSucceeded && message != "" {
		progress = redact(message)
	} else if status == model.JobFailed {
		progress, level = "处理失败："+redact(message), "error"
	} else if status == model.JobPending {
		progress = "处理已中断，等待重新执行"
	}
	jobError := ""
	if status == model.JobFailed {
		jobError = message
	}
	res := tx.Model(&model.Job{}).
		Where("id = ? AND status = ? AND lease_owner = ? AND lease_until IS NOT NULL AND lease_until >= ?",
			jobID, model.JobRunning, owner, now).
		Updates(map[string]any{
			"status": status, "error": jobError, "lease_owner": "",
			"lease_until": nil, "heartbeat_at": nil, "updated_at": now,
			"finished_at":      finishedAt,
			"duration_ms":      durationMS,
			"progress_message": progress,
			"log_json": appendJobLog(job.LogJSON, model.JobLog{
				Time: now, Level: level, Message: progress, DurationMS: durationMS, Metrics: metrics,
			}),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return errLeaseLost
	}
	if err := releaseJobLockTx(tx, jobID); err != nil {
		return err
	}
	return nil
}

// appendJobLog bounds retained progress history, including across retries.
func appendJobLog(raw string, entry model.JobLog) string {
	var logs []model.JobLog
	_ = json.Unmarshal([]byte(raw), &logs)
	logs = append(logs, entry)
	if len(logs) > 200 {
		logs = logs[len(logs)-200:]
	}
	encoded, _ := json.Marshal(logs)
	return string(encoded)
}

// jobProgress only accepts fixed, application-generated messages; never pass secrets or URLs.
func (s *Service) jobProgress(ctx context.Context, job model.Job, message string, current, total int) {
	s.jobProgressWithMetrics(ctx, job, message, current, total, nil)
}

// jobProgressWithMetrics stores bounded, non-secret timing and transfer facts so
// the UI can explain what the worker actually measured.
func (s *Service) jobProgressWithMetrics(ctx context.Context, job model.Job, message string, current, total int, metrics map[string]any) {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.lockJobLease(tx, job.ID, job.LeaseOwner); err != nil {
			return err
		}
		var latest model.Job
		if err := tx.Select("id", "log_json").Where("id = ?", job.ID).First(&latest).Error; err != nil {
			return err
		}
		now := s.now()
		elapsedMS := int64(0)
		if job.StartedAt != nil && !job.StartedAt.IsZero() {
			elapsedMS = now.Sub(*job.StartedAt).Milliseconds()
			if elapsedMS < 0 {
				elapsedMS = 0
			}
		}
		return tx.Model(&model.Job{}).Where("id = ? AND status = ? AND lease_owner = ?", job.ID, model.JobRunning, job.LeaseOwner).
			Updates(map[string]any{"progress_message": message, "progress_current": current, "progress_total": total,
				"log_json": appendJobLog(latest.LogJSON, model.JobLog{
					Time: now, Level: "info", Message: message, ElapsedMS: elapsedMS, Metrics: metrics,
				}), "updated_at": now}).Error
	})
	if err != nil && !errors.Is(err, errLeaseLost) && ctx.Err() == nil {
		s.logf("record job progress failed")
	}
}

// succeedJob completes a job without a caller-owned projection. Production
// handlers use finishJobTx directly so their result rows commit atomically.
func (s *Service) succeedJob(jobID, owner string) bool {
	err := s.db.Transaction(func(tx *gorm.DB) error {
		return s.finishJobTx(tx, jobID, owner, model.JobSucceeded, "")
	})
	if errors.Is(err, errLeaseLost) {
		s.logf("complete job %s ignored: lease lost", jobID)
		return false
	}
	if err != nil {
		s.logf("complete job: %v", err)
		return false
	}
	return true
}

func releaseJobLockTx(tx *gorm.DB, jobID string) error {
	var job model.Job
	if err := tx.Select("material_id", "kind").Where("id = ?", jobID).First(&job).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if key := jobLockKey(job); key != "" {
		return tx.Where("key = ? AND job_id = ?", key, jobID).Delete(&model.JobLock{}).Error
	}
	return nil
}
