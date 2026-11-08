package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/jobexec"
	"github.com/nic/paper-collect/backend/internal/model"
	"github.com/nic/paper-collect/backend/internal/sysstat"
	"gorm.io/gorm"
)

// Node liveness is derived from the last heartbeat when a node is read.
const (
	NodeOnline   = "online"
	NodeStale    = "stale"
	NodeOffline  = "offline"
	NodeDraining = "draining"
	NodeDisabled = "disabled"

	NodeTokenPrefix     = "nd-"
	NodeHeartbeat       = 10 * time.Second
	nodeStaleAfter      = 30 * time.Second
	nodeMetricsRetained = 360 // one hour at the heartbeat interval
	nodeStatsWindow     = time.Hour
	maxClaimWait        = 25 * time.Second
)

// NodeView is a node with its derived status.
type NodeView struct {
	Node            model.Node
	Status          string
	Kinds           []string
	Slots           map[string]int
	ConfiguredSlots map[string]int
	OCRProviderIDs  []string
	RunningKinds    map[string]int // jobs currently leased, by kind
	Succeeded       int            // finished within nodeStatsWindow
	Failed          int
}

// NodeClaim is one worker request for work.
type NodeClaim struct {
	Kinds          []string
	OCRProviderIDs []string
	Wait           time.Duration
	Protocol       int
}

// NodePoint is one in-memory metrics sample.
type NodePoint struct {
	Time     time.Time `json:"time"`
	CPU      float64   `json:"cpu_percent"`
	MemUsed  int64     `json:"mem_used"`
	MemTotal int64     `json:"mem_total"`
	NetRx    int64     `json:"net_rx_bps"`
	NetTx    int64     `json:"net_tx_bps"`
	Running  int       `json:"running"`
}

// NodeInput creates a worker node with master-controlled runtime settings.
type NodeInput struct {
	Name           string
	Slots          *map[string]int
	CacheMaxBytes  *int64
	OCRProviderIDs *[]string
}

// NodePatch updates a node. Nil fields are unchanged.
type NodePatch struct {
	Name           *string
	Enabled        *bool
	Draining       *bool
	Slots          *map[string]int
	CacheMaxBytes  *int64
	OCRProviderIDs *[]string
}

// Heartbeat is what a node reports every NodeHeartbeat.
type Heartbeat struct {
	Version   string         `json:"version"`
	Protocol  int            `json:"protocol"`
	StartedAt time.Time      `json:"started_at"`
	Kinds     []string       `json:"kinds"`
	Slots     map[string]int `json:"slots"`
	Running   []string       `json:"running_job_ids"`
	Sample    sysstat.Sample `json:"sample"`
	CacheUsed int64          `json:"cache_used"`
	CacheMax  int64          `json:"cache_max"`
	Revision  int64          `json:"config_revision"`
}

// NodeRuntimeConfig is desired worker state delivered by the master.
type NodeRuntimeConfig struct {
	Slots          map[string]int `json:"slots"`
	CacheMaxBytes  int64          `json:"cache_max_bytes"`
	OCRProviderIDs []string       `json:"ocr_provider_ids"`
	HeartbeatMS    int64          `json:"heartbeat_ms"`
	ClaimWaitMS    int64          `json:"claim_wait_ms"`
	Revision       int64          `json:"revision"`
}

// HeartbeatReply tells a node which jobs it no longer owns and delivers the
// current master-controlled runtime configuration.
type HeartbeatReply struct {
	LostJobIDs []string           `json:"lost_job_ids"`
	Draining   bool               `json:"draining"`
	Config     *NodeRuntimeConfig `json:"config,omitempty"`
}

// nodeMetrics keeps the last hour of samples per node in memory. A restart
// clears it; nothing else depends on it.
type nodeMetrics struct {
	mu     sync.Mutex
	points map[string][]NodePoint
}

func (m *nodeMetrics) add(id string, p NodePoint) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.points == nil {
		m.points = map[string][]NodePoint{}
	}
	series := append(m.points[id], p)
	if len(series) > nodeMetricsRetained {
		series = series[len(series)-nodeMetricsRetained:]
	}
	m.points[id] = series
}

