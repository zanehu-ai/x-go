package idempotency

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/zanehu-ai/x-go/auth"
)

const MaxRequestBodyBytes = 1 << 20 // 1 MiB

// Middleware records and replays tenant-scoped write responses when callers
// provide Idempotency-Key. Requests without the header are passed through.
func Middleware(svc *Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.GetHeader("Idempotency-Key")
		if key == "" || !isWriteMethod(c.Request.Method) {
			c.Next()
			return
		}

		body, err := readBoundedBody(c.Request.Body)
		if err != nil {
			if errors.Is(err, errRequestBodyTooLarge) {
				c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
				return
			}
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))

		tenantID, principalID, ok := idempotencyScope(c, body)
		if !ok {
			c.Next()
			return
		}

		result, err := svc.Begin(c.Request.Context(), BeginInput{
			TenantID:    tenantID,
			PrincipalID: principalID,
			Key:         key,
			Method:      c.Request.Method,
			Path:        c.Request.URL.Path,
			RequestHash: RequestHash(c.Request.Method, c.Request.URL.Path, body),
		})
		if err != nil {
			writeMiddlewareError(c, err)
			return
		}
		if result.Replay {
			c.Header("Idempotency-Replayed", "true")
			c.Data(result.ResponseStatus, "application/json; charset=utf-8", []byte(result.ResponseBody))
			c.Abort()
			return
		}

		capture := &responseCapture{ResponseWriter: c.Writer}
		c.Writer = capture
		c.Next()
		if result.Record != nil {
			sensitive := c.Writer.Header().Get("X-Idempotency-Sensitive") == "true"
			if sensitive {
				c.Writer.Header().Del("X-Idempotency-Sensitive")
			}
			if err := svc.Complete(c.Request.Context(), result.Record.ID, c.Writer.Status(), capture.body.String(), sensitive); err != nil {
				_ = c.Error(err)
			}
		}
	}
}

var errRequestBodyTooLarge = errors.New("idempotency: request body too large")

func readBoundedBody(body io.Reader) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	limited := io.LimitReader(body, MaxRequestBodyBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > MaxRequestBodyBytes {
		return nil, errRequestBodyTooLarge
	}
	return data, nil
}

func idempotencyScope(c *gin.Context, body []byte) (tenantID, principalID uint64, ok bool) {
	if claimsVal, exists := c.Get("tenant_claims"); exists {
		if claims, typed := claimsVal.(*auth.TenantClaims); typed && claims != nil {
			return claims.TenantID, claims.PrincipalID, true
		}
	}

	claimsVal, exists := c.Get("platform_claims")
	claims, typed := claimsVal.(*auth.PlatformClaims)
	if !exists || !typed || claims == nil || claims.PrincipalID == 0 {
		return 0, 0, false
	}
	tid, _ := strconv.ParseUint(c.Param("tenant_id"), 10, 64)
	if tid == 0 {
		tid, _ = strconv.ParseUint(c.Query("tenant_id"), 10, 64)
	}
	if tid == 0 && len(body) > 0 {
		var req struct {
			TenantID uint64 `json:"tenant_id"`
		}
		if err := json.Unmarshal(body, &req); err == nil {
			tid = req.TenantID
		}
	}
	if tid == 0 {
		return 0, 0, false
	}
	return tid, claims.PrincipalID, true
}

type responseCapture struct {
	gin.ResponseWriter
	body bytes.Buffer
}

func (w *responseCapture) Write(data []byte) (int, error) {
	_, _ = w.body.Write(data)
	return w.ResponseWriter.Write(data)
}

func (w *responseCapture) WriteString(s string) (int, error) {
	_, _ = w.body.WriteString(s)
	return w.ResponseWriter.WriteString(s)
}

func isWriteMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func writeMiddlewareError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, ErrRequestMismatch), errors.Is(err, ErrRequestInProcess), errors.Is(err, ErrSensitiveResponse):
		c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": err.Error()})
	default:
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "idempotency operation failed"})
	}
}
