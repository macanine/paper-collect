package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/nic/paper-collect/backend/internal/model"
	"github.com/nic/paper-collect/backend/internal/service"
)

func (s *Server) handleListProviders(c *gin.Context) {
	providers, err := s.svc.ListProviders(c.Request.Context())
	if err != nil {
		respondError(c, err)
		return
	}
	out := make([]providerDTO, 0, len(providers))
	for _, p := range providers {
		out = append(out, toProvider(p))
	}
	s.respondData(c, http.StatusOK, out)
}

func (s *Server) handleCreateProvider(c *gin.Context) {
	var req struct {
		Name     string            `json:"name"`
		Kind     string            `json:"kind"`
		Role     string            `json:"role"`
		Enabled  *bool             `json:"enabled"`
		Priority *int              `json:"priority"`
		Weight   *int              `json:"weight"`
		Config   map[string]string `json:"config"`
		Secret   map[string]string `json:"secret"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	in := service.ProviderInput{
		Name:     req.Name,
		Kind:     strings.TrimSpace(req.Kind),
		Role:     strings.TrimSpace(req.Role),
		Enabled:  true,
		Priority: 100,
		Weight:   1,
		Config:   req.Config,
		Secret:   req.Secret,
	}
	if in.Role == "" {
		in.Role = model.RolePrimary
	}
	if req.Enabled != nil {
		in.Enabled = *req.Enabled
	}
	if req.Priority != nil {
		in.Priority = *req.Priority
	}
	if req.Weight != nil {
		in.Weight = *req.Weight
	}
	if in.Config == nil {
		in.Config = map[string]string{}
	}
	if in.Secret == nil {
		in.Secret = map[string]string{}
	}
	provider, err := s.svc.CreateProvider(c.Request.Context(), principal(c), in)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusCreated, toProvider(provider))
}

func (s *Server) handlePatchProvider(c *gin.Context) {
	var req struct {
		Name     *string            `json:"name"`
		Role     *string            `json:"role"`
		Enabled  *bool              `json:"enabled"`
		Priority *int               `json:"priority"`
		Weight   *int               `json:"weight"`
		Config   *map[string]string `json:"config"`
		Secret   *map[string]string `json:"secret"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	patch := service.ProviderPatch{
		Name:     req.Name,
		Role:     req.Role,
		Enabled:  req.Enabled,
		Priority: req.Priority,
		Weight:   req.Weight,
		Config:   req.Config,
		Secret:   req.Secret,
	}
	provider, err := s.svc.UpdateProvider(c.Request.Context(), principal(c), c.Param("id"), patch)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toProvider(provider))
}

func (s *Server) handleTestProvider(c *gin.Context) {
	result, err := s.svc.TestProvider(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, providerTestDTO{
		Healthy:      result.Healthy,
		Message:      result.Message,
		Capabilities: result.Capabilities,
	})
}

func (s *Server) handleGetPolicy(c *gin.Context) {
	policy, err := s.svc.GetPolicy(c.Request.Context())
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toPolicy(policy))
}

func (s *Server) handlePutPolicy(c *gin.Context) {
	var req struct {
		Mode            string `json:"mode"`
		MinimumReplicas int    `json:"minimum_replicas"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	policy, err := s.svc.PutPolicy(c.Request.Context(), principal(c), service.PolicyInput{
		Mode:            strings.TrimSpace(req.Mode),
		MinimumReplicas: req.MinimumReplicas,
	})
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toPolicy(policy))
}

func (s *Server) handleReconcile(c *gin.Context) {
	if _, _, ae := decodeOptionalJSON(c, &struct{}{}); ae != nil {
		respondError(c, ae)
		return
	}
	queued, err := s.svc.Reconcile(c.Request.Context(), principal(c))
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusAccepted, reconcileDTO{Queued: queued})
}

func (s *Server) handleReplicaShare(c *gin.Context) {
	if _, _, ae := decodeOptionalJSON(c, &struct{}{}); ae != nil {
		respondError(c, ae)
		return
	}
	replica, err := s.svc.RefreshReplicaShare(c.Request.Context(), principal(c), c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toReplica(replica))
}
