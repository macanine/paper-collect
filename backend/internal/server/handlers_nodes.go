package server

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/jobexec"
	"github.com/nic/paper-collect/backend/internal/model"
	"github.com/nic/paper-collect/backend/internal/service"
)

// A job result carries parsed document text, so it may exceed the 1 MiB limit
// of ordinary API bodies. Nodes send it gzip-compressed.
const maxNodeResultBytes = 32 << 20

const nodeKey = "node"

// nodeAuth accepts only worker node tokens. Browser sessions and API keys are
// rejected, and node tokens are rejected everywhere else.
func (s *Server) nodeAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		n, err := s.svc.AuthenticateNode(c.Request.Context(), bearerToken(c))
		if err != nil {
			respondError(c, err)
			return
		}
		c.Set(nodeKey, n)
		c.Next()
	}
}

func currentNode(c *gin.Context) model.Node {
	n, _ := c.Get(nodeKey)
	node, _ := n.(model.Node)
	return node
}

func (s *Server) handleNodeHeartbeat(c *gin.Context) {
	var req service.Heartbeat
	if ae := decodeJSON(c, &req); ae != nil {
		respondError(c, ae)
		return
	}
	reply, err := s.svc.NodeHeartbeat(c.Request.Context(), currentNode(c), req, c.ClientIP())
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, reply)
}

func (s *Server) handleNodeClaim(c *gin.Context) {
	var req struct {
		Capacity       map[string]int `json:"capacity"`
		OCRProviderIDs []string       `json:"ocr_provider_ids"`
		WaitMS         int            `json:"wait_ms"`
		Protocol       int            `json:"protocol"`
	}
	if ae := decodeJSON(c, &req); ae != nil {
		respondError(c, ae)
		return
	}
	jobs, err := s.svc.ClaimForNode(c.Request.Context(), currentNode(c), service.NodeClaim{
		Capacity: req.Capacity, OCRProviderIDs: req.OCRProviderIDs,
		Wait: time.Duration(req.WaitMS) * time.Millisecond, Protocol: req.Protocol,
	})
	if err != nil {
		respondError(c, err)
		return
	}
	if len(jobs) == 0 {
		respondNoContent(c)
		return
	}
	s.respondData(c, http.StatusOK, nodeClaimDTO{Jobs: jobs})
}

func (s *Server) handleNodeProgress(c *gin.Context) {
	var req struct {
		Message string         `json:"message"`
		Current int            `json:"current"`
		Total   int            `json:"total"`
		Metrics map[string]any `json:"metrics"`
	}
	if ae := decodeJSON(c, &req, "metrics"); ae != nil {
		respondError(c, ae)
		return
	}
	if err := s.svc.NodeProgress(c.Request.Context(), currentNode(c), c.Param("id"), req.Message, req.Current, req.Total, req.Metrics); err != nil {
		respondError(c, err)
		return
	}
	respondNoContent(c)
}

func (s *Server) handleNodeComplete(c *gin.Context) {
	body := io.Reader(http.MaxBytesReader(c.Writer, c.Request.Body, maxNodeResultBytes))
	if strings.EqualFold(c.GetHeader("Content-Encoding"), "gzip") {
		zr, err := gzip.NewReader(body)
		if err != nil {
			respondError(c, apperr.BadRequest("gzip 请求体无效"))
			return
		}
		defer zr.Close()
		body = io.LimitReader(zr, maxNodeResultBytes)
	}
	var out jobexec.Result
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			respondError(c, apperr.TooLarge("任务结果过大"))
			return
		}
		respondError(c, apperr.BadRequest("任务结果格式错误"))
		return
	}
	if err := s.svc.NodeComplete(c.Request.Context(), currentNode(c), c.Param("id"), out); err != nil {
		respondError(c, err)
		return
	}
	respondNoContent(c)
}

func (s *Server) handleNodeFail(c *gin.Context) {
	var req struct {
		Error       string         `json:"error"`
		Extra       map[string]int `json:"extra"`
		Interrupted bool           `json:"interrupted"`
		Class       string         `json:"class"`
	}
	if ae := decodeJSON(c, &req, "extra"); ae != nil {
		respondError(c, ae)
		return
	}
	if err := s.svc.NodeFail(c.Request.Context(), currentNode(c), c.Param("id"), req.Error, req.Class, req.Extra, req.Interrupted); err != nil {
		respondError(c, err)
		return
	}
	respondNoContent(c)
}

// handleNodeBlob streams an original. Local files support Range requests so a
// node can resume a large download.
func (s *Server) handleNodeBlob(c *gin.Context) {
	body, size, err := s.svc.OpenBlobForNode(c.Request.Context(), c.Param("sha"))
	if err != nil {
		respondError(c, err)
		return
	}
	defer body.Close()
	c.Header("Content-Type", "application/octet-stream")
	if seeker, ok := body.(io.ReadSeeker); ok {
		http.ServeContent(c.Writer, c.Request, "", time.Time{}, seeker)
		return
	}
	c.Header("Content-Length", strconv.FormatInt(size, 10))
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, body); err != nil {
		c.Abort()
	}
}

