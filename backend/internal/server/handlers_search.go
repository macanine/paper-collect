package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/model"
)

func (s *Server) handleSearchQuestions(c *gin.Context) {
	var req struct {
		Query   string `json:"query"`
		Subject string `json:"subject"`
		Grade   string `json:"grade"`
		Year    int    `json:"academic_year"`
		Limit   int    `json:"limit"`
		Offset  int    `json:"offset"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	if strings.TrimSpace(req.Query) == "" {
		respondError(c, apperr.Validation("请输入要搜索的题干"))
		return
	}
	if len([]rune(req.Query)) > 2000 {
		respondError(c, apperr.Validation("搜索词不能超过 2000 个字符"))
		return
	}
	if req.Subject != "" && !model.IsSubject(req.Subject) {
		respondError(c, apperr.Validation("subject 不在固定学科字典中"))
		return
	}
	if req.Grade != "" && !isOneOf(req.Grade, model.Grades...) {
		respondError(c, apperr.Validation("grade 不在允许范围内"))
		return
	}
	if req.Year != 0 && (req.Year < 1900 || req.Year > 2200) {
		respondError(c, apperr.Validation("academic_year 必须在 1900 到 2200 之间"))
		return
	}
	result, err := s.svc.SearchQuestions(c.Request.Context(), req.Query, req.Subject, req.Grade, req.Year, req.Limit, req.Offset)
	if err != nil {
		respondError(c, err)
		return
	}
	items := make([]searchHitDTO, 0, len(result.Items))
	for _, hit := range result.Items {
		items = append(items, toSearchHit(hit))
	}
	s.respondData(c, http.StatusOK, searchResultDTO{Items: items, Total: result.Total, TookMS: result.TookMS})
}

func (s *Server) handleGetQuestion(c *gin.Context) {
	hit, err := s.svc.GetQuestion(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toSearchHit(hit))
}

func (s *Server) handleListMaterialQuestions(c *gin.Context) {
	pg, ae := pageQuery(c)
	if ae != nil {
		respondError(c, ae)
		return
	}
	page, err := s.svc.ListMaterialQuestions(c.Request.Context(), c.Param("id"), pg)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toPage(page, toQuestion))
}

func (s *Server) handleGetIndexState(c *gin.Context) {
	state, err := s.svc.GetMaterialIndexState(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toIndexState(state))
}

func (s *Server) handleEnqueueIndex(c *gin.Context) {
	var req struct {
		Force bool `json:"force"`
	}
	if _, empty, ae := decodeOptionalJSON(c, &req); ae != nil {
		respondError(c, ae)
		return
	} else if empty {
		req.Force = false
	}
	state, jobID, err := s.svc.EnqueueMaterialIndex(c.Request.Context(), principal(c), c.Param("id"), req.Force)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusAccepted, gin.H{"job_id": jobID, "state": toIndexState(state)})
}

func (s *Server) handleGetSearchSettings(c *gin.Context) {
	row, err := s.svc.GetSearchSettings(c.Request.Context())
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toSearchSettings(row))
}

func (s *Server) handlePutSearchSettings(c *gin.Context) {
	var req struct {
		Enabled    bool   `json:"enabled"`
		MaxPages   int    `json:"max_pages"`
		BatchPages int    `json:"batch_pages"`
		BatchChars int    `json:"batch_chars"`
		ProviderID string `json:"provider_id"`
		Model      string `json:"model"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	row, err := s.svc.PutSearchSettings(c.Request.Context(), principal(c), model.SearchSettings{
		Enabled: req.Enabled, MaxPages: req.MaxPages, BatchPages: req.BatchPages,
		BatchChars: req.BatchChars, ProviderID: req.ProviderID, Model: req.Model,
	})
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toSearchSettings(row))
}
