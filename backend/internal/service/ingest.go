package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"time"

	"github.com/nic/paper-collect/backend/internal/archive"
	"github.com/nic/paper-collect/backend/internal/blob"
	"github.com/nic/paper-collect/backend/internal/model"
	"github.com/nic/paper-collect/backend/internal/paths"
	"gorm.io/gorm"
)

// plannedFile is one file an upload delivered, either directly or from inside
// a package. Path is the full source path (package path + "/" + member path).
type plannedFile struct {
	Path      string
	SHA       string
	Size      int64
	MediaType string
}

type expandedMember struct {
	Path      string
	SHA       string
	Size      int64
	MediaType string
}

// maxNestedArchiveDepth bounds packages inside packages.
const maxNestedArchiveDepth = 3

// RunIngest executes one ingest job. Expansion writes only content-addressed files;
// all catalogue changes happen in a single transaction, so a failed job publishes nothing.
func (s *Service) RunIngest(ctx context.Context, jobID string) {
	db := s.db.WithContext(ctx)
	var job model.Job
	if err := db.Where("id = ?", jobID).First(&job).Error; err != nil || job.Kind != model.JobIngest {
		return
	}
	err := s.ingest(ctx, job)
	if err == nil {
		return
	}
	if ctx.Err() != nil {
		if !errors.Is(context.Cause(ctx), errLeaseLost) {
			s.requeueJob(job.ID, job.LeaseOwner)
		}
		return
	}
	s.logf("ingest job failed: %v", err)
	s.failJob(job.ID, job.LeaseOwner, publicMessage(err), func(tx *gorm.DB) error {
		if job.UploadID == "" {
			return nil
		}
		return tx.Model(&model.Upload{}).Where("id = ? AND job_id = ?", job.UploadID, job.ID).
			Updates(map[string]any{"status": model.UploadFailed, "updated_at": s.now()}).Error
	})
}

func (s *Service) ingest(ctx context.Context, job model.Job) error {
	s.jobProgress(ctx, job, "读取上传清单并检查文件", 0, 0)
	ingestStarted := time.Now()
	db := s.db.WithContext(ctx)
	var up model.Upload
	if err := db.Where("id = ?", job.UploadID).First(&up).Error; err != nil {
		return fmt.Errorf("load upload: %w", err)
	}
	var entries []model.UploadEntry
	if err := db.Where("upload_id = ?", up.ID).Order("position ASC").Find(&entries).Error; err != nil {
		return fmt.Errorf("load entries: %w", err)
	}
	if len(entries) == 0 {
		return &archive.Rejected{Reason: "上传清单为空"}
	}

	var files []plannedFile
	skipped := []string{}
	archives := 0
	var totalBytes int64
	for i, e := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if e.Status != model.EntryReceived && e.Status != model.EntryExisting {
			return &archive.Rejected{Reason: "上传文件尚未全部接收"}
		}
		totalBytes += e.SizeBytes
		if e.MediaType == blob.MediaTypes[blob.KindZIP] {
			archives++
			s.jobProgress(ctx, job, fmt.Sprintf("正在检查并展开压缩包（第 %d / %d 个文件）", i+1, len(entries)), i, len(entries))
			members, skippedMembers, err := s.expandPackage(ctx, job, s.archiveSource(e), e.RelativePath, 1)
			if err != nil {
				return err
			}
			skipped = append(skipped, skippedMembers...)
			for _, member := range members {
				files = append(files, plannedFile(member))
			}
			continue
		}
		files = append(files, plannedFile{Path: e.RelativePath, SHA: e.SHA256, Size: e.SizeBytes, MediaType: e.MediaType})
		if (i+1)%25 == 0 || i+1 == len(entries) {
			s.jobProgress(ctx, job, fmt.Sprintf("已检查 %d / %d 个上传文件", i+1, len(entries)), i+1, len(entries))
		}
	}
	if len(files) == 0 {
		return &archive.Rejected{Reason: "没有可归档的文件"}
	}
	if len(skipped) > 0 {
		s.jobProgress(ctx, job, fmt.Sprintf("压缩包中共有 %d 个不支持的文件，已跳过，未归档", len(skipped)), len(entries), len(entries))
	}
	s.jobProgressWithMetrics(ctx, job, fmt.Sprintf("文件检查完成，正在去重、配套并安排存储（%d 个文件，%d 个压缩包）", len(files), archives), len(entries), len(entries), map[string]any{
		"file_count": len(files), "archive_count": archives, "skipped_count": len(skipped),
		"size_bytes": totalBytes, "duration_ms": time.Since(ingestStarted).Milliseconds(),
	})

	sort.SliceStable(files, func(i, j int) bool { return paths.NaturalLess(files[i].Path, files[j].Path) })
	err := db.Transaction(func(tx *gorm.DB) error {
		return s.publishIngest(tx, job, up, files, skipped)
	})
	if err != nil {
		return err
	}
	// Once the catalogue transaction has committed, new ZIP uploads have no
	// remaining consumers.
	for _, e := range entries {
		if e.MediaType == blob.MediaTypes[blob.KindZIP] {
			if err := os.Remove(s.archiveUploadPath(e.ID, e.SHA256)); err != nil && !errors.Is(err, os.ErrNotExist) {
				s.logf("remove temporary archive: %v", err)
			}
		}
	}
	s.notify()
	return nil
}

