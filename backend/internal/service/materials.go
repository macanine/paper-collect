package service

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

// MaterialFilter holds optional list filters. Empty fields are ignored.
type MaterialFilter struct {
	Q       string
	Subject string
	Role    string
	Status  string
	Grade   string
}

// MaterialView is a material with its subject codes and, for detail views, its replicas.
type MaterialView struct {
	Material model.Material
	Subjects []string
	Replicas []model.Replica
}

// MaterialPatch is a partial update. Nil means "leave unchanged".
type MaterialPatch struct {
	Title        *string
	Description  *string
	SubjectCodes *[]string
	Grade        *string
	AcademicYear *int
	Tags         *[]string
	Role         *string
}

func (pt MaterialPatch) empty() bool {
	return pt.Title == nil && pt.Description == nil && pt.SubjectCodes == nil &&
		pt.Grade == nil && pt.AcademicYear == nil && pt.Tags == nil && pt.Role == nil
}

func (s *Service) materialQuery(db *gorm.DB, f MaterialFilter) *gorm.DB {
	q := db.Model(&model.Material{})
	if f.Q != "" {
		pat := likePattern(f.Q)
		q = q.Where("(LOWER(title) LIKE ? ESCAPE '!' OR LOWER(description) LIKE ? ESCAPE '!')", pat, pat)
	}
	if f.Subject != "" {
		sub := db.Model(&model.MaterialSubject{}).Select("material_id").Where("code = ?", f.Subject)
		q = q.Where("id IN (?)", sub)
	}
	if f.Role != "" {
		q = q.Where("role = ?", f.Role)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.Grade != "" {
		q = q.Where("grade = ?", f.Grade)
	}
	return q
}

// ListMaterials returns a filtered, paginated list. Subject matching uses the join table, so a
// comprehensive material matches only when its own subject list contains the filter.
func (s *Service) ListMaterials(ctx context.Context, f MaterialFilter, pg PageQuery) (Page[MaterialView], error) {
	db := s.db.WithContext(ctx)
	var total int64
	if err := s.materialQuery(db, f).Count(&total).Error; err != nil {
		return Page[MaterialView]{}, wrap("count materials", err)
	}
	rows := []model.Material{}
	if err := s.materialQuery(db, f).Order("created_at DESC").Order("id DESC").
		Limit(pg.PageSize).Offset(pg.offset()).Find(&rows).Error; err != nil {
		return Page[MaterialView]{}, wrap("list materials", err)
	}
	ids := make([]string, 0, len(rows))
	for _, m := range rows {
		ids = append(ids, m.ID)
	}
	subjects, err := s.subjectsByMaterial(db, ids)
	if err != nil {
		return Page[MaterialView]{}, err
	}
	items := make([]MaterialView, 0, len(rows))
	for _, m := range rows {
		items = append(items, MaterialView{Material: m, Subjects: subjects[m.ID]})
	}
	return newPage(items, total, pg), nil
}

// GetMaterial returns one material with its blob's replicas. Share URLs are included;
// the caller must hold materials:read.
func (s *Service) GetMaterial(ctx context.Context, id string) (MaterialView, error) {
	db := s.db.WithContext(ctx)
	var m model.Material
	if err := db.Where("id = ?", id).First(&m).Error; err != nil {
		return MaterialView{}, apperr.NotFound("资料不存在")
	}
	subjects, err := s.subjectsByMaterial(db, []string{m.ID})
	if err != nil {
		return MaterialView{}, err
	}
	reps := []model.Replica{}
	if err := db.Where("blob_id = ?", m.BlobID).Order("created_at ASC").Order("id ASC").Find(&reps).Error; err != nil {
		return MaterialView{}, wrap("load replicas", err)
	}
	return MaterialView{Material: m, Subjects: subjects[m.ID], Replicas: reps}, nil
}

// UpdateMaterial applies a partial update. Blob, hash, source path and status are not editable.
func (s *Service) UpdateMaterial(ctx context.Context, p auth.Principal, id string, pt MaterialPatch) (MaterialView, error) {
	if pt.empty() {
		return MaterialView{}, apperr.Validation("至少需要提供一个可修改字段")
	}
	var oldFileName, newFileName string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		oldFileName, newFileName, err = s.applyMaterialPatchTx(tx, id, pt, s.now())
		if err != nil {
			return err
		}
		return s.audit(tx, p, "material.update", id)
	})
	if err != nil {
		return MaterialView{}, err
	}
	if newFileName != "" && newFileName != oldFileName {
		s.renameLocalReplicaFiles(ctx, id, newFileName)
	}
	return s.GetMaterial(ctx, id)
}