func (m *nodeMetrics) get(id string) []NodePoint {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]NodePoint{}, m.points[id]...)
}

func (s *Service) nodeStatus(n model.Node) string {
	lease := s.workerSettings().LeaseDuration
	switch {
	case !n.Enabled:
		return NodeDisabled
	case n.LastSeenAt == nil || s.now().Sub(*n.LastSeenAt) > lease:
		return NodeOffline
	case s.now().Sub(*n.LastSeenAt) > max(nodeStaleAfter, 3*s.workerSettings().HeartbeatInterval):
		return NodeStale
	case n.Draining:
		return NodeDraining
	}
	return NodeOnline
}

func defaultNodeSlots() map[string]int {
	return map[string]int{
		model.JobReplicate: 2,
		model.JobAnalyze:   1,
		model.JobIndex:     1,
	}
}

func (s *Service) validateNodeOCRProviders(tx *gorm.DB, ids []string) ([]string, error) {
	out := uniqueStrings(ids)
	if len(out) > 32 {
		return nil, apperr.Validation("每个节点最多配置 32 个 OCR 服务")
	}
	if len(out) == 0 {
		return []string{}, nil
	}
	var count int64
	if err := tx.Model(&model.OCRProvider{}).Where("id IN ? AND enabled = ?", out, true).Count(&count).Error; err != nil {
		return nil, err
	}
	if count != int64(len(out)) {
		return nil, apperr.Validation("ocr_provider_ids 包含不存在或已停用的服务")
	}
	return out, nil
}

func (s *Service) nodeView(n model.Node) NodeView {
	v := NodeView{
		Node: n, Status: s.nodeStatus(n), Kinds: model.DecodeStrings(n.KindsJSON),
		Slots: map[string]int{}, ConfiguredSlots: decodeSlots(n.ConfiguredSlotsJSON),
		OCRProviderIDs: model.DecodeStrings(n.OCRProviderIDsJSON), RunningKinds: map[string]int{},
	}
	_ = json.Unmarshal([]byte(n.SlotsJSON), &v.Slots)
	return v
}

