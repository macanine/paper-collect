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
	filter := service.CollectionFilter{Q: q, PackID: strings.TrimSpace(c.Query("pack_id"))}
	page, err := s.svc.ListCollections(c.Request.Context(), filter, pg)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toPage(page, toCollection))
}

func (s *Server) handleCreateCollection(c *gin.Context) {
	var req struct {
		Title       string   `json:"title"`
		Description string   `json:"description"`
		PackIDs     []string `json:"pack_ids"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	view, err := s.svc.CreateCollection(c.Request.Context(), principal(c), service.CollectionInput{
		Title: req.Title, Description: req.Description, PackIDs: req.PackIDs,
	})
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusCreated, toCollectionDetail(view))
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
		Title       *string `json:"title"`
		Description *string `json:"description"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	view, err := s.svc.UpdateCollection(c.Request.Context(), principal(c), c.Param("id"),
		service.CollectionPatch{Title: req.Title, Description: req.Description})
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toCollectionDetail(view))
}

func (s *Server) handlePutCollectionItems(c *gin.Context) {
	var req struct {
		PackIDs []string `json:"pack_ids"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	view, err := s.svc.ReplaceItems(c.Request.Context(), principal(c), c.Param("id"), req.PackIDs)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toCollectionDetail(view))
}

func (s *Server) handleAddCollectionPacks(c *gin.Context) {
	var req struct {
		PackIDs []string `json:"pack_ids"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	view, err := s.svc.AddCollectionPacks(c.Request.Context(), principal(c), c.Param("id"), req.PackIDs)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toCollectionDetail(view))
}

func (s *Server) handleDeleteCollection(c *gin.Context) {
	if err := s.svc.DeleteCollection(c.Request.Context(), principal(c), c.Param("id")); err != nil {
		respondError(c, err)
		return
	}
	respondNoContent(c)
}