func (s *Service) archiveSource(e model.UploadEntry) string {
	zipPath := s.archiveUploadPath(e.ID, e.SHA256)
	if _, err := os.Stat(zipPath); err != nil {
		return s.blobs.Path(e.SHA256)
	}
	return zipPath
}

// publishIngest deduplicates by content, records every arrival as a source
// and queues new files for the AI pipeline, all in one transaction.
func (s *Service) publishIngest(tx *gorm.DB, job model.Job, up model.Upload, files []plannedFile, skipped []string) error {
	now := s.now()
	if err := s.lockJobLease(tx, job.ID, job.LeaseOwner); err != nil {
		return fmt.Errorf("claim ingest job: %w", err)
	}

	// Pass 1: existing content only gains a source. The same content twice in
	// one upload is one new file plus a duplicate source.
	type newFile struct {
		plannedFile
		ID string
	}
	var fresh []newFile
	freshBySHA := map[string]string{}
	duplicates := 0
	var restoredPacks []string
	for _, f := range files {
		var existing model.Material
		err := tx.Unscoped().Where("sha256 = ?", f.SHA).First(&existing).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("find material: %w", err)
		}
		materialID := ""
		switch {
		case err == nil:
			materialID = existing.ID
			if existing.DeletedAt.Valid {
				// Uploading a trashed file again brings it (and its pack) back.
				if err := tx.Unscoped().Model(&model.Material{}).Where("id = ?", existing.ID).Update("deleted_at", nil).Error; err != nil {
					return fmt.Errorf("restore material: %w", err)
				}
				if err := tx.Unscoped().Model(&model.Pack{}).Where("id = ?", existing.PackID).Update("deleted_at", nil).Error; err != nil {
					return fmt.Errorf("restore pack: %w", err)
				}
				restoredPacks = append(restoredPacks, existing.PackID)
			}
		case freshBySHA[f.SHA] != "":
			materialID = freshBySHA[f.SHA]
		}
		if materialID != "" {
			duplicates++
			if err := tx.Create(&model.MaterialSource{ID: newID(), MaterialID: materialID, UploadID: up.ID, UserID: up.UserID,
				Path: f.Path, Duplicate: true, CreatedAt: now}).Error; err != nil {
				return fmt.Errorf("record source: %w", err)
			}
			continue
		}
		id := newID()
		freshBySHA[f.SHA] = id
		fresh = append(fresh, newFile{plannedFile: f, ID: id})
	}

	// New files are archived without any catalogue: the AI reads each one,
	// groups the batch into sets and decides every set. Upload hints are only
	// evidence for the model.
	created := 0
	for _, f := range fresh {
		b, err := s.ensureBlobTx(tx, f.plannedFile, now)
		if err != nil {
			return err
		}
		m := &model.Material{
			ID: f.ID, Part: model.PartOther, OriginalName: path.Base(f.Path), Title: truncateRunes(stripExt(f.Path), maxTitleRunes),
			FileName: path.Base(f.Path), Stage: model.StageProcessing, AIStep: model.StepRead, AttentionJSON: "[]", BatchID: up.ID,
			Status: model.StatusArchived, SyncStatus: model.SyncWaitingCatalog, BlobID: b.ID, MediaType: f.MediaType, SizeBytes: f.Size, SHA256: f.SHA,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(m).Error; err != nil {
			return fmt.Errorf("create material: %w", err)
		}
		if err := tx.Create(&model.MaterialSource{ID: newID(), MaterialID: m.ID, UploadID: up.ID, UserID: up.UserID,
			Path: f.Path, CreatedAt: now}).Error; err != nil {
			return fmt.Errorf("record source: %w", err)
		}
		if err := s.enqueueReadTx(tx, m.ID, now); err != nil {
			return err
		}
		created++
	}
	for _, packID := range restoredPacks {
		if _, err := s.refreshPackNamesTx(tx, packID); err != nil {
			return err
		}
	}
	skippedJSON, _ := json.Marshal(skipped)
	if err := tx.Model(&model.Upload{}).Where("id = ?", up.ID).Updates(map[string]any{
		"status": model.UploadCompleted, "new_count": created, "duplicate_count": duplicates,
		"skipped_json": string(skippedJSON), "updated_at": now,
	}).Error; err != nil {
		return fmt.Errorf("complete upload: %w", err)
	}
	if err := s.finishJobTxWithMetrics(tx, job.ID, job.LeaseOwner, model.JobSucceeded,
		fmt.Sprintf("新增 %d 个文件，%d 个已存在，跳过 %d 个；新文件交给 AI 阅读、分组和编目", created, duplicates, len(skipped)),
		map[string]any{"new_count": created, "duplicate_count": duplicates, "skipped_count": len(skipped)}); err != nil {
		return fmt.Errorf("complete job: %w", err)
	}
	return s.auditSystem(tx, "ingest.complete", up.ID)
}

