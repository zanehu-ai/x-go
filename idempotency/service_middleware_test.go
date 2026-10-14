package idempotency

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/zanehu-ai/x-go/auth"
)

type fakeIdempotencyRepo struct {
	records map[string]*Record
	nextID  uint64
}

func (f *fakeIdempotencyRepo) Create(_ context.Context, rec *Record) error {
	if f.records == nil {
		f.records = map[string]*Record{}
	}
	k := fakeKey(rec.TenantID, rec.PrincipalID, rec.Method, rec.Path, rec.Key)
	if _, exists := f.records[k]; exists {
		return ErrDuplicate
	}
	f.nextID++
	rec.ID = f.nextID
	f.records[k] = rec
	return nil
}

func (f *fakeIdempotencyRepo) Get(_ context.Context, tenantID, principalID uint64, method, path, key string) (*Record, error) {
	if f.records == nil {
		return nil, ErrNotFound
	}
	rec := f.records[fakeKey(tenantID, principalID, method, path, key)]
	if rec == nil {
		return nil, ErrNotFound
	}
	return rec, nil
}

func (f *fakeIdempotencyRepo) Complete(_ context.Context, id uint64, status string, responseStatus int, responseBody string) error {
	if status == "" {
		status = StatusCompleted
	}
	for _, rec := range f.records {
		if rec.ID == id {
			rec.Status = status
			rec.ResponseStatus = &responseStatus
			rec.ResponseBody = &responseBody
			rec.RunningExpiresAt = nil
			return nil
		}
	}
	return ErrNotFound
}

func (f *fakeIdempotencyRepo) Restart(_ context.Context, id uint64, requestHash string, runningExpiresAt, updatedAt time.Time) error {
	for _, rec := range f.records {
		if rec.ID == id && rec.Status == StatusInProgress {
			rec.RequestHash = requestHash
			rec.RunningExpiresAt = &runningExpiresAt
			rec.UpdatedAt = updatedAt
			return nil
		}
	}
	return ErrNotFound
}

func (f *fakeIdempotencyRepo) DeleteExpired(_ context.Context, before time.Time, limit int) (int64, error) {
	var deleted int64
	for key, rec := range f.records {
		if deleted >= int64(limit) {
			break
		}
		if rec.ExpiresAt.Before(before) {
			delete(f.records, key)
			deleted++
		}
	}
	return deleted, nil
}

func TestMiddlewareReplaysCompletedResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := NewService(&fakeIdempotencyRepo{})
	calls := 0
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("tenant_claims", &auth.TenantClaims{TenantID: 2, PrincipalID: 99, PrincipalType: "tenant_member"})
		c.Next()
	}, Middleware(svc))
	r.POST("/billing/meter", func(c *gin.Context) {
		calls++
		c.JSON(http.StatusCreated, gin.H{"call": calls})
	})

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/billing/meter", strings.NewReader(`{"amount":1}`))
		req.Header.Set("Idempotency-Key", "idem-1")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("request %d status = %d, want 201 body=%s", i, w.Code, w.Body.String())
		}
		if !bytes.Contains(w.Body.Bytes(), []byte(`"call":1`)) {
			t.Fatalf("request %d body = %s, want first response", i, w.Body.String())
		}
		if i == 1 && w.Header().Get("Idempotency-Replayed") != "true" {
			t.Fatalf("second request missing replay header")
		}
	}
	if calls != 1 {
		t.Fatalf("handler calls = %d, want 1", calls)
	}
}

func TestServiceSensitiveResponseNotReplayed(t *testing.T) {
	repo := &fakeIdempotencyRepo{}
	svc := NewService(repo)
	hash := RequestHash(http.MethodPost, "/api-keys", []byte(`{}`))
	res, err := svc.Begin(context.Background(), BeginInput{
		TenantID:    1,
		PrincipalID: 2,
		Key:         "k",
		Method:      http.MethodPost,
		Path:        "/api-keys",
		RequestHash: hash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Complete(context.Background(), res.Record.ID, http.StatusCreated, `{"secret":"x"}`, true); err != nil {
		t.Fatal(err)
	}
	_, err = svc.Begin(context.Background(), BeginInput{
		TenantID:    1,
		PrincipalID: 2,
		Key:         "k",
		Method:      http.MethodPost,
		Path:        "/api-keys",
		RequestHash: hash,
	})
	if !errors.Is(err, ErrSensitiveResponse) {
		t.Fatalf("err = %v, want ErrSensitiveResponse", err)
	}
}

func TestServiceRestartsStaleInProgress(t *testing.T) {
	repo := &fakeIdempotencyRepo{}
	svc := NewService(repo)
	now := time.Unix(1000, 0)
	svc.now = func() time.Time { return now }
	hash := RequestHash(http.MethodPost, "/jobs", []byte(`{}`))
	first, err := svc.Begin(context.Background(), BeginInput{
		TenantID:    1,
		PrincipalID: 2,
		Key:         "k",
		Method:      http.MethodPost,
		Path:        "/jobs",
		RequestHash: hash,
	})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(runningTimeout + time.Second)
	second, err := svc.Begin(context.Background(), BeginInput{
		TenantID:    1,
		PrincipalID: 2,
		Key:         "k",
		Method:      http.MethodPost,
		Path:        "/jobs",
		RequestHash: hash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Started || second.Record.ID != first.Record.ID {
		t.Fatalf("stale restart = %+v, want same record restarted", second)
	}
}

func fakeKey(tenantID, principalID uint64, method, path, key string) string {
	return strings.Join([]string{
		strings.TrimSpace(key),
		strings.ToUpper(strings.TrimSpace(method)),
		strings.TrimSpace(path),
		strconv.FormatUint(tenantID, 10),
		strconv.FormatUint(principalID, 10),
	}, "|")
}
