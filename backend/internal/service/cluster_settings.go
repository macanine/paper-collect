package service

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/jobexec"
	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

const clusterSettingsID = "default"

// ClusterSettingsView is the master-controlled scheduling policy.
type ClusterSettingsView struct {
	Settings model.ClusterSettings
	Kinds    []string
	Slots    map[string]int
}

// ClusterSettingsPatch changes scheduling policy. Nil fields are unchanged.
type ClusterSettingsPatch struct {
	MasterKinds                *[]string
	MasterIngestConcurrency    *int
	MasterReplicateConcurrency *int
	MasterAnalyzeConcurrency   *int
	MasterIndexConcurrency     *int
	LeaseSeconds               *int
	HeartbeatSeconds           *int
	ClaimWaitSeconds           *int
	PollSeconds                *int
}

func defaultClusterSettings() model.ClusterSettings {
	return model.ClusterSettings{
		ID:                         clusterSettingsID,
		MasterKindsJSON:            model.EncodeStrings(append([]string(nil), jobexec.RemoteKinds...)),
		MasterIngestConcurrency:    1,
		MasterReplicateConcurrency: 2,
		MasterAnalyzeConcurrency:   2,
		MasterIndexConcurrency:     2,
		LeaseSeconds:               120,
		HeartbeatSeconds:           10,
		ClaimWaitSeconds:           25,
		PollSeconds:                2,
		Revision:                   1,
	}
}

func (s *Service) ensureClusterSettings(ctx context.Context) (model.ClusterSettings, error) {
	db := s.db.WithContext(ctx)
	var row model.ClusterSettings
	err := db.Where("id = ?", clusterSettingsID).First(&row).Error
	if err == nil {
		return row, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.ClusterSettings{}, apperr.Internal(err)
	}
	now := s.now()
	row = defaultClusterSettings()
	row.CreatedAt, row.UpdatedAt = now, now
	if err := db.Create(&row).Error; err != nil {
		if err := db.Where("id = ?", clusterSettingsID).First(&row).Error; err != nil {
			return model.ClusterSettings{}, apperr.Internal(err)
		}
	}
	return row, nil
}

func clusterSettingsView(row model.ClusterSettings) ClusterSettingsView {
	return ClusterSettingsView{
		Settings: row,
		Kinds:    model.DecodeStrings(row.MasterKindsJSON),
		Slots: map[string]int{
			model.JobIngest:    row.MasterIngestConcurrency,
			model.JobReplicate: row.MasterReplicateConcurrency,
			model.JobAnalyze:   row.MasterAnalyzeConcurrency,
			model.JobIndex:     row.MasterIndexConcurrency,
		},
	}
}

func (s *Service) applyClusterSettings(row model.ClusterSettings) {
	worker := s.workerSettings()
	worker.Kinds = model.DecodeStrings(row.MasterKindsJSON)
	worker.IngestConcurrency = row.MasterIngestConcurrency
	worker.ReplicateConcurrency = row.MasterReplicateConcurrency
	worker.AnalyzeConcurrency = row.MasterAnalyzeConcurrency
	worker.IndexConcurrency = row.MasterIndexConcurrency
	worker.LeaseDuration = time.Duration(row.LeaseSeconds) * time.Second
	worker.HeartbeatInterval = time.Duration(row.HeartbeatSeconds) * time.Second
	worker.ClaimWait = time.Duration(row.ClaimWaitSeconds) * time.Second
	worker.PollInterval = time.Duration(row.PollSeconds) * time.Second
	s.setWorkerSettings(worker)
}

// EnsureClusterSettings creates or loads the master-controlled scheduling row.
// It must run before RegisterMaster starts local runners.
func (s *Service) EnsureClusterSettings(ctx context.Context) (ClusterSettingsView, error) {
	row, err := s.ensureClusterSettings(ctx)
	if err != nil {
		return ClusterSettingsView{}, err
	}
	s.applyClusterSettings(row)
	return clusterSettingsView(row), nil
}

// GetClusterSettings returns the persisted scheduling policy.
func (s *Service) GetClusterSettings(ctx context.Context) (ClusterSettingsView, error) {
	row, err := s.ensureClusterSettings(ctx)
	if err != nil {
		return ClusterSettingsView{}, err
	}
	return clusterSettingsView(row), nil
}

func normalizeMasterKinds(values []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, kind := range values {
		kind = trim(kind)
		if !jobexec.IsRemoteKind(kind) {
			return nil, apperr.Validation("master_kinds 只能包含 replicate、analyze、index")
		}
		if !seen[kind] {
			seen[kind] = true
			out = append(out, kind)
		}
	}
	canonical := []string{model.JobReplicate, model.JobAnalyze, model.JobIndex}
	slices.SortFunc(out, func(a, b string) int {
		return slices.Index(canonical, a) - slices.Index(canonical, b)
	})
	return out, nil
}

