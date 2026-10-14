package resp

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/zanehu-ai/x-go/logger"
)

const (
	CodeSuccess      = 0
	CodeBadRequest   = 1001
	CodeNotFound     = 1002
	CodeConflict     = 1003
	CodeInternal     = 5001
	CodeUnauthorized = 2001
	CodeTokenExpired = 2002
	CodeForbidden    = 2003
)

// Response is the unified API response envelope returned by all endpoints.
type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
	TraceID string      `json:"trace_id,omitempty"`
}

// PageData wraps a paginated list response with total count and pagination info.
type PageData struct {
	List     interface{} `json:"list"`
	Total    int64       `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
}

// Success responds with HTTP 200 and the given data.
func Success(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Response{
		Code:    CodeSuccess,
		Message: "success",
		Data:    data,
		TraceID: c.GetString("trace_id"),
	})
}

// SuccessPage responds with HTTP 200 and a paginated list.
func SuccessPage(c *gin.Context, list interface{}, total int64, page, pageSize int) {
	c.JSON(http.StatusOK, Response{
		Code:    CodeSuccess,
		Message: "success",
		Data: PageData{
			List:     list,
			Total:    total,
			Page:     page,
			PageSize: pageSize,
		},
		TraceID: c.GetString("trace_id"),
	})
}

// Error responds with the given HTTP status and business error code.
func Error(c *gin.Context, httpStatus int, code int, message string) {
	c.JSON(httpStatus, Response{
		Code:    code,
		Message: message,
		Data:    nil,
		TraceID: c.GetString("trace_id"),
	})
}

// InternalError logs the error and responds with HTTP 500.
func InternalError(c *gin.Context, err error) {
	logger.Error("request error",
		logger.String("method", c.Request.Method),
		logger.String("path", c.Request.URL.Path),
		logger.String("error", err.Error()),
		logger.String("trace_id", c.GetString("trace_id")),
	)
	c.JSON(http.StatusInternalServerError, Response{
		Code:    CodeInternal,
		Message: "internal server error",
		Data:    nil,
		TraceID: c.GetString("trace_id"),
	})
}

// Created responds with HTTP 201 and the given data.
func Created(c *gin.Context, data interface{}) {
	c.JSON(http.StatusCreated, Response{
		Code:    CodeSuccess,
		Message: "created",
		Data:    data,
		TraceID: c.GetString("trace_id"),
	})
}

// PageParams holds parsed pagination query parameters.
type PageParams struct {
	Page     int
	PageSize int
}

// ParsePageParams extracts page and page_size from query string with defaults (page=1, page_size=20, max=100).
func ParsePageParams(c *gin.Context) PageParams {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return PageParams{Page: page, PageSize: pageSize}
}
