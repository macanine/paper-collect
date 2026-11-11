package service

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The master schedules its own jobs with one dispatcher goroutine instead of a
// polling loop per slot. Each pass reads the free capacity of every kind,
// claims up to that many jobs in a single transaction and starts one goroutine
// per claimed job; the goroutine exits with the job. Pending work, a finished
// job and a settings change all kick the dispatcher, so a chain moves to its
// next stage at once. While idle the fallback poll backs off, because every
// local path that queues work kicks it.
//
// Running jobs share one lease renewer: one transaction per heartbeat interval
// covers all of them, and the renewer exits when nothing is running.

// maxIdleWait bounds the idle back-off. It only matters for work this process
// was not told about, such as a retry whose next_attempt_at has passed.
const maxIdleWait = 30 * time.Second

// localKindOrder lists the kinds the master may run, downstream stages first so
// a chain already under way finishes before new chains start.
var localKindOrder = []string{model.JobEntity, model.JobDecide, model.JobGroup, model.JobIndex, model.JobAnalyze, model.JobReplicate, model.JobIngest}

// StartWorker starts the dispatcher and the reaper. Ingest, group, decide,
// entity and replicas on a local drive only ever run on the master. Other
// replicate, analyze and index jobs run here for the kinds the master keeps for
// itself; worker nodes claim them over HTTP.
func (s *Service) StartWorker(ctx context.Context) {
	go s.reaperLoop(ctx)
	go s.dispatchLoop(ctx)
}

// restartLocalRunners applies new settings: capacity is read on every pass.
func (s *Service) restartLocalRunners() { s.kickDispatcher() }

func (s *Service) kickDispatcher() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

func (s *Service) dispatchLoop(ctx context.Context) {
	idle := 0
	for ctx.Err() == nil {
		claimed, hungry := s.dispatchOnce(ctx)
		if claimed > 0 {
			idle = 0
			continue // a full pass may have left more work for the same capacity
		}
		var timer *time.Timer
		var timeout <-chan time.Time
		if hungry {
			timer = time.NewTimer(s.idleWait(ctx, idle))
			timeout = timer.C
			idle++
		}
		select {
		case <-ctx.Done():
		case <-s.kick:
			idle = 0
		case <-timeout:
		}
		if timer != nil {
			timer.Stop()
		}
	}
}

// idleWait doubles the poll interval while nothing arrives, but never sleeps
// past the next scheduled retry.
func (s *Service) idleWait(ctx context.Context, idle int) time.Duration {
	poll := s.workerSettings().PollInterval
	wait := poll << min(idle, 10)
	if limit := max(poll, maxIdleWait); wait > limit {
		wait = limit
	}
	var next []model.Job
	if err := s.db.WithContext(ctx).Select("next_attempt_at").
		Where("status = ? AND next_attempt_at IS NOT NULL", model.JobPending).
		Order("next_attempt_at ASC").Limit(1).Find(&next).Error; err == nil && len(next) == 1 && next[0].NextAttemptAt != nil {
		if until := next[0].NextAttemptAt.Sub(s.now()); until < wait {
			wait = max(until, poll)
		}
	}
	return wait
}

// localClaims returns one claim request per kind with free local capacity.
func (s *Service) localClaims(ctx context.Context) ([]claimRequest, error) {
	settings := s.workerSettings()
	owner := claimer{ID: settings.InstanceID, Name: settings.NodeName}
	draining, err := s.masterDraining(ctx)
	if err != nil {
		return nil, err
	}
	s.slotMu.Lock()
	defer s.slotMu.Unlock()
	var out []claimRequest
	for _, kind := range localKindOrder {
		free := localLimit(settings, kind) - s.activeSlots[kind]
		if free <= 0 {
			continue
		}
		req := claimRequest{Kind: kind, Limit: free, Owner: owner}
		switch kind {
		case model.JobAnalyze, model.JobIndex:
			if draining || !slices.Contains(settings.Kinds, kind) {
				continue
			}
		case model.JobReplicate:
			// A draining master, or one that leaves replicas to nodes, still
			// writes to local drives, which no node can reach.
			if draining || !slices.Contains(settings.Kinds, kind) {
				req.Owner.LocalDriveOnly = true
				req.Owner.AllowRoutedRemote = !draining
			}
		}
		out = append(out, req)
	}
	return out, nil
}

