package server

import (
	"archive/zip"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/model"
	"github.com/nic/paper-collect/backend/internal/service"
)

type countDTO struct {
	Count int `json:"count"`
}

type packPatchRequest struct {
	Title        *string   `json:"title"`
	DocType      *string   `json:"doc_type"`
	SubjectCodes *[]string `json:"subject_codes"`
	Grade        *string   `json:"grade"`
	Term         *string   `json:"term"`
	Cohort       *int      `json:"cohort"`
	Region       *string   `json:"region"`
	Organization *string   `json:"organization"`
	ExamDate     *string   `json:"exam_date"`
	ExamName     *string   `json:"exam_name"`
	Chapter      *string   `json:"chapter"`
	Tags         *[]string `json:"tags"`
	Description  *string   `json:"description"`
	UnlockFields []string  `json:"unlock_fields"`
}

func (r packPatchRequest) patch() service.PackPatch {
	return service.PackPatch{
		Title: r.Title, DocType: r.DocType, SubjectCodes: r.SubjectCodes, Grade: r.Grade, Term: r.Term,
		Cohort: r.Cohort, Region: r.Region, Organization: r.Organization, ExamDate: r.ExamDate,
		ExamName: r.ExamName, Chapter: r.Chapter, Tags: r.Tags, Description: r.Description,
		UnlockFields: r.UnlockFields,
	}
}

func (s *Server) handleListPacks(c *gin.Context) {
	pg, ae := pageQuery(c)
	if ae != nil {
		respondError(c, ae)
		return
	}
	f := service.PackFilter{}
	q := strings.TrimSpace(c.Query("q"))
	if utf8.RuneCountInString(q) > 200 {
		respondError(c, apperr.Validation("搜索词不能超过 200 个字符"))
		return
	}
	f.Q = q
	if v := c.Query("subject"); v != "" {
		if !model.IsSubject(v) {
			respondError(c, apperr.Validation("subject 不在固定学科字典中"))
			return
		}
		f.Subject = v
	}
	if v := c.Query("grade"); v != "" {
		if !isOneOf(v, model.Grades...) {
			respondError(c, apperr.Validation("grade 不在允许范围内"))
			return
		}
		f.Grade = v
	}
	if v := c.Query("doc_type"); v != "" {
		if !isOneOf(v, model.DocTypes...) {
			respondError(c, apperr.Validation("doc_type 不在允许范围内"))
			return
		}
		f.DocType = v
	}
	if v := c.Query("term"); v != "" {
		if !isOneOf(v, model.Terms...) {
			respondError(c, apperr.Validation("term 不在允许范围内"))
			return
		}
		f.Term = v
	}
	if v := c.Query("cohort"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1900 || n > 2200 {
			respondError(c, apperr.Validation("cohort 必须是 1900 到 2200 之间的整数"))
			return
		}
		f.Cohort = n
	}
	if v := c.Query("stage"); v != "" {
		if !isOneOf(v, model.MaterialStages...) {
			respondError(c, apperr.Validation("stage 不在允许范围内"))
			return
		}
		f.Stage = v
	}
	if v := c.Query("missing"); v != "" {
		if v != "answers" {
			respondError(c, apperr.Validation("missing 只能是 answers"))
			return
		}
		f.Missing = v
	}
	if v := c.Query("sort"); v != "" {
		if !isOneOf(v, "updated", "cohort", "title") {
			respondError(c, apperr.Validation("sort 只能是 updated、cohort 或 title"))
			return
		}
		f.Sort = v
	}
	f.Region = strings.TrimSpace(c.Query("region"))
	f.Organization = strings.TrimSpace(c.Query("organization"))
	f.Tag = strings.TrimSpace(c.Query("tag"))
	f.CollectionID = strings.TrimSpace(c.Query("collection_id"))
	f.MaterialID = strings.TrimSpace(c.Query("material_id"))
	f.Content = isTrueQuery(c, "content")
	f.Trashed = isTrueQuery(c, "trashed")
	page, err := s.svc.ListPacks(c.Request.Context(), f, pg)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toPage(page, toPack))
}

func (s *Server) handlePackFacets(c *gin.Context) {
	facets, err := s.svc.GetPackFacets(c.Request.Context())
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toFacets(facets))
}

