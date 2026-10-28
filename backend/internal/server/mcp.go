package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/model"
	"github.com/nic/paper-collect/backend/internal/service"
)

// supportedProtocols lists the MCP revisions this server negotiates.
var supportedProtocols = []string{"2025-03-26", "2025-06-18", "2025-11-25"}

type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type mcpToolResult struct {
	Content []mcpContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

func (s *Server) handleMCP(c *gin.Context) {
	var req jsonrpcRequest
	if err := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, maxJSONBodyBytes)).Decode(&req); err != nil {
		writeRPCError(c, nil, -32700, "解析失败")
		return
	}
	if req.JSONRPC != "2.0" || strings.TrimSpace(req.Method) == "" {
		writeRPCError(c, req.ID, -32600, "请求非法")
		return
	}
	isNotification := len(req.ID) == 0 || string(req.ID) == "null"

	switch req.Method {
	case "initialize":
		if isNotification {
			c.Status(http.StatusAccepted)
			return
		}
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &params)
		version := negotiateProtocol(params.ProtocolVersion)
		writeRPCResult(c, req.ID, map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "paper-collect", "version": "1.0.0"},
		})
	case "notifications/initialized":
		c.Status(http.StatusAccepted)
	case "ping":
		writeRPCResult(c, req.ID, map[string]any{})
	case "tools/list":
		if isNotification {
			c.Status(http.StatusAccepted)
			return
		}
		writeRPCResult(c, req.ID, map[string]any{"tools": visibleTools(principal(c))})
	case "tools/call":
		if isNotification {
			c.Status(http.StatusAccepted)
			return
		}
		s.handleToolCall(c, req)
	default:
		writeRPCError(c, req.ID, -32601, "方法未知")
	}
}

func negotiateProtocol(client string) string {
	for _, version := range supportedProtocols {
		if client == version {
			return client
		}
	}
	return supportedProtocols[len(supportedProtocols)-1]
}

func writeRPCResult(c *gin.Context, id json.RawMessage, result any) {
	c.JSON(http.StatusOK, jsonrpcResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func writeRPCError(c *gin.Context, id json.RawMessage, code int, message string) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	c.JSON(http.StatusOK, jsonrpcResponse{JSONRPC: "2.0", ID: id, Error: &jsonrpcError{Code: code, Message: message}})
}

func (s *Server) handleToolCall(c *gin.Context, req jsonrpcRequest) {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeRPCError(c, req.ID, -32602, "参数非法")
		return
	}
	p := principal(c)
	var (
		result any
		err    error
	)
	switch params.Name {
	case "materials_search":
		result, err = s.toolMaterialsSearch(c, p, params.Arguments)
	case "materials_get":
		result, err = s.toolMaterialsGet(c, p, params.Arguments)
	case "collections_list":
		result, err = s.toolCollectionsList(c, p, params.Arguments)
	case "collections_get":
		result, err = s.toolCollectionsGet(c, p, params.Arguments)
	case "jobs_get":
		result, err = s.toolJobsGet(c, p, params.Arguments)
	case "questions_search":
		result, err = s.toolQuestionsSearch(c, p, params.Arguments)
	default:
		writeRPCError(c, req.ID, -32602, "未知工具")
		return
	}
	if err != nil {
		ae := apperr.As(err)
		if ae.Code == "validation_error" || ae.Status == http.StatusBadRequest {
			// Invalid tool arguments are protocol-level parameter errors.
			writeRPCError(c, req.ID, -32602, ae.Message)
			return
		}
		writeRPCResult(c, req.ID, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: ae.Message}},
			IsError: true,
		})
		return
	}
	raw, err := json.Marshal(result)
	if err != nil {
		writeRPCError(c, req.ID, -32603, "内部错误")
		return
	}
	writeRPCResult(c, req.ID, mcpToolResult{Content: []mcpContent{{Type: "text", Text: string(raw)}}})
}

func requireToolScope(p interface{ Has(string) bool }, scope string) error {
	if !p.Has(scope) {
		return apperr.Forbidden("权限不足")
	}
	return nil
}

