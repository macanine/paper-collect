package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/nic/paper-collect/backend/internal/model"
	"github.com/nic/paper-collect/backend/internal/service"
)

func (s *Server) handleListAIProviders(c *gin.Context) {
	items, err := s.svc.ListAIProviders(c.Request.Context())
	if err != nil {
		respondError(c, err)
		return
	}
	out := make([]aiProviderDTO, 0, len(items))
	for _, item := range items {
		out = append(out, toAIProvider(item))
	}
	s.respondData(c, http.StatusOK, out)
}

func (s *Server) handleCreateAIProvider(c *gin.Context) {
	var req struct {
		Name         string   `json:"name"`
		BaseURL      string   `json:"base_url"`
		APIKey       *string  `json:"api_key"`
		Models       []string `json:"models"`
		DefaultModel string   `json:"default_model"`
		VisionModel  string   `json:"vision_model"`
		Enabled      *bool    `json:"enabled"`
		Priority     *int     `json:"priority"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	in := service.AIProviderInput{
		Name: req.Name, BaseURL: req.BaseURL, APIKey: req.APIKey,
		Models: req.Models, DefaultModel: req.DefaultModel, VisionModel: req.VisionModel,
		Enabled: true, Priority: 100,
	}
	if req.Enabled != nil {
		in.Enabled = *req.Enabled
	}
	if req.Priority != nil {
		in.Priority = *req.Priority
	}
	view, err := s.svc.CreateAIProvider(c.Request.Context(), principal(c), in)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusCreated, toAIProvider(view))
}

func (s *Server) handlePatchAIProvider(c *gin.Context) {
	var req struct {
		Name         *string   `json:"name"`
		BaseURL      *string   `json:"base_url"`
		APIKey       *string   `json:"api_key"`
		Models       *[]string `json:"models"`
		DefaultModel *string   `json:"default_model"`
		VisionModel  *string   `json:"vision_model"`
		Enabled      *bool     `json:"enabled"`
		Priority     *int      `json:"priority"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	view, err := s.svc.UpdateAIProvider(c.Request.Context(), principal(c), c.Param("id"), service.AIProviderPatch{
		Name: req.Name, BaseURL: req.BaseURL, APIKey: req.APIKey, Models: req.Models,
		DefaultModel: req.DefaultModel, VisionModel: req.VisionModel,
		Enabled: req.Enabled, Priority: req.Priority,
	})
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toAIProvider(view))
}

func (s *Server) handleProbeAIProviders(c *gin.Context) {
	var req struct {
		BaseURL    string  `json:"base_url"`
		APIKey     *string `json:"api_key"`
		ProviderID string  `json:"provider_id"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	result, err := s.svc.ProbeAIModels(c.Request.Context(), service.ProbeInput{
		BaseURL: req.BaseURL, APIKey: req.APIKey, ProviderID: req.ProviderID,
	})
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, gin.H{"models": result.Models, "count": result.Count})
}

func (s *Server) handleProbeSavedAIProvider(c *gin.Context) {
	result, err := s.svc.ProbeAIModels(c.Request.Context(), service.ProbeInput{ProviderID: c.Param("id")})
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, gin.H{"models": result.Models, "count": result.Count})
}

func (s *Server) handleTestAIProvider(c *gin.Context) {
	var req struct {
		Model string `json:"model"`
	}
	if _, empty, ae := decodeOptionalJSON(c, &req); ae != nil {
		respondError(c, ae)
		return
	} else if empty {
		req.Model = ""
	}
	result, err := s.svc.TestAIProvider(c.Request.Context(), c.Param("id"), req.Model)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, gin.H{"healthy": result.Healthy, "model": result.Model, "message": result.Message})
}

func (s *Server) handleGetAISettings(c *gin.Context) {
	view, err := s.svc.GetAISettings(c.Request.Context())
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toAISettings(view))
}

func (s *Server) handlePutAISettings(c *gin.Context) {
	var req struct {
		AnalysisEnabled bool    `json:"analysis_enabled"`
		OCRProviderID   *string `json:"ocr_provider_id"`
		MinerUBaseURL   string  `json:"mineru_base_url"`
		MinerUToken     *string `json:"mineru_token"`
		MaxPages        int     `json:"max_pages"`
		MaxImages       int     `json:"max_images"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	view, err := s.svc.PutAISettings(c.Request.Context(), principal(c), service.AISettingsInput{
		AnalysisEnabled: req.AnalysisEnabled, OCRProviderID: req.OCRProviderID,
		MinerUBaseURL: req.MinerUBaseURL,
		MinerUToken:   req.MinerUToken, MaxPages: req.MaxPages, MaxImages: req.MaxImages,
	})
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toAISettings(view))
}