// ListNodes returns the master first, then workers by name.
func (s *Service) ListNodes(ctx context.Context) ([]NodeView, error) {
	var rows []model.Node
	if err := s.db.WithContext(ctx).Order("is_master DESC").Order("name ASC").Order("id ASC").Find(&rows).Error; err != nil {
		return nil, apperr.Internal(err)
	}
	var running []struct {
		LeaseOwner, Kind string
		Count            int
	}
	if err := s.db.WithContext(ctx).Model(&model.Job{}).Select("lease_owner, kind, COUNT(*) AS count").
		Where("status = ?", model.JobRunning).Group("lease_owner, kind").Scan(&running).Error; err != nil {
		return nil, apperr.Internal(err)
	}
	var finished []struct {
		NodeID, Status string
		Count          int
	}
	if err := s.db.WithContext(ctx).Model(&model.Job{}).Select("node_id, status, COUNT(*) AS count").
		Where("status IN ? AND finished_at >= ?", []string{model.JobSucceeded, model.JobFailed}, s.now().Add(-nodeStatsWindow)).
		Group("node_id, status").Scan(&finished).Error; err != nil {
		return nil, apperr.Internal(err)
	}
	out := make([]NodeView, 0, len(rows))
	for _, n := range rows {
		v := s.nodeView(n)
		for _, r := range running {
			if r.LeaseOwner == n.ID {
				v.RunningKinds[r.Kind] = r.Count
			}
		}
		for _, f := range finished {
			if f.NodeID != n.ID {
				continue
			}
			if f.Status == model.JobSucceeded {
				v.Succeeded = f.Count
			} else {
				v.Failed = f.Count
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// NodeMetrics returns the in-memory history of one node.
func (s *Service) NodeMetrics(ctx context.Context, id string) ([]NodePoint, error) {
	var n model.Node
	if err := s.db.WithContext(ctx).Select("id").Where("id = ?", id).First(&n).Error; err != nil {
		return nil, apperr.NotFound("节点不存在")
	}
	return s.metrics.get(id), nil
}

func validateNodeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8RuneLen(name) > 64 {
		return "", apperr.Validation("节点名称必须为 1 到 64 个字符")
	}
	return name, nil
}

// CreateNode registers a worker and returns its token once. Only the hash is stored.
func (s *Service) CreateNode(ctx context.Context, p auth.Principal, in NodeInput) (NodeView, string, error) {
	name, err := validateNodeName(in.Name)
	if err != nil {
		return NodeView{}, "", err
	}
	slots := defaultNodeSlots()
	if in.Slots != nil {
		slots = *in.Slots
	}
	rawSlots, _, err := encodeSlots(slots)
	if err != nil {
		return NodeView{}, "", err
	}
	cacheMax := int64(20 << 30)
	if in.CacheMaxBytes != nil {
		cacheMax = *in.CacheMaxBytes
	}
	if cacheMax < 0 {
		return NodeView{}, "", apperr.Validation("cache_max_bytes 不能为负数")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return NodeView{}, "", apperr.Internal(err)
	}
	token := NodeTokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	now := s.now()
	n := model.Node{
		ID: newID(), Name: name, TokenHash: auth.HashToken(token), Enabled: true,
		KindsJSON: "[]", SlotsJSON: "{}", ConfiguredSlotsJSON: rawSlots,
		ConfiguredCacheMax: cacheMax, OCRProviderIDsJSON: "[]", ConfigRevision: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	ocrIDs := []string{}
	if in.OCRProviderIDs != nil {
		ocrIDs = *in.OCRProviderIDs
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ocrIDs, err := s.validateNodeOCRProviders(tx, ocrIDs)
		if err != nil {
			return err
		}
		n.OCRProviderIDsJSON = model.EncodeStrings(ocrIDs)
		if err := tx.Create(&n).Error; err != nil {
			return err
		}
		return s.audit(tx, p, "node.create", n.ID)
	})
	if err != nil {
		var ae *apperr.Error
		if errors.As(err, &ae) {
			return NodeView{}, "", err
		}
		return NodeView{}, "", apperr.Internal(err)
	}
	return s.nodeView(n), token, nil
}

// UpdateNode renames, enables/disables or drains a node. A disabled node is
// rejected on its next request; its running jobs return by lease expiry.
func (s *Service) UpdateNode(ctx context.Context, p auth.Principal, id string, in NodePatch) (NodeView, error) {
	var n model.Node
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ?", id).First(&n).Error; err != nil {
			return apperr.NotFound("节点不存在")
		}
		updates := map[string]any{"updated_at": s.now()}
		if in.Name != nil {
			name, err := validateNodeName(*in.Name)
			if err != nil {
				return err
			}
			updates["name"] = name
		}
		if in.Enabled != nil {
			if n.IsMaster && !*in.Enabled {
				return apperr.Validation("主端不能停用；可以设置为排空")
			}
			updates["enabled"] = *in.Enabled
		}
		if in.Draining != nil {
			updates["draining"] = *in.Draining
		}
		configChanged := in.Slots != nil || in.CacheMaxBytes != nil || in.OCRProviderIDs != nil
		if configChanged && n.IsMaster {
			return apperr.Validation("主端运行配置请在集群调度设置中修改")
		}
		if in.Slots != nil {
			raw, _, err := encodeSlots(*in.Slots)
			if err != nil {
				return err
			}
			updates["configured_slots_json"] = raw
		}
		if in.CacheMaxBytes != nil {
			if *in.CacheMaxBytes < 0 {
				return apperr.Validation("cache_max_bytes 不能为负数")
			}
			updates["configured_cache_max"] = *in.CacheMaxBytes
		}
		if in.OCRProviderIDs != nil {
			ids, err := s.validateNodeOCRProviders(tx, *in.OCRProviderIDs)
			if err != nil {
				return err
			}
			updates["ocr_provider_ids_json"] = model.EncodeStrings(ids)
		}
		if configChanged {
			updates["config_revision"] = n.ConfigRevision + 1
		}
		if err := tx.Model(&n).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.Where("id = ?", id).First(&n).Error; err != nil {
			return err
		}
		return s.audit(tx, p, "node.update", id)
	})
	if err != nil {
		var ae *apperr.Error
		if errors.As(err, &ae) {
			return NodeView{}, err
		}
		return NodeView{}, apperr.Internal(err)
	}
	s.notify() // a resumed node may have work waiting
	return s.nodeView(n), nil
}

