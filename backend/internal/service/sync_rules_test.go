package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

var syncAdmin = auth.Principal{UserID: "admin", Role: model.RoleAdmin}

type syncEnv struct {
	t    *testing.T
	svc  *Service
	conn *gorm.DB
	ctx  context.Context
}

func newSyncEnv(t *testing.T) *syncEnv {
	t.Helper()
	svc, conn := newWorkerTestService(t, t.TempDir(), "sync")
	if err := svc.Bootstrap(context.Background(), "admin", "a-long-password"); err != nil {
		t.Fatal(err)
	}
	return &syncEnv{t: t, svc: svc, conn: conn, ctx: context.Background()}
}

func (e *syncEnv) drive(name, kind string, mutate func(*DriveInput)) model.Drive {
	e.t.Helper()
	in := DriveInput{Name: name, Kind: kind, Enabled: true, ReadOrder: 100, Config: map[string]string{}, Secret: map[string]string{}}
	if mutate != nil {
		mutate(&in)
	}
	d, err := e.svc.CreateDrive(e.ctx, syncAdmin, in)
	if err != nil {
		e.t.Fatalf("create drive: %v", err)
	}
	return d
}

// material stores real content in the CAS and creates a pack + file that is
// ready for planning (not waiting for its catalogue).
func (e *syncEnv) material(title, fileName string, content []byte, pack func(*model.Pack)) model.Material {
	e.t.Helper()
	tmp, err := e.svc.blobs.NewTemp()
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := tmp.Write(content); err != nil {
		e.t.Fatal(err)
	}
	_ = tmp.Close()
	sha := sha256Hex(content)
	if err := e.svc.blobs.Publish(tmp.Name(), sha); err != nil {
		e.t.Fatal(err)
	}
	now := time.Now().UTC()
	p := model.Pack{ID: newID(), Title: title, DocType: "monthly", SubjectsJSON: `["math"]`, Grade: "grade12", Cohort: 2026, TagsJSON: "[]", CreatedAt: now, UpdatedAt: now}
	if pack != nil {
		pack(&p)
	}
	b := model.Blob{ID: newID(), SHA256: sha, SizeBytes: int64(len(content)), MediaType: "application/pdf", FileName: fileName, CreatedAt: now}
	m := model.Material{ID: newID(), PackID: p.ID, Part: model.PartQuestions, Title: title, FileName: fileName, OriginalName: fileName,
		Stage: model.StageReady, AttentionJSON: "[]", Status: model.StatusArchived, SyncStatus: model.SyncNotRequired,
		BlobID: b.ID, MediaType: "application/pdf", SizeBytes: int64(len(content)), SHA256: sha, CreatedAt: now, UpdatedAt: now}
	for _, row := range []any{&p, &b, &m} {
		if err := e.conn.Create(row).Error; err != nil {
			e.t.Fatal(err)
		}
	}
	return m
}

func (e *syncEnv) rules() []SyncRule {
	e.t.Helper()
	rules, err := e.svc.ListRules(e.ctx)
	if err != nil {
		e.t.Fatal(err)
	}
	return rules
}

func (e *syncEnv) apply(rules []SyncRule) SyncPreview {
	e.t.Helper()
	preview, err := e.svc.PreviewRules(e.ctx, syncAdmin, rules, 0, 100)
	if err != nil {
		e.t.Fatalf("preview: %v", err)
	}
	if _, err := e.svc.PutRules(e.ctx, syncAdmin, rules, preview.PreviewToken); err != nil {
		e.t.Fatalf("put rules: %v", err)
	}
	return preview
}

func (e *syncEnv) drain() {
	e.t.Helper()
	e.svc.ProcessAvailable(e.ctx, 100)
}

func (e *syncEnv) replica(m model.Material, driveID string) model.Replica {
	e.t.Helper()
	var r model.Replica
	if err := e.conn.Where("blob_id = ? AND drive_id = ?", m.BlobID, driveID).First(&r).Error; err != nil {
		e.t.Fatalf("replica on %s: %v", driveID, err)
	}
	return r
}

func (e *syncEnv) reload(m model.Material) model.Material {
	e.t.Helper()
	var out model.Material
	if err := e.conn.Where("id = ?", m.ID).First(&out).Error; err != nil {
		e.t.Fatal(err)
	}
	return out
}

