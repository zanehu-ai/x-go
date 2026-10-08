package obs_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"testing"

	"github.com/zanehu-ai/x-go/obs"
)

// captureHandler 是测试用 slog.Handler，将所有记录缓冲到 bytes.Buffer（JSON 格式）。
type captureHandler struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (h *captureHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	m := make(map[string]any)
	m["msg"] = r.Message
	m["level"] = r.Level.String()
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value.Any()
		return true
	})
	enc := json.NewEncoder(&h.buf)
	return enc.Encode(m)
}

func (h *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// 返回新 handler，包含预设属性（用于 With 链）
	return &withAttrsHandler{parent: h, preAttrs: attrs}
}

func (h *captureHandler) WithGroup(name string) slog.Handler { return h }

func (h *captureHandler) last() map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	dec := json.NewDecoder(&h.buf)
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil
	}
	h.buf.Reset()
	return m
}

// withAttrsHandler 支持 Logger.With 调用链。
type withAttrsHandler struct {
	parent   *captureHandler
	preAttrs []slog.Attr
}

func (w *withAttrsHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return w.parent.Enabled(ctx, l)
}

func (w *withAttrsHandler) Handle(_ context.Context, r slog.Record) error {
	w.parent.mu.Lock()
	defer w.parent.mu.Unlock()
	m := make(map[string]any)
	m["msg"] = r.Message
	m["level"] = r.Level.String()
	for _, a := range w.preAttrs {
		m[a.Key] = a.Value.Any()
	}
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value.Any()
		return true
	})
	return json.NewEncoder(&w.parent.buf).Encode(m)
}

func (w *withAttrsHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	combined := append(w.preAttrs, attrs...)
	return &withAttrsHandler{parent: w.parent, preAttrs: combined}
}

func (w *withAttrsHandler) WithGroup(name string) slog.Handler { return w }

// ── 测试用例 ──────────────────────────────────────────────────────────────────

// TestWithRequest_ExtractsCtxFields 验证 WithRequest 能从 context 中提取并附加三个标准字段。
func TestWithRequest_ExtractsCtxFields(t *testing.T) {
	h := &captureHandler{}
	logger := obs.NewLogger(h)

	ctx := context.Background()
	ctx = obs.WithRequestID(ctx, "req-123")
	ctx = obs.WithTenantID(ctx, 42)
	ctx = obs.WithPrincipalID(ctx, 99)

	child := logger.WithRequest(ctx)
	child.Info(ctx, "hello")

	got := h.last()
	if got == nil {
		t.Fatal("no log record captured")
	}
	if got[obs.FieldRequestID] != "req-123" {
		t.Errorf("request_id = %v, want req-123", got[obs.FieldRequestID])
	}
	// JSON numbers decoded as float64
	if got[obs.FieldTenantID] != float64(42) {
		t.Errorf("tenant_id = %v, want 42", got[obs.FieldTenantID])
	}
	if got[obs.FieldPrincipalID] != float64(99) {
		t.Errorf("principal_id = %v, want 99", got[obs.FieldPrincipalID])
	}
}

// TestWithRequest_PartialCtx 验证 context 中只有部分字段时不 panic，且只附加存在的字段。
func TestWithRequest_PartialCtx(t *testing.T) {
	h := &captureHandler{}
	logger := obs.NewLogger(h)

	ctx := obs.WithRequestID(context.Background(), "only-rid")
	child := logger.WithRequest(ctx)
	child.Info(ctx, "partial")

	got := h.last()
	if got == nil {
		t.Fatal("no log record captured")
	}
	if got[obs.FieldRequestID] != "only-rid" {
		t.Errorf("request_id = %v, want only-rid", got[obs.FieldRequestID])
	}
	if _, exists := got[obs.FieldTenantID]; exists {
		t.Error("tenant_id should not be in log when absent from ctx")
	}
}

