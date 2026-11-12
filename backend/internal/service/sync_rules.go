package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"
	"unicode"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/jobexec"
	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

type SyncTarget struct {
	DriveID      string `json:"drive_id"`
	PathTemplate string `json:"path_template"`
	Share        bool   `json:"share"`
	Required     bool   `json:"required"`
}
type SyncRule struct {
	ID         string               `json:"id"`
	Name       string               `json:"name"`
	Enabled    bool                 `json:"enabled"`
	IsDefault  bool                 `json:"is_default"`
	Conditions model.RuleConditions `json:"conditions"`
	Targets    []SyncTarget         `json:"targets"`
}

// SyncAction is one planned step for one file on one drive. Status, Error and
// ShareStatus describe the replica as it is now, before the step runs.
type SyncAction struct {
	MaterialID  string        `json:"material_id"`
	FileName    string        `json:"file_name"`
	RuleID      string        `json:"rule_id"`
	RuleName    string        `json:"rule_name"`
	DriveID     string        `json:"drive_id"`
	ReplicaID   string        `json:"replica_id"`
	Action      string        `json:"action"`
	DesiredPath string        `json:"desired_path"`
	RemotePath  string        `json:"remote_path"`
	Reason      string        `json:"reason"`
	Status      string        `json:"status"`
	Error       string        `json:"error"`
	ShareStatus string        `json:"share_status"`
	Share       bool          `json:"share"`
	Required    bool          `json:"required"`
	Replica     model.Replica `json:"-"`
}
type SyncPreview struct {
	PreviewToken string         `json:"preview_token"`
	Counts       map[string]int `json:"counts"`
	Items        []SyncAction   `json:"items"`
	Total        int            `json:"total"`
}

// All planners serialize before reading rules or reserving paths. SQLite uses
// its single immediate writer; PostgreSQL uses a transaction advisory lock.
func lockSyncTx(tx *gorm.DB) error {
	if tx.Dialector.Name() == "postgres" {
		return tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(0x73796e6372756c65)).Error
	}
	return nil
}
func loadRules(tx *gorm.DB) ([]SyncRule, error) {
	rows := []model.Rule{}
	if err := tx.Order("is_default ASC").Order("position ASC").Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := []SyncRule{}
	for _, r := range rows {
		item := SyncRule{ID: r.ID, Name: r.Name, Enabled: r.Enabled, IsDefault: r.IsDefault, Targets: []SyncTarget{}}
		if err := json.Unmarshal([]byte(r.ConditionsJSON), &item.Conditions); err != nil {
			return nil, err
		}
		var targets []model.RuleTarget
		if err := tx.Where("rule_id = ?", r.ID).Order("position ASC").Find(&targets).Error; err != nil {
			return nil, err
		}
		for _, t := range targets {
			item.Targets = append(item.Targets, SyncTarget{t.DriveID, t.PathTemplate, t.Share, t.Required})
		}
		out = append(out, item)
	}
	return out, nil
}
func (s *Service) ListRules(ctx context.Context) ([]SyncRule, error) {
	return loadRules(s.db.WithContext(ctx))
}
func matchesRule(c model.RuleConditions, m model.Material, p model.Pack) bool {
	any := func(want, have []string) bool {
		if len(want) == 0 {
			return true
		}
		for _, a := range want {
			for _, b := range have {
				if a == b {
					return true
				}
			}
		}
		return false
	}
	cohort := len(c.Cohorts) == 0
	for _, v := range c.Cohorts {
		if v == p.Cohort {
			cohort = true
		}
	}
	return any(c.Subjects, model.DecodeStrings(p.SubjectsJSON)) && any(c.DocTypes, []string{p.DocType}) && any(c.Grades, []string{p.Grade}) && cohort && any(c.Regions, []string{p.Region}) && any(c.Tags, model.DecodeStrings(p.TagsJSON)) && any(c.Parts, []string{m.Part}) && any(c.MediaTypes, []string{m.MediaType}) && m.SizeBytes >= c.MinSize && (c.MaxSize == 0 || m.SizeBytes <= c.MaxSize)
}

var templateKeys = []string{"学科", "年级", "学期", "届别", "地区", "机构", "类型", "考试年月", "套卷", "部件", "文件名", "sha8"}

