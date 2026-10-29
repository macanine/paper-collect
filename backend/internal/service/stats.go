package service

import (
	"context"

	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

type MaterialStats struct {
	Total, PendingStorage, Archived, Degraded int64
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

func (s *Service) GetMaterialStats(ctx context.Context) (MaterialStats, error) {
	rows, err := groupedStatusCounts(s.db.WithContext(ctx), &model.Material{})
	if err != nil {
		return MaterialStats{}, err
	}
	var result MaterialStats
	for _, row := range rows {
		result.Total += row.N
		switch row.Status {
		case model.StatusPendingStorage:
			result.PendingStorage = row.N
		case model.StatusArchived:
			result.Archived = row.N
		case model.StatusDegraded:
			result.Degraded = row.N
		}
	}
	return result, nil
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