// applyMaterialPatchTx validates and writes the same whitelisted fields as the
// public material PATCH endpoint. It is shared by manual edits and automatic
// application of trusted-after-validation AI suggestions.
func (s *Service) applyMaterialPatchTx(tx *gorm.DB, id string, pt MaterialPatch, now time.Time) (string, string, error) {
	if pt.empty() {
		return "", "", apperr.Validation("至少需要提供一个可修改字段")
	}
	updates := map[string]any{}
	var newSubjects []string
	var newTitle string
	if pt.Title != nil {
		t, err := validateTitle(*pt.Title)
		if err != nil {
			return "", "", err
		}
		newTitle = t
		updates["title"] = t
	}
	if pt.Description != nil {
		d, err := validateDescription(*pt.Description)
		if err != nil {
			return "", "", err
		}
		updates["description"] = d
	}
	if pt.SubjectCodes != nil {
		codes, err := validateOptionalSubjects(*pt.SubjectCodes)
		if err != nil {
			return "", "", err
		}
		newSubjects = codes
	}
	if pt.Grade != nil {
		if err := validateGrade(*pt.Grade); err != nil {
			return "", "", err
		}
		updates["grade"] = *pt.Grade
	}
	if pt.AcademicYear != nil {
		if err := validateYear(*pt.AcademicYear); err != nil {
			return "", "", err
		}
		updates["academic_year"] = *pt.AcademicYear
	}
	if pt.Tags != nil {
		tags, err := validateTags(*pt.Tags)
		if err != nil {
			return "", "", err
		}
		updates["tags_json"] = model.EncodeStrings(tags)
	}
	var newRole string
	if pt.Role != nil {
		if err := validateRole(*pt.Role); err != nil {
			return "", "", err
		}
		newRole = *pt.Role
		updates["role"] = newRole
	}
	updates["updated_at"] = now

	var m model.Material
	if err := tx.Where("id = ?", id).First(&m).Error; err != nil {
		return "", "", apperr.NotFound("资料不存在")
	}
	oldFileName := m.FileName
	newFileName := ""
	if newTitle != "" {
		newFileName = fileNameFromTitle(newTitle, m.FileName)
		if newFileName != m.FileName {
			updates["file_name"] = newFileName
		}
	}
	oldRole := m.Role
	if err := tx.Model(&model.Material{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return "", "", wrap("update material", err)
	}
	// Collection-item roles default to the material role at ingest. When the
	// material role changes, follow only those items still on the old default;
	// explicitly customised item roles are left untouched.
	if newRole != "" && oldRole != "" && newRole != oldRole {
		if err := tx.Model(&model.CollectionItem{}).
			Where("material_id = ? AND role = ?", id, oldRole).
			Update("role", newRole).Error; err != nil {
			return "", "", wrap("sync collection item roles", err)
		}
	}
	if newRole != "" {
		if err := s.syncMaterialIndexForRoleTx(tx, id, newRole, now); err != nil {
			return "", "", wrap("sync material index state", err)
		}
	}
	if newSubjects != nil {
		if err := tx.Where("material_id = ?", id).Delete(&model.MaterialSubject{}).Error; err != nil {
			return "", "", wrap("clear subjects", err)
		}
		if err := insertMaterialSubjects(tx, id, newSubjects); err != nil {
			return "", "", err
		}
	}
	return oldFileName, newFileName, nil
}

// fileNameFromTitle keeps one canonical name: the download/storage filename
// follows the editable title, preserving the original extension.
func fileNameFromTitle(title, oldFileName string) string {
	ext := filepath.Ext(oldFileName)
	name := strings.TrimSpace(title)
	if ext != "" && !strings.EqualFold(filepath.Ext(name), ext) {
		name += ext
	}
	if n := runeLen(name); n > 255 {
		keep := 255 - runeLen(ext)
		if keep < 1 {
			keep = 255
		}
		name = string([]rune(name)[:keep]) + ext
	}
	return name
}

// renameLocalReplicaFiles keeps local provider copies in sync when a material
// is renamed. Remote adapters have no rename capability, so their objects keep
// the name used at upload time and only the catalogue name changes.
func (s *Service) renameLocalReplicaFiles(ctx context.Context, materialID, newFileName string) {
	db := s.db.WithContext(ctx)
	var material model.Material
	if err := db.Where("id = ?", materialID).First(&material).Error; err != nil {
		return
	}
	var replicas []model.Replica
	if err := db.Where("blob_id = ?", material.BlobID).Find(&replicas).Error; err != nil {
		return
	}
	for _, replica := range replicas {
		var provider model.StorageProvider
		if err := db.Where("id = ?", replica.ProviderID).First(&provider).Error; err != nil || provider.Kind != model.KindLocal {
			continue
		}
		root := filepath.Join(s.blobs.DataDir(), "providers", provider.ID)
		oldPath := filepath.Join(root, filepath.FromSlash(replica.RemoteKey))
		newKey := objectKey(material.SHA256, safeRemoteName(newFileName))
		newPath := filepath.Join(root, filepath.FromSlash(newKey))
		if oldPath == newPath {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(newPath), 0o700); err != nil {
			continue
		}
		if err := os.Rename(oldPath, newPath); err != nil {
			continue
		}
		_ = db.Model(&model.Replica{}).Where("id = ?", replica.ID).Updates(map[string]any{
			"remote_key": newKey, "remote_name": safeRemoteName(newFileName), "updated_at": s.now(),
		}).Error
	}
}

func insertMaterialSubjects(tx *gorm.DB, materialID string, codes []string) error {
	if len(codes) == 0 {
		return nil
	}
	rows := make([]model.MaterialSubject, 0, len(codes))
	for _, c := range codes {
		rows = append(rows, model.MaterialSubject{MaterialID: materialID, Code: c})
	}
	return wrap("insert subjects", tx.Create(&rows).Error)
}

// subjectsByMaterial loads subject codes in dictionary order, in chunks to stay within bind limits.
func (s *Service) subjectsByMaterial(db *gorm.DB, ids []string) (map[string][]string, error) {
	out := map[string][]string{}
	order := map[string]int{}
	for i, sub := range model.Subjects {
		order[sub.Code] = i
	}
	for start := 0; start < len(ids); start += 500 {
		end := start + 500
		if end > len(ids) {
			end = len(ids)
		}
		var rows []model.MaterialSubject
		if err := db.Where("material_id IN ?", ids[start:end]).Find(&rows).Error; err != nil {
			return nil, wrap("load subjects", err)
		}
		for _, r := range rows {
			out[r.MaterialID] = append(out[r.MaterialID], r.Code)
		}
	}
	for id := range out {
		codes := out[id]
		sort.Slice(codes, func(i, j int) bool { return order[codes[i]] < order[codes[j]] })
		out[id] = codes
	}
	for _, id := range ids {
		if _, ok := out[id]; !ok {
			out[id] = []string{}
		}
	}
	return out, nil
}
