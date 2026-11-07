package service

import (
	"context"

	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

// LibraryStats summarises the library for the dashboard.
type LibraryStats struct {
	Packs, Files                                                               int64
	Processing, Review, Ready, Failed                                          int64
	MissingAnswers, Trashed                                                    int64
	Archived, Missing, WaitingCatalog, Syncing, Synced, Attention, NotRequired int64
}

type JobStats struct {
	Total, Pending, Running, Succeeded, Failed int64
}

type statusCount struct {
	Status string
	N      int64
}

// Each summary uses one grouped query, without loading list rows or job logs.
func groupedStatusCounts(db *gorm.DB, table any) ([]statusCount, error) {
	var counts []statusCount
	if err := db.Model(table).Select("status, COUNT(*) AS n").Group("status").Scan(&counts).Error; err != nil {
		return nil, wrap("count statuses", err)
	}
	return counts, nil
}

// GetLibraryStats counts packs, files by curation stage and storage health.
func (s *Service) GetLibraryStats(ctx context.Context) (LibraryStats, error) {
	db := s.db.WithContext(ctx)
	var out LibraryStats
	if err := db.Model(&model.Pack{}).Count(&out.Packs).Error; err != nil {
		return out, wrap("count packs", err)
	}
	if err := db.Unscoped().Model(&model.Pack{}).Where("deleted_at IS NOT NULL").Count(&out.Trashed).Error; err != nil {
		return out, wrap("count trash", err)
	}
	if err := s.packQuery(db, PackFilter{Missing: "answers"}).Count(&out.MissingAnswers).Error; err != nil {
		return out, wrap("count incomplete packs", err)
	}
	var stages []struct {
		Stage string
		N     int64
	}
	if err := db.Model(&model.Material{}).Select("stage, COUNT(*) AS n").Group("stage").Scan(&stages).Error; err != nil {
		return out, wrap("count stages", err)
	}
	for _, row := range stages {
		out.Files += row.N
		switch row.Stage {
		case model.StageProcessing:
			out.Processing = row.N
		case model.StageReview:
			out.Review = row.N
		case model.StageReady:
			out.Ready = row.N
		case model.StageFailed:
			out.Failed = row.N
		}
	}
	rows, err := groupedStatusCounts(db, &model.Material{})
	if err != nil {
		return out, err
	}
	for _, row := range rows {
		switch row.Status {
		case model.StatusArchived:
			out.Archived = row.N
		case model.StatusMissing:
			out.Missing = row.N
		}
	}
	var syncRows []struct {
		SyncStatus string
		N          int64
	}
	if err := db.Model(&model.Material{}).Select("sync_status, COUNT(*) AS n").Group("sync_status").Scan(&syncRows).Error; err != nil {
		return out, err
	}
	for _, r := range syncRows {
		switch r.SyncStatus {
		case model.SyncWaitingCatalog:
			out.WaitingCatalog = r.N
		case model.SyncNotRequired:
			out.NotRequired = r.N
		case model.SyncSyncing:
			out.Syncing = r.N
		case model.SyncSynced:
			out.Synced = r.N
		case model.SyncAttention:
			out.Attention = r.N
		}
	}
	return out, nil
}

func (s *Service) GetJobStats(ctx context.Context) (JobStats, error) {
	rows, err := groupedStatusCounts(s.db.WithContext(ctx), &model.Job{})
	if err != nil {
		return JobStats{}, err
	}
	var result JobStats
	for _, row := range rows {
		result.Total += row.N
		switch row.Status {
		case model.JobPending:
			result.Pending = row.N
		case model.JobRunning:
			result.Running = row.N
		case model.JobSucceeded:
			result.Succeeded = row.N
		case model.JobFailed:
			result.Failed = row.N
		}
	}
	return result, nil
}
