// Package utils contains stateless helper utilities shared across the Synapse
// platform. The backoff helper here is a thin, semantics-preserving wrapper over
// x-go/job.BackoffPolicy, exposed with the call shape that 818-gaming
// (templates/game) ExponentialBackoffCalculator.java uses, so cargo / game / future
// templates can share the same exponential-backoff math.
//
// # Relationship to x-go/job.BackoffPolicy
//
// job.BackoffPolicy already encodes the same exponential formula
// (delay = base * multiplier^attempt, capped at max) and is the canonical
// implementation. This package does NOT introduce a second algorithm; instead it:
//
//   - Re-uses job.BackoffPolicy.Delay for the math.
//   - Maps the Java retryCount convention (1-based, retryCount=1 means "first
//     retry, wait initial interval") onto the Go attempt convention (0-based,
//     attempt=0 means "first retry, wait base"). The shift is retryCount-1.
//   - Adds the two pure helpers Java exposes that the Go BackoffPolicy lacks:
//     ShouldRetry and IsPermanentFailure.
//
// Java parity table (initial=10m, multiplier=2, no max):
//
//	retryCount  | Go attempt | delay
//	------------+------------+--------
//	0           | 0          | 10m  (Java treats <=0 as initial)
//	1           | 0          | 10m
//	2           | 1          | 20m
//	3           | 2          | 40m
//	4           | 3          | 80m
//	5           | 4          | 160m
//
// The Java implementation has no Max cap (it only guards against int64
// multiplication overflow). Calculator therefore defaults Max to a value large
// enough that the cap is unreachable for the documented retryCount range; users
// who want a hard cap should pass a Calculator built with NewCalculator and an
// explicit Max.
package utils

import (
	"errors"
	"time"

	"github.com/zanehu-ai/x-go/job"
)

// ErrNegativeRetryCount is returned by Calculator.CalculateBackoff and
// Calculator.CalculateNextRetryTime when retryCount is < 0. The Java reference
// silently treats retryCount<=0 as the initial interval; Go callers prefer an
// explicit error for invalid input, so we reject negatives but still accept 0
// (mapped to attempt 0) and 1 (also mapped to attempt 0, matching Java).
var ErrNegativeRetryCount = errors.New("utils: retryCount must be >= 0")

// Default tuning, matching templates/game ExponentialBackoffCalculator.java
// constants.
const (
	// DefaultInitialInterval is the wait before the first retry. Java:
	// DEFAULT_INITIAL_INTERVAL_MINUTES = 10.
	DefaultInitialInterval = 10 * time.Minute

	// DefaultMaxRetryCount is the Java DEFAULT_MAX_RETRY_COUNT.
	DefaultMaxRetryCount = 5

	// DefaultMultiplier is the Java fixed multiplier (2). Exposed so callers
	// constructing a custom Calculator can keep parity by default.
	DefaultMultiplier = 2.0

	// defaultMax is a sentinel "effectively no cap" value. Java has no Max,
	// so we pick a value beyond the documented retryCount range
	// (10m * 2^retryCount stays under 100 years for retryCount<32).
	defaultMax = 100 * 365 * 24 * time.Hour
)

// Calculator wraps a job.BackoffPolicy and exposes the call shape that
// templates/game's ExponentialBackoffCalculator.java uses. The zero value is
// not usable; construct via DefaultCalculator or NewCalculator.
type Calculator struct {
	policy   job.BackoffPolicy
	maxRetry int
}

// DefaultCalculator returns a Calculator matching the Java
// ExponentialBackoffCalculator default constants (10-minute initial interval,
// multiplier 2, max retry count 5, no effective cap).
func DefaultCalculator() Calculator {
	return Calculator{
		policy: job.BackoffPolicy{
			Base:       DefaultInitialInterval,
			Max:        defaultMax,
			Multiplier: DefaultMultiplier,
		},
		maxRetry: DefaultMaxRetryCount,
	}
}

// NewCalculator builds a Calculator with custom tuning. initial is the wait
// before the first retry (Java initialIntervalMinutes), multiplier is the
// growth factor (>=1), maxCap is the maximum delay (use a large value to
// disable), and maxRetry is the threshold ShouldRetry/IsPermanentFailure
// compare against.
//
// Returns job.ErrInvalidBackoffPolicy if the policy is invalid (initial<=0,
// multiplier<1, etc.) or ErrNegativeRetryCount if maxRetry<0.
func NewCalculator(initial time.Duration, multiplier float64, maxCap time.Duration, maxRetry int) (Calculator, error) {
	if maxRetry < 0 {
		return Calculator{}, ErrNegativeRetryCount
	}
	policy := job.BackoffPolicy{
		Base:       initial,
		Max:        maxCap,
		Multiplier: multiplier,
	}
	if err := policy.Validate(); err != nil {
		return Calculator{}, err
	}
	return Calculator{policy: policy, maxRetry: maxRetry}, nil
}

// CalculateBackoff returns the wait duration before the retryCount-th retry,
// matching the Java calculateBackoffMinutes(retryCount) semantics. retryCount
// uses the Java 1-based convention: retryCount=1 means "first retry" and
// returns the initial interval.
//
// As a convenience, retryCount=0 is also accepted and returns the initial
// interval (Java's `if (retryCount <= 0) return initialIntervalMinutes`
// branch). Negative retryCount returns ErrNegativeRetryCount.
func (c Calculator) CalculateBackoff(retryCount int) (time.Duration, error) {
	if retryCount < 0 {
		return 0, ErrNegativeRetryCount
	}
	// Java: retryCount<=0 yields initialInterval. retryCount=N>=1 yields
	// initial * 2^(N-1). job.BackoffPolicy.Delay(attempt=K) yields
	// base * multiplier^K. So map retryCount in {0,1} to attempt 0; map
	// retryCount=N>=2 to attempt N-1.
	attempt := retryCount - 1
	if attempt < 0 {
		attempt = 0
	}
	return c.policy.Delay(attempt)
}

// CalculateNextRetryTime returns lastRetryTime + CalculateBackoff(retryCount),
// matching Java's calculateNextRetryTime. If lastRetryTime is the zero value,
// the current time is used (Java's `if (lastRetryTime == null)` branch).
func (c Calculator) CalculateNextRetryTime(lastRetryTime time.Time, retryCount int) (time.Time, error) {
	delay, err := c.CalculateBackoff(retryCount)
	if err != nil {
		return time.Time{}, err
	}
	if lastRetryTime.IsZero() {
		lastRetryTime = time.Now().UTC()
	}
	return lastRetryTime.Add(delay), nil
}

// ShouldRetry reports whether retryCount is still below the configured max,
// matching Java shouldRetry(retryCount, maxRetryCount). Negative retryCount is
// treated as 0 (always retry).
func (c Calculator) ShouldRetry(retryCount int) bool {
	if retryCount < 0 {
		retryCount = 0
	}
	return retryCount < c.maxRetry
}

// IsPermanentFailure is the inverse of ShouldRetry: it reports whether
// retryCount has reached or exceeded the configured max, matching Java
// isPermanentFailure(retryCount, maxRetryCount).
func (c Calculator) IsPermanentFailure(retryCount int) bool {
	if retryCount < 0 {
		retryCount = 0
	}
	return retryCount >= c.maxRetry
}

// Policy returns the underlying job.BackoffPolicy so callers that already work
// with the canonical type (e.g. jobs runner) can interop without converting.
func (c Calculator) Policy() job.BackoffPolicy {
	return c.policy
}

// MaxRetryCount returns the configured max retry threshold.
func (c Calculator) MaxRetryCount() int {
	return c.maxRetry
}
