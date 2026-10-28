package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/model"
)

func (s *Server) handleLogin(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	pair, err := s.svc.Login(c.Request.Context(), req.Username, req.Password, c.ClientIP())
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, loginDTO{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		ExpiresIn:    pair.ExpiresIn,
		User:         toUser(pair.User),
	})
}

func (s *Server) handleRefresh(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	pair, err := s.svc.Refresh(c.Request.Context(), req.RefreshToken)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, loginDTO{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		ExpiresIn:    pair.ExpiresIn,
		User:         toUser(pair.User),
	})
}

func (s *Server) handleLogout(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	_, _, ae := decodeOptionalJSON(c, &req)
	if ae != nil {
		respondError(c, ae)
		return
	}
	if err := s.svc.Logout(c.Request.Context(), principal(c), req.RefreshToken); err != nil {
		respondError(c, err)
		return
	}
	respondNoContent(c)
}

func (s *Server) handleAgentToken(c *gin.Context) {
	key := strings.TrimSpace(c.GetHeader("X-API-Key"))
	if key == "" {
		respondError(c, apperr.Unauthorized("API Key 无效"))
		return
	}
	token, err := s.svc.AgentToken(c.Request.Context(), key)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, agentTokenDTO{AccessToken: token, ExpiresIn: 900})
}

func (s *Server) handleMe(c *gin.Context) {
	user, err := s.svc.Me(c.Request.Context(), principal(c))
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toUser(user))
}

func (s *Server) handleSubjects(c *gin.Context) {
	s.respondData(c, http.StatusOK, model.Subjects)
}
