// Package catalogai holds everything about AI cataloguing that does not touch
// the database: prompts, reply parsing, checks, consistency rules and name
// normalisation. Worker nodes and the master use the same code, so a reply is
// judged by identical rules wherever the model was called.
//
// The model makes every decision (what a file is, which files form a set, the
// catalogue of a set, which school a name refers to). Code here never guesses;
// it only verifies a reply against the evidence the model was given, derives
// values that follow arithmetically from the model's own answer, and reports
// problems so the model can correct itself.
package catalogai

import (
	"strings"

	"github.com/nic/paper-collect/backend/internal/model"
)

// Regions is the fixed region list. Provinces plus the national paper types
// that gaokao material is usually labelled with.
var Regions = []string{
	"全国甲卷", "全国乙卷", "新高考I卷", "新高考II卷", "全国卷",
	"北京", "天津", "河北", "山西", "内蒙古", "辽宁", "吉林", "黑龙江", "上海", "江苏",
	"浙江", "安徽", "福建", "江西", "山东", "河南", "湖北", "湖南", "广东", "广西",
	"海南", "重庆", "四川", "贵州", "云南", "西藏", "陕西", "甘肃", "青海", "宁夏",
	"新疆", "香港", "澳门", "台湾",
}

// IsRegion reports whether v is "" or a value from Regions.
func IsRegion(v string) bool {
	if v == "" {
		return true
	}
	for _, r := range Regions {
		if r == v {
			return true
		}
	}
	return false
}

// DocTypeLabels names every document type for prompts and titles.
var DocTypeLabels = map[string]string{
	"gaokao_real": "高考真题", "gaokao_mock": "高考模拟/适应性考试", "zhongkao_real": "中考真题", "zhongkao_mock": "中考模拟",
	"joint": "联考/联盟考", "monthly": "月考", "midterm": "期中", "final": "期末", "opening": "开学考/摸底",
	"unit": "单元测", "weekly": "周测/小测", "topic": "专题练习", "lecture": "讲义", "study_guide": "学案/导学案",
	"courseware": "课件", "homework": "作业/练习册", "other": "其他",
}

// ExamDocTypes are document types whose sets are exam papers.
var ExamDocTypes = map[string]bool{
	"gaokao_real": true, "gaokao_mock": true, "zhongkao_real": true, "zhongkao_mock": true,
	"joint": true, "monthly": true, "midterm": true, "final": true, "opening": true,
	"unit": true, "weekly": true, "topic": true,
}

// ComposedTitleDocTypes are exam types whose display title is composed from
// the catalogue fields (届 + 地区 + 机构 + 类型 + 年级 + 学科). Every other
// document — 专题练习、讲义、学案、课件、作业、其他 — carries a name of its
// own, which becomes the title as the model wrote it.
var ComposedTitleDocTypes = map[string]bool{
	"gaokao_real": true, "gaokao_mock": true, "zhongkao_real": true, "zhongkao_mock": true,
	"joint": true, "monthly": true, "midterm": true, "final": true, "opening": true,
	"unit": true, "weekly": true,
}

// IsComposedTitleDocType reports whether a set of this type is named from its
// catalogue fields rather than by its own name.
func IsComposedTitleDocType(v string) bool { return ComposedTitleDocTypes[v] }

// DocTypeGaokaoReal is the national college entrance exam: one organizer and
// one exam brand, so a set of this type records no organization.
const DocTypeGaokaoReal = "gaokao_real"

// GradeLabels names grade codes.
var GradeLabels = map[string]string{
	"grade7": "初一", "grade8": "初二", "grade9": "初三",
	"grade10": "高一", "grade11": "高二", "grade12": "高三",
}

// RoleLabels names file roles for prompts.
var RoleLabels = map[string]string{
	model.PartQuestions: "试题卷（只有题）", model.PartAnswers: "答案/解析（只有答案）",
	model.PartFull: "前部试题、后部集中答案", model.PartTeacher: "教师版（每题后紧跟答案解析）",
	model.PartAnswerSheet: "答题卡", model.PartListening: "听力音频或听力原文",
	model.PartMain: "讲义/课件/学案等单一资料正文", model.PartOther: "无法判断",
}

// uniqueRoles may appear at most once in one set.
var uniqueRoles = map[string]bool{
	model.PartQuestions: true, model.PartAnswers: true, model.PartFull: true,
	model.PartTeacher: true, model.PartAnswerSheet: true,
}

func isDocType(v string) bool {
	_, ok := DocTypeLabels[v]
	return ok
}

func isGrade(v string) bool {
	_, ok := GradeLabels[v]
	return ok
}

func isRole(v string) bool {
	for _, p := range model.MaterialParts {
		if p == v {
			return true
		}
	}
	return false
}

func subjectCodes() string {
	codes := make([]string, 0, len(model.Subjects))
	for _, s := range model.Subjects {
		codes = append(codes, s.Code+"="+s.Name)
	}
	return strings.Join(codes, "，")
}

func docTypeCodes() string {
	order := []string{"gaokao_real", "gaokao_mock", "zhongkao_real", "zhongkao_mock", "joint", "monthly", "midterm", "final",
		"opening", "unit", "weekly", "topic", "lecture", "study_guide", "courseware", "homework", "other"}
	parts := make([]string, 0, len(order))
	for _, k := range order {
		parts = append(parts, k+"="+DocTypeLabels[k])
	}
	return strings.Join(parts, "，")
}

func roleCodes() string {
	parts := make([]string, 0, len(model.MaterialParts))
	for _, p := range model.MaterialParts {
		parts = append(parts, p+"="+RoleLabels[p])
	}
	return strings.Join(parts, "，")
}

// gradeNumber returns 7..12 for a grade code, 0 otherwise.
func gradeNumber(code string) int {
	switch code {
	case "grade7":
		return 7
	case "grade8":
		return 8
	case "grade9":
		return 9
	case "grade10":
		return 10
	case "grade11":
		return 11
	case "grade12":
		return 12
	}
	return 0
}

// CohortFromSchoolYear returns the graduation year (届) of a class that is in
// grade during the school year starting in start (September of start). Senior
// grades graduate after grade 12, junior grades after grade 9.
func CohortFromSchoolYear(start int, grade string) int {
	g := gradeNumber(grade)
	if g == 0 || start == 0 {
		return 0
	}
	final := 12
	if g <= 9 {
		final = 9
	}
	return start + (final - g) + 1
}

// SchoolYearFromCohort inverts CohortFromSchoolYear.
func SchoolYearFromCohort(cohort int, grade string) int {
	g := gradeNumber(grade)
	if g == 0 || cohort == 0 {
		return 0
	}
	final := 12
	if g <= 9 {
		final = 9
	}
	return cohort - (final - g) - 1
}

// TermOfMonth returns the term a month falls in: August to January is the
// first term, February to July the second.
func TermOfMonth(month int) string {
	switch {
	case month >= 8 || month == 1:
		return "first"
	case month >= 2 && month <= 7:
		return "second"
	}
	return ""
}
