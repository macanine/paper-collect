package server

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/service"
)

func (s *Server) handleListCollections(c *gin.Context) {
	pg, ae := pageQuery(c)
	if ae != nil {
		respondError(c, ae)
		return
	}
	q := strings.TrimSpace(c.Query("q"))
	if utf8.RuneCountInString(q) > 200 {
		respondError(c, apperr.Validation("搜索词不能超过 200 个字符"))
		return
	}
	filter := service.CollectionFilter{Q: q}
	switch strings.ToLower(strings.TrimSpace(c.Query("roots"))) {
	case "true", "1":
		filter.RootsOnly = true
	}
	filter.ParentID = strings.TrimSpace(c.Query("parent_id"))
	filter.MaterialID = strings.TrimSpace(c.Query("material_id"))
	page, err := s.svc.ListCollections(c.Request.Context(), filter, pg)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toPage(page, toCollection))
}

func (s *Server) handleCreateCollection(c *gin.Context) {
	var req struct {
		Title        string   `json:"title"`
		Description  string   `json:"description"`
		Kind         string   `json:"kind"`
		ParentID     string   `json:"parent_id"`
		SubjectCodes []string `json:"subject_codes"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	in := service.CollectionInput{
		Title:        req.Title,
		Description:  req.Description,
		Kind:         req.Kind,
		ParentID:     req.ParentID,
		SubjectCodes: req.SubjectCodes,
	}
	col, err := s.svc.CreateCollection(c.Request.Context(), principal(c), in)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusCreated, toCollection(col))
}

func (s *Server) handleGetCollection(c *gin.Context) {
	view, err := s.svc.GetCollection(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toCollectionDetail(view))
}

func (s *Server) handlePatchCollection(c *gin.Context) {
	var req struct {
		Title        *string   `json:"title"`
		Description  *string   `json:"description"`
		Kind         *string   `json:"kind"`
		ParentID     *string   `json:"parent_id"`
		SubjectCodes *[]string `json:"subject_codes"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	patch := service.CollectionPatch{
		Title:        req.Title,
		Description:  req.Description,
		Kind:         req.Kind,
		ParentID:     req.ParentID,
		SubjectCodes: req.SubjectCodes,
	}
	col, err := s.svc.UpdateCollection(c.Request.Context(), principal(c), c.Param("id"), patch)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toCollection(col))
}

func (s *Server) handlePutCollectionItems(c *gin.Context) {
	var req struct {
		Items []struct {
			MaterialID   string  `json:"material_id"`
			Position     *int    `json:"position"`
			Role         *string `json:"role"`
			RelativePath *string `json:"relative_path"`
		} `json:"items"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	items := make([]service.ItemInput, 0, len(req.Items))
	for _, it := range req.Items {
		items = append(items, service.ItemInput{
			MaterialID:   it.MaterialID,
			Position:     it.Position,
			Role:         it.Role,
			RelativePath: it.RelativePath,
		})
	}
	view, err := s.svc.ReplaceItems(c.Request.Context(), principal(c), c.Param("id"), items)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toCollectionDetail(view))
}

func (s *Server) handleDeleteCollection(c *gin.Context) {
	cascade := isTrueQuery(c, "cascade")
	if err := s.svc.DeleteCollection(c.Request.Context(), principal(c), c.Param("id"), cascade); err != nil {
		respondError(c, err)
		return
	}
	respondNoContent(c)
}
