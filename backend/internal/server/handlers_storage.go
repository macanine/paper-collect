package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/nic/paper-collect/backend/internal/model"
	"github.com/nic/paper-collect/backend/internal/service"
)

func (s *Server) handleListDrives(c *gin.Context) {
	rows, err := s.svc.ListDrives(c.Request.Context())
	if err != nil {
		respondError(c, err)
		return
	}
	out := make([]driveDTO, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.driveWithInfo(c, r))
	}
	s.respondData(c, http.StatusOK, out)
}

func (s *Server) handleCreateDrive(c *gin.Context) {
	var req struct {
		Name         string            `json:"name"`
		Kind         string            `json:"kind"`
		Enabled      *bool             `json:"enabled"`
		RootPath     string            `json:"root_path"`
		MaxFileBytes int64             `json:"max_file_bytes"`
		AllowedExts  []string          `json:"allowed_exts"`
		ReadOrder    *int              `json:"read_order"`
		OnPathChange string            `json:"on_path_change"`
		Config       map[string]string `json:"config"`
		Secret       map[string]string `json:"secret"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	in := service.DriveInput{
		Name: req.Name, Kind: strings.TrimSpace(req.Kind), Enabled: true, RootPath: strings.TrimSpace(req.RootPath),
		MaxFileBytes: req.MaxFileBytes, AllowedExts: req.AllowedExts, ReadOrder: 100,
		OnPathChange: strings.TrimSpace(req.OnPathChange), Config: req.Config, Secret: req.Secret,
	}
	if req.Enabled != nil {
		in.Enabled = *req.Enabled
	}
	if req.ReadOrder != nil {
		in.ReadOrder = *req.ReadOrder
	}
	if in.Config == nil {
		in.Config = map[string]string{}
	}
	if in.Secret == nil {
		in.Secret = map[string]string{}
	}
	drive, err := s.svc.CreateDrive(c.Request.Context(), principal(c), in)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusCreated, s.driveWithInfo(c, drive))
}

func (s *Server) handlePatchDrive(c *gin.Context) {
	var req struct {
		Name         *string            `json:"name"`
		Enabled      *bool              `json:"enabled"`
		RootPath     *string            `json:"root_path"`
		MaxFileBytes *int64             `json:"max_file_bytes"`
		AllowedExts  *[]string          `json:"allowed_exts"`
		ReadOrder    *int               `json:"read_order"`
		OnPathChange *string            `json:"on_path_change"`
		Config       *map[string]string `json:"config"`
		Secret       *map[string]string `json:"secret"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	drive, err := s.svc.UpdateDrive(c.Request.Context(), principal(c), c.Param("id"), service.DrivePatch{
		Name: req.Name, Enabled: req.Enabled, RootPath: req.RootPath, MaxFileBytes: req.MaxFileBytes,
		AllowedExts: req.AllowedExts, ReadOrder: req.ReadOrder, OnPathChange: req.OnPathChange,
		Config: req.Config, Secret: req.Secret,
	})
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, s.driveWithInfo(c, drive))
}

func (s *Server) handleTestDrive(c *gin.Context) {
	if _, _, err := decodeOptionalJSON(c, &struct{}{}); err != nil {
		respondError(c, err)
		return
	}
	r, err := s.svc.TestDrive(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, driveTestDTO{Healthy: r.Healthy, Message: r.Message, Capabilities: r.Capabilities})
}

func (s *Server) handleListRules(c *gin.Context) {
	rules, err := s.svc.ListRules(c.Request.Context())
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, rules)
}

func (s *Server) handlePreviewRules(c *gin.Context) {
	pg, err := pageQuery(c)
	if err != nil {
		respondError(c, err)
		return
	}
	var req struct {
		Rules []service.SyncRule `json:"rules"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	preview, e := s.svc.PreviewRules(c.Request.Context(), principal(c), req.Rules, (pg.Page-1)*pg.PageSize, pg.PageSize)
	if e != nil {
		respondError(c, e)
		return
	}
	s.respondData(c, http.StatusOK, preview)
}

func (s *Server) handlePutRules(c *gin.Context) {
	var req struct {
		Rules        []service.SyncRule `json:"rules"`
		PreviewToken string             `json:"preview_token"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	n, err := s.svc.PutRules(c.Request.Context(), principal(c), req.Rules, req.PreviewToken)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusAccepted, reconcileDTO{Queued: n})
}

func (s *Server) handleExplainSync(c *gin.Context) {
	actions, err := s.svc.ExplainSync(c.Request.Context(), c.Query("material_id"))
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, actions)
}

func (s *Server) handleSyncMaterial(c *gin.Context) {
	if _, _, err := decodeOptionalJSON(c, &struct{}{}); err != nil {
		respondError(c, err)
		return
	}
	n, err := s.svc.SyncMaterial(c.Request.Context(), principal(c), c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusAccepted, reconcileDTO{Queued: n})
}

func (s *Server) handleListReplicas(c *gin.Context) {
	pg, err := pageQuery(c)
	if err != nil {
		respondError(c, err)
		return
	}
	page, e := s.svc.ListReplicas(c.Request.Context(), c.Query("status"), c.Query("drive_id"), pg)
	if e != nil {
		respondError(c, e)
		return
	}
	s.respondData(c, http.StatusOK, toPage(page, toReplica))
}

func (s *Server) handleBulkReplicas(c *gin.Context) {
	var req struct {
		IDs    []string `json:"ids"`
		Action string   `json:"action"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	results, err := s.svc.BulkReplicas(c.Request.Context(), principal(c), req.IDs, req.Action)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, results)
}

func (s *Server) handleReconcile(c *gin.Context) {
	if _, _, err := decodeOptionalJSON(c, &struct{}{}); err != nil {
		respondError(c, err)
		return
	}
	n, err := s.svc.Reconcile(c.Request.Context(), principal(c))
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusAccepted, reconcileDTO{Queued: n})
}

func (s *Server) handleReplicaShare(c *gin.Context) {
	if _, _, err := decodeOptionalJSON(c, &struct{}{}); err != nil {
		respondError(c, err)
		return
	}
	r, err := s.svc.RefreshReplicaShare(c.Request.Context(), principal(c), c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toReplica(r))
}

// driveWithInfo adds adapter capabilities and replica counts, without network calls.
func (s *Server) driveWithInfo(c *gin.Context, d model.Drive) driveDTO {
	out := toDrive(d)
	out.Capabilities, out.Stats = s.svc.DriveInfo(c.Request.Context(), d)
	return out
}