func (e *syncEnv) localFile(driveID, rel string) string {
	return filepath.Join(e.svc.localRoot(driveID), filepath.FromSlash(rel))
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func fallbackTo(rules []SyncRule, targets ...SyncTarget) []SyncRule {
	out := append([]SyncRule(nil), rules...)
	out[len(out)-1].Targets = targets
	return out
}

func isConflict(err error) bool {
	var ae *apperr.Error
	return errors.As(err, &ae) && ae.Status == http.StatusConflict
}

func isValidation(err error) bool {
	var ae *apperr.Error
	return errors.As(err, &ae) && ae.Status == http.StatusBadRequest
}

func TestMatchesRuleConditions(t *testing.T) {
	p := model.Pack{SubjectsJSON: `["math","physics"]`, DocType: "monthly", Grade: "grade12", Cohort: 2026, Region: "浙江", TagsJSON: `["一轮"]`}
	m := model.Material{Part: model.PartQuestions, MediaType: "application/pdf", SizeBytes: 1000}
	cases := []struct {
		name string
		c    model.RuleConditions
		want bool
	}{
		{"empty matches all", model.RuleConditions{}, true},
		{"any subject matches", model.RuleConditions{Subjects: []string{"chinese", "physics"}}, true},
		{"subject miss", model.RuleConditions{Subjects: []string{"chinese"}}, false},
		{"fields are AND", model.RuleConditions{Subjects: []string{"math"}, DocTypes: []string{"final"}}, false},
		{"cohort", model.RuleConditions{Cohorts: []int{2025, 2026}}, true},
		{"cohort miss", model.RuleConditions{Cohorts: []int{2025}}, false},
		{"region and tag", model.RuleConditions{Regions: []string{"浙江"}, Tags: []string{"一轮"}}, true},
		{"part miss", model.RuleConditions{Parts: []string{model.PartAnswers}}, false},
		{"media type", model.RuleConditions{MediaTypes: []string{"application/pdf"}}, true},
		{"size inside", model.RuleConditions{MinSize: 1000, MaxSize: 1000}, true},
		{"below min", model.RuleConditions{MinSize: 1001}, false},
		{"above max", model.RuleConditions{MaxSize: 999}, false},
		{"max zero is unbounded", model.RuleConditions{MinSize: 1}, true},
	}
	for _, tc := range cases {
		if got := matchesRule(tc.c, m, p); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRenderSyncPathTemplate(t *testing.T) {
	p := model.Pack{Title: "2026 杭州/一模: 数学", SubjectsJSON: `["math"]`, DocType: "monthly", Grade: "grade12", Cohort: 2026}
	m := model.Material{FileName: "试题.pdf", Part: model.PartQuestions, SHA256: "abcdef0123456789"}
	got, err := renderSyncPath(model.DefaultPathTemplate, m, p)
	if err != nil {
		t.Fatal(err)
	}
	want := "数学/2026届/" + docTypeLabels["monthly"] + "/2026 杭州_一模_ 数学/试题.pdf"
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	// Missing values get their documented fallbacks; multiple subjects become 综合.
	empty := model.Pack{SubjectsJSON: `[]`}
	got, err = renderSyncPath("{学科}/{类型}/{套卷}/{部件}/{届别}/{sha8}-{文件名}", model.Material{FileName: "a.pdf", SHA256: "abcdef0123456789"}, empty)
	if err != nil {
		t.Fatal(err)
	}
	if got != "未分学科/未分类/未命名资料/其他/未知/abcdef01-a.pdf" {
		t.Fatalf("fallback path = %q", got)
	}
	multi := model.Pack{SubjectsJSON: `["math","physics"]`}
	if got, _ := renderSyncPath("{学科}/{文件名}", m, multi); got != "综合/试题.pdf" {
		t.Fatalf("multi-subject path = %q", got)
	}
	long := model.Material{FileName: strings.Repeat("长", 150) + ".pdf"}
	got, _ = renderSyncPath("{文件名}", long, empty)
	if n := len([]rune(got)); n != 100 || !strings.HasSuffix(got, ".pdf") {
		t.Fatalf("long name not truncated to 100 runes keeping extension: %d %q", n, got)
	}
}

func TestValidateTemplate(t *testing.T) {
	for _, ok := range []string{"{文件名}", model.DefaultPathTemplate, "资料/{sha8}/{文件名}"} {
		if err := validateTemplate(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "/abs/{文件名}", `a\{文件名}`, "{学科}", "a//{文件名}", "../{文件名}", "./{文件名}", "{未知}/{文件名}", "{学科/{文件名}"} {
		if err := validateTemplate(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestValidateRulesStructure(t *testing.T) {
	e := newSyncEnv(t)
	local := e.drive("本地", model.KindLocal, nil)
	pan := e.drive("123", model.KindPan123, func(in *DriveInput) {
		in.Secret = map[string]string{"client_id": "id", "client_secret": "secret"}
	})
	fallback := SyncRule{ID: "default", Name: "其他", Enabled: true, IsDefault: true}
	target := SyncTarget{DriveID: local.ID, PathTemplate: model.DefaultPathTemplate}
	cases := map[string][]SyncRule{
		"empty list":             {},
		"no fallback":            {{ID: "a", Name: "a", Enabled: true}},
		"fallback not last":      {fallback, {ID: "a", Name: "a", Enabled: true}},
		"fallback disabled":      {{ID: "default", Name: "其他", IsDefault: true}},
		"fallback has condition": {{ID: "default", Name: "其他", Enabled: true, IsDefault: true, Conditions: model.RuleConditions{Grades: []string{"grade12"}}}},
		"duplicate id":           {{ID: "default", Name: "a", Enabled: true}, fallback},
		"blank name":             {{ID: "a", Name: " ", Enabled: true}, fallback},
		"bad subject":            {{ID: "a", Name: "a", Enabled: true, Conditions: model.RuleConditions{Subjects: []string{"alchemy"}}}, fallback},
		"bad part":               {{ID: "a", Name: "a", Enabled: true, Conditions: model.RuleConditions{Parts: []string{"cover"}}}, fallback},
		"bad size range":         {{ID: "a", Name: "a", Enabled: true, Conditions: model.RuleConditions{MinSize: 10, MaxSize: 5}}, fallback},
		"duplicate drive":        {{ID: "a", Name: "a", Enabled: true, Targets: []SyncTarget{target, target}}, fallback},
		"unknown drive":          {{ID: "a", Name: "a", Enabled: true, Targets: []SyncTarget{{DriveID: "nope", PathTemplate: "{文件名}"}}}, fallback},
		"123pan target":          {{ID: "a", Name: "a", Enabled: true, Targets: []SyncTarget{{DriveID: pan.ID, PathTemplate: "{文件名}"}}}, fallback},
		"bad template":           {{ID: "a", Name: "a", Enabled: true, Targets: []SyncTarget{{DriveID: local.ID, PathTemplate: "{学科}"}}}, fallback},
	}
	for name, rules := range cases {
		if _, err := e.svc.PreviewRules(e.ctx, syncAdmin, rules, 0, 10); !isValidation(err) {
			t.Errorf("%s: want validation error, got %v", name, err)
		}
	}
	if _, err := e.svc.PreviewRules(e.ctx, syncAdmin, []SyncRule{{ID: "a", Name: "a", Enabled: true, Targets: []SyncTarget{target}}, fallback}, 0, 10); err != nil {
		t.Fatalf("valid rules rejected: %v", err)
	}
}

func TestBootstrapSeedsFallbackRuleOnly(t *testing.T) {
	e := newSyncEnv(t)
	rules := e.rules()
	if len(rules) != 1 || !rules[0].IsDefault || !rules[0].Enabled || len(rules[0].Targets) != 0 {
		t.Fatalf("bootstrap rules = %+v", rules)
	}
	var drives int64
	e.conn.Model(&model.Drive{}).Count(&drives)
	if drives != 0 {
		t.Fatalf("bootstrap created %d drives, want none", drives)
	}
	if err := e.svc.Bootstrap(e.ctx, "admin", "a-long-password"); err != nil {
		t.Fatal(err)
	}
	if again := e.rules(); len(again) != 1 {
		t.Fatalf("bootstrap is not idempotent: %d rules", len(again))
	}
}

func TestFirstMatchingRuleWinsAndUploadsToLocalDrive(t *testing.T) {
	e := newSyncEnv(t)
	a := e.drive("数学盘", model.KindLocal, nil)
	b := e.drive("兜底盘", model.KindLocal, func(in *DriveInput) { in.RootPath = "备份" })
	math := e.material("数学月考", "数学月考-试题.pdf", []byte("math paper"), nil)
	chinese := e.material("语文月考", "语文月考-试题.pdf", []byte("chinese paper"), func(p *model.Pack) { p.SubjectsJSON = `["chinese"]` })

	rules := fallbackTo(e.rules(), SyncTarget{DriveID: b.ID, PathTemplate: "{学科}/{文件名}", Required: true})
	rules = append([]SyncRule{
		{ID: "disabled", Name: "停用的规则", Enabled: false, Targets: []SyncTarget{{DriveID: b.ID, PathTemplate: "停用/{文件名}"}}},
		{ID: "math", Name: "数学", Enabled: true, Conditions: model.RuleConditions{Subjects: []string{"math"}}, Targets: []SyncTarget{{DriveID: a.ID, PathTemplate: "{学科}/{届别}/{文件名}", Required: true}}},
	}, rules...)
	preview := e.apply(rules)
	if preview.Counts["upload"] != 2 || preview.Total != 2 {
		t.Fatalf("preview counts = %+v total %d", preview.Counts, preview.Total)
	}
	if got := e.reload(math).SyncStatus; got != model.SyncSyncing {
		t.Fatalf("sync status before worker = %s", got)
	}
	e.drain()

	r := e.replica(math, a.ID)
	if r.Status != model.ReplicaReady || r.RuleID != "math" || r.RemotePath != "数学/2026届/数学月考-试题.pdf" || r.Verification != "sha256" {
		t.Fatalf("math replica = %+v", r)
	}
	if data, err := os.ReadFile(e.localFile(a.ID, r.RemotePath)); err != nil || string(data) != "math paper" {
		t.Fatalf("local drive file: %q %v", data, err)
	}
	if err := e.conn.Where("blob_id = ? AND drive_id = ?", math.BlobID, b.ID).First(&model.Replica{}).Error; err == nil {
		t.Fatal("first matching rule must govern alone; fallback drive got the math file")
	}
	c := e.replica(chinese, b.ID)
	if c.RemotePath != "备份/语文/语文月考-试题.pdf" || c.RuleID != "default" {
		t.Fatalf("fallback replica = %+v", c)
	}
	if e.reload(math).SyncStatus != model.SyncSynced || e.reload(chinese).SyncStatus != model.SyncSynced {
		t.Fatal("both files should be synced")
	}

	explained, err := e.svc.ExplainSync(e.ctx, math.ID)
	if err != nil || len(explained) != 1 || explained[0].RuleName != "数学" || explained[0].Action != "unchanged" {
		t.Fatalf("explain = %+v %v", explained, err)
	}
}

func TestPreviewDoesNotWriteAndTokenIsBound(t *testing.T) {
	e := newSyncEnv(t)
	d := e.drive("本地", model.KindLocal, nil)
	m := e.material("卷", "卷-试题.pdf", []byte("x"), nil)
	rules := fallbackTo(e.rules(), SyncTarget{DriveID: d.ID, PathTemplate: "{文件名}"})

	preview, err := e.svc.PreviewRules(e.ctx, syncAdmin, rules, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	var count int64
	e.conn.Model(&model.Replica{}).Count(&count)
	if count != 0 || len(e.rules()[0].Targets) != 0 {
		t.Fatal("preview must not write replicas or rules")
	}
	if len(preview.Items) != 1 || preview.Items[0].MaterialID != m.ID || preview.Items[0].DesiredPath != "卷-试题.pdf" {
		t.Fatalf("preview items = %+v", preview.Items)
	}

	if _, err := e.svc.PutRules(e.ctx, syncAdmin, rules, "not-a-token"); !isConflict(err) {
		t.Fatalf("bad token: %v", err)
	}
	other := fallbackTo(e.rules(), SyncTarget{DriveID: d.ID, PathTemplate: "改/{文件名}"})
	if _, err := e.svc.PutRules(e.ctx, syncAdmin, other, preview.PreviewToken); !isConflict(err) {
		t.Fatalf("different draft: %v", err)
	}
	if _, err := e.svc.PutRules(e.ctx, auth.Principal{UserID: "someone-else", Role: model.RoleAdmin}, rules, preview.PreviewToken); !isConflict(err) {
		t.Fatalf("other user: %v", err)
	}
	name := "改名的网盘"
	if _, err := e.svc.UpdateDrive(e.ctx, syncAdmin, d.ID, DrivePatch{Name: &name}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.PutRules(e.ctx, syncAdmin, rules, preview.PreviewToken); !isConflict(err) {
		t.Fatalf("drive changed after preview: %v", err)
	}

	fresh, err := e.svc.PreviewRules(e.ctx, syncAdmin, rules, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	e.svc.now = func() time.Time { return time.Now().Add(16 * time.Minute) }
	if _, err := e.svc.PutRules(e.ctx, syncAdmin, rules, fresh.PreviewToken); !isConflict(err) {
		t.Fatalf("expired token: %v", err)
	}
	e.svc.now = time.Now
	if _, err := e.svc.PutRules(e.ctx, syncAdmin, rules, fresh.PreviewToken); err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}
}

func TestPathCollisionAddsShaSuffix(t *testing.T) {
	e := newSyncEnv(t)
	d := e.drive("本地", model.KindLocal, nil)
	first := e.material("卷A", "同名.pdf", []byte("first"), nil)
	second := e.material("卷B", "同名.pdf", []byte("second"), nil)
	e.apply(fallbackTo(e.rules(), SyncTarget{DriveID: d.ID, PathTemplate: "{文件名}"}))
	e.drain()
	a, b := e.replica(first, d.ID), e.replica(second, d.ID)
	if a.RemotePath != "同名.pdf" || b.RemotePath != "同名 ("+second.SHA256[:8]+").pdf" {
		t.Fatalf("collision paths: %q %q", a.RemotePath, b.RemotePath)
	}
	if a.Status != model.ReplicaReady || b.Status != model.ReplicaReady {
		t.Fatalf("statuses %s %s", a.Status, b.Status)
	}
	// Replanning keeps each file on the path it already owns.
	if _, err := e.svc.Reconcile(e.ctx, syncAdmin); err != nil {
		t.Fatal(err)
	}
	e.drain()
	if got := e.replica(second, d.ID).RemotePath; got != b.RemotePath {
		t.Fatalf("replan moved the suffixed file to %q", got)
	}
}

func TestDriveLimitsSkipWithoutFailing(t *testing.T) {
	e := newSyncEnv(t)
	small := e.drive("小盘", model.KindLocal, func(in *DriveInput) { in.MaxFileBytes = 4 })
	docx := e.drive("只收 docx", model.KindLocal, func(in *DriveInput) { in.AllowedExts = []string{"DOCX"} })
	m := e.material("卷", "卷.pdf", []byte("larger than four bytes"), nil)
	e.apply(fallbackTo(e.rules(),
		SyncTarget{DriveID: small.ID, PathTemplate: "{文件名}", Required: true},
		SyncTarget{DriveID: docx.ID, PathTemplate: "{文件名}", Required: true},
	))
	e.drain()
	if r := e.replica(m, small.ID); r.Status != model.ReplicaSkipped || r.SkipReason != "超过大小上限" {
		t.Fatalf("size skip = %+v", r)
	}
	if r := e.replica(m, docx.ID); r.Status != model.ReplicaSkipped || r.SkipReason != "扩展名不允许" {
		t.Fatalf("ext skip = %+v", r)
	}
	if got := e.reload(m).SyncStatus; got != model.SyncNotRequired {
		t.Fatalf("skipped targets must not count as failures, sync status = %s", got)
	}
	var jobs int64
	e.conn.Model(&model.Job{}).Where("status = ?", model.JobPending).Count(&jobs)
	if jobs != 0 {
		t.Fatalf("%d replicate jobs left pending for skipped targets", jobs)
	}
}

func TestRenameMovesRemoteOrDriftsWhenKept(t *testing.T) {
	e := newSyncEnv(t)
	move := e.drive("移动", model.KindLocal, nil)
	keep := e.drive("保持", model.KindLocal, func(in *DriveInput) { in.OnPathChange = model.OnPathChangeKeep })
	m := e.material("旧卷", "旧卷-试题.pdf", []byte("content"), nil)
	e.apply(fallbackTo(e.rules(),
		SyncTarget{DriveID: move.ID, PathTemplate: "{套卷}/{文件名}", Required: true},
		SyncTarget{DriveID: keep.ID, PathTemplate: "{套卷}/{文件名}", Required: true},
	))
	e.drain()
	oldPath := e.replica(m, move.ID).RemotePath

	if err := e.conn.Model(&model.Pack{}).Where("id = ?", m.PackID).Update("title", "新卷").Error; err != nil {
		t.Fatal(err)
	}
	if err := e.conn.Model(&model.Material{}).Where("id = ?", m.ID).Update("file_name", "新卷-试题.pdf").Error; err != nil {
		t.Fatal(err)
	}
	e.svc.renameMaterialFiles(e.ctx, []string{m.ID})
	e.drain()

	moved := e.replica(m, move.ID)
	if moved.Status != model.ReplicaReady || moved.RemotePath != "新卷/新卷-试题.pdf" || moved.DesiredPath != moved.RemotePath {
		t.Fatalf("moved replica = %+v", moved)
	}
	if _, err := os.Stat(e.localFile(move.ID, oldPath)); !os.IsNotExist(err) {
		t.Fatal("old local file should be gone after move")
	}
	if _, err := os.Stat(e.localFile(move.ID, moved.RemotePath)); err != nil {
		t.Fatalf("moved file missing: %v", err)
	}
	kept := e.replica(m, keep.ID)
	if kept.RemotePath != oldPath || kept.DesiredPath != "新卷/新卷-试题.pdf" || kept.Status != model.ReplicaReady {
		t.Fatalf("kept replica = %+v", kept)
	}
	if got := e.reload(m).SyncStatus; got != model.SyncAttention {
		t.Fatalf("path drift should need attention, got %s", got)
	}
	explained, err := e.svc.ExplainSync(e.ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]string{}
	for _, a := range explained {
		actions[a.DriveID] = a.Action
	}
	if actions[keep.ID] != "drift" || actions[move.ID] != "unchanged" {
		t.Fatalf("explain actions = %+v", actions)
	}
	page, err := e.svc.ListReplicas(e.ctx, "drift", "", PageQuery{Page: 1, PageSize: 10})
	if err != nil || page.Total != 1 || page.Items[0].ID != kept.ID {
		t.Fatalf("drift filter = %+v %v", page.Items, err)
	}
}

func TestRuleChangeOrphansAndBulkActions(t *testing.T) {
	e := newSyncEnv(t)
	d := e.drive("本地", model.KindLocal, nil)
	m := e.material("卷", "卷.pdf", []byte("orphan me"), nil)
	n := e.material("卷2", "卷2.pdf", []byte("forget me"), nil)
	e.apply(fallbackTo(e.rules(), SyncTarget{DriveID: d.ID, PathTemplate: "{文件名}"}))
	e.drain()
	r := e.replica(m, d.ID)
	f := e.replica(n, d.ID)

	if _, err := e.svc.BulkReplicas(e.ctx, syncAdmin, []string{r.ID}, "bogus"); !isValidation(err) {
		t.Fatalf("bad action: %v", err)
	}
	results, err := e.svc.BulkReplicas(e.ctx, syncAdmin, []string{r.ID}, "delete_remote")
	if err != nil || results[0].Success {
		t.Fatalf("deleting a governed replica must fail per item: %+v %v", results, err)
	}

	preview := e.apply(fallbackTo(e.rules()))
	if preview.Counts["orphan"] != 2 {
		t.Fatalf("preview counts = %+v", preview.Counts)
	}
	r, f = e.replica(m, d.ID), e.replica(n, d.ID)
	if r.Status != model.ReplicaOrphaned || r.RuleID != "" {
		t.Fatalf("orphaned replica = %+v", r)
	}
	if _, err := os.Stat(e.localFile(d.ID, r.RemotePath)); err != nil {
		t.Fatal("orphaning must never delete remote content")
	}
	if got := e.reload(m).SyncStatus; got != model.SyncNotRequired {
		t.Fatalf("sync status = %s", got)
	}

	results, err = e.svc.BulkReplicas(e.ctx, syncAdmin, []string{r.ID, "missing-id"}, "delete_remote")
	if err != nil || !results[0].Success || results[1].Success {
		t.Fatalf("delete_remote results = %+v %v", results, err)
	}
	if _, err := os.Stat(e.localFile(d.ID, r.RemotePath)); !os.IsNotExist(err) {
		t.Fatal("delete_remote should remove the file")
	}
	if err := e.conn.Where("id = ?", r.ID).First(&model.Replica{}).Error; err == nil {
		t.Fatal("delete_remote should remove the record")
	}
	var deletion model.Job
	if err := e.conn.Where("replica_id = ?", r.ID).Order("created_at DESC").First(&deletion).Error; err != nil || deletion.Status != model.JobSucceeded {
		t.Fatalf("deletion lease job = %+v %v", deletion, err)
	}

	if results, _ := e.svc.BulkReplicas(e.ctx, syncAdmin, []string{f.ID}, "forget"); !results[0].Success {
		t.Fatalf("forget = %+v", results)
	}
	if _, err := os.Stat(e.localFile(d.ID, f.RemotePath)); err != nil {
		t.Fatal("forget must keep the remote file")
	}
}

func TestNeverUploadedReplicaIsCancelledNotOrphaned(t *testing.T) {
	e := newSyncEnv(t)
	d := e.drive("本地", model.KindLocal, nil)
	m := e.material("卷", "卷.pdf", []byte("x"), nil)
	e.apply(fallbackTo(e.rules(), SyncTarget{DriveID: d.ID, PathTemplate: "{文件名}"}))
	preview := e.apply(fallbackTo(e.rules()))
	if preview.Counts["cancel"] != 1 {
		t.Fatalf("counts = %+v", preview.Counts)
	}
	if err := e.conn.Where("blob_id = ?", m.BlobID).First(&model.Replica{}).Error; err == nil {
		t.Fatal("a replica never written to the drive should be deleted")
	}
	var pending int64
	e.conn.Model(&model.Job{}).Where("status IN ?", []string{model.JobPending, model.JobRunning}).Count(&pending)
	if pending != 0 {
		t.Fatalf("%d jobs left for a cancelled target", pending)
	}
}

func TestWaitingCatalogUntilReadyOrForced(t *testing.T) {
	e := newSyncEnv(t)
	d := e.drive("本地", model.KindLocal, nil)
	e.apply(fallbackTo(e.rules(), SyncTarget{DriveID: d.ID, PathTemplate: "{文件名}", Required: true}))
	m := e.material("卷", "卷.pdf", []byte("wait"), nil)
	if err := e.conn.Model(&model.Material{}).Where("id = ?", m.ID).Updates(map[string]any{"sync_status": model.SyncWaitingCatalog, "stage": model.StageProcessing}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Reconcile(e.ctx, syncAdmin); err != nil {
		t.Fatal(err)
	}
	if got := e.reload(m); got.SyncStatus != model.SyncWaitingCatalog || got.SyncPlannedAt != nil {
		t.Fatalf("reconcile must not plan uncatalogued files: %+v", got)
	}
	preview, err := e.svc.PreviewRules(e.ctx, syncAdmin, e.rules(), 0, 10)
	if err != nil || preview.Counts["waiting_catalog"] != 1 {
		t.Fatalf("preview = %+v %v", preview.Counts, err)
	}
	// A committed catalogue decision plans it.
	if err := e.conn.Transaction(func(tx *gorm.DB) error { _, err := e.svc.planMaterialTx(tx, m.ID, true); return err }); err != nil {
		t.Fatal(err)
	}
	if got := e.reload(m); got.SyncStatus != model.SyncSyncing || got.SyncPlannedAt == nil {
		t.Fatalf("ready file should be planned: %+v", got)
	}
	e.drain()
	if got := e.reload(m).SyncStatus; got != model.SyncSynced {
		t.Fatalf("sync status = %s", got)
	}

	other := e.material("卷2", "卷2.pdf", []byte("force"), nil)
	e.conn.Model(&model.Material{}).Where("id = ?", other.ID).Update("sync_status", model.SyncWaitingCatalog)
	if n, err := e.svc.SyncMaterial(e.ctx, syncAdmin, other.ID); err != nil || n != 1 {
		t.Fatalf("forced sync queued %d: %v", n, err)
	}
}

func TestFailedRequiredTargetNeedsAttentionAndRetryRecovers(t *testing.T) {
	e := newSyncEnv(t)
	d := e.drive("本地", model.KindLocal, nil)
	m := e.material("卷", "卷.pdf", []byte("conflict"), nil)
	// Something else already occupies the target path with different bytes.
	occupied := e.localFile(d.ID, "卷.pdf")
	if err := os.MkdirAll(filepath.Dir(occupied), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(occupied, []byte("someone else"), 0o640); err != nil {
		t.Fatal(err)
	}
	e.apply(fallbackTo(e.rules(), SyncTarget{DriveID: d.ID, PathTemplate: "{文件名}", Required: true}))
	e.drain()
	r := e.replica(m, d.ID)
	if r.Status != model.ReplicaFailed || !strings.Contains(r.Error, "conflict") && !strings.Contains(r.Error, "path_taken") {
		t.Fatalf("conflicting upload = %+v", r)
	}
	if data, _ := os.ReadFile(occupied); string(data) != "someone else" {
		t.Fatal("upload overwrote an existing file")
	}
	if got := e.reload(m).SyncStatus; got != model.SyncAttention {
		t.Fatalf("sync status = %s", got)
	}

	if err := os.Remove(occupied); err != nil {
		t.Fatal(err)
	}
	results, err := e.svc.BulkReplicas(e.ctx, syncAdmin, []string{r.ID}, "retry")
	if err != nil || !results[0].Success {
		t.Fatalf("retry = %+v %v", results, err)
	}
	e.drain()
	if got := e.replica(m, d.ID).Status; got != model.ReplicaReady {
		t.Fatalf("retried replica status = %s", got)
	}
}

func TestDisabledDriveSkipsButStaysReadable(t *testing.T) {
	e := newSyncEnv(t)
	d := e.drive("本地", model.KindLocal, nil)
	m := e.material("卷", "卷.pdf", []byte("readable"), nil)
	e.apply(fallbackTo(e.rules(), SyncTarget{DriveID: d.ID, PathTemplate: "{文件名}"}))
	e.drain()
	off := false
	if _, err := e.svc.UpdateDrive(e.ctx, syncAdmin, d.ID, DrivePatch{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Reconcile(e.ctx, syncAdmin); err != nil {
		t.Fatal(err)
	}
	r := e.replica(m, d.ID)
	if r.Status != model.ReplicaSkipped || r.SkipReason != "网盘已停用" || r.RemoteID == "" {
		t.Fatalf("disabled drive replica = %+v", r)
	}
	if err := os.Remove(e.svc.blobs.Path(m.SHA256)); err != nil {
		t.Fatal(err)
	}
	dl, err := e.svc.OpenMaterialDownload(e.ctx, m.ID)
	if err != nil {
		t.Fatalf("download from disabled drive: %v", err)
	}
	defer dl.Reader.Close()
	buf := make([]byte, 64)
	k, _ := dl.Reader.Read(buf)
	if string(buf[:k]) != "readable" {
		t.Fatalf("downloaded %q", buf[:k])
	}
}

func TestPurgeKeepsCloudCopiesAsOrphans(t *testing.T) {
	e := newSyncEnv(t)
	local := e.drive("本地", model.KindLocal, nil)
	m := e.material("卷", "卷.pdf", []byte("purge"), nil)
	e.apply(fallbackTo(e.rules(), SyncTarget{DriveID: local.ID, PathTemplate: "{文件名}"}))
	e.drain()
	r := e.replica(m, local.ID)
	// A second, non-local drive that already holds a copy.
	cloud := model.Drive{ID: newID(), Name: "云盘", Kind: model.KindWebDAV, Enabled: true, OnPathChange: model.OnPathChangeMove, ConfigJSON: "{}", AllowedExtsJSON: "[]"}
	if err := e.conn.Create(&cloud).Error; err != nil {
		t.Fatal(err)
	}
	remote := model.Replica{ID: newID(), BlobID: m.BlobID, DriveID: cloud.ID, RuleID: "default", Status: model.ReplicaReady, RemoteID: "卷.pdf", RemotePath: "卷.pdf", DesiredPath: "卷.pdf", SizeBytes: m.SizeBytes}
	if err := e.conn.Create(&remote).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.svc.purgeMaterials(e.ctx, []string{m.ID}); err != nil {
		t.Fatal(err)
	}
	if err := e.conn.Where("id = ?", r.ID).First(&model.Replica{}).Error; err == nil {
		t.Fatal("local drive replica should be removed on purge")
	}
	if _, err := os.Stat(e.localFile(local.ID, r.RemotePath)); !os.IsNotExist(err) {
		t.Fatal("local drive file should be removed on purge")
	}
	var kept model.Replica
	if err := e.conn.Where("id = ?", remote.ID).First(&kept).Error; err != nil || kept.Status != model.ReplicaOrphaned || kept.RuleID != "" {
		t.Fatalf("cloud replica after purge = %+v %v", kept, err)
	}
	if err := e.conn.Where("id = ?", m.BlobID).First(&model.Blob{}).Error; err != nil {
		t.Fatal("blob row must survive while an orphaned cloud copy references it")
	}
}