func intArg(args map[string]any, name string, fallback, min, max int) (int, error) {
	raw, ok := args[name]
	if !ok || raw == nil {
		return fallback, nil
	}
	number, ok := raw.(float64)
	if !ok || number != float64(int(number)) {
		return 0, apperr.Validation("参数 " + name + " 必须是整数")
	}
	value := int(number)
	if value < min || value > max {
		return 0, apperr.Validation("参数 " + name + " 超出范围")
	}
	return value, nil
}

func stringArg(args map[string]any, name string) (string, error) {
	raw, ok := args[name]
	if !ok || raw == nil {
		return "", nil
	}
	value, ok := raw.(string)
	if !ok {
		return "", apperr.Validation("参数 " + name + " 必须是字符串")
	}
	return strings.TrimSpace(value), nil
}

func allowedArgs(args map[string]any, names ...string) error {
	allowed := map[string]bool{}
	for _, name := range names {
		allowed[name] = true
	}
	for key := range args {
		if !allowed[key] {
			return apperr.Validation("未知参数: " + key)
		}
	}
	return nil
}

func (s *Server) toolMaterialsSearch(c *gin.Context, p interface{ Has(string) bool }, args map[string]any) (any, error) {
	if err := requireToolScope(p, model.ScopeMaterialsRead); err != nil {
		return nil, err
	}
	if err := allowedArgs(args, "q", "subject", "page", "page_size"); err != nil {
		return nil, err
	}
	q, err := stringArg(args, "q")
	if err != nil {
		return nil, err
	}
	subject, err := stringArg(args, "subject")
	if err != nil {
		return nil, err
	}
	if subject != "" && !model.IsSubject(subject) {
		return nil, apperr.Validation("subject 不在固定学科字典中")
	}
	pageNumber, err := intArg(args, "page", 1, 1, 1<<30)
	if err != nil {
		return nil, err
	}
	pageSize, err := intArg(args, "page_size", 20, 1, 100)
	if err != nil {
		return nil, err
	}
	page, err := s.svc.ListMaterials(c.Request.Context(), service.MaterialFilter{Q: q, Subject: subject}, service.PageQuery{Page: pageNumber, PageSize: pageSize})
	if err != nil {
		return nil, err
	}
	return toPage(page, func(v service.MaterialView) materialDTO { return toMaterial(v.Material, v.Subjects) }), nil
}

func (s *Server) toolMaterialsGet(c *gin.Context, p interface{ Has(string) bool }, args map[string]any) (any, error) {
	if err := requireToolScope(p, model.ScopeMaterialsRead); err != nil {
		return nil, err
	}
	if err := allowedArgs(args, "id"); err != nil {
		return nil, err
	}
	id, err := stringArg(args, "id")
	if err != nil || id == "" {
		return nil, apperr.Validation("缺少 id")
	}
	view, err := s.svc.GetMaterial(c.Request.Context(), id)
	if err != nil {
		return nil, err
	}
	return toMaterialDetail(view), nil
}

func (s *Server) toolCollectionsList(c *gin.Context, p interface{ Has(string) bool }, args map[string]any) (any, error) {
	if err := requireToolScope(p, model.ScopeCollectionsRead); err != nil {
		return nil, err
	}
	if err := allowedArgs(args, "q", "page", "page_size"); err != nil {
		return nil, err
	}
	q, err := stringArg(args, "q")
	if err != nil {
		return nil, err
	}
	pageNumber, err := intArg(args, "page", 1, 1, 1<<30)
	if err != nil {
		return nil, err
	}
	pageSize, err := intArg(args, "page_size", 20, 1, 100)
	if err != nil {
		return nil, err
	}
	page, err := s.svc.ListCollections(c.Request.Context(), service.CollectionFilter{Q: q}, service.PageQuery{Page: pageNumber, PageSize: pageSize})
	if err != nil {
		return nil, err
	}
	return toPage(page, toCollection), nil
}

func (s *Server) toolCollectionsGet(c *gin.Context, p interface{ Has(string) bool }, args map[string]any) (any, error) {
	if err := requireToolScope(p, model.ScopeCollectionsRead); err != nil {
		return nil, err
	}
	if err := allowedArgs(args, "id"); err != nil {
		return nil, err
	}
	id, err := stringArg(args, "id")
	if err != nil || id == "" {
		return nil, apperr.Validation("缺少 id")
	}
	view, err := s.svc.GetCollection(c.Request.Context(), id)
	if err != nil {
		return nil, err
	}
	return toCollectionDetail(view), nil
}

