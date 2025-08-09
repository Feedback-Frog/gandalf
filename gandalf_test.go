package gandalf

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

// PublicAPITestSuite tests the public API functions
type PublicAPITestSuite struct {
	suite.Suite
	dbPath string
}

func (s *PublicAPITestSuite) SetupTest() {
	s.dbPath = fmt.Sprintf("test_public_api_%d.db", time.Now().UnixNano())
}

func (s *PublicAPITestSuite) TearDownTest() {
	if s.dbPath != "" {
		_ = os.RemoveAll(s.dbPath)
	}
}

func (s *PublicAPITestSuite) TestNewRateLimiter() {
	// Test successful creation
	limiter, err := NewRateLimiter(s.dbPath)
	s.Require().NoError(err)
	s.Require().NotNil(limiter)
	defer limiter.Close()

	// Verify it uses default static provider (100 requests per second)
	_, _, err = limiter.Limit("test_key", func() (any, error) {
		return "success", nil
	})
	s.NoError(err, "Should allow first request with default provider")

	// Test with invalid path (should fail)
	invalidPath := "/invalid/path/that/does/not/exist/db"
	invalidLimiter, err := NewRateLimiter(invalidPath)
	s.Error(err, "Should fail with invalid path")
	s.Nil(invalidLimiter, "Should return nil limiter on error")
}

func (s *PublicAPITestSuite) TestNewRateLimiterWithProvider() {
	provider := &StaticProvider{
		TimeInterval: 5,
		TimeUnit:     "minute",
	}

	limiter, err := NewRateLimiterWithProvider(s.dbPath, provider)
	s.Require().NoError(err)
	s.Require().NotNil(limiter)
	defer limiter.Close()

	// Verify it uses the custom provider
	for i := 0; i < 5; i++ {
		_, _, err = limiter.Limit("test_key", func() (any, error) {
			return "success", nil
		})
		s.Require().NoError(err, "Should allow request %d", i+1)
	}

	// Should be at limit
	_, _, err = limiter.Limit("test_key", func() (any, error) {
		return "fail", nil
	})
	s.ErrorIs(err, ErrRateLimitExceeded, "Should hit rate limit after 5 requests")

	// Test with invalid path
	invalidPath := "/invalid/path/that/does/not/exist/db"
	invalidLimiter, err := NewRateLimiterWithProvider(invalidPath, provider)
	s.Error(err, "Should fail with invalid path")
	s.Nil(invalidLimiter, "Should return nil limiter on error")
}

func (s *PublicAPITestSuite) TestNewRateLimiterWithProviderAndTime() {
	provider := &StaticProvider{
		TimeInterval: 3,
		TimeUnit:     "second",
	}
	mockTime := NewMockTimeProvider(time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC))

	limiter, err := NewRateLimiterWithProviderAndTime(s.dbPath, provider, mockTime)
	s.Require().NoError(err)
	s.Require().NotNil(limiter)
	defer limiter.Close()

	// Use up the limit
	for i := 0; i < 3; i++ {
		_, _, err = limiter.Limit("test_key", func() (any, error) {
			return "success", nil
		})
		s.Require().NoError(err, "Should allow request %d", i+1)
	}

	// Should be at limit
	_, _, err = limiter.Limit("test_key", func() (any, error) {
		return "fail", nil
	})
	s.ErrorIs(err, ErrRateLimitExceeded, "Should hit rate limit")

	// Advance mock time and verify reset
	mockTime.Advance(time.Second)
	_, _, err = limiter.Limit("test_key", func() (any, error) {
		return "success", nil
	})
	s.NoError(err, "Should allow request after time advance")

	// Test with invalid path
	invalidPath := "/invalid/path/that/does/not/exist/db"
	invalidLimiter, err := NewRateLimiterWithProviderAndTime(invalidPath, provider, mockTime)
	s.Error(err, "Should fail with invalid path")
	s.Nil(invalidLimiter, "Should return nil limiter on error")
}

func (s *PublicAPITestSuite) TestLimit() {
	provider := &StaticProvider{
		TimeInterval: 2,
		TimeUnit:     "hour",
	}
	limiter, err := NewRateLimiterWithProvider(s.dbPath, provider)
	s.Require().NoError(err)
	defer limiter.Close()

	// Test successful function execution
	result, fnErr, rateLimitErr := limiter.Limit("test_key", func() (any, error) {
		return "test_result", nil
	})
	s.NoError(rateLimitErr, "Should not have rate limit error")
	s.NoError(fnErr, "Should not have function error")
	s.Equal("test_result", result, "Should return function result")

	// Test function that returns error
	result, fnErr, rateLimitErr = limiter.Limit("test_key", func() (any, error) {
		return nil, fmt.Errorf("function error")
	})
	s.NoError(rateLimitErr, "Should not have rate limit error")
	s.Error(fnErr, "Should have function error")
	s.Equal("function error", fnErr.Error(), "Should return function error")
	s.Nil(result, "Should return nil result on function error")

	// Hit rate limit
	_, _, rateLimitErr = limiter.Limit("test_key", func() (any, error) {
		return "should not execute", nil
	})
	s.ErrorIs(rateLimitErr, ErrRateLimitExceeded, "Should hit rate limit")

	// Test with unknown key (provider that returns error)
	errorProvider := &mockRateLimitDataProvider{limits: map[string]RateLimitData{}}
	errorPath := fmt.Sprintf("test_error_api_%d.db", time.Now().UnixNano())
	errorLimiter, err := NewRateLimiterWithProvider(errorPath, errorProvider)
	s.Require().NoError(err)
	defer func() {
		errorLimiter.Close()
		os.RemoveAll(errorPath)
	}()

	_, _, rateLimitErr = errorLimiter.Limit("unknown_key", func() (any, error) {
		return "should not execute", nil
	})
	s.Error(rateLimitErr, "Should have rate limit error for unknown key")
	s.Contains(rateLimitErr.Error(), "failed to get rate limit data", "Should contain provider error")
}