// AuthenticateNode resolves a worker token to an enabled worker node.
func (s *Service) AuthenticateNode(ctx context.Context, token string) (model.Node, error) {
	if !strings.HasPrefix(token, NodeTokenPrefix) || len(token) != len(NodeTokenPrefix)+43 {
		return model.Node{}, apperr.Unauthorized("节点凭据无效")
	}
	var n model.Node
	if err := s.db.WithContext(ctx).Where("token_hash = ? AND is_master = ?", auth.HashToken(token), false).First(&n).Error; err != nil {
		return model.Node{}, apperr.Unauthorized("节点凭据无效")
	}
	if !n.Enabled {
		return model.Node{}, apperr.Unauthorized("节点已停用")
	}
	return n, nil
}

// RegisterMaster creates or refreshes the master's own node row and leases
// local jobs to it. It must run before RecoverInterrupted.
func (s *Service) RegisterMaster(ctx context.Context, name, version string) (model.Node, error) {
	settings := s.workerSettings()
	var n model.Node
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := s.now()
		err := tx.Where("is_master = ?", true).First(&n).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if strings.TrimSpace(name) == "" {
				name = "主端"
			}
			n = model.Node{
				ID: newID(), Name: name, IsMaster: true, Enabled: true,
				ConfiguredSlotsJSON: "{}", OCRProviderIDsJSON: "[]", ConfigRevision: 1,
				CreatedAt: now,
			}
		} else if err != nil {
			return err
		}
		n.Version, n.Protocol, n.StartedAt, n.LastSeenAt, n.UpdatedAt = version, jobexec.Protocol, &now, &now, now
		n.KindsJSON = model.EncodeStrings(settings.Kinds)
		slots, _ := json.Marshal(localSlots(settings))
		n.SlotsJSON = string(slots)
		return tx.Save(&n).Error
	})
	if err != nil {
		return model.Node{}, err
	}
	settings.InstanceID, settings.NodeName = n.ID, n.Name
	s.setWorkerSettings(settings)
	return n, nil
}

func localSlots(worker WorkerSettings) map[string]int {
	slots := map[string]int{}
	for _, kind := range worker.Kinds {
		switch kind {
		case model.JobReplicate:
			slots[kind] = worker.ReplicateConcurrency
		case model.JobAnalyze:
			slots[kind] = worker.AnalyzeConcurrency
		case model.JobIndex:
			slots[kind] = worker.IndexConcurrency
		}
	}
	return slots
}

