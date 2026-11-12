package service

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/nic/paper-collect/backend/internal/jobexec"

	"github.com/nic/paper-collect/backend/internal/catalogai"
	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

// Chinese display labels used for canonical names and model prompts.
var (
	docTypeLabels = map[string]string{
		"gaokao_real": "高考真题", "gaokao_mock": "高考模拟", "zhongkao_real": "中考真题", "zhongkao_mock": "中考模拟",
		"joint": "联考", "monthly": "月考", "midterm": "期中", "final": "期末", "opening": "开学考",
		"unit": "单元测", "weekly": "周测", "topic": "专题练习", "lecture": "讲义", "study_guide": "学案",
		"courseware": "课件", "homework": "作业", "other": "其他",
	}
	gradeLabels = map[string]string{
		"grade7": "初一", "grade8": "初二", "grade9": "初三",
		"grade10": "高一", "grade11": "高二", "grade12": "高三",
	}
	termLabels = map[string]string{"first": "上学期", "second": "下学期"}
	partLabels = map[string]string{
		model.PartQuestions: "试题卷", model.PartAnswers: "答案解析", model.PartFull: "试题与答案",
		model.PartTeacher: "教师版", model.PartAnswerSheet: "答题卡", model.PartListening: "听力",
		model.PartMain: "", model.PartOther: "",
	}
)

func subjectLabel(code string) string {
	for _, s := range model.Subjects {
		if s.Code == code {
			return s.Name
		}
	}
	return ""
}

// examDocTypes are pack types whose files are exam papers (completeness
// applies). Other types are single-document resources.
var examDocTypes = catalogai.ExamDocTypes

// canonicalPackTitle builds the display name from catalogue fields, for example
// "2025届 浙江 Z20联盟 9月联考 高三 数学". It returns "" when nothing is known.
func canonicalPackTitle(p model.Pack) string {
	parts := []string{}
	if p.Cohort > 0 {
		parts = append(parts, fmt.Sprintf("%d届", p.Cohort))
	}
	if p.Region != "" {
		parts = append(parts, p.Region)
	}
	if p.Organization != "" && p.DocType != catalogai.DocTypeGaokaoReal {
		parts = append(parts, p.Organization)
	}
	if label := docTypeLabels[p.DocType]; label != "" && p.DocType != "other" {
		if examDocTypes[p.DocType] && len(p.ExamDate) == 7 {
			month := strings.TrimPrefix(p.ExamDate[5:], "0")
			label = month + "月" + label
		}
		parts = append(parts, label)
	}
	if g := gradeLabels[p.Grade]; g != "" {
		parts = append(parts, g)
	}
	if t := termLabels[p.Term]; t != "" {
		parts = append(parts, t)
	}
	subjects := []string{}
	for _, code := range model.DecodeStrings(p.SubjectsJSON) {
		if l := subjectLabel(code); l != "" {
			subjects = append(subjects, l)
		}
	}
	if len(subjects) > 0 {
		parts = append(parts, strings.Join(subjects, "·"))
	}
	// A chapter that only repeats the subject ("信息技术" on 信息技术) adds
	// nothing; content like "水的电离" is kept.
	chapter := p.Chapter
	for _, s := range subjects {
		if chapter == s {
			chapter = ""
			break
		}
	}
	if chapter != "" {
		parts = append(parts, chapter)
	}
	// Without type, grade or subject the name says nothing useful; let the
	// caller fall back to the exam name or the original file name.
	if p.DocType == "" && p.Grade == "" && len(subjects) == 0 {
		return ""
	}
	return truncateRunes(strings.Join(parts, " "), maxTitleRunes)
}