func (s *PublicAPITestSuite) TestPeek() {
	provider := &StaticProvider{
		TimeInterval: 10,
		TimeUnit:     "minute",
	}
	limiter, err := NewRateLimiterWithProvider(s.dbPath, provider)
	s.Require().NoError(err)
	defer limiter.Close()

	// Test peek on unset key
	left, unit, err := limiter.Peek("new_key")
	s.ErrorIs(err, ErrKeyNotSet, "Should return ErrKeyNotSet for new key")
	s.Equal(0, left, "Should return 0 for unset key")
	s.Equal("", unit, "Should return empty unit for unset key")

	// Use the key once
	_, _, err = limiter.Limit("new_key", func() (any, error) {
		return "success", nil
	})
	s.Require().NoError(err)

	// Now peek should work
	left, unit, err = limiter.Peek("new_key")
	s.NoError(err, "Should not error after key is set")
	s.Equal(9, left, "Should have 9 requests left after using 1")
	s.Equal("minute", unit, "Should return correct unit")

	// Use up more requests
	for i := 0; i < 5; i++ {
		_, _, err = limiter.Limit("new_key", func() (any, error) {
			return "success", nil
		})
		s.Require().NoError(err)
	}

	// Peek again
	left, unit, err = limiter.Peek("new_key")
	s.NoError(err, "Should not error")
	s.Equal(4, left, "Should have 4 requests left after using 6 total")
	s.Equal("minute", unit, "Should return correct unit")

	// Use up all remaining requests
	for i := 0; i < 4; i++ {
		_, _, err = limiter.Limit("new_key", func() (any, error) {
			return "success", nil
		})
		s.Require().NoError(err)
	}

	// Peek when at limit
	left, unit, err = limiter.Peek("new_key")
	s.NoError(err, "Should not error when at limit")
	s.Equal(0, left, "Should have 0 requests left")
	s.Equal("minute", unit, "Should return correct unit")
}

func (s *PublicAPITestSuite) TestPurge() {
	provider := &StaticProvider{
		TimeInterval: 3,
		TimeUnit:     "second",
	}
	limiter, err := NewRateLimiterWithProvider(s.dbPath, provider)
	s.Require().NoError(err)
	defer limiter.Close()

	// Set up multiple keys with different states
	keys := []string{"key1", "key2", "key3"}
	for _, key := range keys {
		// Use each key once
		_, _, err = limiter.Limit(key, func() (any, error) {
			return "success", nil
		})
		s.Require().NoError(err)
	}

	// Verify keys are set
	for _, key := range keys {
		left, _, err := limiter.Peek(key)
		s.Require().NoError(err)
		s.Equal(2, left, "Each key should have 2 requests left")
	}

	// Purge specific key
	err = limiter.Purge("key1")
	s.NoError(err, "Should not error when purging specific key")

	// Verify key1 is purged, others remain
	_, _, err = limiter.Peek("key1")
	s.ErrorIs(err, ErrKeyNotSet, "key1 should be purged")

	for _, key := range []string{"key2", "key3"} {
		left, _, err := limiter.Peek(key)
		s.Require().NoError(err)
		s.Equal(2, left, "%s should still have 2 requests left", key)
	}

	// Purge all keys
	err = limiter.Purge("")
	s.NoError(err, "Should not error when purging all keys")

	// Verify all keys are purged
	for _, key := range []string{"key2", "key3"} {
		_, _, err := limiter.Peek(key)
		s.ErrorIs(err, ErrKeyNotSet, "%s should be purged", key)
	}
}

func (s *PublicAPITestSuite) TestClose() {
	limiter, err := NewRateLimiter(s.dbPath)
	s.Require().NoError(err)
	s.Require().NotNil(limiter)

	// Use the limiter to ensure it's working
	_, _, err = limiter.Limit("test_key", func() (any, error) {
		return "success", nil
	})
	s.NoError(err, "Should work before close")

	// Close the limiter
	err = limiter.Close()
	s.NoError(err, "Should not error when closing")

	// Multiple closes should not error
	err = limiter.Close()
	s.NoError(err, "Should not error on second close")
}

func TestGetDurationForUnit(t *testing.T) {
	tests := []struct {
		unit     string
		expected time.Duration
	}{
		{"millisecond", time.Millisecond},
		{"second", time.Second},
		{"minute", time.Minute},
		{"hour", time.Hour},
		{"day", 24 * time.Hour},
		{"", time.Second},        // Default case
		{"invalid", time.Second}, // Invalid unit defaults to second
		{"SECOND", time.Second},  // Case sensitive - should default
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("unit_%s", tt.unit), func(t *testing.T) {
			result := GetDurationForUnit(tt.unit)
			assert.Equal(t, tt.expected, result, "Duration should match expected value for unit: %s", tt.unit)
		})
	}
}

func TestRealTimeProvider(t *testing.T) {
	provider := &RealTimeProvider{}
	now := provider.Now()
	assert.True(t, time.Since(now) < time.Second, "RealTimeProvider should return current time")
}

func TestPublicAPITestSuite(t *testing.T) {
	suite.Run(t, new(PublicAPITestSuite))
}
