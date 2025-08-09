package gandalf

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

// EdgeCaseTestSuite tests edge cases and error conditions
type EdgeCaseTestSuite struct {
	suite.Suite
	dbPath string
}

func (s *EdgeCaseTestSuite) SetupTest() {
	s.dbPath = fmt.Sprintf("test_edge_cases_%d.db", time.Now().UnixNano())
}

func (s *EdgeCaseTestSuite) TearDownTest() {
	if s.dbPath != "" {
		_ = os.RemoveAll(s.dbPath)
	}
}

func (s *EdgeCaseTestSuite) TestGetResetTimeEdgeCases() {
	now := time.Date(2023, 6, 15, 14, 30, 45, 500000000, time.UTC)

	tests := []struct {
		name        string
		unit        string
		expectError bool
		validate    func(resetTime time.Time) bool
	}{
		{
			name:        "millisecond_reset",
			unit:        "millisecond",
			expectError: false,
			validate: func(resetTime time.Time) bool {
				expected := time.Date(2023, 6, 15, 14, 30, 45, 501000000, time.UTC)
				return resetTime.Equal(expected)
			},
		},
		{
			name:        "second_reset",
			unit:        "second",
			expectError: false,
			validate: func(resetTime time.Time) bool {
				expected := time.Date(2023, 6, 15, 14, 30, 46, 0, time.UTC)
				return resetTime.Equal(expected)
			},
		},
		{
			name:        "minute_reset",
			unit:        "minute",
			expectError: false,
			validate: func(resetTime time.Time) bool {
				expected := time.Date(2023, 6, 15, 14, 31, 0, 0, time.UTC)
				return resetTime.Equal(expected)
			},
		},
		{
			name:        "hour_reset",
			unit:        "hour",
			expectError: false,
			validate: func(resetTime time.Time) bool {
				expected := time.Date(2023, 6, 15, 15, 0, 0, 0, time.UTC)
				return resetTime.Equal(expected)
			},
		},
		{
			name:        "day_reset",
			unit:        "day",
			expectError: false,
			validate: func(resetTime time.Time) bool {
				expected := time.Date(2023, 6, 16, 0, 0, 0, 0, time.UTC)
				return resetTime.Equal(expected)
			},
		},
		{
			name:        "empty_unit_defaults_to_day",
			unit:        "",
			expectError: false,
			validate: func(resetTime time.Time) bool {
				expected := time.Date(2023, 6, 16, 0, 0, 0, 0, time.UTC)
				return resetTime.Equal(expected)
			},
		},
		{
			name:        "month_reset",
			unit:        "month",
			expectError: false,
			validate: func(resetTime time.Time) bool {
				expected := time.Date(2023, 7, 1, 0, 0, 0, 0, time.UTC)
				return resetTime.Equal(expected)
			},
		},
		{
			name:        "invalid_unit",
			unit:        "invalid_unit",
			expectError: true,
			validate:    nil,
		},
		{
			name:        "year_unit_invalid",
			unit:        "year",
			expectError: true,
			validate:    nil,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			resetTime, err := getResetTime(now, tt.unit)

			if tt.expectError {
				s.Error(err, "Expected error for unit: %s", tt.unit)
				s.Contains(err.Error(), "invalid rate limit unit", "Error should mention invalid unit")
			} else {
				s.NoError(err, "Should not error for unit: %s", tt.unit)
				if tt.validate != nil {
					s.True(tt.validate(resetTime), "Reset time validation failed for unit: %s, got: %v", tt.unit, resetTime)
				}
			}
		})
	}
}

func (s *EdgeCaseTestSuite) TestMonthResetEdgeCases() {
	// Test month reset at end of year
	now := time.Date(2023, 12, 15, 14, 30, 45, 0, time.UTC)
	resetTime, err := getResetTime(now, "month")
	s.NoError(err)
	expected := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	s.True(resetTime.Equal(expected), "Month reset should handle year rollover")

	// Test month reset in February
	now = time.Date(2023, 2, 15, 10, 0, 0, 0, time.UTC)
	resetTime, err = getResetTime(now, "month")
	s.NoError(err)
	expected = time.Date(2023, 3, 1, 0, 0, 0, 0, time.UTC)
	s.True(resetTime.Equal(expected), "Month reset should work for February")
}

func (s *EdgeCaseTestSuite) TestDayResetWithTimezones() {
	// Test day reset with different timezones
	loc, _ := time.LoadLocation("America/New_York")
	now := time.Date(2023, 6, 15, 23, 30, 0, 0, loc)
	resetTime, err := getResetTime(now, "day")
	s.NoError(err)
	expected := time.Date(2023, 6, 16, 0, 0, 0, 0, loc)
	s.True(resetTime.Equal(expected), "Day reset should respect timezone")
}