func (s *Server) handleTestMinerU(c *gin.Context) {
	healthy, message, err := s.svc.TestMinerU(c.Request.Context())
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, gin.H{"healthy": healthy, "message": message})
}

func (s *Server) handleListOCRProviders(c *gin.Context) {
	items, err := s.svc.ListOCRProviders(c.Request.Context())
	if err != nil {
		respondError(c, err)
		return
	}
	out := make([]ocrProviderDTO, 0, len(items))
	for _, item := range items {
		out = append(out, toOCRProvider(item))
	}
	s.respondData(c, http.StatusOK, out)
}

func (s *Server) handleCreateOCRProvider(c *gin.Context) {
	var req struct {
		Name     string  `json:"name"`
		Kind     string  `json:"kind"`
		BaseURL  string  `json:"base_url"`
		Token    *string `json:"token"`
		Model    string  `json:"model"`
		Enabled  *bool   `json:"enabled"`
		Priority *int    `json:"priority"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	in := service.OCRProviderInput{
		Name: req.Name, Kind: strings.TrimSpace(req.Kind), BaseURL: req.BaseURL,
		Token: req.Token, Model: req.Model, Enabled: true, Priority: 100,
	}
	if in.Kind == "" {
		in.Kind = model.OCRKindPaddleOCR
	}
	if req.Enabled != nil {
		in.Enabled = *req.Enabled
	}
	if req.Priority != nil {
		in.Priority = *req.Priority
	}
	view, err := s.svc.CreateOCRProvider(c.Request.Context(), principal(c), in)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusCreated, toOCRProvider(view))
}

func (s *Server) handlePatchOCRProvider(c *gin.Context) {
	var req struct {
		Name     *string `json:"name"`
		BaseURL  *string `json:"base_url"`
		Token    *string `json:"token"`
		Model    *string `json:"model"`
		Enabled  *bool   `json:"enabled"`
		Priority *int    `json:"priority"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	view, err := s.svc.UpdateOCRProvider(c.Request.Context(), principal(c), c.Param("id"), service.OCRProviderPatch{
		Name: req.Name, BaseURL: req.BaseURL, Token: req.Token, Model: req.Model,
		Enabled: req.Enabled, Priority: req.Priority,
	})
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toOCRProvider(view))
}

func (s *Server) handleTestOCRProvider(c *gin.Context) {
	result, err := s.svc.TestOCRProvider(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, gin.H{"healthy": result.Healthy, "message": result.Message})
}

func (s *Server) handleEnqueueAnalysis(c *gin.Context) {
	var req struct {
		Force bool `json:"force"`
	}
	if _, empty, ae := decodeOptionalJSON(c, &req); ae != nil {
		respondError(c, ae)
		return
	} else if empty {
		req.Force = false
	}
	view, jobID, err := s.svc.EnqueueMaterialAnalysis(c.Request.Context(), principal(c), c.Param("id"), req.Force)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusAccepted, gin.H{"job_id": jobID, "analysis": toMaterialAnalysis(view)})
}

func (s *Server) handleGetAnalysis(c *gin.Context) {
	view, err := s.svc.GetMaterialAnalysis(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toMaterialAnalysis(view))
}

func (s *Server) handleApplyAnalysis(c *gin.Context) {
	var req struct {
		Title        *string   `json:"title"`
		SubjectCodes *[]string `json:"subject_codes"`
		Grade        *string   `json:"grade"`
		AcademicYear *int      `json:"academic_year"`
		Role         *string   `json:"role"`
		Tags         *[]string `json:"tags"`
	}
	if _, _, ae := decodeOptionalJSON(c, &req); ae != nil {
		respondError(c, ae)
		return
	}
	view, err := s.svc.ApplyMaterialAnalysis(c.Request.Context(), principal(c), c.Param("id"), &service.MaterialPatch{
		Title: req.Title, SubjectCodes: req.SubjectCodes, Grade: req.Grade,
		AcademicYear: req.AcademicYear, Role: req.Role, Tags: req.Tags,
	})
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toMaterialDetail(view))
}