func validateClusterSettings(row model.ClusterSettings) error {
	if _, err := normalizeMasterKinds(model.DecodeStrings(row.MasterKindsJSON)); err != nil {
		return err
	}
	for label, value := range map[string]int{
		"master_ingest_concurrency":    row.MasterIngestConcurrency,
		"master_replicate_concurrency": row.MasterReplicateConcurrency,
		"master_analyze_concurrency":   row.MasterAnalyzeConcurrency,
		"master_index_concurrency":     row.MasterIndexConcurrency,
	} {
		if value < 1 || value > 64 {
			return apperr.Validation(label + " 必须在 1 到 64 之间")
		}
	}
	if row.LeaseSeconds < 30 || row.LeaseSeconds > 3600 {
		return apperr.Validation("lease_seconds 必须在 30 到 3600 之间")
	}
	if row.HeartbeatSeconds < 1 || row.HeartbeatSeconds >= row.LeaseSeconds {
		return apperr.Validation("heartbeat_seconds 必须大于等于 1 且小于 lease_seconds")
	}
	if row.ClaimWaitSeconds < 1 || row.ClaimWaitSeconds > 25 || row.ClaimWaitSeconds >= row.LeaseSeconds {
		return apperr.Validation("claim_wait_seconds 必须在 1 到 25 之间且小于 lease_seconds")
	}
	if row.PollSeconds < 1 || row.PollSeconds > 60 {
		return apperr.Validation("poll_seconds 必须在 1 到 60 之间")
	}
	return nil
}

// UpdateClusterSettings persists a policy revision and applies it to local
// runners immediately. Workers receive the same revision on their next heartbeat.
func (s *Service) UpdateClusterSettings(ctx context.Context, p auth.Principal, patch ClusterSettingsPatch) (ClusterSettingsView, error) {
	row, err := s.ensureClusterSettings(ctx)
	if err != nil {
		return ClusterSettingsView{}, err
	}
	if patch.MasterKinds != nil {
		kinds, err := normalizeMasterKinds(*patch.MasterKinds)
		if err != nil {
			return ClusterSettingsView{}, err
		}
		row.MasterKindsJSON = model.EncodeStrings(kinds)
	}
	for _, field := range []struct {
		input *int
		dst   *int
	}{
		{patch.MasterIngestConcurrency, &row.MasterIngestConcurrency},
		{patch.MasterReplicateConcurrency, &row.MasterReplicateConcurrency},
		{patch.MasterAnalyzeConcurrency, &row.MasterAnalyzeConcurrency},
		{patch.MasterIndexConcurrency, &row.MasterIndexConcurrency},
		{patch.LeaseSeconds, &row.LeaseSeconds},
		{patch.HeartbeatSeconds, &row.HeartbeatSeconds},
		{patch.ClaimWaitSeconds, &row.ClaimWaitSeconds},
		{patch.PollSeconds, &row.PollSeconds},
	} {
		if field.input != nil {
			*field.dst = *field.input
		}
	}
	if err := validateClusterSettings(row); err != nil {
		return ClusterSettingsView{}, err
	}
	row.Revision++
	row.UpdatedAt = s.now()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		view := clusterSettingsView(row)
		slots, _ := json.Marshal(localSlots(WorkerSettings{
			Kinds:                view.Kinds,
			ReplicateConcurrency: row.MasterReplicateConcurrency,
			AnalyzeConcurrency:   row.MasterAnalyzeConcurrency,
			IndexConcurrency:     row.MasterIndexConcurrency,
		}))
		if err := tx.Model(&model.Node{}).Where("is_master = ?", true).Updates(map[string]any{
			"kinds_json": view.Settings.MasterKindsJSON,
			"slots_json": string(slots),
			"updated_at": row.UpdatedAt,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.Node{}).Where("is_master = ?", false).Updates(map[string]any{
			"config_revision": gorm.Expr("config_revision + 1"),
			"updated_at":      row.UpdatedAt,
		}).Error; err != nil {
			return err
		}
		return s.audit(tx, p, "cluster.settings_update", row.ID)
	})
	if err != nil {
		return ClusterSettingsView{}, apperr.Internal(err)
	}
	s.applyClusterSettings(row)
	s.restartLocalRunners()
	s.notify()
	return clusterSettingsView(row), nil
}

func (s *Service) clusterRuntimeConfig(n model.Node) NodeRuntimeConfig {
	settings := s.workerSettings()
	slots := decodeSlots(n.ConfiguredSlotsJSON)
	return NodeRuntimeConfig{
		Slots:          slots,
		CacheMaxBytes:  n.ConfiguredCacheMax,
		OCRProviderIDs: append([]string{}, model.DecodeStrings(n.OCRProviderIDsJSON)...),
		HeartbeatMS:    settings.HeartbeatInterval.Milliseconds(),
		ClaimWaitMS:    settings.ClaimWait.Milliseconds(),
		Revision:       n.ConfigRevision,
	}
}

func decodeSlots(raw string) map[string]int {
	out := map[string]int{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return map[string]int{}
	}
	for kind, count := range out {
		if !jobexec.IsRemoteKind(kind) || count < 1 {
			delete(out, kind)
		}
	}
	return out
}

func encodeSlots(slots map[string]int) (string, map[string]int, error) {
	out := map[string]int{}
	for kind, count := range slots {
		kind = trim(kind)
		if !jobexec.IsRemoteKind(kind) {
			return "", nil, apperr.Validation("slots 只能包含 replicate、analyze、index")
		}
		if count < 0 || count > 64 {
			return "", nil, apperr.Validation(kind + " 的槽位必须在 0 到 64 之间")
		}
		if count > 0 {
			out[kind] = count
		}
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return "", nil, apperr.Internal(err)
	}
	return string(raw), out, nil
}

func trim(value string) string {
	return strings.TrimSpace(value)
}