func (s *Server) handleGetPack(c *gin.Context) {
	view, err := s.svc.GetPack(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toPack(view))
}

func (s *Server) handlePatchPack(c *gin.Context) {
	var req packPatchRequest
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	view, err := s.svc.UpdatePack(c.Request.Context(), principal(c), c.Param("id"), req.patch())
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toPack(view))
}

type packConfirmDTO struct {
	Pack     packDTO `json:"pack"`
	Accepted int     `json:"accepted"`
}

func (s *Server) handleConfirmPack(c *gin.Context) {
	var req struct {
		Patch   packPatchRequest `json:"patch"`
		Members []struct {
			ID              string `json:"id"`
			Part            string `json:"part"`
			AnswerStartPage int    `json:"answer_start_page"`
		} `json:"members"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	edits := make([]service.MemberEdit, 0, len(req.Members))
	for _, m := range req.Members {
		edits = append(edits, service.MemberEdit{ID: m.ID, Part: m.Part, AnswerStartPage: m.AnswerStartPage})
	}
	view, n, err := s.svc.ConfirmPack(c.Request.Context(), principal(c), c.Param("id"), req.Patch.patch(), edits)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, packConfirmDTO{Pack: toPack(view), Accepted: n})
}

func (s *Server) handleBulkPatchPacks(c *gin.Context) {
	var req struct {
		IDs   []string         `json:"ids"`
		Patch packPatchRequest `json:"patch"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	n, err := s.svc.BulkUpdatePacks(c.Request.Context(), principal(c), req.IDs, req.Patch.patch())
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, countDTO{Count: n})
}

func (s *Server) handleMergePacks(c *gin.Context) {
	var req struct {
		SourceIDs []string `json:"source_ids"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	view, err := s.svc.MergePacks(c.Request.Context(), principal(c), c.Param("id"), req.SourceIDs)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toPack(view))
}

func (s *Server) handleReorderPack(c *gin.Context) {
	var req struct {
		MaterialIDs []string `json:"material_ids"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	view, err := s.svc.ReorderPack(c.Request.Context(), principal(c), c.Param("id"), req.MaterialIDs)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toPack(view))
}

func (s *Server) handleTrashPacks(c *gin.Context) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	ids := req.IDs
	n, err := s.svc.TrashPacks(c.Request.Context(), principal(c), ids)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, countDTO{Count: n})
}

func (s *Server) handleRestorePacks(c *gin.Context) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	ids := req.IDs
	n, err := s.svc.RestorePacks(c.Request.Context(), principal(c), ids)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, countDTO{Count: n})
}

func (s *Server) handlePurgePacks(c *gin.Context) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := decodeJSON(c, &req); err != nil {
		respondError(c, err)
		return
	}
	ids := req.IDs
	n, err := s.svc.PurgePacks(c.Request.Context(), principal(c), ids)
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, countDTO{Count: n})
}

// handleDownloadPack streams every file of a pack as one ZIP, named by the
// canonical file names. Files are stored without recompression.
func (s *Server) handleDownloadPack(c *gin.Context) {
	ctx := c.Request.Context()
	view, err := s.svc.GetPack(ctx, c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	if len(view.Members) == 0 {
		respondError(c, apperr.NotFound("套卷中没有文件"))
		return
	}
	// Open the first file before writing headers so an unavailable original
	// still yields a JSON error instead of a broken archive.
	first, err := s.svc.OpenMaterialDownload(ctx, view.Members[0].ID)
	if err != nil {
		respondError(c, err)
		return
	}
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", contentDisposition(view.Pack.Title+".zip"))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Status(http.StatusOK)
	zw := zip.NewWriter(c.Writer)
	used := map[string]bool{}
	for i, member := range view.Members {
		download := first
		if i > 0 {
			download, err = s.svc.OpenMaterialDownload(ctx, member.ID)
			if err != nil {
				break
			}
		}
		name := member.FileName
		if used[name] {
			name = strings.TrimSuffix(name, path.Ext(name)) + " (" + strconv.Itoa(i+1) + ")" + path.Ext(name)
		}
		used[name] = true
		w, werr := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store, Flags: 0x800})
		if werr == nil {
			_, werr = io.Copy(w, download.Reader)
		}
		_ = download.Reader.Close()
		if werr != nil {
			break
		}
	}
	_ = zw.Close()
}
