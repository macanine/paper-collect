package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/service"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
)

func pageQuery(c *gin.Context) (service.PageQuery, *apperr.Error) {
	page, err := queryInt(c, "page", 1)
	if err != nil {
		return service.PageQuery{}, err
	}
	pageSize, err := queryInt(c, "page_size", defaultPageSize)
	if err != nil {
		return service.PageQuery{}, err
	}
	if page < 1 {
		return service.PageQuery{}, apperr.Validation("page 必须大于等于 1")
	}
	if pageSize < 1 || pageSize > maxPageSize {
		return service.PageQuery{}, apperr.Validation("page_size 必须在 1 到 100 之间")
	}
	return service.PageQuery{Page: page, PageSize: pageSize}, nil
}

func queryInt(c *gin.Context, name string, fallback int) (int, *apperr.Error) {
	raw, ok := c.GetQuery(name)
	if !ok || raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, apperr.Validation("查询参数 " + name + " 必须是整数")
	}
	return value, nil
}

func (s *Server) handleListUsers(c *gin.Context) {
	pg, ae := pageQuery(c)
	if ae != nil {
		respondError(c, ae)
		return
	}
	page, err := s.svc.ListUsers(c.Request.Context(), pg)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toPage(page, toUser))
}

func (s *Server) handleCreateUser(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	user, err := s.svc.CreateUser(c.Request.Context(), principal(c), req.Username, req.Password, req.Role)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusCreated, toUser(user))
}

func (s *Server) handleListKeys(c *gin.Context) {
	pg, ae := pageQuery(c)
	if ae != nil {
		respondError(c, ae)
		return
	}
	page, err := s.svc.ListKeys(c.Request.Context(), pg)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toPage(page, toAPIKey))
}

func (s *Server) handleCreateKey(c *gin.Context) {
	var req struct {
		Name      string   `json:"name"`
		Scopes    []string `json:"scopes"`
		ExpiresAt *string  `json:"expires_at"`
	}
	if err := decodeJSON(c, &req, "expires_at"); err != nil {
		respondError(c, err)
		return
	}
	var expiresAt *time.Time
	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		parsed, perr := time.Parse(time.RFC3339, *req.ExpiresAt)
		if perr != nil {
			respondError(c, apperr.Validation("expires_at 必须是 RFC3339 时间"))
			return
		}
		expiresAt = &parsed
	}
	key, secret, err := s.svc.CreateKey(c.Request.Context(), principal(c), req.Name, req.Scopes, expiresAt)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusCreated, createKeyDTO{Key: toAPIKey(key), Secret: secret})
}

func (s *Server) handleRevokeKey(c *gin.Context) {
	if err := s.svc.RevokeKey(c.Request.Context(), principal(c), c.Param("id")); err != nil {
		respondError(c, err)
		return
	}
	respondNoContent(c)
}

func (s *Server) handleListAudit(c *gin.Context) {
	pg, ae := pageQuery(c)
	if ae != nil {
		respondError(c, ae)
		return
	}
	page, err := s.svc.ListAudit(pg)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toPage(page, toAudit))
}
