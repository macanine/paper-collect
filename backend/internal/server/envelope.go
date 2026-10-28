package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/nic/paper-collect/backend/internal/apperr"
)

const (
	requestIDKey     = "request_id"
	principalKey     = "principal"
	maxJSONBodyBytes = 1 << 20
)

type envelope struct {
	Data      any    `json:"data"`
	RequestID string `json:"request_id"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorEnvelope struct {
	Error     errorBody `json:"error"`
	RequestID string    `json:"request_id"`
}

func (s *Server) respondData(c *gin.Context, status int, data any) {
	c.JSON(status, envelope{Data: data, RequestID: requestID(c)})
}

func respondError(c *gin.Context, err error) {
	ae := apperr.As(err)
	if c.Writer.Written() {
		c.Abort()
		return
	}
	c.AbortWithStatusJSON(ae.Status, errorEnvelope{
		Error:     errorBody{Code: ae.Code, Message: ae.Message},
		RequestID: requestID(c),
	})
}

func respondNoContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

func requestID(c *gin.Context) string {
	if v, ok := c.Get(requestIDKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// readBody reads at most maxJSONBodyBytes. Oversized bodies map to 413.
func readBody(c *gin.Context, allowEmpty bool) ([]byte, bool, *apperr.Error) {
	if c.Request.Body == nil {
		if allowEmpty {
			return nil, true, nil
		}
		return nil, false, apperr.BadRequest("请求体不能为空")
	}
	limited := http.MaxBytesReader(c.Writer, c.Request.Body, maxJSONBodyBytes)
	data, err := io.ReadAll(limited)
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return nil, false, apperr.TooLarge("请求体不能超过 1 MiB")
	}
	if err != nil {
		return nil, false, apperr.BadRequest("读取请求体失败")
	}
	if len(bytes.TrimSpace(data)) == 0 {
		if allowEmpty {
			return nil, true, nil
		}
		return nil, false, apperr.BadRequest("请求体不能为空")
	}
	return data, false, nil
}

// decodeJSON decodes an object body, rejects unknown fields, trailing data, a
// non-object root, and null values except for explicitly nullable fields.
func decodeJSON(c *gin.Context, dst any, nullable ...string) *apperr.Error {
	ok, empty, ae := decodeOptionalJSON(c, dst, nullable...)
	_ = ok
	if ae != nil {
		return ae
	}
	if empty {
		return apperr.BadRequest("请求体不能为空")
	}
	return nil
}

// decodeOptionalJSON accepts an empty body for endpoints such as logout where the
// body is optional. The empty result is reported so callers can distinguish it.
func decodeOptionalJSON(c *gin.Context, dst any, nullable ...string) (bool, bool, *apperr.Error) {
	data, empty, ae := readBody(c, true)
	if ae != nil {
		return false, false, ae
	}
	if empty {
		return false, true, nil
	}
	trimmed := bytes.TrimSpace(data)
	if trimmed[0] != '{' {
		return false, false, apperr.BadRequest("JSON 请求体必须是对象")
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) && typeErr.Field != "" {
			return false, false, apperr.BadRequest("请求字段类型错误: " + typeErr.Field)
		}
		if strings.Contains(err.Error(), "unknown field") {
			return false, false, apperr.Validation("请求包含未知字段")
		}
		return false, false, apperr.BadRequest("JSON 请求体格式错误")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return false, false, apperr.BadRequest("JSON 请求体只能包含一个对象")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return false, false, apperr.BadRequest("JSON 请求体格式错误")
	}
	allowed := map[string]bool{}
	for _, name := range nullable {
		allowed[name] = true
	}
	for key, value := range raw {
		if string(bytes.TrimSpace(value)) == "null" && !allowed[key] {
			return false, false, apperr.Validation("字段不能为 null: " + key)
		}
	}
	return true, false, nil
}