func validateTemplate(t string) error {
	if t == "" || len(t) > 2048 || strings.HasPrefix(t, "/") || strings.Contains(t, "\\") || !strings.HasSuffix(t, "{文件名}") {
		return apperr.Validation("路径模板必须为相对路径，并以 {文件名} 结尾")
	}
	for _, seg := range strings.Split(t, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return apperr.Validation("路径模板包含无效目录")
		}
	}
	rest := t
	for _, k := range templateKeys {
		rest = strings.ReplaceAll(rest, "{"+k+"}", "x")
	}
	if strings.ContainsAny(rest, "{}") {
		return apperr.Validation("路径模板包含未知变量")
	}
	return nil
}
func cleanSyncSegment(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "." || s == ".." {
		return "", apperr.Validation("路径不能包含 . 或 ..")
	}
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if s == "" {
		s = "未知"
	}
	if len([]rune(s)) > 100 {
		ext := path.Ext(s)
		if len([]rune(ext)) > 20 {
			ext = ""
		}
		s = truncateRunes(strings.TrimSuffix(s, ext), 100-len([]rune(ext))) + ext
	}
	return s, nil
}
func renderSyncPath(t string, m model.Material, p model.Pack) (string, error) {
	subjects := model.DecodeStrings(p.SubjectsJSON)
	sub := "未分学科"
	if len(subjects) == 1 {
		sub = subjectLabel(subjects[0])
	}
	if len(subjects) > 1 {
		sub = "综合"
	}
	name := m.FileName
	if name == "" {
		name = m.OriginalName
	}
	cohort := ""
	if p.Cohort > 0 {
		cohort = fmt.Sprintf("%d届", p.Cohort)
	}
	values := map[string]string{"学科": sub, "年级": gradeLabels[p.Grade], "学期": termLabels[p.Term], "届别": cohort, "地区": p.Region, "机构": p.Organization, "类型": docTypeLabels[p.DocType], "考试年月": p.ExamDate, "套卷": p.Title, "部件": partLabels[m.Part], "文件名": name, "sha8": truncateRunes(m.SHA256, 8)}
	if values["类型"] == "" {
		values["类型"] = "未分类"
	}
	if values["套卷"] == "" {
		values["套卷"] = "未命名资料"
	}
	if values["部件"] == "" {
		values["部件"] = "其他"
	}
	// Clean values before substitution: catalogue slashes never introduce folders.
	for k, v := range values {
		v, e := cleanSyncSegment(v)
		if e != nil {
			return "", e
		}
		values[k] = v
	}
	parts := strings.Split(t, "/")
	for i, seg := range parts {
		for _, k := range templateKeys {
			seg = strings.ReplaceAll(seg, "{"+k+"}", values[k])
		}
		var e error
		parts[i], e = cleanSyncSegment(seg)
		if e != nil {
			return "", e
		}
	}
	return strings.Join(parts, "/"), nil
}
func validateRules(tx *gorm.DB, rules []SyncRule) error {
	if len(rules) == 0 || len(rules) > 100 {
		return apperr.Validation("需要 1 到 100 条规则，最后一条必须为兜底规则")
	}
	ids := map[string]bool{}
	for i, r := range rules {
		if r.ID == "" || ids[r.ID] || len(r.ID) > 36 {
			return apperr.Validation("规则 ID 必须唯一且不超过 36 字符")
		}
		ids[r.ID] = true
		if strings.TrimSpace(r.Name) == "" || runeLen(r.Name) > 128 {
			return apperr.Validation("规则名称必须为 1 到 128 字符")
		}
		if r.IsDefault != (i == len(rules)-1) || r.IsDefault && (!r.Enabled || !emptyConditions(r.Conditions)) {
			return apperr.Validation("最后一条必须为启用的无条件兜底规则")
		}
		c := r.Conditions
		if c.MinSize < 0 || c.MaxSize < 0 || c.MaxSize > 0 && c.MaxSize < c.MinSize {
			return apperr.Validation("文件大小范围无效")
		}
		for _, v := range c.Subjects {
			if !model.IsSubject(v) {
				return apperr.Validation("学科条件无效")
			}
		}
		for _, group := range []struct{ values, allowed []string }{{c.DocTypes, model.DocTypes}, {c.Grades, model.Grades}, {c.Parts, model.MaterialParts}} {
			for _, v := range group.values {
				if !isOneOf(v, group.allowed...) {
					return apperr.Validation("规则条件值无效")
				}
			}
		}
		seen := map[string]bool{}
		for _, t := range r.Targets {
			if seen[t.DriveID] {
				return apperr.Validation("同一规则不能重复指定网盘")
			}
			seen[t.DriveID] = true
			var d model.Drive
			if err := tx.Where("id = ?", t.DriveID).First(&d).Error; err != nil {
				return apperr.Validation("目标网盘不存在")
			}
			if d.Kind == model.KindPan123 {
				return apperr.Validation("123 云盘尚不支持同步")
			}
			if err := validateTemplate(t.PathTemplate); err != nil {
				return err
			}
		}
	}
	return nil
}
func emptyConditions(c model.RuleConditions) bool {
	return len(c.Subjects)+len(c.DocTypes)+len(c.Grades)+len(c.Cohorts)+len(c.Regions)+len(c.Tags)+len(c.Parts)+len(c.MediaTypes) == 0 && c.MinSize == 0 && c.MaxSize == 0
}

