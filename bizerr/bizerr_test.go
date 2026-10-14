package bizerr

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/zanehu-ai/x-go/resp"
)

func init() { gin.SetMode(gin.TestMode) }

func TestBizError_Error(t *testing.T) {
	err := &BizError{Code: resp.CodeBadRequest, Message: "test error"}
	if err.Error() != "test error" {
		t.Errorf("Error() = %q, want %q", err.Error(), "test error")
	}
}

func TestNew(t *testing.T) {
	err := New(resp.CodeNotFound, "not found")
	if err.Code != resp.CodeNotFound || err.Message != "not found" {
		t.Errorf("New() = {%d, %q}, want {%d, %q}", err.Code, err.Message, resp.CodeNotFound, "not found")
	}
}

func TestConvenienceConstructors(t *testing.T) {
	tests := []struct {
		name string
		fn   func(string) *BizError
		code int
	}{
		{"BadRequest", BadRequest, resp.CodeBadRequest},
		{"NotFound", NotFound, resp.CodeNotFound},
		{"Unauthorized", Unauthorized, resp.CodeUnauthorized},
		{"Forbidden", Forbidden, resp.CodeForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fn("msg")
			if err.Code != tt.code {
				t.Errorf("code = %d, want %d", err.Code, tt.code)
			}
			if err.Message != "msg" {
				t.Errorf("message = %q, want %q", err.Message, "msg")
			}
		})
	}
}

func TestHTTPStatus(t *testing.T) {
	tests := []struct {
		code   int
		expect int
	}{
		{resp.CodeSuccess, http.StatusOK},
		{resp.CodeUnauthorized, http.StatusUnauthorized},
		{resp.CodeTokenExpired, http.StatusUnauthorized},
		{resp.CodeForbidden, http.StatusForbidden},
		{resp.CodeNotFound, http.StatusNotFound},
		{resp.CodeInternal, http.StatusInternalServerError},
		{resp.CodeBadRequest, http.StatusBadRequest},
		{resp.CodeConflict, http.StatusConflict},
		{9999, http.StatusBadRequest}, // unknown code
	}
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := HTTPStatus(tt.code)
			if got != tt.expect {
				t.Errorf("HTTPStatus(%d) = %d, want %d", tt.code, got, tt.expect)
			}
		})
	}
}

func TestHandleError_BizError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"BadRequest", BadRequest("bad request"), http.StatusBadRequest},
		{"NotFound", NotFound("not found"), http.StatusNotFound},
		{"Unauthorized", Unauthorized("unauthorized"), http.StatusUnauthorized},
		{"Forbidden", Forbidden("forbidden"), http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			HandleError(c, tt.err)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestHandleError_NilError(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	HandleError(c, nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (no response written)", w.Code)
	}
}

func TestHandleError_GenericError(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	HandleError(c, errors.New("something went wrong"))
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}
