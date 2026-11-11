// Package server is the HTTP and MCP adapter over the application service.
// It owns request framing, authentication, scope checks and the response envelope.
package server

import (
	"errors"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/model"
	"github.com/nic/paper-collect/backend/internal/service"
)

// Options configures the HTTP server.
type Options struct {
	Service *service.Service
	WebDist string // absolute path to web/dist; may not exist
}

// Server holds the service and static-file configuration.
type Server struct {
	svc     *service.Service
	webDist string
	router  *gin.Engine
}

// Only an in-process MCP dispatch can attach an already authenticated principal.
// HTTP scope middleware still runs for the forwarded request.
type internalPrincipalKey struct{}

// New validates dependencies and returns a Server.
func New(opts Options) (*Server, error) {
	if opts.Service == nil {
		return nil, errors.New("server: 缺少 service")
	}
	return &Server{svc: opts.Service, webDist: opts.WebDist}, nil
}

// Router builds the complete Gin engine. It is safe to call once per Server.
func (s *Server) Router() *gin.Engine {
	r := gin.New()
	s.router = r
	r.Use(requestIDMiddleware(), requestLogMiddleware(), recoveryMiddleware())

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	r.POST("/mcp", s.authMiddleware(), s.agentOnly(), s.handleMCP)
	r.GET("/mcp", methodNotAllowed)
	r.PUT("/mcp", methodNotAllowed)
	r.DELETE("/mcp", methodNotAllowed)

	api := r.Group("/api/v1")
	api.POST("/auth/login", s.handleLogin)
	api.POST("/auth/refresh", s.handleRefresh)
	authGroup := api.Group("", s.authMiddleware())
	authGroup.POST("/auth/logout", s.handleLogout)
	authGroup.GET("/auth/me", s.handleMe)

	authGroup.GET("/subjects", s.requireScope(model.ScopeMaterialsRead), s.handleSubjects)
	authGroup.GET("/users", s.requireScope(model.ScopeUsersManage), s.handleListUsers)
	authGroup.POST("/users", s.requireScope(model.ScopeUsersManage), s.handleCreateUser)
	authGroup.GET("/api-keys", s.requireScope(model.ScopeKeysManage), s.handleListKeys)
	authGroup.POST("/api-keys", s.requireScope(model.ScopeKeysManage), s.handleCreateKey)
	authGroup.DELETE("/api-keys/:id", s.requireScope(model.ScopeKeysManage), s.handleRevokeKey)
	authGroup.GET("/audit-logs", s.requireScope(model.ScopeAuditRead), s.handleListAudit)

	authGroup.POST("/search/questions", s.requireScope(model.ScopeMaterialsRead), s.handleSearchQuestions)
	authGroup.GET("/search/questions/:id", s.requireScope(model.ScopeMaterialsRead), s.handleGetQuestion)
	authGroup.GET("/search/settings", s.requireScope(model.ScopeSearchRead), s.handleGetSearchSettings)
	authGroup.PUT("/search/settings", s.requireScope(model.ScopeSearchWrite), s.handlePutSearchSettings)

	authGroup.GET("/ai/providers", s.requireScope(model.ScopeAIRead), s.handleListAIProviders)
	authGroup.POST("/ai/providers", s.requireScope(model.ScopeAIWrite), s.handleCreateAIProvider)
	authGroup.PATCH("/ai/providers/:id", s.requireScope(model.ScopeAIWrite), s.handlePatchAIProvider)
	authGroup.POST("/ai/providers/probe", s.requireScope(model.ScopeAIWrite), s.handleProbeAIProviders)
	authGroup.POST("/ai/providers/:id/probe", s.requireScope(model.ScopeAIWrite), s.handleProbeSavedAIProvider)
	authGroup.POST("/ai/providers/:id/test", s.requireScope(model.ScopeAIWrite), s.handleTestAIProvider)
	authGroup.GET("/ai/ocr-providers", s.requireScope(model.ScopeAIRead), s.handleListOCRProviders)
	authGroup.POST("/ai/ocr-providers", s.requireScope(model.ScopeAIWrite), s.handleCreateOCRProvider)
	authGroup.PATCH("/ai/ocr-providers/:id", s.requireScope(model.ScopeAIWrite), s.handlePatchOCRProvider)
	authGroup.POST("/ai/ocr-providers/:id/test", s.requireScope(model.ScopeAIWrite), s.handleTestOCRProvider)
	authGroup.GET("/ai/settings", s.requireScope(model.ScopeAIRead), s.handleGetAISettings)
	authGroup.PUT("/ai/settings", s.requireScope(model.ScopeAIWrite), s.handlePutAISettings)

	authGroup.GET("/packs", s.requireScope(model.ScopeMaterialsRead), s.handleListPacks)
	authGroup.GET("/packs/facets", s.requireScope(model.ScopeMaterialsRead), s.handlePackFacets)
	authGroup.POST("/packs/bulk", s.requireScope(model.ScopeMaterialsWrite), s.handleBulkPatchPacks)
	authGroup.POST("/packs/trash", s.requireScope(model.ScopeMaterialsWrite), s.handleTrashPacks)
	authGroup.POST("/packs/restore", s.requireScope(model.ScopeMaterialsWrite), s.handleRestorePacks)
	authGroup.POST("/packs/purge", s.requireScope(model.ScopeMaterialsWrite), s.handlePurgePacks)
	authGroup.GET("/packs/:id", s.requireScope(model.ScopeMaterialsRead), s.handleGetPack)
	authGroup.PATCH("/packs/:id", s.requireScope(model.ScopeMaterialsWrite), s.handlePatchPack)
	authGroup.POST("/packs/:id/merge", s.requireScope(model.ScopeMaterialsWrite), s.handleMergePacks)
	authGroup.PUT("/packs/:id/order", s.requireScope(model.ScopeMaterialsWrite), s.handleReorderPack)
	authGroup.GET("/packs/:id/download", s.requireScope(model.ScopeMaterialsRead), s.handleDownloadPack)

	authGroup.GET("/materials", s.requireScope(model.ScopeMaterialsRead), s.handleListMaterials)
	authGroup.GET("/inbox", s.requireScope(model.ScopeMaterialsRead), s.handleListReviewPacks)
	authGroup.GET("/materials/stats", s.requireScope(model.ScopeMaterialsRead), s.handleLibraryStats)
	authGroup.POST("/materials/accept", s.requireScope(model.ScopeMaterialsWrite), s.handleAcceptMaterials)
	authGroup.GET("/materials/:id", s.requireScope(model.ScopeMaterialsRead), s.handleGetMaterial)
	authGroup.PATCH("/materials/:id", s.requireScope(model.ScopeMaterialsWrite), s.handlePatchMaterial)
	authGroup.GET("/materials/:id/download", s.requireScope(model.ScopeMaterialsRead), s.handleDownloadMaterial)
	authGroup.POST("/materials/:id/pull-cache", s.requireScope(model.ScopeMaterialsRead), s.handlePullMaterialCache)
	authGroup.GET("/materials/:id/pull-cache", s.requireScope(model.ScopeMaterialsRead), s.handleDownloadPullCache)
	authGroup.GET("/materials/:id/preview", s.requireScope(model.ScopeMaterialsRead), s.handlePreviewMaterial)
	authGroup.DELETE("/materials/:id", s.requireScope(model.ScopeMaterialsWrite), s.handleDeleteMaterial)
	authGroup.POST("/materials/:id/analysis", s.requireScope(model.ScopeMaterialsWrite), s.handleRereadMaterial)
	authGroup.GET("/materials/:id/analysis", s.requireScope(model.ScopeMaterialsRead), s.handleGetMaterialCard)
	authGroup.GET("/materials/:id/questions", s.requireScope(model.ScopeMaterialsRead), s.handleListMaterialQuestions)
	authGroup.GET("/materials/:id/index", s.requireScope(model.ScopeMaterialsRead), s.handleGetIndexState)
	authGroup.POST("/materials/:id/index", s.requireScope(model.ScopeMaterialsWrite), s.handleEnqueueIndex)
	authGroup.GET("/packs/:id/ai", s.requireScope(model.ScopeMaterialsRead), s.handleGetPackAI)
	authGroup.POST("/packs/:id/ai/redecide", s.requireScope(model.ScopeMaterialsWrite), s.handleRedecidePack)
	authGroup.GET("/entities", s.requireScope(model.ScopeMaterialsRead), s.handleListEntities)
	authGroup.PATCH("/entities/:id", s.requireScope(model.ScopeMaterialsWrite), s.handlePatchEntity)
	authGroup.POST("/entities/:id/merge", s.requireScope(model.ScopeMaterialsWrite), s.handleMergeEntity)
	authGroup.POST("/ai/rerun", s.requireScope(model.ScopeAIWrite), s.handleRerunAI)

	authGroup.GET("/collections", s.requireScope(model.ScopeCollectionsRead), s.handleListCollections)
	authGroup.POST("/collections", s.requireScope(model.ScopeCollectionsWrite), s.handleCreateCollection)
	authGroup.GET("/collections/:id", s.requireScope(model.ScopeCollectionsRead), s.handleGetCollection)
	authGroup.PATCH("/collections/:id", s.requireScope(model.ScopeCollectionsWrite), s.handlePatchCollection)
	authGroup.PUT("/collections/:id/items", s.requireScope(model.ScopeCollectionsWrite), s.handlePutCollectionItems)
	authGroup.POST("/collections/:id/items", s.requireScope(model.ScopeCollectionsWrite), s.handleAddCollectionPacks)
	authGroup.DELETE("/collections/:id", s.requireScope(model.ScopeCollectionsWrite), s.handleDeleteCollection)

	authGroup.GET("/uploads", s.requireScope(model.ScopeUploadsWrite), s.handleListUploads)
	authGroup.POST("/uploads", s.requireScope(model.ScopeUploadsWrite), s.handleCreateUpload)
	authGroup.GET("/uploads/:id", s.requireScope(model.ScopeUploadsWrite), s.handleGetUpload)
	authGroup.PUT("/uploads/:id/files/:entry_id", s.requireScope(model.ScopeUploadsWrite), s.handlePutUploadEntry)
	authGroup.POST("/uploads/:id/finalize", s.requireScope(model.ScopeUploadsWrite), s.handleFinalizeUpload)

	authGroup.GET("/jobs", s.requireScope(model.ScopeJobsRead), s.handleListJobs)
	authGroup.GET("/jobs/stats", s.requireScope(model.ScopeJobsRead), s.handleJobStats)
	authGroup.GET("/jobs/:id", s.requireScope(model.ScopeJobsRead), s.handleGetJob)
	authGroup.POST("/jobs/:id/retry", s.requireScope(model.ScopeJobsWrite), s.handleRetryJob)
	authGroup.POST("/jobs/:id/cancel", s.requireScope(model.ScopeJobsWrite), s.handleCancelJob)
	authGroup.POST("/jobs/retry-failed", s.requireScope(model.ScopeJobsWrite), s.handleRetryFailedJobs)

	authGroup.GET("/storage/drives", s.requireScope(model.ScopeStorageRead), s.handleListDrives)
	authGroup.POST("/storage/drives", s.requireScope(model.ScopeStorageWrite), s.handleCreateDrive)
	authGroup.PATCH("/storage/drives/:id", s.requireScope(model.ScopeStorageWrite), s.handlePatchDrive)
	authGroup.POST("/storage/drives/:id/test", s.requireScope(model.ScopeStorageWrite), s.handleTestDrive)
	authGroup.GET("/storage/rules", s.requireScope(model.ScopeStorageRead), s.handleListRules)
	authGroup.PUT("/storage/rules", s.requireScope(model.ScopeStorageWrite), s.handlePutRules)
	authGroup.POST("/storage/rules/preview", s.requireScope(model.ScopeStorageWrite), s.handlePreviewRules)
	authGroup.GET("/storage/rules/explain", s.requireScope(model.ScopeStorageRead), s.handleExplainSync)
	authGroup.GET("/storage/replicas", s.requireScope(model.ScopeStorageRead), s.handleListReplicas)
	authGroup.POST("/storage/replicas/bulk", s.requireScope(model.ScopeStorageWrite), s.handleBulkReplicas)
	authGroup.POST("/materials/:id/sync", s.requireScope(model.ScopeStorageWrite), s.handleSyncMaterial)
	authGroup.POST("/storage/reconcile", s.requireScope(model.ScopeStorageWrite), s.handleReconcile)
	authGroup.POST("/storage/replicas/:id/share", s.requireScope(model.ScopeStorageWrite), s.handleReplicaShare)

	authGroup.GET("/nodes", s.requireScope(model.ScopeNodesManage), s.handleListNodes)
	authGroup.POST("/nodes", s.requireScope(model.ScopeNodesManage), s.handleCreateNode)
	authGroup.PATCH("/nodes/:id", s.requireScope(model.ScopeNodesManage), s.handlePatchNode)
	authGroup.GET("/nodes/:id/metrics", s.requireScope(model.ScopeNodesManage), s.handleNodeMetrics)
	authGroup.GET("/cluster/settings", s.requireScope(model.ScopeNodesManage), s.handleGetClusterSettings)
	authGroup.PATCH("/cluster/settings", s.requireScope(model.ScopeNodesManage), s.handlePatchClusterSettings)

	// Worker nodes only; not part of the public API or MCP.
	node := r.Group("/internal/v1", s.nodeAuth())
	node.POST("/heartbeat", s.handleNodeHeartbeat)
	node.POST("/jobs/claim", s.handleNodeClaim)
	node.POST("/jobs/:id/progress", s.handleNodeProgress)
	node.POST("/jobs/:id/complete", s.handleNodeComplete)
	node.POST("/jobs/:id/fail", s.handleNodeFail)
	node.GET("/blobs/:sha", s.handleNodeBlob)
	node.POST("/drives/:id/refresh-token", s.handleNodeRefreshToken)

	r.NoRoute(s.handleNoRoute)
	return r
}

