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
	s.respondData(c, http.StatusOK, probeDTO{Models: nonNilStrings(result.Models), Count: result.Count})
}

func (s *Server) handleProbeSavedAIProvider(c *gin.Context) {
	result, err := s.svc.ProbeAIModels(c.Request.Context(), service.ProbeInput{ProviderID: c.Param("id")})
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, probeDTO{Models: nonNilStrings(result.Models), Count: result.Count})
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
	s.respondData(c, http.StatusOK, providerTestDTO{Healthy: result.Healthy, Model: result.Model, Message: result.Message})
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
		OCRProviderID *string `json:"ocr_provider_id"`
		MaxPages      int     `json:"max_pages"`
		MaxImages     int     `json:"max_images"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	view, err := s.svc.PutAISettings(c.Request.Context(), principal(c), service.AISettingsInput{
		OCRProviderID: req.OCRProviderID, MaxPages: req.MaxPages, MaxImages: req.MaxImages,
	})
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toAISettings(view))
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
	s.respondData(c, http.StatusOK, providerTestDTO{Healthy: result.Healthy, Message: result.Message})
}

// handleRereadMaterial lets the AI read one file again; its set is then
// decided again.
func (s *Server) handleRereadMaterial(c *gin.Context) {
	if _, _, ae := decodeOptionalJSON(c, &struct{}{}); ae != nil {
		respondError(c, ae)
		return
	}
	if err := s.svc.RereadMaterial(c.Request.Context(), principal(c), c.Param("id")); err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusAccepted, queuedDTO{Queued: true})
}

func (s *Server) handleGetMaterialCard(c *gin.Context) {
	view, err := s.svc.GetMaterialCard(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toMaterialCard(view))
}

func (s *Server) handleGetPackAI(c *gin.Context) {
	view, err := s.svc.GetPackAI(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toPackAI(view))
}

func (s *Server) handleRedecidePack(c *gin.Context) {
	var req struct {
		Force bool `json:"force"`
	}
	if _, _, ae := decodeOptionalJSON(c, &req); ae != nil {
		respondError(c, ae)
		return
	}
	queued, err := s.svc.RedecidePack(c.Request.Context(), principal(c), c.Param("id"), req.Force)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusAccepted, queuedDTO{Queued: queued})
}

func (s *Server) handleRerunAI(c *gin.Context) {
	var req struct {
		Mode   string `json:"mode"`
		Scope  string `json:"scope"`
		DryRun bool   `json:"dry_run"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	out, err := s.svc.RerunAI(c.Request.Context(), principal(c), req.Mode, req.Scope, req.DryRun)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, rerunDTO{Materials: out.Materials, Packs: out.Packs, EstimatedCalls: out.EstimatedCalls, Applied: out.Applied})
}

func (s *Server) handleListEntities(c *gin.Context) {
	pg, ae := pageQuery(c)
	if ae != nil {
		respondError(c, ae)
		return
	}
	page, err := s.svc.ListEntities(c.Request.Context(), c.Query("kind"), c.Query("q"), pg)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toPage(page, toEntity))
}

func (s *Server) handlePatchEntity(c *gin.Context) {
	var req struct {
		Name          *string  `json:"name"`
		Region        *string  `json:"region"`
		Locked        *bool    `json:"locked"`
		RemoveAliases []string `json:"remove_aliases"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	view, err := s.svc.UpdateEntity(c.Request.Context(), principal(c), c.Param("id"), service.EntityPatch{
		Name: req.Name, Region: req.Region, Locked: req.Locked, RemoveAliases: req.RemoveAliases,
	})
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toEntity(view))
}

func (s *Server) handleMergeEntity(c *gin.Context) {
	var req struct {
		Into string `json:"into"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	view, err := s.svc.MergeEntity(c.Request.Context(), principal(c), c.Param("id"), req.Into)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toEntity(view))
}