func localLimit(settings WorkerSettings, kind string) int {
	switch kind {
	case model.JobIngest:
		return settings.IngestConcurrency
	case model.JobGroup, model.JobEntity:
		return 1
	case model.JobDecide:
		return max(settings.AnalyzeConcurrency, 1)
	case model.JobReplicate:
		return settings.ReplicateConcurrency
	case model.JobAnalyze:
		return settings.AnalyzeConcurrency
	case model.JobIndex:
		return settings.IndexConcurrency
	}
	return 0
}

// dispatchOnce runs one claim pass. hungry is true when some kind had free
// capacity, so the caller should poll again later even without a kick.
func (s *Service) dispatchOnce(ctx context.Context) (claimed int, hungry bool) {
	reqs, err := s.localClaims(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.logf("plan local claims: %v", err)
		}
		return 0, true
	}
	if len(reqs) == 0 {
		return 0, false
	}
	jobs, err := s.claimBatch(ctx, reqs)
	if err != nil {
		if ctx.Err() == nil {
			s.logf("claim jobs: %v", err)
		}
		return 0, true
	}
	for _, job := range jobs {
		s.slotMu.Lock()
		s.activeSlots[job.Kind]++
		s.slotMu.Unlock()
		go func(job model.Job) {
			defer func() {
				s.releaseWorkerSlot(job.Kind)
				s.kickDispatcher()
			}()
			s.executeJob(ctx, job)
		}(job)
	}
	return len(jobs), true
}

func (s *Service) releaseWorkerSlot(kind string) {
	s.slotMu.Lock()
	defer s.slotMu.Unlock()
	if s.activeSlots[kind] <= 1 {
		delete(s.activeSlots, kind)
		return
	}
	s.activeSlots[kind]--
}

// claimRequest asks for up to Limit jobs of one kind for one owner.
type claimRequest struct {
	Kind  string
	Limit int
	Owner claimer
}