// desiredActions is shared by preview and application; reservations include both
// actual and desired paths, including orphaned copies and earlier preview rows.
func (s *Service) desiredActions(tx *gorm.DB, m model.Material, rules []SyncRule, reserved map[string]string) ([]SyncAction, error) {
	var p model.Pack
	if err := tx.Where("id = ?", m.PackID).First(&p).Error; err != nil {
		return nil, err
	}
	var existing []model.Replica
	if err := tx.Where("blob_id = ?", m.BlobID).Find(&existing).Error; err != nil {
		return nil, err
	}
	have := map[string]model.Replica{}
	for _, r := range existing {
		have[r.DriveID] = r
	}
	var rule SyncRule
	for _, r := range rules {
		if r.Enabled && matchesRule(r.Conditions, m, p) {
			rule = r
			break
		}
	}
	out := []SyncAction{}
	for _, target := range rule.Targets {
		var d model.Drive
		if err := tx.Where("id = ?", target.DriveID).First(&d).Error; err != nil {
			return nil, err
		}
		key, err := renderSyncPath(target.PathTemplate, m, p)
		if err != nil {
			return nil, err
		}
		if d.Kind == model.KindLanzou {
			key = path.Base(key)
		} else if d.RootPath != "" {
			key = d.RootPath + "/" + key
		}
		// A copy stored beside a taken path keeps the suffixed name: adopting it
		// avoids planning a move onto the occupied path on every re-sync.
		if r, ok := have[d.ID]; ok && r.Status == model.ReplicaReady && r.RemotePath != "" &&
			r.RemotePath == jobexec.ConflictPath(key, m.SHA256) {
			key = r.RemotePath
		}
		base := key
		for attempt := 0; ; attempt++ {
			owner := reserved[d.ID+"\x00"+key]
			if owner == "" || owner == m.BlobID {
				break
			}
			suffix := " (" + truncateRunes(m.SHA256, 8) + ")"
			if attempt > 0 {
				suffix = fmt.Sprintf(" (%s-%d)", truncateRunes(m.SHA256, 8), attempt)
			}
			ext := path.Ext(base)
			stem := strings.TrimSuffix(path.Base(base), ext)
			name := truncateRunes(stem, 100-runeLen(suffix)-runeLen(ext)) + suffix + ext
			key = path.Join(path.Dir(base), name)
		}
		reserved[d.ID+"\x00"+key] = m.BlobID
		r, ok := have[d.ID]
		delete(have, d.ID)
		if !ok {
			r = model.Replica{ID: newID(), BlobID: m.BlobID, DriveID: d.ID, SizeBytes: m.SizeBytes, CreatedAt: s.now()}
		}
		a := SyncAction{MaterialID: m.ID, FileName: m.FileName, RuleID: rule.ID, RuleName: rule.Name, DriveID: d.ID, DesiredPath: key, RemotePath: r.RemotePath, Action: "unchanged",
			ReplicaID: r.ID, Status: r.Status, Error: r.Error, ShareStatus: r.ShareStatus, Share: target.Share, Required: target.Required}
		r.RuleID = rule.ID
		r.DesiredPath = key
		r.Share = target.Share
		r.Required = target.Required
		r.SkipReason = ""
		limit := d.MaxFileBytes
		if d.Kind == model.KindLanzou && (limit == 0 || limit > 100<<20) {
			limit = 100 << 20
		}
		switch {
		case !d.Enabled:
			a.Reason = "网盘已停用"
		case limit > 0 && m.SizeBytes > limit:
			a.Reason = "超过大小上限"
		case len(model.DecodeStrings(d.AllowedExtsJSON)) > 0 && !isOneOf(strings.ToLower(path.Ext(m.FileName)), model.DecodeStrings(d.AllowedExtsJSON)...):
			a.Reason = "扩展名不允许"
		}
		if a.Reason != "" {
			a.Action = "skip"
			r.Status = model.ReplicaSkipped
			r.SkipReason = a.Reason
		} else if r.RemoteID == "" {
			a.Action = "upload"
			r.Status = model.ReplicaPending
		} else if r.RemotePath != key {
			if d.OnPathChange == model.OnPathChangeMove {
				a.Action = "move"
				r.Status = model.ReplicaPending
			} else {
				a.Action = "drift"
				r.Status = model.ReplicaReady
				a.Reason = "网盘设置为保持原位"
			}
		} else if target.Share && r.ShareStatus != model.ShareStatusReady && r.ShareStatus != model.ShareStatusUnsupported {
			a.Action = "share"
			r.Status = model.ReplicaPending
		} else {
			r.Status = model.ReplicaReady
		}
		// An active worker owns the actual operation. It will persist its result,
		// then plan again using the latest desired fields.
		for _, old := range existing {
			if old.ID == r.ID && (old.Status == model.ReplicaUploading || old.Status == model.ReplicaMoving) {
				r.Status = old.Status
			}
		}
		r.UpdatedAt = s.now()
		a.Replica = r
		out = append(out, a)
	}
	for _, old := range existing {
		r, ok := have[old.DriveID]
		if !ok {
			continue
		}
		a := SyncAction{MaterialID: m.ID, FileName: m.FileName, DriveID: r.DriveID, RemotePath: r.RemotePath, DesiredPath: r.DesiredPath, Action: "orphan",
			ReplicaID: r.ID, Status: r.Status, Error: r.Error, ShareStatus: r.ShareStatus}
		busy := r.Status == model.ReplicaUploading || r.Status == model.ReplicaMoving
		if r.RemoteID == "" && !busy {
			// Nothing was ever written to the drive, so there is nothing to keep.
			a.Action = "cancel"
		} else if r.Status == model.ReplicaOrphaned {
			a.Action = "unchanged"
		}
		r.RuleID = ""
		r.Status = model.ReplicaOrphaned
		r.UpdatedAt = s.now()
		a.Replica = r
		out = append(out, a)
	}
	return out, nil
}
func loadReservations(tx *gorm.DB) (map[string]string, error) {
	var rows []model.Replica
	if err := tx.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, r := range rows {
		for _, p := range []string{r.DesiredPath, r.RemotePath} {
			if p != "" {
				out[r.DriveID+"\x00"+p] = r.BlobID
			}
		}
	}
	return out, nil
}
func (s *Service) planMaterialTx(tx *gorm.DB, id string, force bool) (int, error) {
	if err := lockSyncTx(tx); err != nil {
		return 0, err
	}
	var m model.Material
	if err := tx.Where("id = ?", id).First(&m).Error; err != nil {
		return 0, err
	}
	if !force && m.SyncStatus == model.SyncWaitingCatalog {
		return 0, nil
	}
	rules, err := loadRules(tx)
	if err != nil {
		return 0, err
	}
	reserved, err := loadReservations(tx)
	if err != nil {
		return 0, err
	}
	actions, err := s.desiredActions(tx, m, rules, reserved)
	if err != nil {
		return 0, err
	}
	queued := 0
	for _, a := range actions {
		r := a.Replica
		if a.Action == "cancel" {
			if err := tx.Model(&model.Job{}).Where("replica_id = ? AND status = ?", r.ID, model.JobPending).Updates(map[string]any{"status": model.JobSucceeded, "progress_message": "同步目标已撤销", "finished_at": s.now()}).Error; err != nil {
				return 0, err
			}
			if err := tx.Delete(&model.Replica{}, "id = ?", r.ID).Error; err != nil {
				return 0, err
			}
			continue
		}
		if err := tx.Save(&r).Error; err != nil {
			return 0, err
		}
		if r.Status == model.ReplicaOrphaned || r.Status == model.ReplicaSkipped {
			if err := tx.Model(&model.Job{}).Where("replica_id = ? AND status = ?", r.ID, model.JobPending).Updates(map[string]any{"status": model.JobSucceeded, "progress_message": "同步目标已撤销或跳过", "finished_at": s.now()}).Error; err != nil {
				return 0, err
			}
		}
		n, err := s.queueReplicaJob(tx, r.ID)
		if err != nil {
			return 0, err
		}
		if n {
			queued++
		}
	}
	if err := tx.Model(&m).Updates(map[string]any{"sync_planned_at": s.now(), "sync_status": model.SyncNotRequired}).Error; err != nil {
		return 0, err
	}
	return queued, s.refreshBlobStatus(tx, m.BlobID)
}
func (s *Service) refreshBlobStatus(tx *gorm.DB, blobID string) error {
	var m model.Material
	if err := tx.Where("blob_id = ?", blobID).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil
		}
		return err
	}
	var rows []model.Replica
	if err := tx.Where("blob_id = ?", blobID).Find(&rows).Error; err != nil {
		return err
	}
	status := model.SyncNotRequired
	active, required, pending, attention := 0, 0, false, false
	for _, r := range rows {
		if r.Status == model.ReplicaOrphaned || r.Status == model.ReplicaSkipped {
			continue
		}
		active++
		if r.Required {
			required++
			if r.Status != model.ReplicaReady {
				pending = true
			}
			if r.Status == model.ReplicaFailed || r.Share && r.ShareStatus == model.ShareStatusFailed {
				attention = true
			}
		}
		if r.RemoteID != "" && r.Status == model.ReplicaReady && r.DesiredPath != r.RemotePath {
			attention = true
		}
	}
	if active > 0 {
		status = model.SyncSynced
		if pending {
			status = model.SyncSyncing
		}
		if required == 0 {
			for _, r := range rows {
				if r.Status == model.ReplicaPending || r.Status == model.ReplicaUploading || r.Status == model.ReplicaMoving {
					status = model.SyncSyncing
				}
			}
		}
		if attention {
			status = model.SyncAttention
		}
	}
	if m.SyncPlannedAt == nil && m.SyncStatus == model.SyncWaitingCatalog {
		status = model.SyncWaitingCatalog
	}
	archiveStatus := model.StatusArchived
	if !s.blobs.Exists(m.SHA256) {
		archiveStatus = model.StatusMissing
	}
	return tx.Model(&m).Updates(map[string]any{"status": archiveStatus, "sync_status": status, "updated_at": s.now()}).Error
}
func (s *Service) SyncMaterial(ctx context.Context, p auth.Principal, id string) (int, error) {
	n := 0
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		n, err = s.planMaterialTx(tx, id, true)
		if err != nil {
			return err
		}
		return s.audit(tx, p, "material.sync", id)
	})
	if err == nil {
		s.notify()
	}
	return n, err
}
func storageVersion(tx *gorm.DB) (string, error) {
	var rules []model.Rule
	var targets []model.RuleTarget
	var drives []model.Drive
	if err := tx.Order("id").Find(&rules).Error; err != nil {
		return "", err
	}
	if err := tx.Order("id").Find(&targets).Error; err != nil {
		return "", err
	}
	if err := tx.Order("id").Find(&drives).Error; err != nil {
		return "", err
	}
	return syncDigest([]any{rules, targets, drives}), nil
}
func syncDigest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

