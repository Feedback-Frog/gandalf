package gandalf

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

// mockRateLimitDataProvider is a simple in-memory provider for testing
type mockRateLimitDataProvider struct {
	limits map[string]RateLimitData
}

func (m *mockRateLimitDataProvider) GetRateLimitData(keyId string) (RateLimitData, error) {
	if data, ok := m.limits[keyId]; ok {
		return data, nil
	}
	return RateLimitData{}, fmt.Errorf("no rate limit data for key: %s", keyId)
}

// MockTimeProvider provides mock time functionality for testing
type MockTimeProvider struct {
	currentTime time.Time
}

func NewMockTimeProvider(initialTime time.Time) *MockTimeProvider {
	return &MockTimeProvider{currentTime: initialTime}
}

func (m *MockTimeProvider) Now() time.Time {
	return m.currentTime
}

func (m *MockTimeProvider) Set(t time.Time) {
	m.currentTime = t
}

func (m *MockTimeProvider) Advance(d time.Duration) {
	m.currentTime = m.currentTime.Add(d)
}

// RateLimiterCoreTestSuite tests the core rate limiting functionality
type RateLimiterCoreTestSuite struct {
	suite.Suite
	limiter  *rateLimiter
	provider *mockRateLimitDataProvider
	mockTime *MockTimeProvider
	dbPath   string
}

func (s *RateLimiterCoreTestSuite) SetupTest() {
	s.provider = &mockRateLimitDataProvider{
		limits: map[string]RateLimitData{
			"key1": {RateLimit: 3, RateLimitUnit: "second"},
			"key2": {RateLimit: 5, RateLimitUnit: "minute"},
			"key3": {RateLimit: 2, RateLimitUnit: "hour"},
			"key4": {RateLimit: 100, RateLimitUnit: "second"}, // High throughput service
			"key5": {RateLimit: 10, RateLimitUnit: "minute"},  // Medium throughput service
			"key6": {RateLimit: 5, RateLimitUnit: "hour"},     // Low throughput service
		},
	}
	// Set mock time to the top of the second for deterministic rate limit resets
	baseTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	s.mockTime = NewMockTimeProvider(baseTime)
	s.dbPath = fmt.Sprintf("test_rl_core_%d.db", time.Now().UnixNano())
	limiter, err := newRateLimiterWithProviders(s.dbPath, s.provider, s.mockTime)
	s.Require().NoError(err)
	s.limiter = limiter
}

func (s *RateLimiterCoreTestSuite) TearDownTest() {
	s.limiter.close()
	if s.dbPath != "" {
		_ = os.RemoveAll(s.dbPath)
	}
}

func (s *RateLimiterCoreTestSuite) TestBasicRateLimiting() {
	// Ensure mock time is at the top of the second for deterministic resets
	s.mockTime.Set(time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC))
	// Test basic rate limiting with key1 (3 per second)
	_, _, err := s.limiter.peek("key1")
	s.Require().Error(err, "Expected error for new key")
	s.Equal(ErrKeyNotSet, err, "Expected ErrKeyNotSet for new key")

	// After first use, should be able to peek
	_, _, err = s.limiter.limit("key1", func() (any, error) { return "ok", nil })
	s.Require().NoError(err, "Failed to consume first request")

	left, unit, err := s.limiter.peek("key1")
	s.Require().NoError(err, "Failed to peek after first use")
	s.Equal(2, left, "Expected 2 requests left after first consumption, got %d", left)
	s.Equal("second", unit, "Expected 'second' unit, got '%s'", unit)

	// Consume remaining requests
	for i := 0; i < 2; i++ {
		_, _, err = s.limiter.limit("key1", func() (any, error) { return "ok", nil })
		s.Require().NoError(err, "Failed to consume request %d", i+2)
	}

	// Should be at limit
	left, _, err = s.limiter.peek("key1")
	s.Require().NoError(err, "Failed to peek after consuming all")
	s.Equal(0, left, "Expected 0 requests left after consuming all, got %d", left)

	// Next request should fail
	_, _, err = s.limiter.limit("key1", func() (any, error) { return "fail", nil })
	s.ErrorIs(err, ErrRateLimitExceeded, "Expected rate limit exceeded error, got %v", err)
}

