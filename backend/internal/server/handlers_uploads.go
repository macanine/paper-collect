package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/nic/paper-collect/backend/internal/service"
)

func (s *Server) handleCreateUpload(c *gin.Context) {
	var req struct {
		Title   string         `json:"title"`
		Hints   uploadHintsDTO `json:"hints"`
		Entries []struct {
			RelativePath string `json:"relative_path"`
			SizeBytes    int64  `json:"size_bytes"`
			SHA256       string `json:"sha256"`
		} `json:"entries"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	in := service.UploadInput{
		Title: req.Title,
		Hints: service.UploadHints{
			SubjectCodes: req.Hints.SubjectCodes, Grade: req.Hints.Grade, DocType: req.Hints.DocType,
			Cohort: req.Hints.Cohort, Region: req.Hints.Region, Organization: req.Hints.Organization, Tags: req.Hints.Tags,
		},
	}
	for _, e := range req.Entries {
		in.Entries = append(in.Entries, service.UploadEntryInput{
			RelativePath: e.RelativePath,
			SizeBytes:    e.SizeBytes,
			SHA256:       e.SHA256,
		})
	}
	view, err := s.svc.CreateUpload(c.Request.Context(), principal(c), in)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusCreated, toUpload(view))
}

func (s *Server) handleListUploads(c *gin.Context) {
	pg, ae := pageQuery(c)
	if ae != nil {
		respondError(c, ae)
		return
	}
	page, err := s.svc.ListUploads(c.Request.Context(), principal(c), pg)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toPage(page, toUpload))
}

func (s *Server) handleGetUpload(c *gin.Context) {
	view, err := s.svc.GetUpload(c.Request.Context(), principal(c), c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toUpload(view))
}

func (s *Server) handlePutUploadEntry(c *gin.Context) {
	declared := c.Request.ContentLength
	entry, err := s.svc.PutEntry(
		c.Request.Context(),
		principal(c),
		c.Param("id"),
		c.Param("entry_id"),
		c.Request.Body,
		declared,
	)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toUploadEntry(entry))
}

func (s *Server) handleFinalizeUpload(c *gin.Context) {
	if _, _, ae := decodeOptionalJSON(c, &struct{}{}); ae != nil {
		respondError(c, ae)
		return
	}
	uploadID := c.Param("id")
	jobID, err := s.svc.Finalize(c.Request.Context(), principal(c), uploadID)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusAccepted, finalizeDTO{JobID: strings.TrimSpace(jobID), UploadID: uploadID})
}