func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := uuid.NewString()
		c.Set(requestIDKey, id)
		c.Header("X-Request-ID", id)
		c.Next()
	}
}

// requestLogMiddleware records API/MCP latency and response size without
// logging query strings, request bodies, credentials or response content.
func requestLogMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		requestPath := c.Request.URL.Path
		if requestPath != "/mcp" && !strings.HasPrefix(requestPath, "/api/") && !strings.HasPrefix(requestPath, "/internal/") {
			return
		}
		size := c.Writer.Size()
		if size < 0 {
			size = 0
		}
		log.Printf(
			"http_request method=%s path=%s status=%d duration_ms=%d response_bytes=%d request_id=%s",
			c.Request.Method,
			requestPath,
			c.Writer.Status(),
			time.Since(started).Milliseconds(),
			size,
			requestID(c),
		)
	}
}

func recoveryMiddleware() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, _ any) {
		log.Printf("panic recovered on %s %s", c.Request.Method, c.Request.URL.Path)
		if !c.Writer.Written() {
			respondError(c, apperr.Internal(errors.New("panic")))
			return
		}
		c.Abort()
	})
}

func methodNotAllowed(c *gin.Context) {
	respondError(c, apperr.New(http.StatusMethodNotAllowed, "method_not_allowed", "该方法不允许"))
}