func (s *RateLimiterCoreTestSuite) TestTimeBasedResets() {
	// Test rate limit reset with key2 (5 per minute)
	for i := 0; i < 5; i++ {
		_, _, err := s.limiter.limit("key2", func() (any, error) { return nil, nil })
		s.Require().NoError(err, "Failed to consume request %d", i+1)
	}

	// Should be at limit
	left, _, _ := s.limiter.peek("key2")
	s.Equal(0, left, "Expected 0 requests left after consuming all, got %d", left)

	// Advance time by 61 seconds
	s.mockTime.Advance(61 * time.Second)

	// Should be able to make requests again
	_, _, err := s.limiter.limit("key2", func() (any, error) { return nil, nil })
	s.Require().NoError(err, "Failed to consume request after time reset")
	left, _, _ = s.limiter.peek("key2")
	s.Equal(4, left, "Expected 4 requests left after reset, got %d", left)
}

func (s *RateLimiterCoreTestSuite) TestPurge() {
	// Test purging rate limits
	_, _, err := s.limiter.limit("key3", func() (any, error) { return nil, nil })
	s.Require().NoError(err, "Failed to consume initial request")
	left, _, err := s.limiter.peek("key3")
	s.Require().NoError(err, "Failed to peek after initial consumption")
	s.Equal(1, left, "Expected 1 request left after initial consumption, got %d", left)

	// Purge the key
	err = s.limiter.purge("key3")
	s.Require().NoError(err, "Failed to purge rate limit")
	_, _, err = s.limiter.peek("key3")
	s.Require().Error(err, "Expected error after purge")
	s.Equal(ErrKeyNotSet, err, "Expected ErrKeyNotSet after purge")
}

func (s *RateLimiterCoreTestSuite) TestUnknownKey() {
	// Test behavior with unknown keys
	_, _, err := s.limiter.peek("unknown")
	s.Error(err, "Expected error for unknown key")
	s.Equal(ErrKeyNotSet, err, "Expected ErrKeyNotSet for unknown key")

	_, _, err = s.limiter.limit("unknown", func() (any, error) { return nil, nil })
	s.Error(err, "Expected error for unknown key")
	s.Contains(err.Error(), "no rate limit data", "Expected 'no rate limit data' error message, got: %v", err)
}

func (s *RateLimiterCoreTestSuite) TestDifferentRateLimits() {
	// Test high throughput service (100/second)
	for i := 0; i < 100; i++ {
		_, _, err := s.limiter.limit("key4", func() (any, error) { return nil, nil })
		s.Require().NoError(err, "Failed to consume request %d", i+1)
	}
	// Next request should fail
	_, _, err := s.limiter.limit("key4", func() (any, error) { return nil, nil })
	s.Require().Error(err, "Expected rate limit exceeded error")
	s.Require().Equal(ErrRateLimitExceeded, err, "Expected ErrRateLimitExceeded, got %v", err)

	// Test medium throughput service (10/minute)
	for i := 0; i < 10; i++ {
		_, _, err := s.limiter.limit("key5", func() (any, error) { return nil, nil })
		s.Require().NoError(err, "Failed to consume request %d", i+1)
	}
	// Next request should fail
	_, _, err = s.limiter.limit("key5", func() (any, error) { return nil, nil })
	s.Require().Error(err, "Expected rate limit exceeded error")
	s.Require().Equal(ErrRateLimitExceeded, err, "Expected ErrRateLimitExceeded, got %v", err)

	// Test low throughput service (5/hour)
	for i := 0; i < 5; i++ {
		_, _, err := s.limiter.limit("key6", func() (any, error) { return nil, nil })
		s.Require().NoError(err, "Failed to consume request %d", i+1)
	}
	// Next request should fail
	_, _, err = s.limiter.limit("key6", func() (any, error) { return nil, nil })
	s.Require().Error(err, "Expected rate limit exceeded error")
	s.Require().Equal(ErrRateLimitExceeded, err, "Expected ErrRateLimitExceeded, got %v", err)
}

func (s *RateLimiterCoreTestSuite) TestPurgeAll() {
	// Use up some limits
	for i := 0; i < 50; i++ {
		_, _, err := s.limiter.limit("key4", func() (any, error) { return nil, nil })
		s.Require().NoError(err, "Failed to consume request %d", i+1)
	}

	// Purge all limits
	err := s.limiter.purge("")
	s.Require().NoError(err, "Failed to purge rate limits")

	// Should be able to use full limit again
	for i := 0; i < 100; i++ {
		_, _, err := s.limiter.limit("key4", func() (any, error) { return nil, nil })
		s.Require().NoError(err, "Failed to consume request %d after purge", i+1)
	}
}

