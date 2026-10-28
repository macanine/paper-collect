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
}

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
	r.Use(requestIDMiddleware(), recoveryMiddleware())

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
	api.POST("/auth/agent-token", s.handleAgentToken)

	authGroup := api.Group("", s.authMiddleware())
	authGroup.POST("/auth/logout", s.handleLogout)
	authGroup.GET("/auth/me", s.handleMe)

	authGroup.GET("/subjects", s.requireScope(model.ScopeMaterialsRead), s.handleSubjects)
	authGroup.GET("/users", s.requireScope(model.ScopeUsersManage), s.rejectAgent(), s.handleListUsers)
	authGroup.POST("/users", s.requireScope(model.ScopeUsersManage), s.rejectAgent(), s.handleCreateUser)
	authGroup.GET("/api-keys", s.requireScope(model.ScopeKeysManage), s.rejectAgent(), s.handleListKeys)
	authGroup.POST("/api-keys", s.requireScope(model.ScopeKeysManage), s.rejectAgent(), s.handleCreateKey)
	authGroup.DELETE("/api-keys/:id", s.requireScope(model.ScopeKeysManage), s.rejectAgent(), s.handleRevokeKey)
	authGroup.GET("/audit-logs", s.requireScope(model.ScopeAuditRead), s.handleListAudit)

	authGroup.POST("/search/questions", s.requireScope(model.ScopeMaterialsRead), s.handleSearchQuestions)
	authGroup.GET("/search/questions/:id", s.requireScope(model.ScopeMaterialsRead), s.handleGetQuestion)
	authGroup.GET("/search/settings", s.requireScope(model.ScopeSearchRead), s.rejectAgent(), s.handleGetSearchSettings)
	authGroup.PUT("/search/settings", s.requireScope(model.ScopeSearchWrite), s.rejectAgent(), s.handlePutSearchSettings)

	authGroup.GET("/ai/providers", s.requireScope(model.ScopeAIRead), s.rejectAgent(), s.handleListAIProviders)
	authGroup.POST("/ai/providers", s.requireScope(model.ScopeAIWrite), s.rejectAgent(), s.handleCreateAIProvider)
	authGroup.PATCH("/ai/providers/:id", s.requireScope(model.ScopeAIWrite), s.rejectAgent(), s.handlePatchAIProvider)
	authGroup.POST("/ai/providers/probe", s.requireScope(model.ScopeAIWrite), s.rejectAgent(), s.handleProbeAIProviders)
	authGroup.POST("/ai/providers/:id/probe", s.requireScope(model.ScopeAIWrite), s.rejectAgent(), s.handleProbeSavedAIProvider)
	authGroup.POST("/ai/providers/:id/test", s.requireScope(model.ScopeAIWrite), s.rejectAgent(), s.handleTestAIProvider)
	authGroup.GET("/ai/settings", s.requireScope(model.ScopeAIRead), s.rejectAgent(), s.handleGetAISettings)
	authGroup.PUT("/ai/settings", s.requireScope(model.ScopeAIWrite), s.rejectAgent(), s.handlePutAISettings)
	authGroup.POST("/ai/mineru/test", s.requireScope(model.ScopeAIWrite), s.rejectAgent(), s.handleTestMinerU)

	authGroup.GET("/materials", s.requireScope(model.ScopeMaterialsRead), s.handleListMaterials)
	authGroup.GET("/materials/:id", s.requireScope(model.ScopeMaterialsRead), s.handleGetMaterial)
	authGroup.PATCH("/materials/:id", s.requireScope(model.ScopeMaterialsWrite), s.handlePatchMaterial)
	authGroup.GET("/materials/:id/download", s.requireScope(model.ScopeMaterialsRead), s.handleDownloadMaterial)
	authGroup.DELETE("/materials/:id", s.requireScope(model.ScopeMaterialsWrite), s.handleDeleteMaterial)
	authGroup.POST("/materials/:id/analysis", s.requireScope(model.ScopeMaterialsWrite), s.handleEnqueueAnalysis)
	authGroup.GET("/materials/:id/analysis", s.requireScope(model.ScopeMaterialsRead), s.handleGetAnalysis)
	authGroup.GET("/materials/:id/questions", s.requireScope(model.ScopeMaterialsRead), s.handleListMaterialQuestions)
	authGroup.GET("/materials/:id/index", s.requireScope(model.ScopeMaterialsRead), s.handleGetIndexState)
	authGroup.POST("/materials/:id/index", s.requireScope(model.ScopeMaterialsWrite), s.handleEnqueueIndex)
	authGroup.POST("/materials/:id/analysis/apply", s.requireScope(model.ScopeMaterialsWrite), s.handleApplyAnalysis)

	authGroup.GET("/collections", s.requireScope(model.ScopeCollectionsRead), s.handleListCollections)
	authGroup.POST("/collections", s.requireScope(model.ScopeCollectionsWrite), s.handleCreateCollection)
	authGroup.GET("/collections/:id", s.requireScope(model.ScopeCollectionsRead), s.handleGetCollection)
	authGroup.PATCH("/collections/:id", s.requireScope(model.ScopeCollectionsWrite), s.handlePatchCollection)
	authGroup.PUT("/collections/:id/items", s.requireScope(model.ScopeCollectionsWrite), s.handlePutCollectionItems)
	authGroup.DELETE("/collections/:id", s.requireScope(model.ScopeCollectionsWrite), s.handleDeleteCollection)

	authGroup.POST("/uploads", s.requireScope(model.ScopeUploadsWrite), s.handleCreateUpload)
	authGroup.GET("/uploads/:id", s.requireScope(model.ScopeUploadsWrite), s.handleGetUpload)
	authGroup.PUT("/uploads/:id/files/:entry_id", s.requireScope(model.ScopeUploadsWrite), s.handlePutUploadEntry)
	authGroup.POST("/uploads/:id/finalize", s.requireScope(model.ScopeUploadsWrite), s.handleFinalizeUpload)

	authGroup.GET("/jobs", s.requireScope(model.ScopeJobsRead), s.handleListJobs)
	authGroup.GET("/jobs/:id", s.requireScope(model.ScopeJobsRead), s.handleGetJob)
	authGroup.POST("/jobs/:id/retry", s.requireScope(model.ScopeJobsWrite), s.handleRetryJob)

	authGroup.GET("/storage/providers", s.requireScope(model.ScopeStorageRead), s.handleListProviders)
	authGroup.POST("/storage/providers", s.requireScope(model.ScopeStorageWrite), s.handleCreateProvider)
	authGroup.PATCH("/storage/providers/:id", s.requireScope(model.ScopeStorageWrite), s.handlePatchProvider)
	authGroup.POST("/storage/providers/:id/test", s.requireScope(model.ScopeStorageWrite), s.handleTestProvider)
	authGroup.GET("/storage/policy", s.requireScope(model.ScopeStorageRead), s.handleGetPolicy)
	authGroup.PUT("/storage/policy", s.requireScope(model.ScopeStorageWrite), s.handlePutPolicy)
	authGroup.POST("/storage/reconcile", s.requireScope(model.ScopeStorageWrite), s.handleReconcile)
	authGroup.POST("/storage/replicas/:id/share", s.requireScope(model.ScopeStorageWrite), s.handleReplicaShare)

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
		bearer := bearerToken(c)
		apiKey := strings.TrimSpace(c.GetHeader("X-API-Key"))
		if bearer == "" {
			respondError(c, apperr.Unauthorized("凭据无效或已过期"))
			return
		}
		p, err := s.svc.Authenticate(c.Request.Context(), bearer, apiKey)
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
			respondError(c, apperr.Forbidden("MCP 仅支持 API Key 代理双凭据"))
			return
		}
		c.Next()
	}
}

func (s *Server) rejectAgent() gin.HandlerFunc {
	return func(c *gin.Context) {
		if principal(c).IsAgent() {
			respondError(c, apperr.Forbidden("代理密钥不能访问该接口"))
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
	if requestPath == "/healthz" || requestPath == "/mcp" ||
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