func (s *EdgeCaseTestSuite) TestZeroRateLimit() {
	provider := &StaticProvider{
		TimeInterval: 0, // Zero rate limit
		TimeUnit:     "second",
	}
	limiter, err := NewRateLimiterWithProvider(s.dbPath, provider)
	s.Require().NoError(err)
	defer limiter.Close()

	// Should immediately hit rate limit with zero limit
	_, _, err = limiter.Limit("zero_key", func() (any, error) {
		return "should not execute", nil
	})
	s.ErrorIs(err, ErrRateLimitExceeded, "Should immediately hit rate limit with zero limit")
}

func (s *EdgeCaseTestSuite) TestNegativeRateLimit() {
	provider := &StaticProvider{
		TimeInterval: -5, // Negative rate limit
		TimeUnit:     "second",
	}
	limiter, err := NewRateLimiterWithProvider(s.dbPath, provider)
	s.Require().NoError(err)
	defer limiter.Close()

	// Should immediately hit rate limit with negative limit
	_, _, err = limiter.Limit("negative_key", func() (any, error) {
		return "should not execute", nil
	})
	s.ErrorIs(err, ErrRateLimitExceeded, "Should immediately hit rate limit with negative limit")
}

func (s *EdgeCaseTestSuite) TestVeryHighRateLimit() {
	provider := &StaticProvider{
		TimeInterval: 1000000, // Very high rate limit
		TimeUnit:     "second",
	}
	limiter, err := NewRateLimiterWithProvider(s.dbPath, provider)
	s.Require().NoError(err)
	defer limiter.Close()

	// Should handle very high rate limits
	for i := 0; i < 100; i++ {
		_, _, err = limiter.Limit("high_key", func() (any, error) {
			return fmt.Sprintf("request_%d", i), nil
		})
		s.NoError(err, "Should handle request %d with very high limit", i)
	}

	// Peek should show remaining requests
	left, unit, err := limiter.Peek("high_key")
	s.NoError(err)
	s.GreaterOrEqual(left, 999800, "Should have most requests left (allowing for concurrency variations)")
	s.LessOrEqual(left, 999900, "Should not exceed expected remaining requests")
	s.Equal("second", unit)
}

func (s *EdgeCaseTestSuite) TestEmptyKeyId() {
	provider := &StaticProvider{
		TimeInterval: 5,
		TimeUnit:     "second",
	}
	limiter, err := NewRateLimiterWithProvider(s.dbPath, provider)
	s.Require().NoError(err)
	defer limiter.Close()

	// Test with empty key ID - should fail
	_, _, err = limiter.Limit("", func() (any, error) {
		return "empty_key", nil
	})
	s.Error(err, "Should reject empty key ID")

	_, _, err = limiter.Peek("")
	s.Error(err, "Should reject empty key ID for peek")
}

func (s *EdgeCaseTestSuite) TestVeryLongKeyId() {
	provider := &StaticProvider{
		TimeInterval: 3,
		TimeUnit:     "minute",
	}
	limiter, err := NewRateLimiterWithProvider(s.dbPath, provider)
	s.Require().NoError(err)
	defer limiter.Close()

	// Test with very long key ID
	longKey := fmt.Sprintf("very_long_key_%s", fmt.Sprintf("%0*d", 1000, 1))
	_, _, err = limiter.Limit(longKey, func() (any, error) {
		return "long_key", nil
	})
	s.NoError(err, "Should handle very long key ID")

	left, unit, err := limiter.Peek(longKey)
	s.NoError(err)
	s.Equal(2, left, "Long key should have 2 requests left")
	s.Equal("minute", unit)
}

