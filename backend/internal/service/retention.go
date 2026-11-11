package service

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nic/paper-collect/backend/internal/model"
)

// Retention removes data that nothing references any more. It never touches a
// file younger than retentionGrace, so an import that has published an original
// but not yet committed its row is safe, and it keeps every failed job, which
// a person may still retry.
const (
	retentionGrace    = 24 * time.Hour
	jobRetention      = 30 * 24 * time.Hour
	retentionBatch    = 500
	retentionInterval = 24 * time.Hour
)

var shaName = regexp.MustCompile(`^[0-9a-f]{64}$`)

// RetentionReport counts what one retention pass removed.
type RetentionReport struct {
	OrphanBlobs   int   `json:"orphan_blobs"`
	OrphanRenders int   `json:"orphan_renders"`
	StaleTemp     int   `json:"stale_temp"`
	Jobs          int   `json:"jobs"`
	FreedBytes    int64 `json:"freed_bytes"`
}

// RunRetention runs every retention step once. A failing step is reported but
// does not stop the others.
func (s *Service) RunRetention(ctx context.Context) (RetentionReport, error) {
	var report RetentionReport
	cutoff := s.now().Add(-retentionGrace)
	errs := []error{
		s.sweepOrphanBlobs(ctx, cutoff, &report),
		s.sweepOrphanRenders(ctx, cutoff, &report),
		s.sweepStaleTemp(cutoff, &report),
		s.pruneJobs(ctx, &report),
	}
	return report, errors.Join(errs...)
}

// sweepOrphanBlobs deletes CAS files whose content has no blob row, which is
// what a crash between a purge commit and its file removal leaves behind.
func (s *Service) sweepOrphanBlobs(ctx context.Context, cutoff time.Time, report *RetentionReport) error {
	root := filepath.Join(s.blobs.DataDir(), "blobs")
	return s.sweepShaFiles(ctx, root, "", cutoff, func(shas []string) (map[string]bool, error) {
		var known []string
		err := s.db.WithContext(ctx).Model(&model.Blob{}).Where("sha256 IN ?", shas).Pluck("sha256", &known).Error
		return toSet(known), err
	}, func(n int, size int64) {
		report.OrphanBlobs += n
		report.FreedBytes += size
	})
}

// sweepOrphanRenders deletes cached Word/PowerPoint previews of originals that
// no longer exist.
func (s *Service) sweepOrphanRenders(ctx context.Context, cutoff time.Time, report *RetentionReport) error {
	root := filepath.Join(s.blobs.DataDir(), "renders", "office-v1")
	return s.sweepShaFiles(ctx, root, ".pdf", cutoff, func(shas []string) (map[string]bool, error) {
		var known []string
		err := s.db.WithContext(ctx).Model(&model.Blob{}).Where("sha256 IN ?", shas).Pluck("sha256", &known).Error
		keep := map[string]bool{}
		for _, sha := range known {
			if s.blobs.Exists(sha) {
				keep[sha] = true
			}
		}
		return keep, err
	}, func(n int, size int64) {
		report.OrphanRenders += n
		report.FreedBytes += size
	})
}

type shaFile struct {
	path string
	size int64
}

// sweepShaFiles walks root for files named <sha><suffix> older than cutoff and
// removes those whose sha keep() does not return, checking in batches.
func (s *Service) sweepShaFiles(ctx context.Context, root, suffix string, cutoff time.Time,
	keep func([]string) (map[string]bool, error), removed func(int, int64)) error {
	batch := map[string]shaFile{}
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		shas := make([]string, 0, len(batch))
		for sha := range batch {
			shas = append(shas, sha)
		}
		kept, err := keep(shas)
		if err != nil {
			return err
		}
		for sha, f := range batch {
			if kept[sha] {
				continue
			}
			if err := os.Remove(f.path); err == nil {
				removed(1, f.size)
			} else if !errors.Is(err, fs.ErrNotExist) {
				s.logf("retention: remove %s: %v", filepath.Base(f.path), err)
			}
		}
		clear(batch)
		return nil
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), suffix) {
			return nil
		}
		sha := strings.TrimSuffix(d.Name(), suffix)
		if !shaName.MatchString(sha) {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.ModTime().After(cutoff) {
			return nil
		}
		batch[sha] = shaFile{path: path, size: info.Size()}
		if len(batch) >= retentionBatch {
			return flush()
		}
		return nil
	})
	if err != nil {
		return err
	}
	return flush()
}

// sweepStaleTemp removes partial files left in DATA_DIR/tmp by interrupted
// writes. Upload archives and the pull cache manage their own files.
func (s *Service) sweepStaleTemp(cutoff time.Time, report *RetentionReport) error {
	entries, err := os.ReadDir(filepath.Join(s.blobs.DataDir(), "tmp"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".part") {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		if os.Remove(filepath.Join(s.blobs.DataDir(), "tmp", entry.Name())) == nil {
			report.StaleTemp++
			report.FreedBytes += info.Size()
		}
	}
	return nil
}

// pruneJobs deletes succeeded jobs that finished before the retention window
// and that no upload, file card, set decision or index state points to.
func (s *Service) pruneJobs(ctx context.Context, report *RetentionReport) error {
	before := s.now().Add(-jobRetention)
	db := s.db.WithContext(ctx)
	for {
		var ids []string
		err := db.Model(&model.Job{}).
			Where("status = ? AND finished_at IS NOT NULL AND finished_at < ?", model.JobSucceeded, before).
			Where("NOT EXISTS (SELECT 1 FROM uploads WHERE uploads.job_id = jobs.id)").
			Where("NOT EXISTS (SELECT 1 FROM file_cards WHERE file_cards.job_id = jobs.id)").
			Where("NOT EXISTS (SELECT 1 FROM pack_decisions WHERE pack_decisions.job_id = jobs.id)").
			Where("NOT EXISTS (SELECT 1 FROM material_index_states WHERE material_index_states.job_id = jobs.id)").
			Order("finished_at ASC").Limit(retentionBatch).Pluck("id", &ids).Error
		if err != nil || len(ids) == 0 {
			return err
		}
		res := db.Where("id IN ? AND status = ?", ids, model.JobSucceeded).Delete(&model.Job{})
		if res.Error != nil {
			return res.Error
		}
		report.Jobs += int(res.RowsAffected)
		if len(ids) < retentionBatch {
			return nil
		}
	}
}

func toSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, v := range values {
		out[v] = true
	}
	return out
}
