package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nic/paper-collect/backend/internal/service"
)

func (s *Server) handleListJobs(c *gin.Context) {
	pg, ae := pageQuery(c)
	if ae != nil {
		respondError(c, ae)
		return
	}
	filter := service.JobFilter{Status: c.Query("status"), Kind: c.Query("kind")}
	page, err := s.svc.ListJobs(c.Request.Context(), filter, pg)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toPage(page, toJob))
}

func (s *Server) handleGetJob(c *gin.Context) {
	job, err := s.svc.GetJob(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toJob(job))
}

func (s *Server) handleRetryJob(c *gin.Context) {
	job, err := s.svc.RetryJob(c.Request.Context(), principal(c), c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusAccepted, toJob(job))
}