func (s *Server) toolJobsGet(c *gin.Context, p interface{ Has(string) bool }, args map[string]any) (any, error) {
	if err := requireToolScope(p, model.ScopeJobsRead); err != nil {
		return nil, err
	}
	if err := allowedArgs(args, "id"); err != nil {
		return nil, err
	}
	id, err := stringArg(args, "id")
	if err != nil || id == "" {
		return nil, apperr.Validation("缺少 id")
	}
	job, err := s.svc.GetJob(c.Request.Context(), id)
	if err != nil {
		return nil, err
	}
	return toJob(job), nil
}

func (s *Server) toolQuestionsSearch(c *gin.Context, p interface{ Has(string) bool }, args map[string]any) (any, error) {
	if err := requireToolScope(p, model.ScopeMaterialsRead); err != nil {
		return nil, err
	}
	if err := allowedArgs(args, "query", "subject", "grade", "limit"); err != nil {
		return nil, err
	}
	query, err := stringArg(args, "query")
	if err != nil || query == "" {
		return nil, apperr.Validation("缺少 query")
	}
	subject, err := stringArg(args, "subject")
	if err != nil {
		return nil, err
	}
	grade, err := stringArg(args, "grade")
	if err != nil {
		return nil, err
	}
	limit, err := intArg(args, "limit", 10, 1, 50)
	if err != nil {
		return nil, err
	}
	result, err := s.svc.SearchQuestions(c.Request.Context(), query, subject, grade, 0, limit, 0)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(result.Items))
	for _, hit := range result.Items {
		items = append(items, map[string]any{
			"question_id": hit.Question.ID,
			"question_no": hit.Question.QuestionNo,
			"text":        hit.Question.Text,
			"page_no":     hit.Question.PageNo,
			"score":       hit.Score,
			"material": map[string]any{
				"id": hit.Material.ID, "title": hit.Material.Title, "file_name": hit.Material.FileName,
			},
		})
	}
	return map[string]any{"items": items, "total": result.Total, "took_ms": result.TookMS}, nil
}

func visibleTools(p interface{ Has(string) bool }) []mcpTool {
	tools := []mcpTool{}
	if p.Has(model.ScopeMaterialsRead) {
		tools = append(tools,
			mcpTool{
				Name:        "materials_search",
				Description: "按关键词与学科检索资料，返回分页摘要。",
				InputSchema: map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"q":         map[string]any{"type": "string"},
						"subject":   map[string]any{"type": "string"},
						"page":      map[string]any{"type": "integer", "minimum": 1},
						"page_size": map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
					},
				},
			},
			mcpTool{
				Name:        "materials_get",
				Description: "读取一份资料的详情与存储副本/分享状态。",
				InputSchema: map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"required":             []string{"id"},
					"properties":           map[string]any{"id": map[string]any{"type": "string"}},
				},
			},
		)
	}
	if p.Has(model.ScopeMaterialsRead) {
		tools = append(tools, mcpTool{
			Name:        "questions_search",
			Description: "按题干文本搜索题目，返回题目、页码和原试卷。",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"query"},
				"properties": map[string]any{
					"query":   map[string]any{"type": "string"},
					"subject": map[string]any{"type": "string"},
					"grade":   map[string]any{"type": "string"},
					"limit":   map[string]any{"type": "integer", "minimum": 1, "maximum": 50},
				},
			},
		})
	}
	if p.Has(model.ScopeCollectionsRead) {
		tools = append(tools,
			mcpTool{
				Name:        "collections_list",
				Description: "列出合集（系列/试卷/文件夹/ZIP）。",
				InputSchema: map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"q":         map[string]any{"type": "string"},
						"page":      map[string]any{"type": "integer", "minimum": 1},
						"page_size": map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
					},
				},
			},
			mcpTool{
				Name:        "collections_get",
				Description: "读取合集详情与条目。",
				InputSchema: map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"required":             []string{"id"},
					"properties":           map[string]any{"id": map[string]any{"type": "string"}},
				},
			},
		)
	}
	if p.Has(model.ScopeJobsRead) {
		tools = append(tools, mcpTool{
			Name:        "jobs_get",
			Description: "读取归档或复制任务状态。",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"id"},
				"properties":           map[string]any{"id": map[string]any{"type": "string"}},
			},
		})
	}
	return tools
}
