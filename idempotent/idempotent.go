package idempotent

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"

	"github.com/zanehu-ai/x-go/lock"
	"github.com/zanehu-ai/x-go/resp"
)

// ErrDuplicateRequest is returned when a request with the same idempotency key is already being processed.
var ErrDuplicateRequest = errors.New("idempotent: duplicate request")

// Check attempts to acquire an idempotency lock for the given key.
// Returns a release function on success, or ErrDuplicateRequest if the key is already held.
// Built on top of lock.Locker for consistent Redis lock semantics.
func Check(ctx context.Context, rdb *redis.Client, key string, ttl time.Duration) (release func(), err error) {
	locker := lock.New(rdb)
	unlock, err := locker.TryLock(ctx, "idem:"+key, ttl)
	if err != nil {
		if errors.Is(err, lock.ErrLockNotAcquired) {
			return nil, ErrDuplicateRequest
		}
		return nil, err
	}
	return unlock, nil
}

// Middleware returns a Gin middleware that enforces idempotency based on the
// Idempotency-Key request header. Requests without the header are passed through.
func Middleware(rdb *redis.Client, ttl time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.GetHeader("Idempotency-Key")
		if key == "" {
			c.Next()
			return
		}

		release, err := Check(c.Request.Context(), rdb, key, ttl)
		if err != nil {
			if errors.Is(err, ErrDuplicateRequest) {
				resp.Error(c, http.StatusConflict, resp.CodeConflict, "duplicate request")
				c.Abort()
				return
			}
			resp.Error(c, http.StatusInternalServerError, resp.CodeInternal, "idempotency check failed")
			c.Abort()
			return
		}
		defer release()

		c.Next()
	}
}
