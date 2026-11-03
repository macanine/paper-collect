package server

import (
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
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
	if stage := c.Query("stage"); stage != "" {
		if !isOneOf(stage, model.MaterialStages...) {
			respondError(c, apperr.Validation("stage 不在允许范围内"))
			return
		}
		filter.Stage = stage
	}
	if part := c.Query("part"); part != "" {
		if !isOneOf(part, model.MaterialParts...) {
			respondError(c, apperr.Validation("part 不在允许范围内"))
			return
		}
		filter.Part = part
	}
	if status := c.Query("status"); status != "" {
		if !isOneOf(status, model.StatusPendingStorage, model.StatusArchived, model.StatusDegraded) {
			respondError(c, apperr.Validation("status 不在允许范围内"))
			return
		}
		filter.Status = status
	}
	if reason := c.Query("reason"); reason != "" {
		if !isOneOf(reason, model.ReviewLowConfidence, model.ReviewPackSuggestion, model.ReviewDuplicate, model.ReviewAnalysisFailed, model.ReviewManual) {
			respondError(c, apperr.Validation("reason 不在允许范围内"))
			return
		}
		filter.Reason = reason
	}
	filter.PackID = strings.TrimSpace(c.Query("pack_id"))
	if sha, ok := c.GetQuery("sha256"); ok && sha != "" {
		value := strings.ToLower(strings.TrimSpace(sha))
		if _, err := hex.DecodeString(value); err != nil || len(value) != 64 {
			respondError(c, apperr.Validation("sha256 必须是 64 位十六进制字符串"))
			return
		}
		filter.SHA256 = value
	}
	page, err := s.svc.ListMaterials(c.Request.Context(), filter, pg)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toPage(page, toMaterial))
}

func (s *Server) handleLibraryStats(c *gin.Context) {
	counts, err := s.svc.GetLibraryStats(c.Request.Context())
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, libraryStatsDTO{
		Packs: counts.Packs, Files: counts.Files, Processing: counts.Processing, Review: counts.Review,
		Ready: counts.Ready, Failed: counts.Failed, MissingAnswers: counts.MissingAnswers, Trashed: counts.Trashed,
		PendingStorage: counts.PendingStorage, Archived: counts.Archived, Degraded: counts.Degraded,
	})
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
		Part            *string `json:"part"`
		AnswerStartPage *int    `json:"answer_start_page"`
		PackID          *string `json:"pack_id"`
		NewPack         bool    `json:"new_pack"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	patch := service.MaterialPatch{Part: req.Part, AnswerStartPage: req.AnswerStartPage, PackID: req.PackID, NewPack: req.NewPack}
	view, err := s.svc.UpdateMaterial(c.Request.Context(), principal(c), c.Param("id"), patch)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toMaterialDetail(view))
}

func (s *Server) handleAcceptMaterials(c *gin.Context) {
	var req struct {
		IDs              []string `json:"ids"`
		AcceptSuggestion bool     `json:"accept_suggestion"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	n, err := s.svc.AcceptMaterials(c.Request.Context(), principal(c), req.IDs, req.AcceptSuggestion)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, countDTO{Count: n})
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

func (s *Server) handlePullMaterialCache(c *gin.Context) {
	cache, err := s.svc.PullMaterialCache(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store, max-age=0")
	s.respondData(c, http.StatusOK, pullCacheDTO{
		DownloadURL: "/api/v1/materials/" + url.PathEscape(c.Param("id")) + "/pull-cache",
		FileName:    cache.FileName,
		MediaType:   cache.MediaType,
		SizeBytes:   cache.SizeBytes,
		SHA256:      cache.SHA256,
		ExpiresAt:   cache.ExpiresAt,
		ExpiresIn:   cache.ExpiresIn,
	})
}

func (s *Server) handleDownloadPullCache(c *gin.Context) {
	download, err := s.svc.OpenMaterialPullCache(c.Request.Context(), c.Param("id"))
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
	c.Header("Content-Length", fmt.Sprintf("%d", download.SizeBytes))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "private, no-store, max-age=0")
	c.Header("X-Pull-Cache-Expires-At", download.ExpiresAt.Format(time.RFC3339))
	c.Header("X-Pull-Cache-Expires-In", fmt.Sprintf("%d", download.ExpiresIn))
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, download.Reader)
}

func (s *Server) handlePreviewMaterial(c *gin.Context) {
	preview, err := s.svc.OpenMaterialPreview(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	defer preview.Reader.Close()
	c.Header("Content-Type", "application/pdf")
	c.Header("Content-Disposition", "inline")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "private, no-store")
	if preview.Size > 0 {
		c.Header("Content-Length", fmt.Sprintf("%d", preview.Size))
	}
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, preview.Reader)
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
	if err := s.svc.TrashMaterial(c.Request.Context(), principal(c), c.Param("id")); err != nil {
		respondError(c, err)
		return
	}
	respondNoContent(c)
}

func isTrueQuery(c *gin.Context, name string) bool {
	value := strings.ToLower(strings.TrimSpace(c.Query(name)))
	return value == "true" || value == "1"
}