type previewClaim struct {
	Version, Draft, User string
	Expires              int64
}

func (s *Service) PreviewRules(ctx context.Context, p auth.Principal, rules []SyncRule, offset, limit int) (SyncPreview, error) {
	out := SyncPreview{Counts: map[string]int{}, Items: []SyncAction{}}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockSyncTx(tx); err != nil {
			return err
		}
		if err := validateRules(tx, rules); err != nil {
			return err
		}
		version, err := storageVersion(tx)
		if err != nil {
			return err
		}
		reserved, err := loadReservations(tx)
		if err != nil {
			return err
		}
		var ms []model.Material
		if err := tx.Order("created_at ASC").Order("id ASC").Find(&ms).Error; err != nil {
			return err
		}
		for _, m := range ms {
			if m.SyncStatus == model.SyncWaitingCatalog {
				out.Counts["waiting_catalog"]++
				continue
			}
			actions, err := s.desiredActions(tx, m, rules, reserved)
			if err != nil {
				return err
			}
			for _, a := range actions {
				out.Counts[a.Action]++
				if a.Action == "unchanged" {
					continue
				}
				if out.Total >= offset && len(out.Items) < limit {
					out.Items = append(out.Items, a)
				}
				out.Total++
			}
		}
		raw, _ := json.Marshal(previewClaim{version, syncDigest(rules), p.UserID, s.now().Add(15 * time.Minute).Unix()})
		out.PreviewToken, err = s.box.Seal(raw, []byte("sync-preview"))
		return err
	})
	return out, err
}
func (s *Service) PutRules(ctx context.Context, p auth.Principal, rules []SyncRule, token string) (int, error) {
	n := 0
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockSyncTx(tx); err != nil {
			return err
		}
		if err := validateRules(tx, rules); err != nil {
			return err
		}
		raw, err := s.box.Open(token, []byte("sync-preview"))
		var claim previewClaim
		if err != nil || json.Unmarshal(raw, &claim) != nil {
			return apperr.Conflict("请先预览规则")
		}
		version, err := storageVersion(tx)
		if err != nil {
			return err
		}
		if claim.Version != version || claim.Draft != syncDigest(rules) || claim.User != p.UserID || claim.Expires < s.now().Unix() {
			return apperr.Conflict("规则或网盘已变化，或预览已过期，请重新预览")
		}
		if err := tx.Where("1 = 1").Delete(&model.RuleTarget{}).Error; err != nil {
			return err
		}
		if err := tx.Where("1 = 1").Delete(&model.Rule{}).Error; err != nil {
			return err
		}
		for i, r := range rules {
			c, _ := json.Marshal(r.Conditions)
			row := model.Rule{ID: r.ID, Name: r.Name, Enabled: r.Enabled, IsDefault: r.IsDefault, Position: i, ConditionsJSON: string(c), CreatedAt: s.now(), UpdatedAt: s.now()}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			for j, t := range r.Targets {
				if err := tx.Create(&model.RuleTarget{ID: newID(), RuleID: r.ID, DriveID: t.DriveID, PathTemplate: t.PathTemplate, Share: t.Share, Required: t.Required, Position: j}).Error; err != nil {
					return err
				}
			}
		}
		var ms []model.Material
		if err := tx.Order("created_at ASC").Order("id ASC").Find(&ms).Error; err != nil {
			return err
		}
		for _, m := range ms {
			q, err := s.planMaterialTx(tx, m.ID, false)
			if err != nil {
				return err
			}
			n += q
		}
		return s.audit(tx, p, "storage.rules_update", "")
	})
	if err == nil {
		s.notify()
	}
	return n, err
}
func (s *Service) ExplainSync(ctx context.Context, id string) ([]SyncAction, error) {
	var out []SyncAction
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var m model.Material
		if err := tx.Where("id = ?", id).First(&m).Error; err != nil {
			return apperr.NotFound("资料不存在")
		}
		rules, err := loadRules(tx)
		if err != nil {
			return err
		}
		reservations, err := loadReservations(tx)
		if err != nil {
			return err
		}
		out, err = s.desiredActions(tx, m, rules, reservations)
		if len(out) == 0 {
			for _, r := range rules {
				var p model.Pack
				if err := tx.Where("id = ?", m.PackID).First(&p).Error; err != nil {
					return err
				}
				if r.Enabled && matchesRule(r.Conditions, m, p) {
					out = append(out, SyncAction{MaterialID: id, RuleID: r.ID, RuleName: r.Name, Action: "not_required"})
					break
				}
			}
		}
		return err
	})
	return out, err
}
