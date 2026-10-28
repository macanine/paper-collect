package server

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/model"
	"github.com/nic/paper-collect/backend/internal/service"
)

func (s *Server) handleListMaterials(c *gin.Context) {
	pg, ae := pageQuery(c)
	if ae != nil {
		respondError(c, ae)
		return
	}
	filter := service.MaterialFilter{}
	if q, ok := c.GetQuery("q"); ok {
		if utf8.RuneCountInString(strings.TrimSpace(q)) > 200 {
			respondError(c, apperr.Validation("搜索词不能超过 200 个字符"))
			return
		}
		filter.Q = strings.TrimSpace(q)
	}
	if subject, ok := c.GetQuery("subject"); ok && subject != "" {
		if !model.IsSubject(subject) {
			respondError(c, apperr.Validation("subject 不在固定学科字典中"))
			return
		}
		filter.Subject = subject
	}
	if role, ok := c.GetQuery("role"); ok && role != "" {
		if !isOneOf(role, model.MaterialRoles...) {
			respondError(c, apperr.Validation("role 不在允许范围内"))
			return
		}
		filter.Role = role
	}
	if status, ok := c.GetQuery("status"); ok && status != "" {
		if !isOneOf(status, model.StatusPendingStorage, model.StatusArchived, model.StatusDegraded) {
			respondError(c, apperr.Validation("status 不在允许范围内"))
			return
		}
		filter.Status = status
	}
	if grade, ok := c.GetQuery("grade"); ok && grade != "" {
		if !isOneOf(grade, model.Grades...) {
			respondError(c, apperr.Validation("grade 不在允许范围内"))
			return
		}
		filter.Grade = grade
	}
	page, err := s.svc.ListMaterials(c.Request.Context(), filter, pg)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toPage(page, func(v service.MaterialView) materialDTO {
		return toMaterial(v.Material, v.Subjects)
	}))
}

func (s *Server) handleGetMaterial(c *gin.Context) {
	view, err := s.svc.GetMaterial(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toMaterialDetail(view))
}

func (s *Server) handlePatchMaterial(c *gin.Context) {
	var req struct {
		Title        *string   `json:"title"`
		Description  *string   `json:"description"`
		SubjectCodes *[]string `json:"subject_codes"`
		Grade        *string   `json:"grade"`
		AcademicYear *int      `json:"academic_year"`
		Tags         *[]string `json:"tags"`
		Role         *string   `json:"role"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	patch := service.MaterialPatch{
		Title:        req.Title,
		Description:  req.Description,
		SubjectCodes: req.SubjectCodes,
		Grade:        req.Grade,
		AcademicYear: req.AcademicYear,
		Tags:         req.Tags,
		Role:         req.Role,
	}
	view, err := s.svc.UpdateMaterial(c.Request.Context(), principal(c), c.Param("id"), patch)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toMaterialDetail(view))
}

func (s *Server) handleDownloadMaterial(c *gin.Context) {
	download, err := s.svc.OpenMaterialDownload(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	defer download.Reader.Close()

	mediaType := download.MediaType
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	c.Header("Content-Type", mediaType)
	c.Header("Content-Disposition", contentDisposition(download.FileName))
	c.Header("X-Content-Type-Options", "nosniff")
	if download.Size > 0 {
		c.Header("Content-Length", fmt.Sprintf("%d", download.Size))
	}
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, download.Reader)
}

func contentDisposition(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "download"
	}
	fallback := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r > 0x7e || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)
	if fallback == "" {
		fallback = "download"
	}
	return `attachment; filename="` + fallback + `"; filename*=UTF-8''` + url.PathEscape(name)
}

func isOneOf(value string, options ...string) bool {
	for _, o := range options {
		if value == o {
			return true
		}
	}
	return false
}

func (s *Server) handleDeleteMaterial(c *gin.Context) {
	cascade := isTrueQuery(c, "cascade")
	if err := s.svc.DeleteMaterial(c.Request.Context(), principal(c), c.Param("id"), cascade); err != nil {
		respondError(c, err)
		return
	}
	respondNoContent(c)
}

func isTrueQuery(c *gin.Context, name string) bool {
	value := strings.ToLower(strings.TrimSpace(c.Query(name)))
	return value == "true" || value == "1"
}