// packTitleFromCatalogue names a set from its catalogue fields, or returns ""
// when they do not identify it. Exam packs are composed from 届、地区、机构、
// 类型、年级、学科; 专题练习、讲义、学案、课件、作业、其他 keep the name the
// model wrote for them, because a field dump says what kind of file it is, not
// what it is about. A set that belongs to a series is titled with the topic's
// canonical name plus its own distinguishing part, so a whole series reads
// consistently. The composed name is still the fallback when no name was
// written at all.
func packTitleFromCatalogue(p model.Pack) string {
	composed := catalogai.IsComposedTitleDocType(p.DocType)
	if !composed && p.Series != "" {
		if specific := seriesSpecific(p); specific != "" {
			return truncateRunes(p.Series+" "+specific, maxTitleRunes)
		}
		return truncateRunes(p.Series, maxTitleRunes)
	}
	if !composed && p.ExamName != "" {
		return truncateRunes(p.ExamName, maxTitleRunes)
	}
	if p.DocType != "" && len(model.DecodeStrings(p.SubjectsJSON)) > 0 {
		if title := canonicalPackTitle(p); title != "" {
			return title
		}
	}
	if p.ExamName != "" {
		return truncateRunes(p.ExamName, maxTitleRunes)
	}
	return ""
}

// seriesNumber finds the 专题NN marker that orders the sets of a series.
var seriesNumber = regexp.MustCompile(`专题\s*\d+`)

const titleNoise = " \t\r\n-—–_·•:：;；,，.。、/|"

// seriesSpecific extracts the part of a set's own name that tells it apart
// from the other sets of the same series: "专题44 水的电离与溶液的pH" out of
// "十年（2012-2021）高考化学真题分项汇编 专题44 水的电离与溶液的pH", or the
// name itself when it does not repeat the series.
func seriesSpecific(p model.Pack) string {
	name := strings.TrimSpace(p.ExamName)
	if name == "" {
		return strings.TrimSpace(p.Chapter)
	}
	if rest, ok := cutSeriesPrefix(name, p.Series); ok {
		name = rest
	} else if loc := seriesNumber.FindStringIndex(name); loc != nil && loc[0] > 0 {
		// The file printed its series another way ("高考真题分类汇编（2012-2021）专题51 …");
		// keep the numbered topic and drop the other series wording.
		name = name[loc[0]:]
	}
	name = trimTrailingQualifiers(trimLeadingNoise(name))
	name = strings.Trim(strings.TrimSpace(name), titleNoise)
	if catalogai.NormalizeName(name) == "" || catalogai.NormalizeName(name) == catalogai.NormalizeName(p.Series) {
		return ""
	}
	return strings.TrimSpace(name)
}

// cutSeriesPrefix cuts the series name off the front of a pack name, comparing
// after normalisation so punctuation and full-width brackets do not matter.
func cutSeriesPrefix(name, series string) (string, bool) {
	target := catalogai.NormalizeName(series)
	if target == "" {
		return name, false
	}
	for i := range name {
		if catalogai.NormalizeName(name[:i]) == target {
			return name[i:], true
		}
	}
	return name, false
}

// leadingClosers are what is left when the series name sat inside brackets
// ("【系列名】专题…") and only the closing bracket remains.
const leadingClosers = "】）)]」』"

// trimLeadingNoise drops separators, stray closing brackets and parenthesised
// qualifiers left in front of the specific part, such as "（全国通用）" after
// the series was cut away.
func trimLeadingNoise(s string) string {
	s = strings.TrimSpace(s)
	for {
		trimmed := strings.TrimSpace(strings.TrimLeft(s, titleNoise+leadingClosers))
		pairs := [][2]string{{"（", "）"}, {"(", ")"}, {"【", "】"}, {"[", "]"}}
		for _, pair := range pairs {
			if !strings.HasPrefix(trimmed, pair[0]) {
				continue
			}
			if end := strings.Index(trimmed, pair[1]); end > 0 {
				trimmed = strings.TrimSpace(trimmed[end+len(pair[1]):])
			}
			break
		}
		if trimmed == s {
			return s
		}
		s = trimmed
	}
}

// trailingQualifiers are version or scope words that do not help tell two sets
// of one series apart.
var trailingQualifiers = []string{"全国通用", "通用版", "全国版", "通用"}