func (s *Server) handleNodeRefreshToken(c *gin.Context) {
	var req struct {
		Previous string `json:"previous"`
		Next     string `json:"next"`
	}
	if ae := decodeJSON(c, &req); ae != nil {
		respondError(c, ae)
		return
	}
	if err := s.svc.NodeSaveRefreshToken(c.Request.Context(), currentNode(c), c.Param("id"), req.Previous, req.Next); err != nil {
		respondError(c, err)
		return
	}
	respondNoContent(c)
}

// Administration of nodes.

func (s *Server) handleListNodes(c *gin.Context) {
	nodes, err := s.svc.ListNodes(c.Request.Context())
	if err != nil {
		respondError(c, err)
		return
	}
	out := make([]nodeDTO, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, toNode(n))
	}
	s.respondData(c, http.StatusOK, out)
}

func (s *Server) handleCreateNode(c *gin.Context) {
	var req struct {
		Name           string          `json:"name"`
		Slots          *map[string]int `json:"slots"`
		CacheMaxBytes  *int64          `json:"cache_max_bytes"`
		OCRProviderIDs *[]string       `json:"ocr_provider_ids"`
	}
	if ae := decodeJSON(c, &req); ae != nil {
		respondError(c, ae)
		return
	}
	n, token, err := s.svc.CreateNode(c.Request.Context(), principal(c), service.NodeInput{
		Name: req.Name, Slots: req.Slots, CacheMaxBytes: req.CacheMaxBytes, OCRProviderIDs: req.OCRProviderIDs,
	})
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusCreated, nodeCreatedDTO{nodeDTO: toNode(n), Token: token})
}

func (s *Server) handlePatchNode(c *gin.Context) {
	var req struct {
		Name           *string         `json:"name"`
		Enabled        *bool           `json:"enabled"`
		Draining       *bool           `json:"draining"`
		Slots          *map[string]int `json:"slots"`
		CacheMaxBytes  *int64          `json:"cache_max_bytes"`
		OCRProviderIDs *[]string       `json:"ocr_provider_ids"`
	}
	if ae := decodeJSON(c, &req); ae != nil {
		respondError(c, ae)
		return
	}
	n, err := s.svc.UpdateNode(c.Request.Context(), principal(c), c.Param("id"), service.NodePatch{
		Name: req.Name, Enabled: req.Enabled, Draining: req.Draining,
		Slots: req.Slots, CacheMaxBytes: req.CacheMaxBytes, OCRProviderIDs: req.OCRProviderIDs,
	})
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toNode(n))
}

func (s *Server) handleGetClusterSettings(c *gin.Context) {
	settings, err := s.svc.GetClusterSettings(c.Request.Context())
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toClusterSettings(settings))
}

func (s *Server) handlePatchClusterSettings(c *gin.Context) {
	var req struct {
		MasterKinds                *[]string `json:"master_kinds"`
		MasterIngestConcurrency    *int      `json:"master_ingest_concurrency"`
		MasterReplicateConcurrency *int      `json:"master_replicate_concurrency"`
		MasterAnalyzeConcurrency   *int      `json:"master_analyze_concurrency"`
		MasterIndexConcurrency     *int      `json:"master_index_concurrency"`
		LeaseSeconds               *int      `json:"lease_seconds"`
		HeartbeatSeconds           *int      `json:"heartbeat_seconds"`
		ClaimWaitSeconds           *int      `json:"claim_wait_seconds"`
		PollSeconds                *int      `json:"poll_seconds"`
	}
	if ae := decodeJSON(c, &req); ae != nil {
		respondError(c, ae)
		return
	}
	settings, err := s.svc.UpdateClusterSettings(c.Request.Context(), principal(c), service.ClusterSettingsPatch{
		MasterKinds: req.MasterKinds, MasterIngestConcurrency: req.MasterIngestConcurrency,
		MasterReplicateConcurrency: req.MasterReplicateConcurrency,
		MasterAnalyzeConcurrency:   req.MasterAnalyzeConcurrency,
		MasterIndexConcurrency:     req.MasterIndexConcurrency,
		LeaseSeconds:               req.LeaseSeconds, HeartbeatSeconds: req.HeartbeatSeconds,
		ClaimWaitSeconds: req.ClaimWaitSeconds, PollSeconds: req.PollSeconds,
	})
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, toClusterSettings(settings))
}

func (s *Server) handleNodeMetrics(c *gin.Context) {
	points, err := s.svc.NodeMetrics(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	s.respondData(c, http.StatusOK, points)
}