// claimBatch leases jobs for several kinds in one transaction. Drive routing
// is read once for the whole batch.
func (s *Service) claimBatch(ctx context.Context, reqs []claimRequest) ([]model.Job, error) {
	var claimed []model.Job
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		claimed = claimed[:0]
		routes := map[string]claimer{}
		for _, req := range reqs {
			if req.Limit <= 0 {
				continue
			}
			scoped, ok := routes[req.Owner.ID]
			if !ok {
				var err error
				if scoped, err = s.applyRouteScope(tx, req.Owner); err != nil {
					return err
				}
				routes[req.Owner.ID] = scoped
			}
			owner := req.Owner
			owner.RoutedManagedIDs, owner.RoutedAllowedIDs = scoped.RoutedManagedIDs, scoped.RoutedAllowedIDs
			jobs, err := s.claimCandidates(tx, owner, []string{req.Kind}, req.Limit)
			if err != nil {
				return err
			}
			claimed = append(claimed, jobs...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

// claimJobFor leases the oldest pending job of the given kinds (any kind when
// empty) to one node.
func (s *Service) claimJobFor(ctx context.Context, owner claimer, kinds []string) (model.Job, error) {
	var claimed []model.Job
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		scoped, err := s.applyRouteScope(tx, owner)
		if err != nil {
			return err
		}
		claimed, err = s.claimCandidates(tx, scoped, kinds, 1)
		return err
	})
	if err != nil {
		return model.Job{}, err
	}
	if len(claimed) == 0 {
		return model.Job{}, errNoJob
	}
	return claimed[0], nil
}

// claimCandidates leases up to limit of the oldest due jobs inside tx. On
// PostgreSQL candidate rows are locked with SKIP LOCKED; on SQLite the write
// lock taken by the transaction already excludes other processes. Jobs whose
// resource lock is held are skipped.
func (s *Service) claimCandidates(tx *gorm.DB, owner claimer, kinds []string, limit int) ([]model.Job, error) {
	var out []model.Job
	tried := make([]string, 0, limit)
	postgres := tx.Dialector.Name() != "sqlite"
	for len(tried) < claimAttempts && len(out) < limit {
		query := owner.scope(tx.Where("status = ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)", model.JobPending, s.now()))
		if len(kinds) > 0 {
			query = query.Where("kind IN ?", kinds)
		}
		if len(tried) > 0 {
			query = query.Where("id NOT IN ?", tried)
		}
		if postgres {
			query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}
		var batch []model.Job
		if err := query.Order("created_at ASC").Order("id ASC").Limit(min(limit-len(out)+2, claimAttempts-len(tried))).Find(&batch).Error; err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}
		for _, job := range batch {
			tried = append(tried, job.ID)
			var got model.Job
			if err := s.tryClaim(tx, owner, job, &got); err != nil {
				if errors.Is(err, errJobLocked) || errors.Is(err, errJobClaimed) {
					continue
				}
				return nil, err
			}
			out = append(out, got)
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

// localLease is a job this process runs under its own lease.
type localLease struct {
	job    model.Job
	cancel context.CancelCauseFunc
}

// holdLease registers a running job with the shared renewer and returns the
// function that releases it.
func (s *Service) holdLease(job model.Job, cancel context.CancelCauseFunc) func() {
	lease := &localLease{job: job, cancel: cancel}
	s.leaseMu.Lock()
	s.leases[job.ID] = lease
	if !s.renewing {
		s.renewing = true
		go s.renewLoop()
	}
	s.leaseMu.Unlock()
	return func() {
		s.leaseMu.Lock()
		if s.leases[job.ID] == lease {
			delete(s.leases, job.ID)
		}
		s.leaseMu.Unlock()
	}
}

func (s *Service) renewLoop() {
	for {
		timer := time.NewTimer(s.workerSettings().HeartbeatInterval)
		<-timer.C
		s.leaseMu.Lock()
		if len(s.leases) == 0 {
			s.renewing = false
			s.leaseMu.Unlock()
			return
		}
		leases := make([]*localLease, 0, len(s.leases))
		for _, lease := range s.leases {
			leases = append(leases, lease)
		}
		s.leaseMu.Unlock()
		s.renewLeases(leases)
	}
}

// renewLeases extends every lease that is still ours in one transaction and
// stops the executions whose lease or resource lock moved on.
func (s *Service) renewLeases(leases []*localLease) {
	now := s.now()
	until := now.Add(s.workerSettings().LeaseDuration)
	byOwner := map[string][]string{}
	for _, lease := range leases {
		byOwner[lease.job.LeaseOwner] = append(byOwner[lease.job.LeaseOwner], lease.job.ID)
	}
	kept := map[string]bool{}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		for owner, ids := range byOwner {
			var live []string
			if err := tx.Model(&model.Job{}).Where("id IN ? AND status = ? AND lease_owner = ? AND lease_until IS NOT NULL AND lease_until >= ?",
				ids, model.JobRunning, owner, now).Pluck("id", &live).Error; err != nil {
				return err
			}
			if len(live) == 0 {
				continue
			}
			if err := tx.Model(&model.Job{}).Where("id IN ? AND lease_owner = ?", live, owner).
				Updates(map[string]any{"lease_until": until, "heartbeat_at": now, "updated_at": now}).Error; err != nil {
				return err
			}
			for _, id := range live {
				kept[id] = true
			}
		}
		var locks []model.JobLock
		ids := make([]string, 0, len(kept))
		for id := range kept {
			ids = append(ids, id)
		}
		if len(ids) > 0 {
			if err := tx.Where("job_id IN ?", ids).Find(&locks).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.JobLock{}).Where("job_id IN ?", ids).
				Updates(map[string]any{"lease_until": until, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		held := map[string]bool{}
		for _, lock := range locks {
			held[lock.Key+"\x00"+lock.JobID] = true
		}
		for _, lease := range leases {
			if key := jobLockKey(lease.job); key != "" && kept[lease.job.ID] && !held[key+"\x00"+lease.job.ID] {
				delete(kept, lease.job.ID)
			}
		}
		return nil
	})
	if err != nil {
		s.logf("renew job leases: %v", err)
		return
	}
	for _, lease := range leases {
		if !kept[lease.job.ID] {
			s.logf("job %s lost its lease; stopping worker", lease.job.ID)
			lease.cancel(errLeaseLost)
		}
	}
}