func (s *Server) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if p, ok := c.Request.Context().Value(internalPrincipalKey{}).(auth.Principal); ok {
			c.Set(principalKey, p)
			c.Next()
			return
		}
		bearer := bearerToken(c)
		if bearer == "" {
			respondError(c, apperr.Unauthorized("凭据无效或已过期"))
			return
		}
		p, err := s.svc.Authenticate(c.Request.Context(), bearer)
		if err != nil {
			respondError(c, err)
			return
		}
		c.Set(principalKey, p)
		c.Next()
	}
}

func bearerToken(c *gin.Context) string {
	header := strings.TrimSpace(c.GetHeader("Authorization"))
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func (s *Server) requireScope(scope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		p := principal(c)
		if !p.Has(scope) {
			respondError(c, apperr.Forbidden("权限不足"))
			return
		}
		c.Next()
	}
}

func (s *Server) agentOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !principal(c).IsAgent() {
			respondError(c, apperr.Forbidden("MCP 仅支持 API 密钥"))
			return
		}
		c.Next()
	}
}

func principal(c *gin.Context) auth.Principal {
	if v, ok := c.Get(principalKey); ok {
		if p, ok := v.(auth.Principal); ok {
			return p
		}
	}
	return auth.Principal{}
}

// handleNoRoute serves the SPA when web/dist exists and JSON 404 otherwise.
func (s *Server) handleNoRoute(c *gin.Context) {
	requestPath := c.Request.URL.Path
	if requestPath == "/healthz" || requestPath == "/mcp" || strings.HasPrefix(requestPath, "/internal/") ||
		strings.HasPrefix(requestPath, "/api/") || requestPath == "/api" {
		respondError(c, apperr.NotFound("接口不存在"))
		return
	}
	if (c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead) && s.serveStatic(c) {
		return
	}
	respondError(c, apperr.NotFound("资源不存在"))
}

func (s *Server) serveStatic(c *gin.Context) bool {
	if s.webDist == "" {
		return false
	}
	info, err := os.Stat(s.webDist)
	if err != nil || !info.IsDir() {
		return false
	}
	clean := path.Clean("/" + c.Request.URL.Path)
	rel := strings.TrimPrefix(clean, "/")
	if rel == "" {
		rel = "index.html"
	}
	candidate := filepath.Join(s.webDist, filepath.FromSlash(rel))
	if withinDir(s.webDist, candidate) {
		if st, err := os.Stat(candidate); err == nil && st.Mode().IsRegular() {
			c.File(candidate)
			return true
		}
	}
	index := filepath.Join(s.webDist, "index.html")
	if st, err := os.Stat(index); err == nil && st.Mode().IsRegular() {
		c.File(index)
		return true
	}
	return false
}

func withinDir(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
