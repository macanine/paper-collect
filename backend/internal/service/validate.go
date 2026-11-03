package service

import (
	"strings"
	"unicode/utf8"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/model"
	"github.com/nic/paper-collect/backend/internal/paths"
)

// Field limits from docs/API.md.
const (
	maxTitleRunes       = 255
	maxDescriptionRunes = 4000
	maxSubjects         = 12
	maxTags             = 30
	maxTagRunes         = 64
	maxYear             = 2200
	minYear             = 1900
	maxUploadEntries    = 2000
	maxUploadBytes      = 512 << 20
	maxBatchBytes       = 2 << 30
	maxCollectionItems  = 5000
)

func runeLen(s string) int { return utf8.RuneCountInString(s) }

func validateTitle(raw string) (string, error) {
	t := strings.TrimSpace(raw)
	if n := runeLen(t); n < 1 || n > maxTitleRunes {
		return "", apperr.Validation("标题长度必须为 1 到 255 个字符")
	}
	return t, nil
}

func validateDescription(raw string) (string, error) {
	if runeLen(raw) > maxDescriptionRunes {
		return "", apperr.Validation("描述不能超过 4000 个字符")
	}
	return raw, nil
}

func validateSubjects(codes []string) ([]string, error) {
	if len(codes) < 1 || len(codes) > maxSubjects {
		return nil, apperr.Validation("学科数量必须为 1 到 12 个")
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if !model.IsSubject(c) {
			return nil, apperr.Validation("学科代码不在固定字典中")
		}
		if seen[c] {
			return nil, apperr.Validation("学科代码不能重复")
		}
		seen[c] = true
	}
	return append([]string{}, codes...), nil
}

// validateOptionalSubjects allows metadata to remain unspecified.
func validateOptionalSubjects(codes []string) ([]string, error) {
	if len(codes) == 0 {
		return []string{}, nil
	}
	return validateSubjects(codes)
}

func validateGrade(g string) error {
	if oneOf(g, model.Grades) {
		return nil
	}
	return apperr.Validation("年级只能是空或 grade7 到 grade12")
}

func validateTerm(t string) error {
	if oneOf(t, model.Terms) {
		return nil
	}
	return apperr.Validation("学期只能是空、first 或 second")
}

func validateDocType(t string) error {
	if t == "" || oneOf(t, model.DocTypes) {
		return nil
	}
	return apperr.Validation("资料类型不在允许范围内")
}

func validatePart(part string) error {
	if oneOf(part, model.MaterialParts) {
		return nil
	}
	return apperr.Validation("文件部件类型不在允许范围内")
}

// validateCohort accepts 0 (unknown) or a graduation year ("届").
func validateCohort(y int) error {
	if y != 0 && (y < minYear || y > maxYear) {
		return apperr.Validation("届别必须为 0 或 1900 到 2200 之间的整数")
	}
	return nil
}

// validateExamDate accepts "" or YYYY-MM.
func validateExamDate(v string) error {
	if v == "" {
		return nil
	}
	if len(v) != 7 || v[4] != '-' {
		return apperr.Validation("考试时间格式必须为 YYYY-MM")
	}
	for i, r := range v {
		if i != 4 && (r < '0' || r > '9') {
			return apperr.Validation("考试时间格式必须为 YYYY-MM")
		}
	}
	if month := v[5:]; month < "01" || month > "12" {
		return apperr.Validation("考试时间月份必须为 01 到 12")
	}
	return nil
}

// validateShortText trims and bounds a free-text catalogue field.
func validateShortText(label, raw string, limit int) (string, error) {
	v := strings.TrimSpace(raw)
	if runeLen(v) > limit {
		return "", apperr.Validation(label + "过长")
	}
	return v, nil
}

func oneOf(v string, allowed []string) bool {
	for _, a := range allowed {
		if a == v {
			return true
		}
	}
	return false
}

func validateTags(raw []string) ([]string, error) {
	if len(raw) > maxTags {
		return nil, apperr.Validation("标签最多 30 个")
	}
	out := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, r := range raw {
		t := strings.TrimSpace(r)
		if n := runeLen(t); n < 1 || n > maxTagRunes {
			return nil, apperr.Validation("每个标签长度必须为 1 到 64 个字符")
		}
		if seen[t] {
			return nil, apperr.Validation("标签不能重复")
		}
		seen[t] = true
		out = append(out, t)
	}
	return out, nil
}

func validateRelativePath(p string) error {
	if err := paths.ValidateFile(p); err != nil {
		return apperr.Validation(err.Error())
	}
	return nil
}

// PageQuery is a validated page request.
type PageQuery struct {
	Page     int
	PageSize int
}

func (q PageQuery) offset() int { return (q.Page - 1) * q.PageSize }

// Page is one page of results with the total count.
type Page[T any] struct {
	Items    []T
	Total    int64
	Page     int
	PageSize int
}

// DefaultPage is used by callers that have no page parameters (e.g. MCP defaults).
var DefaultPage = PageQuery{Page: 1, PageSize: 20}

// likePattern escapes LIKE wildcards with '!' so the ESCAPE clause works on both databases.
func likePattern(q string) string {
	return "%" + likeEscape(strings.ToLower(q)) + "%"
}

// likeEscape escapes LIKE wildcards without changing case.
func likeEscape(q string) string {
	return strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(q)
}
