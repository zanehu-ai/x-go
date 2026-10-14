package bizerr

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/zanehu-ai/x-go/resp"
)

// BizError is a typed business error carrying an error code.
// Services return BizError; handlers use HandleError to map code → HTTP status.
type BizError struct {
	Code    int
	Message string
}

func (e *BizError) Error() string { return e.Message }

// New creates a BizError with the given code and message.
func New(code int, msg string) *BizError {
	return &BizError{Code: code, Message: msg}
}

// BadRequest creates a BizError with CodeBadRequest.
func BadRequest(msg string) *BizError { return New(resp.CodeBadRequest, msg) }

// NotFound creates a BizError with CodeNotFound.
func NotFound(msg string) *BizError { return New(resp.CodeNotFound, msg) }

// Unauthorized creates a BizError with CodeUnauthorized.
func Unauthorized(msg string) *BizError { return New(resp.CodeUnauthorized, msg) }

// Forbidden creates a BizError with CodeForbidden.
func Forbidden(msg string) *BizError { return New(resp.CodeForbidden, msg) }

// HTTPStatus maps a business error code to the appropriate HTTP status code.
func HTTPStatus(code int) int {
	switch code {
	case resp.CodeSuccess:
		return http.StatusOK
	case resp.CodeUnauthorized, resp.CodeTokenExpired:
		return http.StatusUnauthorized
	case resp.CodeForbidden:
		return http.StatusForbidden
	case resp.CodeNotFound:
		return http.StatusNotFound
	case resp.CodeConflict:
		return http.StatusConflict
	case resp.CodeInternal:
		return http.StatusInternalServerError
	default:
		return http.StatusBadRequest
	}
}

// HandleError inspects err: if it is a BizError, responds with the mapped HTTP
// status and business code; otherwise falls back to 500 InternalError.
func HandleError(c *gin.Context, err error) {
	if err == nil {
		return
	}
	var bizErr *BizError
	if errors.As(err, &bizErr) {
		resp.Error(c, HTTPStatus(bizErr.Code), bizErr.Code, bizErr.Message)
		return
	}
	resp.InternalError(c, err)
}