// StartMasterHeartbeat samples the master's own resources on the node interval.
func (s *Service) StartMasterHeartbeat(ctx context.Context) {
	sampler := sysstat.New()
	go func() {
		for {
			settings := s.workerSettings()
			s.recordSample(ctx, settings.InstanceID, Heartbeat{Sample: sampler.Sample(ctx)}, int(s.running.Load()), "")
			timer := time.NewTimer(settings.HeartbeatInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
}

func (s *Service) recordSample(ctx context.Context, id string, hb Heartbeat, running int, ip string) {
	now := s.now()
	sm := hb.Sample
	updates := map[string]any{
		"last_seen_at": now, "updated_at": now, "running": running,
		"cpu_percent": sm.CPUPercent, "cpu_cores": sm.CPUCores, "proc_cpu": sm.ProcCPU,
		"mem_used": int64(sm.MemUsed), "mem_total": int64(sm.MemTotal), "proc_rss": int64(sm.ProcRSS),
		"net_rx_bps": int64(sm.NetRxBps), "net_tx_bps": int64(sm.NetTxBps),
		"cache_used": hb.CacheUsed, "cache_max": hb.CacheMax,
	}
	if ip != "" {
		updates["last_ip"] = ip
	}
	if hb.Version != "" {
		updates["version"] = truncateRunes(hb.Version, 64)
	}
	if hb.Protocol != 0 {
		updates["protocol"] = hb.Protocol
	}
	if !hb.StartedAt.IsZero() {
		updates["started_at"] = hb.StartedAt.UTC()
	}
	if hb.Kinds != nil {
		kinds := []string{}
		for _, k := range hb.Kinds {
			if jobexec.IsRemoteKind(k) {
				kinds = append(kinds, k)
			}
		}
		updates["kinds_json"] = model.EncodeStrings(kinds)
	}
	if hb.Slots != nil {
		raw, _ := json.Marshal(hb.Slots)
		updates["slots_json"] = string(raw)
	}
	if hb.Revision > 0 {
		updates["applied_config_revision"] = hb.Revision
	}
	if err := s.db.WithContext(ctx).Model(&model.Node{}).Where("id = ?", id).Updates(updates).Error; err != nil && ctx.Err() == nil {
		s.logf("record node heartbeat failed")
	}
	s.metrics.add(id, NodePoint{
		Time: now, CPU: sm.CPUPercent, MemUsed: int64(sm.MemUsed), MemTotal: int64(sm.MemTotal),
		NetRx: int64(sm.NetRxBps), NetTx: int64(sm.NetTxBps), Running: running,
	})
}

// NodeHeartbeat records a worker sample and renews the leases of the jobs it
// still owns. Jobs it reports but no longer owns are returned as lost.
func (s *Service) NodeHeartbeat(ctx context.Context, n model.Node, hb Heartbeat, ip string) (HeartbeatReply, error) {
	config := s.clusterRuntimeConfig(n)
	reply := HeartbeatReply{LostJobIDs: []string{}, Draining: n.Draining, Config: &config}
	running := uniqueStrings(hb.Running)
	if len(running) > 64 {
		return reply, apperr.Validation("running_job_ids 过多")
	}
	if len(running) > 0 {
		now := s.now()
		until := now.Add(s.workerSettings().LeaseDuration)
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var owned []string
			if err := tx.Model(&model.Job{}).
				Where("id IN ? AND status = ? AND lease_owner = ? AND lease_until IS NOT NULL AND lease_until >= ?", running, model.JobRunning, n.ID, now).
				Pluck("id", &owned).Error; err != nil {
				return err
			}
			if len(owned) > 0 {
				if err := tx.Model(&model.Job{}).Where("id IN ? AND lease_owner = ?", owned, n.ID).
					Updates(map[string]any{"lease_until": until, "heartbeat_at": now, "updated_at": now}).Error; err != nil {
					return err
				}
				if err := tx.Model(&model.JobLock{}).Where("job_id IN ?", owned).
					Updates(map[string]any{"lease_until": until, "updated_at": now}).Error; err != nil {
					return err
				}
			}
			kept := map[string]bool{}
			for _, id := range owned {
				kept[id] = true
			}
			for _, id := range running {
				if !kept[id] {
					reply.LostJobIDs = append(reply.LostJobIDs, id)
				}
			}
			return nil
		})
		if err != nil {
			return reply, apperr.Internal(err)
		}
	}
	s.recordSample(ctx, n.ID, hb, len(running)-len(reply.LostJobIDs), ip)
	return reply, nil
}

// ClaimForNode leases one job to a worker and returns its prepared input. It
// waits up to req.Wait for work; a nil input means nothing is available. A
// draining node and a node with nothing it can run wait like an idle one, so
// workers never spin on immediate empty replies.
func (s *Service) ClaimForNode(ctx context.Context, n model.Node, req NodeClaim) (*jobexec.Input, error) {
	if req.Protocol != jobexec.Protocol {
		return nil, apperr.Conflict(fmt.Sprintf("节点协议版本 %d 与主端 %d 不一致，请升级节点", req.Protocol, jobexec.Protocol))
	}
	kinds := []string{}
	for _, k := range uniqueStrings(req.Kinds) {
		if jobexec.IsRemoteKind(k) {
			kinds = append(kinds, k)
		}
	}
	if len(kinds) == 0 {
		return nil, apperr.Validation("kinds 至少包含 replicate、analyze、index 中的一项")
	}
	kinds, ocrProviderID, err := s.reachableKinds(ctx, kinds, req.OCRProviderIDs)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	wait := req.Wait
	if wait > maxClaimWait {
		wait = maxClaimWait
	}
	deadline := time.Now().Add(wait)
	owner := claimer{ID: n.ID, Name: n.Name, RemoteOnly: true}
	for ctx.Err() == nil {
		wake := s.wakeSignal()
		var current model.Node
		if err := s.db.WithContext(ctx).Select("enabled", "draining").Where("id = ?", n.ID).First(&current).Error; err != nil {
			if ctx.Err() != nil {
				return nil, nil
			}
			return nil, apperr.Internal(err)
		}
		if !current.Enabled {
			return nil, apperr.Unauthorized("节点已停用")
		}
		if !current.Draining && len(kinds) > 0 {
			job, err := s.claimJobFor(ctx, owner, kinds)
			if err == nil {
				in, err := s.prepareJob(ctx, job, ocrProviderID)
				if err != nil {
					s.handlePrepareError(ctx, job, err)
					continue
				}
				if ctx.Err() != nil { // the worker hung up; nobody would run it
					s.interruptJob(job)
					return nil, nil
				}
				return &in, nil
			}
			if !errors.Is(err, errNoJob) {
				if ctx.Err() != nil {
					return nil, nil
				}
				return nil, apperr.Internal(err)
			}
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, nil
		}
		timer := time.NewTimer(remaining)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, nil
		case <-wake:
			timer.Stop()
		case <-timer.C:
			return nil, nil
		}
	}
	return nil, nil
}

