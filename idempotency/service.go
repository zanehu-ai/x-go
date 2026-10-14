package idempotency

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/zanehu-ai/x-go/jobpayload"
)

var (
	ErrInvalidInput      = errors.New("idempotency: invalid input")
	ErrNotFound          = errors.New("idempotency: not found")
	ErrDuplicate         = errors.New("idempotency: duplicate key")
	ErrRequestMismatch   = errors.New("idempotency: request mismatch")
	ErrRequestInProcess  = errors.New("idempotency: request in progress")
	ErrSensitiveResponse = errors.New("idempotency: sensitive response, please regenerate")
	ErrInternal          = errors.New("idempotency: internal error")
)

const runningTimeout = 60 * time.Second

type repository interface {
	Create(ctx context.Context, rec *Record) error
	Get(ctx context.Context, tenantID, principalID uint64, method, path, key string) (*Record, error)
	Complete(ctx context.Context, id uint64, status string, responseStatus int, responseBody string) error
	Restart(ctx context.Context, id uint64, requestHash string, runningExpiresAt, updatedAt time.Time) error
	DeleteExpired(ctx context.Context, before time.Time, limit int) (int64, error)
}

// Service owns idempotency record lifecycle.
type Service struct {
	repo repository
	now  func() time.Time
	ttl  time.Duration
}

// NewService creates a Service with a 24 hour record TTL.
func NewService(repo repository) *Service {
	return &Service{repo: repo, now: time.Now, ttl: 24 * time.Hour}
}

type BeginInput struct {
	TenantID    uint64
	PrincipalID uint64
	Key         string
	Method      string
	Path        string
	RequestHash string
}

func (s *Service) Begin(ctx context.Context, in BeginInput) (*BeginResult, error) {
	in.Key = strings.TrimSpace(in.Key)
	in.Method = strings.ToUpper(strings.TrimSpace(in.Method))
	in.Path = strings.TrimSpace(in.Path)
	in.RequestHash = strings.TrimSpace(in.RequestHash)
	if in.TenantID == 0 || in.PrincipalID == 0 || in.Key == "" || in.Method == "" || in.Path == "" || in.RequestHash == "" {
		return nil, ErrInvalidInput
	}
	if len(in.Key) > 128 || len(in.Method) > 16 || len(in.Path) > 255 || !validRequestHash(in.RequestHash) {
		return nil, ErrInvalidInput
	}
	if s.repo == nil {
		return nil, ErrInternal
	}
	existing, err := s.repo.Get(ctx, in.TenantID, in.PrincipalID, in.Method, in.Path, in.Key)
	if err == nil {
		return s.decisionForExisting(ctx, existing, in.RequestHash)
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	now := s.now()
	running := now.Add(runningTimeout)
	rec := &Record{
		TenantID:         in.TenantID,
		PrincipalID:      in.PrincipalID,
		Key:              in.Key,
		Method:           in.Method,
		Path:             in.Path,
		RequestHash:      in.RequestHash,
		Status:           StatusInProgress,
		CreatedAt:        now,
		UpdatedAt:        now,
		ExpiresAt:        now.Add(s.ttl),
		RunningExpiresAt: &running,
	}
	if err := s.repo.Create(ctx, rec); err != nil {
		if errors.Is(err, ErrDuplicate) {
			existing, getErr := s.repo.Get(ctx, in.TenantID, in.PrincipalID, in.Method, in.Path, in.Key)
			if getErr != nil {
				return nil, getErr
			}
			return s.decisionForExisting(ctx, existing, in.RequestHash)
		}
		return nil, err
	}
	return &BeginResult{Record: rec, Started: true}, nil
}

// Complete finalizes an in-progress record. When sensitive=true the response
// body is not persisted; future replays return ErrSensitiveResponse.
func (s *Service) Complete(ctx context.Context, id uint64, responseStatus int, responseBody string, sensitive bool) error {
	if id == 0 || responseStatus < 100 || responseStatus > 599 {
		return ErrInvalidInput
	}
	if s.repo == nil {
		return ErrInternal
	}
	status := StatusCompleted
	body := responseBody
	if sensitive {
		status = StatusCompletedSensitive
		body = ""
	}
	return s.repo.Complete(ctx, id, status, responseStatus, body)
}

func (s *Service) decisionForExisting(ctx context.Context, rec *Record, requestHash string) (*BeginResult, error) {
	if rec == nil {
		return nil, ErrNotFound
	}
	if rec.RequestHash != requestHash {
		return nil, ErrRequestMismatch
	}

	switch rec.Status {
	case StatusCompleted:
		status := 200
		if rec.ResponseStatus != nil {
			status = *rec.ResponseStatus
		}
		body := ""
		if rec.ResponseBody != nil {
			body = *rec.ResponseBody
		}
		return &BeginResult{Record: rec, Replay: true, ResponseStatus: status, ResponseBody: body}, nil
	case StatusCompletedSensitive:
		return nil, ErrSensitiveResponse
	case StatusInProgress:
		if rec.RunningExpiresAt != nil && s.now().After(*rec.RunningExpiresAt) {
			now := s.now()
			running := now.Add(runningTimeout)
			if err := s.repo.Restart(ctx, rec.ID, requestHash, running, now); err != nil {
				return nil, err
			}
			rec.UpdatedAt = now
			rec.RunningExpiresAt = &running
			return &BeginResult{Record: rec, Started: true}, nil
		}
		return nil, ErrRequestInProcess
	default:
		return nil, ErrRequestInProcess
	}
}

// GCJob deletes expired idempotency records. It matches job.JobFunc.
func (s *Service) GCJob(ctx context.Context, payload map[string]any) error {
	limit := jobpayload.Int(payload, "limit", 1000)
	if s.repo == nil {
		return ErrInternal
	}
	_, err := s.repo.DeleteExpired(ctx, s.now(), limit)
	return err
}

func validRequestHash(hash string) bool {
	hash = strings.TrimPrefix(hash, hashPrefix)
	if len(hash) != 64 {
		return false
	}
	for _, r := range hash {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}
