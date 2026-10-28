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
	for _, v := range model.Grades {
		if v == g {
			return nil
		}
	}
	return apperr.Validation("年级只能是空、grade10、grade11 或 grade12")
}

func validateYear(y int) error {
	if y != 0 && (y < minYear || y > maxYear) {
		return apperr.Validation("学年必须为 0 或 1900 到 2200 之间的整数")
	}
	return nil
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

func validateRole(role string) error {
	for _, r := range model.MaterialRoles {
		if r == role {
			return nil
		}
	}
	return apperr.Validation("资料角色不在允许范围内")
}

func validateCollectionKind(kind string) error {
	for _, k := range model.CollectionKinds {
		if k == kind {
			return nil
		}
	}
	return apperr.Validation("合集类型不在允许范围内")
}

func validateRelativePath(p string) error {
	if err := paths.ValidateFile(p); err != nil {
		return apperr.Validation(err.Error())
	}
	return nil
}

// validatePage normalises pagination. page >= 1 and 1 <= page_size <= 100.
func validatePage(page, pageSize int) (PageQuery, error) {
	if page < 1 {
		return PageQuery{}, apperr.Validation("page 必须大于等于 1")
	}
	if pageSize < 1 || pageSize > 100 {
		return PageQuery{}, apperr.Validation("page_size 必须在 1 到 100 之间")
	}
	return PageQuery{Page: page, PageSize: pageSize}, nil
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
	r := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_")
	return "%" + r.Replace(strings.ToLower(q)) + "%"
}