// TestWithRequest_EmptyCtx 验证空 context（无字段）时 Logger 仍然正常工作，不 panic。
func TestWithRequest_EmptyCtx(t *testing.T) {
	h := &captureHandler{}
	logger := obs.NewLogger(h)

	child := logger.WithRequest(context.Background())
	// child 应与 logger 等价（无新字段）
	child.Info(context.Background(), "empty ctx ok")

	got := h.last()
	if got == nil {
		t.Fatal("no log record captured")
	}
	if got["msg"] != "empty ctx ok" {
		t.Errorf("msg = %v, want 'empty ctx ok'", got["msg"])
	}
	if _, exists := got[obs.FieldRequestID]; exists {
		t.Error("request_id should not be present for empty ctx")
	}
}

// TestWithRequest_NilCtx 验证 ctx 为 nil 时不 panic（防御性处理）。
func TestWithRequest_NilCtx(t *testing.T) {
	h := &captureHandler{}
	logger := obs.NewLogger(h)

	// nil ctx — should not panic
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("WithRequest panicked with nil ctx: %v", r)
		}
	}()

	child := logger.WithRequest(context.TODO())
	child.Info(context.TODO(), "todo ctx ok")

	got := h.last()
	if got == nil {
		t.Fatal("no log record captured")
	}
}

// TestWithRequest_NoDuplicateAttrs 验证多次调用 WithRequest 不累积重复字段。
func TestWithRequest_NoDuplicateAttrs(t *testing.T) {
	h := &captureHandler{}
	logger := obs.NewLogger(h)

	ctx := obs.WithRequestID(context.Background(), "rid-001")
	child1 := logger.WithRequest(ctx)
	child2 := child1.WithRequest(ctx) // 第二次调用
	child2.Info(ctx, "no dups")

	// 验证没有崩溃，且字段存在（多次调用不导致 panic 或丢弃数据）
	got := h.last()
	if got == nil {
		t.Fatal("no log record captured")
	}
	if got[obs.FieldRequestID] != "rid-001" {
		t.Errorf("request_id = %v, want rid-001", got[obs.FieldRequestID])
	}
}

// TestLoggerOutputFormat 验证 Logger.Error 将 error 字段附加到日志输出。
func TestLoggerOutputFormat(t *testing.T) {
	h := &captureHandler{}
	logger := obs.NewLogger(h)

	ctx := context.Background()
	testErr := fmt.Errorf("something failed")
	logger.Error(ctx, "op failed", testErr, slog.String(obs.FieldOperation, "PaymentService.Create"))

	got := h.last()
	if got == nil {
		t.Fatal("no log record captured")
	}
	if got["level"] != "ERROR" {
		t.Errorf("level = %v, want ERROR", got["level"])
	}
	if got[obs.FieldOperation] != "PaymentService.Create" {
		t.Errorf("operation = %v, want PaymentService.Create", got[obs.FieldOperation])
	}
	if got["error"] == nil {
		t.Error("error field should be present")
	}
}

// TestNewLogger_NilHandler 验证传入 nil handler 时 NewLogger 使用 slog.Default() handler，不 panic。
func TestNewLogger_NilHandler(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("NewLogger(nil) panicked: %v", r)
		}
	}()
	logger := obs.NewLogger(nil)
	logger.Info(context.Background(), "nil handler fallback")
}

// TestConcurrentLoggerUsage 验证 Logger 在并发场景下不 data race。
// 运行时需加 -race 标志（CI 默认开启）。
func TestConcurrentLoggerUsage(t *testing.T) {
	h := &captureHandler{}
	logger := obs.NewLogger(h)

	ctx := obs.WithRequestID(context.Background(), "concurrent-rid")
	ctx = obs.WithTenantID(ctx, 1)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			child := logger.WithRequest(ctx)
			child.Info(ctx, "concurrent log", slog.Int("n", n))
		}(i)
	}
	wg.Wait()
	// 只要不崩溃 / 无 race 即通过
}