// ensureBlobTx returns the storage object for a content hash, creating it once.
func (s *Service) ensureBlobTx(tx *gorm.DB, f plannedFile, now time.Time) (model.Blob, error) {
	var b model.Blob
	err := tx.Where("sha256 = ?", f.SHA).First(&b).Error
	if err == nil {
		return b, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return b, fmt.Errorf("find blob: %w", err)
	}
	b = model.Blob{ID: newID(), SHA256: f.SHA, SizeBytes: f.Size, MediaType: f.MediaType, FileName: path.Base(f.Path), CreatedAt: now}
	if err := tx.Create(&b).Error; err != nil {
		return b, fmt.Errorf("create blob: %w", err)
	}
	return b, nil
}

// expandPackage validates a ZIP completely, then extracts its supported members into the CAS.
// Each member's real format must match its extension, otherwise the whole package is rejected.
func (s *Service) expandPackage(ctx context.Context, job model.Job, zipPath, prefix string, depth int) ([]expandedMember, []string, error) {
	members, skippedNames, err := archive.InspectWithSkipped(zipPath, s.limits)
	if err != nil {
		return nil, nil, err
	}
	skipped := make([]string, 0, len(skippedNames))
	for _, name := range skippedNames {
		skipped = append(skipped, prefix+"/"+name)
	}
	if len(members) == 0 {
		if depth > 1 {
			return nil, skipped, nil
		}
		return nil, skipped, &archive.Rejected{Reason: "压缩包中没有支持归档的文件"}
	}
	s.jobProgress(ctx, job, fmt.Sprintf("压缩包检查通过，准备展开 %d 个受支持的文件", len(members)), 0, len(members))
	var out []expandedMember
	var nested []struct{ path, tmp string }
	defer func() {
		for _, n := range nested {
			_ = os.Remove(n.tmp)
		}
	}()
	done := 0
	err = archive.Extract(zipPath, members, func(m archive.Member, r io.Reader) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		tmp, err := s.blobs.NewTemp()
		if err != nil {
			return err
		}
		tmpPath := tmp.Name()
		n, sum, copyErr := blob.CopyHashed(tmp, ingestContextReader{ctx: ctx, reader: r})
		closeErr := tmp.Close()
		if copyErr == nil {
			copyErr = closeErr
		}
		if copyErr != nil {
			_ = os.Remove(tmpPath)
			return copyErr
		}
		kind, sniffErr := blob.Sniff(tmpPath)
		expected, _ := blob.ExtensionKind(m.Path)
		if sniffErr != nil || !blob.Compatible(kind, expected) {
			_ = os.Remove(tmpPath)
			skipped = append(skipped, prefix+"/"+m.Path)
			return nil
		}
		done++
		if kind == blob.KindZIP {
			if depth >= maxNestedArchiveDepth {
				_ = os.Remove(tmpPath)
				skipped = append(skipped, prefix+"/"+m.Path)
				return nil
			}
			nested = append(nested, struct{ path, tmp string }{prefix + "/" + m.Path, tmpPath})
			return nil
		}
		if err := s.blobs.Publish(tmpPath, sum); err != nil {
			return err
		}
		out = append(out, expandedMember{Path: prefix + "/" + m.Path, SHA: sum, Size: n, MediaType: blob.MediaTypes[expected]})
		if done%25 == 0 || done == len(members) {
			s.jobProgress(ctx, job, fmt.Sprintf("压缩包展开进度：%d / %d 个文件", done, len(members)), done, len(members))
		}
		return nil
	})
	if err != nil {
		return nil, skipped, err
	}
	for _, n := range nested {
		inner, innerSkipped, err := s.expandPackage(ctx, job, n.tmp, n.path, depth+1)
		if err != nil {
			var rejected *archive.Rejected
			if errors.As(err, &rejected) {
				skipped = append(skipped, n.path)
				continue
			}
			return nil, skipped, err
		}
		out = append(out, inner...)
		skipped = append(skipped, innerSkipped...)
	}
	if len(out) == 0 && depth == 1 {
		return nil, skipped, &archive.Rejected{Reason: "压缩包中没有可归档的文件"}
	}
	return out, skipped, nil
}

type ingestContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r ingestContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

// failJob marks a job failed and optionally applies related projection writes
// in the same transaction, guarded by the job lease.
func (s *Service) failJob(jobID, owner, message string, projections ...func(*gorm.DB) error) bool {
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.finishJobTx(tx, jobID, owner, model.JobFailed, message); err != nil {
			return err
		}
		for _, projection := range projections {
			if err := projection(tx); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if !errors.Is(err, errLeaseLost) {
			s.logf("fail job: %v", err)
		}
		return false
	}
	return true
}

// requeueJob returns an interrupted job to pending and applies related
// projection resets atomically.
func (s *Service) requeueJob(jobID, owner string, projections ...func(*gorm.DB) error) bool {
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.finishJobTx(tx, jobID, owner, model.JobPending, ""); err != nil {
			return err
		}
		for _, projection := range projections {
			if err := projection(tx); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		s.notify()
		return true
	}
	return false
}