// TestMillisecondRateLimiting tests millisecond-based rate limiting with aggressive time mocking
func (s *RateLimiterCoreTestSuite) TestMillisecondRateLimiting() {
	// Create a provider for millisecond rate limiting
	millisecondProvider := &mockRateLimitDataProvider{
		limits: map[string]RateLimitData{
			"millisecond_key": {RateLimit: 10, RateLimitUnit: "millisecond"},
		},
	}

	// Set up mock time at the top of a millisecond for deterministic resets
	baseTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	millisecondMockTime := NewMockTimeProvider(baseTime)

	millisecondDBPath := fmt.Sprintf("test_millisecond_core_%d.db", time.Now().UnixNano())
	millisecondLimiter, err := newRateLimiterWithProviders(millisecondDBPath, millisecondProvider, millisecondMockTime)
	s.Require().NoError(err)
	defer func() {
		millisecondLimiter.close()
		_ = os.RemoveAll(millisecondDBPath)
	}()

	// Test millisecond rate limiting - should allow 10 requests per millisecond
	for i := 0; i < 10; i++ {
		_, _, err := millisecondLimiter.limit("millisecond_key", func() (any, error) { return nil, nil })
		s.Require().NoError(err, "Failed to consume millisecond request %d", i+1)
		// Advance time by microsecond to simulate time passing within the millisecond
		millisecondMockTime.Advance(time.Microsecond)
	}

	// Should be at limit
	left, unit, err := millisecondLimiter.peek("millisecond_key")
	s.Require().NoError(err, "Failed to peek after consuming all millisecond requests")
	s.Equal(0, left, "Expected 0 requests left after consuming all, got %d", left)
	s.Equal("millisecond", unit, "Expected 'millisecond' unit, got '%s'", unit)

	// Next request should fail
	_, _, err = millisecondLimiter.limit("millisecond_key", func() (any, error) { return "fail", nil })
	s.ErrorIs(err, ErrRateLimitExceeded, "Expected rate limit exceeded error, got %v", err)

	// Advance time to the next millisecond boundary
	millisecondMockTime.Advance(time.Millisecond)

	// Should be able to make requests again
	_, _, err = millisecondLimiter.limit("millisecond_key", func() (any, error) { return nil, nil })
	s.Require().NoError(err, "Failed to consume request after millisecond reset")
	left, _, _ = millisecondLimiter.peek("millisecond_key")
	s.Equal(9, left, "Expected 9 requests left after millisecond reset, got %d", left)
}

// TestSecondRateLimitingWithAggressiveTime tests second-based rate limiting with aggressive time mocking
func (s *RateLimiterCoreTestSuite) TestSecondRateLimitingWithAggressiveTime() {
	// Ensure mock time is at the top of the second for deterministic resets
	s.mockTime.Set(time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC))

	// Test second rate limiting with key1 (3 per second)
	for i := 0; i < 3; i++ {
		_, _, err := s.limiter.limit("key1", func() (any, error) { return "ok", nil })
		s.Require().NoError(err, "Failed to consume second request %d", i+1)
		// Advance time by 100ms for each request to simulate time passing within the second
		s.mockTime.Advance(100 * time.Millisecond)
	}

	// Should be at limit
	left, _, err := s.limiter.peek("key1")
	s.Require().NoError(err, "Failed to peek after consuming all second requests")
	s.Equal(0, left, "Expected 0 requests left after consuming all, got %d", left)

	// Next request should fail
	_, _, err = s.limiter.limit("key1", func() (any, error) { return "fail", nil })
	s.ErrorIs(err, ErrRateLimitExceeded, "Expected rate limit exceeded error, got %v", err)

	// Advance time to the next second boundary
	s.mockTime.Advance(time.Second)

	// Should be able to make requests again
	_, _, err = s.limiter.limit("key1", func() (any, error) { return "ok", nil })
	s.Require().NoError(err, "Failed to consume request after second reset")
	left, _, _ = s.limiter.peek("key1")
	s.Equal(2, left, "Expected 2 requests left after second reset, got %d", left)
}

func TestRateLimiterCoreSuite(t *testing.T) {
	suite.Run(t, new(RateLimiterCoreTestSuite))
}