// trimTrailingQualifiers drops trailing "（全国通用）"-style words left after
// the specific part.
func trimTrailingQualifiers(s string) string {
	for {
		trimmed := strings.TrimSpace(s)
		changed := false
		for _, qualifier := range trailingQualifiers {
			for _, pattern := range []string{"（%s）", "(%s)", "【%s】", "[%s]"} {
				suffix := fmt.Sprintf(pattern, qualifier)
				if strings.HasSuffix(trimmed, suffix) {
					trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, suffix))
					changed = true
				}
			}
		}
		if !changed {
			return trimmed
		}
		s = trimmed
	}
}

func truncateRunes(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit])
}

// refreshPackNamesTx recomputes the pack title (unless locked) and the title
// and canonical download file name of every member. It returns the materials
// whose file name changed so local replicas can be renamed after commit.
func (s *Service) refreshPackNamesTx(tx *gorm.DB, packID string) ([]string, error) {
	var pack model.Pack
	if err := tx.Where("id = ?", packID).First(&pack).Error; err != nil {
		return nil, wrap("load pack", err)
	}
	var members []model.Material
	if err := tx.Where("pack_id = ?", packID).Order("position ASC").Order("created_at ASC").Find(&members).Error; err != nil {
		return nil, wrap("load pack members", err)
	}
	title := pack.Title
	if !pack.Locked()[catalogai.FieldTitle] {
		title = packTitleFromCatalogue(pack)
		if title == "" && len(members) > 0 {
			ids := make([]string, 0, len(members))
			for _, m := range members {
				ids = append(ids, m.ID)
			}
			cards := loadCards(tx, ids)
			for _, m := range members {
				if t := cards[m.ID].TitleText; t != "" {
					title = truncateRunes(t, maxTitleRunes)
					break
				}
			}
		}
		if title == "" && pack.Title != "" {
			title = pack.Title
		}
		if title == "" {
			title = "未命名资料"
		}
		if title != pack.Title {
			if err := tx.Model(&model.Pack{}).Where("id = ?", packID).Update("title", title).Error; err != nil {
				return nil, wrap("rename pack", err)
			}
		}
	}
	renamed := []string{}
	used := map[string]int{}
	for _, m := range members {
		label := partLabels[m.Part]
		name := title
		if label != "" {
			name = title + " " + label
		}
		name = truncateRunes(name, maxTitleRunes-8)
		used[name]++
		if n := used[name]; n > 1 {
			name = fmt.Sprintf("%s (%d)", name, n)
		}
		fileName := sanitizeFileName(name) + strings.ToLower(path.Ext(m.OriginalName))
		renamed = append(renamed, m.ID)
		if name == m.Title && fileName == m.FileName {
			continue
		}
		if err := tx.Model(&model.Material{}).Where("id = ?", m.ID).
			Updates(map[string]any{"title": name, "file_name": fileName}).Error; err != nil {
			return nil, wrap("rename material", err)
		}
	}
	return renamed, nil
}

// renameMaterialFiles syncs local replica names after a committed rename.
func (s *Service) renameMaterialFiles(ctx context.Context, ids []string) {
	for _, id := range ids {
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { _, err := s.planMaterialTx(tx, id, false); return err })
		if err != nil {
			s.logf("replan renamed material: %s", jobexec.Redact(err.Error()))
		}
	}
	s.notify()
}

var unsafeFileChars = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]+`)

func sanitizeFileName(name string) string {
	name = unsafeFileChars.ReplaceAllString(name, " ")
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return "资料"
	}
	return name
}

// packCompleteness reports which exam parts a pack has. Questions are present
// when there is a question paper, a full paper or a teacher copy; answers when
// there is an answer file, a full paper or a teacher copy.
func packCompleteness(parts []string) (hasQuestions, hasAnswers, hasSheet bool) {
	for _, p := range parts {
		switch p {
		case model.PartQuestions:
			hasQuestions = true
		case model.PartAnswers:
			hasAnswers = true
		case model.PartFull, model.PartTeacher:
			hasQuestions, hasAnswers = true, true
		case model.PartAnswerSheet:
			hasSheet = true
		}
	}
	return
}
