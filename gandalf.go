// Package gandalf provides a simple rate limiting library for Go applications.
// It provides an easy-to-use API for rate limiting operations with persistence via BadgerDB.
package gandalf

import (
	"errors"
	"time"
)

var (
	ErrRateLimitExceeded = errors.New("rate limit exceeded")
)

// ErrKeyNotSet is returned when attempting to peek at a key that has not been set
var ErrKeyNotSet = errors.New("key not set")

// ErrTransactionConflict is returned when a database transaction conflict occurs after all retries are exhausted
var ErrTransactionConflict = errors.New("database transaction conflict after retries exhausted")

// ErrDatabaseError is returned for other database-related errors
var ErrDatabaseError = errors.New("database error")

// RateLimiter provides rate limiting functionality
type RateLimiter struct {
	limiter *rateLimiter
}

// NewRateLimiter creates a new rate limiter with the specified BadgerDB path
func NewRateLimiter(dbPath string) (*RateLimiter, error) {
	rl, err := newRateLimiter(dbPath)
	if err != nil {
		return nil, err
	}
	return &RateLimiter{limiter: rl}, nil
}

// NewRateLimiterWithProvider creates a new rate limiter with a custom data provider
func NewRateLimiterWithProvider(dbPath string, provider RateLimitDataProvider) (*RateLimiter, error) {
	return NewRateLimiterWithProviderAndTime(dbPath, provider, &RealTimeProvider{})
}

// NewRateLimiterWithProviderAndTime creates a new rate limiter with a custom data provider and time provider
// This is primarily used for testing
func NewRateLimiterWithProviderAndTime(dbPath string, provider RateLimitDataProvider, timeProvider TimeProvider) (*RateLimiter, error) {
	rl, err := newRateLimiterWithProviders(dbPath, provider, timeProvider)
	if err != nil {
		return nil, err
	}
	return &RateLimiter{limiter: rl}, nil
}

// Limit executes a function with rate limiting and returns (functionResult, functionError, rateLimitError)
// If rate limit is exceeded, returns (nil, nil, ErrRateLimitExceeded)
// If rate limit allows execution, consumes one request and returns function results
func (r *RateLimiter) Limit(keyId string, fn func() (any, error)) (any, error, error) {
	return r.limiter.limit(keyId, fn)
}

// Peek returns the current limit and limit unit for a given key without consuming a request.
// If the key is not set, it returns 0, "", and ErrKeyNotSet.
func (r *RateLimiter) Peek(keyId string) (requestsLeft int, limitUnit string, err error) {
	return r.limiter.peek(keyId)
}

// Purge resets the current limits for a key, or optionally all keys if keyId is empty
func (r *RateLimiter) Purge(keyId string) error {
	return r.limiter.purge(keyId)
}

// Close closes the underlying BadgerDB database
func (r *RateLimiter) Close() error {
	return r.limiter.close()
}

// RateLimitData represents the rate limit information for a service
type RateLimitData struct {
	RateLimit     int       `json:"rate_limit"`      // Number of requests allowed
	RateLimitUnit string    `json:"rate_limit_unit"` // "millisecond", "second", "minute", "hour", "day"
	ResetTime     time.Time `json:"reset_time"`      // When the current window resets
	LastReset     time.Time `json:"last_reset"`      // When the last reset occurred
	RequestsLeft  int       `json:"requests_left"`   // Remaining requests in current window
}

// RateLimitDataProvider is an interface for getting rate limit data
type RateLimitDataProvider interface {
	GetRateLimitData(keyId string) (RateLimitData, error)
}

// TimeProvider interface for getting current time (allows mocking in tests)
type TimeProvider interface {
	Now() time.Time
}

// RealTimeProvider provides real time
type RealTimeProvider struct{}

func (r *RealTimeProvider) Now() time.Time {
	return time.Now()
}

// GetDurationForUnit returns the duration for a given rate limit unit
func GetDurationForUnit(unit string) time.Duration {
	switch unit {
	case "millisecond":
		return time.Millisecond
	case "second":
		return time.Second
	case "minute":
		return time.Minute
	case "hour":
		return time.Hour
	case "day":
		return 24 * time.Hour
	default:
		return time.Second
	}
}