// reachableKinds drops kinds whose required services are unavailable to this
// node. Provider IDs selected in the master UI are an explicit reachability
// declaration; without an explicit list, only a non-internal selected OCR URL
// is assumed reachable.
func (s *Service) reachableKinds(ctx context.Context, kinds, ocrProviderIDs []string) ([]string, string, error) {
	needsOCR := slices.Contains(kinds, model.JobAnalyze) || slices.Contains(kinds, model.JobIndex)
	if !needsOCR {
		return kinds, "", nil
	}
	settings, err := s.ensureAISettings(ctx)
	if err != nil {
		return nil, "", err
	}
	ocrProvider, ocrAvailable, err := s.nodeOCRProvider(ctx, settings, ocrProviderIDs)
	if err != nil {
		return nil, "", err
	}
	var providers []model.AIProvider
	if err := s.db.WithContext(ctx).Select("base_url").Where("enabled = ?", true).Find(&providers).Error; err != nil {
		return nil, "", err
	}
	// Analysis fails over between endpoints, so one reachable endpoint is enough.
	analyzeBlocked := !ocrAvailable || len(providers) == 0
	allModelsInternal := true
	for _, p := range providers {
		allModelsInternal = allModelsInternal && isMasterInternalURL(p.BaseURL)
	}
	analyzeBlocked = analyzeBlocked || allModelsInternal
	// Indexing uses exactly one segmentation endpoint.
	indexBlocked := !ocrAvailable
	segmentationResolved := false
	if search, err := s.ensureSearchSettings(ctx); err == nil {
		if provider, _, err := s.segmentationTarget(ctx, search); err == nil {
			segmentationResolved = true
			indexBlocked = indexBlocked || isMasterInternalURL(provider.BaseURL)
		}
	}
	indexBlocked = indexBlocked || !segmentationResolved
	out := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		if (kind == model.JobAnalyze && analyzeBlocked) || (kind == model.JobIndex && indexBlocked) {
			continue
		}
		out = append(out, kind)
	}
	providerID := ""
	if ocrAvailable {
		providerID = ocrProvider.ID
	}
	return out, providerID, nil
}

