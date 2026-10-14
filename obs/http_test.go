package obs_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/zanehu-ai/x-go/obs"
)

// TestDoWithContext_InjectsHeader 验证当 ctx 含 request_id 时，DoWithContext 将其注入请求头。
func TestDoWithContext_InjectsHeader(t *testing.T) {
	const wantID = "inject-me-123"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("X-Request-ID")
		if got != wantID {
			t.Errorf("server received X-Request-ID = %q, want %q", got, wantID)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx := obs.WithRequestID(context.Background(), wantID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := obs.DoWithContext(ctx, srv.Client(), req)
	if err != nil {
		t.Fatalf("DoWithContext: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

// TestDoWithContext_NoHeaderWhenMissing 验证 ctx 不含 request_id 时不注入空头，也不 panic。
func TestDoWithContext_NoHeaderWhenMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("X-Request-ID")
		if got != "" {
			t.Errorf("X-Request-ID should be absent, got %q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx := context.Background() // no request_id
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := obs.DoWithContext(ctx, srv.Client(), req)
	if err != nil {
		t.Fatalf("DoWithContext: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
}

// TestDoWithContext_Concurrent 验证 DoWithContext 并发调用无 data race。
func TestDoWithContext_Concurrent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := srv.Client()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ctx := obs.WithRequestID(context.Background(), "concurrent-id")
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
			resp, err := obs.DoWithContext(ctx, client, req)
			if err != nil {
				return
			}
			_ = resp.Body.Close()
		}(i)
	}
	wg.Wait()
}