func (s *EdgeCaseTestSuite) TestSpecialCharactersInKeyId() {
	provider := &StaticProvider{
		TimeInterval: 2,
		TimeUnit:     "hour",
	}
	limiter, err := NewRateLimiterWithProvider(s.dbPath, provider)
	s.Require().NoError(err)
	defer limiter.Close()

	// Test with special characters in key ID
	specialKeys := []string{
		"key-with-dashes",
		"key_with_underscores",
		"key.with.dots",
		"key@with@symbols",
		"key with spaces",
		"key/with/slashes",
		"key\\with\\backslashes",
		"key(with)parentheses",
		"key[with]brackets",
		"key{with}braces",
		"key#with#hash",
		"key$with$dollar",
		"key%with%percent",
		"key^with^caret",
		"key&with&ampersand",
		"key*with*asterisk",
		"key+with+plus",
		"key=with=equals",
		"key|with|pipe",
		"key<with>brackets",
		"key?with?question",
		"key,with,comma",
		"key;with;semicolon",
		"key:with:colon",
		"key\"with\"quotes",
		"key'with'apostrophe",
		"key`with`backtick",
		"key~with~tilde",
		"key!with!exclamation",
	}

	for _, key := range specialKeys {
		s.Run(fmt.Sprintf("special_key_%s", key), func() {
			_, _, err = limiter.Limit(key, func() (any, error) {
				return key, nil
			})
			s.NoError(err, "Should handle key with special characters: %s", key)

			left, unit, err := limiter.Peek(key)
			s.NoError(err, "Should peek key with special characters: %s", key)
			s.Equal(1, left, "Special key should have 1 request left: %s", key)
			s.Equal("hour", unit)
		})
	}
}

func (s *EdgeCaseTestSuite) TestUnicodeKeyId() {
	provider := &StaticProvider{
		TimeInterval: 3,
		TimeUnit:     "second",
	}
	limiter, err := NewRateLimiterWithProvider(s.dbPath, provider)
	s.Require().NoError(err)
	defer limiter.Close()

	// Test with Unicode characters in key ID
	unicodeKeys := []string{
		"key_with_émojis_🚀",
		"key_with_中文",
		"key_with_العربية",
		"key_with_русский",
		"key_with_日本語",
		"key_with_한국어",
		"key_with_हिन्दी",
		"key_with_ελληνικά",
		"key_with_עברית",
		"key_with_ไทย",
	}

	for _, key := range unicodeKeys {
		s.Run(fmt.Sprintf("unicode_key_%s", key), func() {
			_, _, err = limiter.Limit(key, func() (any, error) {
				return key, nil
			})
			s.NoError(err, "Should handle Unicode key: %s", key)

			left, unit, err := limiter.Peek(key)
			s.NoError(err, "Should peek Unicode key: %s", key)
			s.Equal(2, left, "Unicode key should have 2 requests left: %s", key)
			s.Equal("second", unit)
		})
	}
}

func (s *EdgeCaseTestSuite) TestPurgeNonExistentKey() {
	limiter, err := NewRateLimiter(s.dbPath)
	s.Require().NoError(err)
	defer limiter.Close()

	// Purge a key that doesn't exist should not error
	err = limiter.Purge("nonexistent_key")
	s.NoError(err, "Should not error when purging nonexistent key")
}

func (s *EdgeCaseTestSuite) TestMultiplePurgeAll() {
	provider := &StaticProvider{
		TimeInterval: 5,
		TimeUnit:     "minute",
	}
	limiter, err := NewRateLimiterWithProvider(s.dbPath, provider)
	s.Require().NoError(err)
	defer limiter.Close()

	// Set up some keys
	keys := []string{"key1", "key2", "key3"}
	for _, key := range keys {
		_, _, err = limiter.Limit(key, func() (any, error) {
			return "success", nil
		})
		s.Require().NoError(err)
	}

	// Multiple purge all operations
	for i := 0; i < 3; i++ {
		err = limiter.Purge("")
		s.NoError(err, "Should not error on purge all #%d", i+1)
	}

	// Verify all keys are gone
	for _, key := range keys {
		_, _, err := limiter.Peek(key)
		s.ErrorIs(err, ErrKeyNotSet, "Key %s should not exist after purge all", key)
	}
}

func TestEdgeCaseTestSuite(t *testing.T) {
	suite.Run(t, new(EdgeCaseTestSuite))
}

// Test error constants
func TestErrorConstants(t *testing.T) {
	assert.Equal(t, "rate limit exceeded", ErrRateLimitExceeded.Error())
	assert.Equal(t, "key not set", ErrKeyNotSet.Error())
	assert.Equal(t, "database transaction conflict after retries exhausted", ErrTransactionConflict.Error())
	assert.Equal(t, "database error", ErrDatabaseError.Error())
}

// Test that errors are properly typed
func TestErrorTypes(t *testing.T) {
	var err error

	err = ErrRateLimitExceeded
	assert.ErrorIs(t, err, ErrRateLimitExceeded)

	err = ErrKeyNotSet
	assert.ErrorIs(t, err, ErrKeyNotSet)

	err = ErrTransactionConflict
	assert.ErrorIs(t, err, ErrTransactionConflict)

	err = ErrDatabaseError
	assert.ErrorIs(t, err, ErrDatabaseError)
}