// isMasterInternalURL recognises loopback addresses and single-label names,
// which a worker outside the master's network cannot resolve. Private IPs are
// left alone because workers on the same LAN can reach them.
func isMasterInternalURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return host == "localhost" || !strings.Contains(host, ".")
}

func (s *Service) handlePrepareError(ctx context.Context, job model.Job, err error) {
	if errors.Is(err, errJobDone) || errors.Is(err, errLeaseLost) {
		return
	}
	if ctx.Err() != nil {
		s.interruptJob(job)
		return
	}
	message, extra := jobexec.Failure(job.Kind, err)
	var ae *apperr.Error
	if errors.As(err, &ae) {
		message = redact(ae.Message)
	}
	s.logf("prepare %s job failed: %s", job.Kind, message)
	s.recordFailure(job, message, extra)
}

// nodeJob loads a job that the node must currently own.
func (s *Service) nodeJob(ctx context.Context, n model.Node, jobID string) (model.Job, error) {
	var job model.Job
	if err := s.db.WithContext(ctx).Where("id = ?", jobID).First(&job).Error; err != nil {
		return model.Job{}, apperr.NotFound("任务不存在")
	}
	if job.Status != model.JobRunning || job.LeaseOwner != n.ID {
		return job, apperr.Conflict("任务租约已失效")
	}
	return job, nil
}

// NodeProgress records a node's progress message under the job lease.
func (s *Service) NodeProgress(ctx context.Context, n model.Node, jobID, message string, current, total int, metrics map[string]any) error {
	job, err := s.nodeJob(ctx, n, jobID)
	if err != nil {
		return err
	}
	if len(metrics) > 20 {
		metrics = nil
	}
	s.jobProgressWithMetrics(ctx, job, redact(truncateRunes(message, 200)), current, total, metrics)
	return nil
}

// NodeComplete commits a node's result. A repeated delivery after a successful
// commit is acknowledged without writing again.
func (s *Service) NodeComplete(ctx context.Context, n model.Node, jobID string, out jobexec.Result) error {
	job, err := s.nodeJob(ctx, n, jobID)
	if err != nil {
		if job.ID != "" && job.Status == model.JobSucceeded && job.NodeID == n.ID {
			return nil
		}
		return err
	}
	if !out.HasResult(job.Kind) {
		return apperr.Validation("任务结果与任务类型不符")
	}
	if err := s.CommitJob(ctx, job, out); err != nil {
		if errors.Is(err, errLeaseLost) {
			return apperr.Conflict("任务租约已失效")
		}
		s.logf("commit %s job from node %s: %v", job.Kind, n.Name, err)
		s.recordFailure(job, "写入任务结果失败", nil)
		return nil
	}
	s.notify()
	return nil
}

// NodeFail records a failure reported by a node. An interrupted job (for
// example a node shutting down) returns to the queue instead.
func (s *Service) NodeFail(ctx context.Context, n model.Node, jobID, message string, extra map[string]int, interrupted bool) error {
	job, err := s.nodeJob(ctx, n, jobID)
	if err != nil {
		return err
	}
	if interrupted {
		s.interruptJob(job)
		return nil
	}
	message = redact(truncateRunes(message, 300))
	if message == "" {
		message = "节点执行失败"
	}
	s.recordFailure(job, message, extra)
	return nil
}

// NodeSaveRefreshToken stores a OneDrive token rotated on a worker. The
// compare-and-swap in SaveRefreshToken ignores stale rotations.
func (s *Service) NodeSaveRefreshToken(ctx context.Context, n model.Node, driveID, previous, next string) error {
	if strings.TrimSpace(previous) == "" || strings.TrimSpace(next) == "" {
		return apperr.Validation("previous 与 next 不能为空")
	}
	if err := s.SaveRefreshToken(ctx, driveID, previous, next); err != nil {
		return apperr.Internal(err)
	}
	return nil
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
